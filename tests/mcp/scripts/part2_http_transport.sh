#!/usr/bin/env bash
# Part 2: HTTP/SSE Transport Integration Tests
# Tests Steps 7-10 from CLAUDE_CODE_INTEGRATION.md
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
RESULTS_FILE="${RESULTS_DIR}/part2_http_results.txt"

PASS=0
FAIL=0
SKIP=0
SERVER_PID=""
SESSION_ID=""

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

cleanup() {
    if [[ -n "$SERVER_PID" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
        log "Stopping evalhub-mcp HTTP server (PID ${SERVER_PID})..."
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
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
            --max-time 10 2>/dev/null || true
    else
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -d "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}" \
            --max-time 10 2>/dev/null || true
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

###############################################################################
# Prerequisite checks
###############################################################################
header "Prerequisites"

# build the MCP server binary if it doesn't exist
if [[ ! -x "$EVALHUB_MCP_BIN" ]]; then
    make build-mcp
fi

if [[ ! -x "$EVALHUB_MCP_BIN" ]]; then
    fail "Binary '${EVALHUB_MCP_BIN}' not found or not executable."
    echo -e "${RED}Cannot continue without the evalhub-mcp binary. Exiting.${NC}"
    exit 1
fi
pass "evalhub-mcp binary found: ${EVALHUB_MCP_BIN}"

if ! command -v curl &>/dev/null; then
    fail "curl is required for HTTP transport testing"
    exit 1
fi
pass "curl found"

if lsof -i :"$EVALHUB_PORT" &>/dev/null || ss -tln 2>/dev/null | grep -q ":${EVALHUB_PORT} "; then
    fail "Port ${EVALHUB_PORT} is already in use. Stop the existing process or set EVALHUB_PORT."
    exit 1
fi
pass "Port ${EVALHUB_PORT} is available"

export EVALHUB_BASE_URL
export EVALHUB_TENANT

###############################################################################
# Step 7 — Start the evalhub-mcp Server in HTTP Mode
###############################################################################
header "Step 7: Start evalhub-mcp in HTTP Mode"

log "Starting server: ${EVALHUB_MCP_BIN} --transport http --host ${EVALHUB_HOST} --port ${EVALHUB_PORT}"
"$EVALHUB_MCP_BIN" --transport http --host "$EVALHUB_HOST" --port "$EVALHUB_PORT" \
    > "${RESULTS_DIR}/server_stdout.log" 2> "${RESULTS_DIR}/server_stderr.log" &
SERVER_PID=$!

log "Waiting ${STARTUP_WAIT}s for server startup (PID ${SERVER_PID})..."
sleep "$STARTUP_WAIT"

if kill -0 "$SERVER_PID" 2>/dev/null; then
    pass "Step 7 — Server process is running (PID ${SERVER_PID})"
else
    fail "Step 7 — Server process exited prematurely"
    log "Server stderr:"
    cat "${RESULTS_DIR}/server_stderr.log" 2>/dev/null || true
    exit 1
fi

HEALTH_CHECK=$(curl -s -o /dev/null -w "%{http_code}" "${EVALHUB_HTTP_URL}" --max-time 5 2>/dev/null || echo "000")
if [[ "$HEALTH_CHECK" != "000" ]]; then
    pass "Step 7 — Server is accepting HTTP connections (status ${HEALTH_CHECK})"
else
    fail "Step 7 — Server is not accepting HTTP connections"
fi

###############################################################################
# Step 8 — Register the MCP Server in Claude Code (HTTP)
###############################################################################
header "Step 8: Register MCP Server (HTTP)"

if command -v claude &>/dev/null; then
    log "Removing any existing 'evalhub' MCP registration..."
    claude mcp remove evalhub 2>/dev/null || true

    log "Registering evalhub via HTTP transport..."
    if claude mcp add evalhub --transport http "${EVALHUB_HTTP_URL}" 2>&1; then
        pass "Step 8 — Server registered with Claude Code (HTTP)"
    else
        fail "Step 8 — Failed to register server with Claude Code (HTTP)"
    fi
else
    skip "Step 8 — Claude Code CLI not available"
fi

###############################################################################
# Step 9 — Verify Connection
###############################################################################
header "Step 9: Verify Connection"

if command -v claude &>/dev/null; then
    MCP_LIST=$(claude mcp list 2>&1 || true)
    if echo "$MCP_LIST" | grep -qi "evalhub"; then
        pass "Step 9 — 'claude mcp list' shows evalhub (HTTP)"
    else
        fail "Step 9 — 'claude mcp list' does not show evalhub"
        log "Output: $MCP_LIST"
    fi
else
    skip "Step 9 — Claude Code CLI not available; testing HTTP directly"
fi

INIT_HEADERS="${RESULTS_DIR}/init_headers.txt"
INIT_RESPONSE=$(curl -s -D "$INIT_HEADERS" -X POST "${EVALHUB_HTTP_URL}" \
    -H "Content-Type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"integration-test","version":"1.0"}}}' \
    --max-time 10 2>/dev/null || true)

if echo "$INIT_RESPONSE" | grep -q '"result"'; then
    pass "Step 9 — HTTP initialize handshake succeeded"
else
    fail "Step 9 — HTTP initialize handshake failed"
    log "Response: ${INIT_RESPONSE:-<empty>}"
fi

SESSION_ID=$(grep -i 'mcp-session-id' "$INIT_HEADERS" 2>/dev/null | sed 's/[^:]*: *//' | tr -d '\r\n' || true)
if [[ -n "$SESSION_ID" ]]; then
    log "Session ID: ${SESSION_ID}"
else
    log "No Mcp-Session-Id header in response (server may not require sessions)"
fi

###############################################################################
# Step 10 — Repeat All Functional Verification (Resources, Tools, Prompts)
###############################################################################
header "Step 10: Functional Verification over HTTP"

# --- 10a: Resources ---
log "Testing resources/list..."
post_notification "notifications/initialized" '{}' >/dev/null
RESOURCES_RESPONSE=$(post_jsonrpc "resources/list" '{}' 3)

declare -a EXPECTED_RESOURCES=(
    "providers"
    "benchmarks"
    "collections"
    "jobs"
    "server/version"
)

for res in "${EXPECTED_RESOURCES[@]}"; do
    if echo "$RESOURCES_RESPONSE" | grep -qi "$res"; then
        pass "Step 10 — Resource '${res}' found via HTTP"
    else
        fail "Step 10 — Resource '${res}' NOT found via HTTP"
    fi
done

read_resource_http() {
    local uri="$1"
    local label="$2"
    local needs_backend="${3:-true}"
    local resp
    resp=$(post_jsonrpc "resources/read" "{\"uri\":\"${uri}\"}" 10)
    if echo "$resp" | grep -q '"result"'; then
        pass "Step 10 — Read resource ${label} (${uri}) via HTTP"
    elif [[ "$needs_backend" == "true" ]] && echo "$resp" | grep -qiE "error|refused|connect|timeout|resolve|ECONNREFUSED"; then
        skip "Step 10 — Read resource ${label} (${uri}) via HTTP — backend not reachable"
    else
        fail "Step 10 — Failed to read resource ${label} (${uri}) via HTTP"
    fi
}

read_resource_http "evalhub://server/version" "Server Version" false
read_resource_http "evalhub://providers"      "Providers"
read_resource_http "evalhub://benchmarks"     "Benchmarks"
read_resource_http "evalhub://collections"    "Collections"
read_resource_http "evalhub://jobs"           "Jobs"

# --- 10b: Tools ---
log "Testing tools/list..."
TOOLS_RESPONSE=$(post_jsonrpc "tools/list" '{}' 4)

declare -a EXPECTED_TOOLS=(
    "submit_evaluation"
    "cancel_job"
    "get_job_status"
)

for tool in "${EXPECTED_TOOLS[@]}"; do
    if echo "$TOOLS_RESPONSE" | grep -qi "$tool"; then
        pass "Step 10 — Tool '${tool}' found via HTTP"
    else
        fail "Step 10 — Tool '${tool}' NOT found via HTTP"
    fi
done

# --- 10c: Prompts ---
log "Testing prompts/list..."
PROMPTS_RESPONSE=$(post_jsonrpc "prompts/list" '{}' 5)

declare -a EXPECTED_PROMPTS=(
    "edd_workflow"
    "evaluate_model"
    "compare_runs"
)

for prompt in "${EXPECTED_PROMPTS[@]}"; do
    if echo "$PROMPTS_RESPONSE" | grep -qi "$prompt"; then
        pass "Step 10 — Prompt '${prompt}' found via HTTP"
    else
        fail "Step 10 — Prompt '${prompt}' NOT found via HTTP"
    fi
done

log "Testing prompts/get edd_workflow..."
PROMPT_GET=$(post_jsonrpc "prompts/get" '{"name":"edd_workflow","arguments":{"application_type":"rag"}}' 6)
if echo "$PROMPT_GET" | grep -q '"messages"'; then
    pass "Step 10 — prompts/get edd_workflow(rag) returns messages via HTTP"
else
    fail "Step 10 — prompts/get edd_workflow(rag) did not return messages via HTTP"
fi

# --- 10d: Autocompletion ---
log "Testing completion/complete..."
COMPLETE_RESPONSE=$(post_jsonrpc "completion/complete" \
    '{"ref":{"type":"ref/resource","uri":"evalhub://benchmarks/{id}"},"argument":{"name":"id","value":""}}' 7)
if echo "$COMPLETE_RESPONSE" | grep -q '"completion"'; then
    pass "Step 10 — Autocompletion works via HTTP"
else
    fail "Step 10 — Autocompletion did not return results via HTTP"
fi

# --- 10e: Parity check ---
header "Transport Parity Check"
log "Comparing stdio vs HTTP tool counts..."

STDIO_PARITY_OUT="${RESULTS_DIR}/stdio_parity_stdout.tmp"
: > "$STDIO_PARITY_OUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"parity-test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{}}'
        sleep "$STDIO_WAIT"
    } | "$EVALHUB_MCP_BIN" >"$STDIO_PARITY_OUT" 2>/dev/null
) &
_pid=$!
( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) &
_wd=$!
wait "$_pid" 2>/dev/null || true
kill "$_wd" 2>/dev/null || true
wait "$_wd" 2>/dev/null || true
STDIO_TOOLS=$(cat "$STDIO_PARITY_OUT" 2>/dev/null || true)
HTTP_TOOLS="$TOOLS_RESPONSE"

STDIO_TOOL_COUNT=$(echo "$STDIO_TOOLS" | python3 -c "
import sys, json
for line in sys.stdin:
    try:
        msg = json.loads(line.strip())
        if 'result' in msg and 'tools' in msg['result']:
            print(len(msg['result']['tools']))
    except: pass
" 2>/dev/null || echo "0")

HTTP_TOOL_COUNT=$(echo "$HTTP_TOOLS" | python3 -c "
import sys, json
data = sys.stdin.read().strip()
count = None
try:
    msg = json.loads(data)
    if 'result' in msg and 'tools' in msg['result']:
        count = len(msg['result']['tools'])
except Exception: pass
if count is None:
    for line in data.split('\n'):
        line = line.strip()
        if line.startswith('data: '):
            line = line[6:]
        if not line:
            continue
        try:
            msg = json.loads(line)
            if 'result' in msg and 'tools' in msg['result']:
                count = len(msg['result']['tools'])
                break
        except Exception: pass
print(count if count is not None else 0)
" 2>/dev/null || echo "0")

if [[ "$STDIO_TOOL_COUNT" == "$HTTP_TOOL_COUNT" ]] && [[ "$STDIO_TOOL_COUNT" != "0" ]]; then
    pass "Transport parity — Tool count matches: stdio=${STDIO_TOOL_COUNT}, HTTP=${HTTP_TOOL_COUNT}"
else
    fail "Transport parity — Tool count mismatch: stdio=${STDIO_TOOL_COUNT}, HTTP=${HTTP_TOOL_COUNT}"
fi

###############################################################################
# Summary
###############################################################################
header "Part 2 Summary: HTTP/SSE Transport"
TOTAL=$((PASS + FAIL + SKIP))
echo -e "  ${GREEN}Passed:  ${PASS}${NC}"
echo -e "  ${RED}Failed:  ${FAIL}${NC}"
echo -e "  ${YELLOW}Skipped: ${SKIP}${NC}"
echo -e "  Total:   ${TOTAL}"
echo ""
echo "Server logs: ${RESULTS_DIR}/server_stdout.log, ${RESULTS_DIR}/server_stderr.log"
echo "Detailed results written to: ${RESULTS_FILE}"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
