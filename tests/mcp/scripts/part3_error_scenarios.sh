#!/usr/bin/env bash
# Part 3: Error Scenario Testing
# Tests Steps 11-13 from CLAUDE_CODE_INTEGRATION.md
# JIRA: RHOAIENG-60353
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
BIN_DIR="${BIN_DIR:-bin}"
EVALHUB_MCP_BIN="${EVALHUB_MCP_BIN:-${BIN_DIR}/evalhub-mcp}"
EVALHUB_BASE_URL="${EVALHUB_BASE_URL:-http://localhost:8080}"
EVALHUB_TENANT="${EVALHUB_TENANT:-tenant}"
EVALHUB_HOST="${EVALHUB_HOST:-localhost}"
EVALHUB_PORT="${EVALHUB_PORT:-3001}"
EVALHUB_HTTP_URL="http://${EVALHUB_HOST}:${EVALHUB_PORT}"
STARTUP_WAIT="${STARTUP_WAIT:-5}"
STDIO_TIMEOUT="${STDIO_TIMEOUT:-30}"
STDIO_WAIT="${STDIO_WAIT:-5}"
MSG_DELAY="${MSG_DELAY:-0.1}"
RESULTS_DIR="${RESULTS_DIR:-${BIN_DIR}/test-results}"
RESULTS_FILE="${RESULTS_DIR}/part3_error_results.txt"

PASS=0
FAIL=0
SKIP=0
SERVER_PID=""
SESSION_ID=""

ORIG_EVALHUB_BASE_URL="${EVALHUB_BASE_URL}"
ORIG_EVALHUB_TOKEN="${EVALHUB_TOKEN:-}"
ORIG_EVALHUB_TENANT="${EVALHUB_TENANT}"

###############################################################################
# Helpers
###############################################################################
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

mkdir -p "$RESULTS_DIR"
: > "$RESULTS_FILE"

log()    { echo -e "${CYAN}[INFO]${NC} $*"; }
pass()   { echo -e "${GREEN}[PASS]${NC} $*"; PASS=$((PASS+1)); echo "PASS: $*" >> "$RESULTS_FILE"; }
fail()   { echo -e "${RED}[FAIL]${NC} $*"; FAIL=$((FAIL+1)); echo "FAIL: $*" >> "$RESULTS_FILE"; }
skip()   { echo -e "${YELLOW}[SKIP]${NC} $*"; SKIP=$((SKIP+1)); echo "SKIP: $*" >> "$RESULTS_FILE"; }
header() { echo -e "\n${BOLD}=== $* ===${NC}"; }

stop_server() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
        SERVER_PID=""
    fi
}

restore_env() {
    export EVALHUB_BASE_URL="$ORIG_EVALHUB_BASE_URL"
    export EVALHUB_TOKEN="$ORIG_EVALHUB_TOKEN"
    export EVALHUB_TENANT="$ORIG_EVALHUB_TENANT"
}

cleanup() {
    stop_server
    restore_env
    if command -v claude &>/dev/null; then
        claude mcp remove evalhub 2>/dev/null || true
    fi
}
trap cleanup EXIT

post_jsonrpc() {
    local method="$1"
    local params="$2"
    if [[ -z "$params" ]]; then params="{}"; fi
    local id="${3:-1}"
    if [[ -n "$SESSION_ID" ]]; then
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -H "Mcp-Session-Id: ${SESSION_ID}" \
            -d "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}" \
            --max-time 10 2>/dev/null || echo '{"error":"connection_refused"}'
    else
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}" \
            --max-time 10 2>/dev/null || echo '{"error":"connection_refused"}'
    fi
}

post_notification() {
    local method="$1"
    local params="$2"
    if [[ -z "$params" ]]; then params="{}"; fi
    if [[ -n "$SESSION_ID" ]]; then
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -H "Mcp-Session-Id: ${SESSION_ID}" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"${method}\",\"params\":${params}}" \
            --max-time 10 2>/dev/null || true
    else
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"method\":\"${method}\",\"params\":${params}}" \
            --max-time 10 2>/dev/null || true
    fi
}

send_stdio_lines() {
    local stdout_tmp="${RESULTS_DIR}/stdio_stdout.tmp"
    : > "$stdout_tmp"
    (
        {
            for msg in "$@"; do
                printf '%s\n' "$msg"
                sleep "$MSG_DELAY"
            done
            sleep "$STDIO_WAIT"
        } | "$EVALHUB_MCP_BIN" >"$stdout_tmp" 2>"${RESULTS_DIR}/stdio_stderr.log"
    ) &
    local pid=$!
    ( sleep "$STDIO_TIMEOUT" && kill "$pid" 2>/dev/null ) &
    local wd=$!
    wait "$pid" 2>/dev/null || true
    kill "$wd" 2>/dev/null || true
    wait "$wd" 2>/dev/null || true
    cat "$stdout_tmp" 2>/dev/null || echo '{"error":"stdio_failed"}'
}

start_http_server() {
    SESSION_ID=""
    log "Starting HTTP server on ${EVALHUB_HOST}:${EVALHUB_PORT}..."
    "$EVALHUB_MCP_BIN" --transport http --host "$EVALHUB_HOST" --port "$EVALHUB_PORT" \
        > "${RESULTS_DIR}/server_error_stdout.log" 2> "${RESULTS_DIR}/server_error_stderr.log" &
    SERVER_PID=$!
    sleep "$STARTUP_WAIT"
}

init_http_session() {
    local init_headers="${RESULTS_DIR}/init_headers.txt"
    curl -s -D "$init_headers" -X POST "${EVALHUB_HTTP_URL}" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' \
        --max-time 10 2>/dev/null || true
    SESSION_ID=$(grep -i 'mcp-session-id' "$init_headers" 2>/dev/null | sed 's/[^:]*: *//' | tr -d '\r\n' || true)
    post_notification "notifications/initialized" '{}' >/dev/null
}

###############################################################################
# Prerequisite checks
###############################################################################
header "Prerequisites"

# build the MCP server binary if missing or not executable (supports relative paths, e.g. bin/evalhub-mcp)
if [[ ! -x "$EVALHUB_MCP_BIN" ]]; then
    make build-mcp
fi

if [[ ! -x "$EVALHUB_MCP_BIN" ]]; then
    fail "MCP binary '${EVALHUB_MCP_BIN}' was not found or is not executable."
    exit 1
fi
pass "evalhub-mcp binary found"

###############################################################################
# Step 11 — Server Not Running (HTTP)
###############################################################################
header "Step 11: Server Not Running (HTTP)"

stop_server

if command -v claude &>/dev/null; then
    claude mcp remove evalhub 2>/dev/null || true
    claude mcp add evalhub --transport http "${EVALHUB_HTTP_URL}" 2>/dev/null || true
fi

log "Attempting HTTP request with no server running on port ${EVALHUB_PORT}..."
NO_SERVER_RESPONSE=$(curl -s -X POST "${EVALHUB_HTTP_URL}" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' \
    --max-time 5 2>&1 || echo "CONNECTION_REFUSED")

if echo "$NO_SERVER_RESPONSE" | grep -qiE "refused|reset|failed|error|CONNECTION_REFUSED"; then
    pass "Step 11 — Connection correctly refused when server not running"
else
    fail "Step 11 — Unexpected response when server not running: ${NO_SERVER_RESPONSE}"
fi

log "Testing tools/call with no server..."
NO_SERVER_TOOL=$(curl -s -X POST "${EVALHUB_HTTP_URL}" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_job_status","arguments":{"job_id":"test-123"}}}' \
    --max-time 5 2>&1 || echo "CONNECTION_REFUSED")

if echo "$NO_SERVER_TOOL" | grep -qiE "refused|reset|failed|error|CONNECTION_REFUSED"; then
    pass "Step 11 — Tool call correctly fails when server not running"
else
    fail "Step 11 — Tool call did not fail as expected when server not running"
fi

log "Testing resources/read with no server..."
NO_SERVER_RESOURCE=$(curl -s -X POST "${EVALHUB_HTTP_URL}" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"evalhub://providers"}}' \
    --max-time 5 2>&1 || echo "CONNECTION_REFUSED")

if echo "$NO_SERVER_RESOURCE" | grep -qiE "refused|reset|failed|error|CONNECTION_REFUSED"; then
    pass "Step 11 — Resource read correctly fails when server not running"
else
    fail "Step 11 — Resource read did not fail as expected when server not running"
fi

###############################################################################
# Step 12 — Invalid Authentication
###############################################################################
header "Step 12: Invalid Authentication"

# --- 12a: Test via stdio with bad token ---
log "Testing stdio with invalid EVALHUB_TOKEN..."
export EVALHUB_TOKEN="invalid-token-12345"

INVALID_AUTH_OUT="${RESULTS_DIR}/stdio_auth_out.tmp"
: > "$INVALID_AUTH_OUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"evalhub://providers"}}'
        sleep "$STDIO_WAIT"
    } | env EVALHUB_TOKEN="invalid-token-12345" "$EVALHUB_MCP_BIN" >"$INVALID_AUTH_OUT" 2>&1
) &
_pid=$!
( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) &
_wd=$!
wait "$_pid" 2>/dev/null || true
kill "$_wd" 2>/dev/null || true
wait "$_wd" 2>/dev/null || true
INVALID_AUTH_STDIO=$(cat "$INVALID_AUTH_OUT" 2>/dev/null || true)

if echo "$INVALID_AUTH_STDIO" | grep -qiE "auth|unauthorized|401|forbidden|403|token|invalid|denied"; then
    pass "Step 12 — stdio returns auth error with invalid token"
elif echo "$INVALID_AUTH_STDIO" | grep -qiE "error"; then
    pass "Step 12 — stdio returns error with invalid token (verify message is actionable)"
    log "Error response: $(echo "$INVALID_AUTH_STDIO" | grep -i error | head -3)"
else
    fail "Step 12 — stdio did not return an error with invalid token"
    log "Response: $(echo "$INVALID_AUTH_STDIO" | head -5)"
fi

# --- 12b: Test via HTTP with bad token ---
restore_env
export EVALHUB_TOKEN="invalid-token-12345"

log "Starting HTTP server with invalid token..."
start_http_server

if kill -0 "$SERVER_PID" 2>/dev/null; then
    pass "Step 12 — HTTP server starts even with invalid token"

    INIT_HEADERS_AUTH="${RESULTS_DIR}/init_headers_auth.txt"
    INVALID_AUTH_HTTP_INIT=$(curl -s -D "$INIT_HEADERS_AUTH" -X POST "${EVALHUB_HTTP_URL}" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' \
        --max-time 10 2>/dev/null || true)
    SESSION_ID=$(grep -i 'mcp-session-id' "$INIT_HEADERS_AUTH" 2>/dev/null | sed 's/[^:]*: *//' | tr -d '\r\n' || true)

    if echo "$INVALID_AUTH_HTTP_INIT" | grep -q '"result"'; then
        pass "Step 12 — HTTP server initializes with invalid token (auth checked per-operation)"

        post_notification "notifications/initialized" '{}' >/dev/null
        INVALID_AUTH_HTTP_RES=$(post_jsonrpc "resources/read" '{"uri":"evalhub://providers"}' 3)

        if echo "$INVALID_AUTH_HTTP_RES" | grep -qiE "auth|unauthorized|401|forbidden|403|token|invalid|denied|error"; then
            pass "Step 12 — HTTP resource read fails with actionable auth error"
        else
            fail "Step 12 — HTTP resource read with invalid token did not produce clear error"
            log "Response: $(echo "$INVALID_AUTH_HTTP_RES" | head -5)"
        fi
    else
        if echo "$INVALID_AUTH_HTTP_INIT" | grep -qiE "auth|token|denied"; then
            pass "Step 12 — HTTP server rejects connection with invalid token (auth at init)"
        else
            fail "Step 12 — HTTP initialize returned unexpected response"
        fi
    fi
else
    fail "Step 12 — HTTP server failed to start with invalid token"
fi

stop_server
restore_env

###############################################################################
# Step 13 — Backend Unreachable
###############################################################################
header "Step 13: Backend Unreachable"

# --- 13a: Verify server starts with unreachable backend ---
export EVALHUB_BASE_URL="https://nonexistent.example.com"

log "Testing server startup with unreachable backend (stdio)..."
UNREACHABLE_INIT_OUT="${RESULTS_DIR}/stdio_unreachable_init.tmp"
: > "$UNREACHABLE_INIT_OUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$STDIO_WAIT"
    } | env EVALHUB_BASE_URL="https://nonexistent.example.com" "$EVALHUB_MCP_BIN" >"$UNREACHABLE_INIT_OUT" 2>&1
) &
_pid=$!; ( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) & _wd=$!
wait "$_pid" 2>/dev/null || true; kill "$_wd" 2>/dev/null || true; wait "$_wd" 2>/dev/null || true
UNREACHABLE_INIT=$(cat "$UNREACHABLE_INIT_OUT" 2>/dev/null || true)

if echo "$UNREACHABLE_INIT" | grep -q '"result"'; then
    pass "Step 13 — Server starts successfully with unreachable backend (stdio)"
else
    fail "Step 13 — Server failed to start with unreachable backend (stdio)"
    log "Response: $(echo "$UNREACHABLE_INIT" | head -5)"
fi

# --- 13b: Verify operations return connectivity errors ---
log "Testing resource read with unreachable backend (stdio)..."
UNREACHABLE_READ_OUT="${RESULTS_DIR}/stdio_unreachable_read.tmp"
: > "$UNREACHABLE_READ_OUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"evalhub://providers"}}'
        sleep "$STDIO_WAIT"
    } | env EVALHUB_BASE_URL="https://nonexistent.example.com" "$EVALHUB_MCP_BIN" >"$UNREACHABLE_READ_OUT" 2>&1
) &
_pid=$!; ( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) & _wd=$!
wait "$_pid" 2>/dev/null || true; kill "$_wd" 2>/dev/null || true; wait "$_wd" 2>/dev/null || true
UNREACHABLE_READ=$(cat "$UNREACHABLE_READ_OUT" 2>/dev/null || true)

if echo "$UNREACHABLE_READ" | grep -qiE "error|unreachable|timeout|connect|resolve|dns|network|ECONNREFUSED"; then
    pass "Step 13 — Resource read returns connectivity error with unreachable backend"
else
    fail "Step 13 — Resource read did not return a clear connectivity error"
    log "Response: $(echo "$UNREACHABLE_READ" | head -5)"
fi

log "Testing tool call with unreachable backend (stdio)..."
UNREACHABLE_TOOL_OUT="${RESULTS_DIR}/stdio_unreachable_tool.tmp"
: > "$UNREACHABLE_TOOL_OUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_job_status","arguments":{"job_id":"test-123"}}}'
        sleep "$STDIO_WAIT"
    } | env EVALHUB_BASE_URL="https://nonexistent.example.com" "$EVALHUB_MCP_BIN" >"$UNREACHABLE_TOOL_OUT" 2>&1
) &
_pid=$!; ( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) & _wd=$!
wait "$_pid" 2>/dev/null || true; kill "$_wd" 2>/dev/null || true; wait "$_wd" 2>/dev/null || true
UNREACHABLE_TOOL=$(cat "$UNREACHABLE_TOOL_OUT" 2>/dev/null || true)

if echo "$UNREACHABLE_TOOL" | grep -qiE "error|unreachable|timeout|connect|resolve|dns|network|ECONNREFUSED"; then
    pass "Step 13 — Tool call returns connectivity error with unreachable backend"
else
    fail "Step 13 — Tool call did not return a clear connectivity error"
    log "Response: $(echo "$UNREACHABLE_TOOL" | head -5)"
fi

# --- 13c: Same via HTTP ---
log "Starting HTTP server with unreachable backend..."
start_http_server

if kill -0 "$SERVER_PID" 2>/dev/null; then
    pass "Step 13 — HTTP server starts successfully with unreachable backend"

    init_http_session

    UNREACHABLE_HTTP_RES=$(post_jsonrpc "resources/read" '{"uri":"evalhub://providers"}' 3)
    if echo "$UNREACHABLE_HTTP_RES" | grep -qiE "error|unreachable|timeout|connect|resolve|dns|network"; then
        pass "Step 13 — HTTP resource read returns connectivity error"
    else
        fail "Step 13 — HTTP resource read did not return connectivity error"
    fi
else
    fail "Step 13 — HTTP server failed to start with unreachable backend"
fi

stop_server
restore_env

###############################################################################
# Summary
###############################################################################
header "Part 3 Summary: Error Scenarios"
TOTAL=$((PASS + FAIL + SKIP))
echo -e "  ${GREEN}Passed:  ${PASS}${NC}"
echo -e "  ${RED}Failed:  ${FAIL}${NC}"
echo -e "  ${YELLOW}Skipped: ${SKIP}${NC}"
echo -e "  Total:   ${TOTAL}"
echo ""
echo "Detailed results written to: ${RESULTS_FILE}"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
