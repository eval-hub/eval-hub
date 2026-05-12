#!/usr/bin/env bash
# Part 1: stdio Transport Integration Tests
# Tests Steps 1-6 from CLAUDE_CODE_INTEGRATION.md
# JIRA: RHOAIENG-60353
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
BIN_DIR="${BIN_DIR:-bin}"
EVALHUB_MCP_BIN="${EVALHUB_MCP_BIN:-${BIN_DIR}/evalhub-mcp}"
EVALHUB_BASE_URL="${EVALHUB_BASE_URL:-http://localhost:8080}"
EVALHUB_TENANT="${EVALHUB_TENANT:-tenant}"
RESULTS_DIR="${RESULTS_DIR:-${BIN_DIR}/test-results}"
RESULTS_FILE="${RESULTS_DIR}/part1_stdio_results.txt"
STDIO_TIMEOUT="${STDIO_TIMEOUT:-30}"
STDIO_WAIT="${STDIO_WAIT:-5}"
MSG_DELAY="${MSG_DELAY:-0.1}"

PASS=0
FAIL=0
SKIP=0

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

# send_stdio sends JSON-RPC messages to the MCP server via stdio.
#
# Each invocation is a fresh process: initialize → notifications/initialized →
# the caller's actual request.  A short sleep keeps stdin open so the Go
# process has time to flush its response before EOF triggers shutdown.
# The entire pipeline runs in a background subshell with a watchdog timer
# for portability (no coreutils `timeout` needed — works on macOS).
send_stdio() {
    local method="$1"
    local params="$2"
    if [[ -z "$params" ]]; then params="{}"; fi
    local id="${3:-3}"
    local stderr_log="${RESULTS_DIR}/stdio_stderr.log"
    local stdout_tmp="${RESULTS_DIR}/stdio_stdout.tmp"

    : > "$stdout_tmp"
    (
        {
            printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"integration-test","version":"1.0"}}}'
            sleep "$MSG_DELAY"
            printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
            sleep "$MSG_DELAY"
            printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}"
            sleep "$STDIO_WAIT"
        } | "$EVALHUB_MCP_BIN" >"$stdout_tmp" 2>"$stderr_log"
    ) &
    local pid=$!
    ( sleep "$STDIO_TIMEOUT" && kill "$pid" 2>/dev/null ) &
    local watchdog=$!
    wait "$pid" 2>/dev/null || true
    kill "$watchdog" 2>/dev/null || true
    wait "$watchdog" 2>/dev/null || true
    cat "$stdout_tmp" 2>/dev/null || true
}

# extract_response pulls out the JSON-RPC response line matching a given id
# from the multi-line NDJSON output (which also contains the initialize response).
extract_response() {
    local all_output="$1"
    local target_id="$2"
    echo "$all_output" | python3 -c "
import sys, json
target = int(sys.argv[1])
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        msg = json.loads(line)
        if msg.get('id') == target:
            print(line)
            sys.exit(0)
    except:
        pass
" "$target_id" 2>/dev/null
}

show_stderr_on_fail() {
    local stderr_log="${RESULTS_DIR}/stdio_stderr.log"
    if [[ -s "$stderr_log" ]]; then
        log "Server stderr: $(head -5 "$stderr_log")"
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
    fail "Binary '${EVALHUB_MCP_BIN}' not found on PATH. Set EVALHUB_MCP_BIN to the absolute path."
    echo -e "${RED}Cannot continue without the evalhub-mcp binary. Exiting.${NC}"
    exit 1
fi
pass "evalhub-mcp binary found: $(command -v "$EVALHUB_MCP_BIN")"

export EVALHUB_BASE_URL
export EVALHUB_TENANT

if command -v claude &>/dev/null; then
    pass "Claude Code CLI found: $(command -v claude)"
else
    skip "Claude Code CLI not found — interactive tests will be skipped"
fi

###############################################################################
# Step 1 — Register the MCP Server in Claude Code (stdio)
###############################################################################
header "Step 1: Register MCP Server (stdio)"

if command -v claude &>/dev/null; then
    log "Removing any existing 'evalhub' MCP registration..."
    claude mcp remove evalhub 2>/dev/null || true

    log "Registering evalhub via stdio transport..."
    if claude mcp add evalhub --transport stdio -- "$EVALHUB_MCP_BIN" 2>&1; then
        pass "Step 1 — Server registered with Claude Code (stdio)"
    else
        fail "Step 1 — Failed to register server with Claude Code"
    fi
else
    skip "Step 1 — Claude Code CLI not available; testing binary directly"
fi

###############################################################################
# Step 2 — Verify Server Launches Correctly
###############################################################################
header "Step 2: Verify Server Launches"

log "Sending MCP initialize request via stdio..."
INIT_STDOUT="${RESULTS_DIR}/stdio_init_stdout.tmp"
INIT_STDERR="${RESULTS_DIR}/stdio_init_stderr.log"
: > "$INIT_STDOUT"
(
    {
        printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"integration-test","version":"1.0"}}}'
        sleep "$MSG_DELAY"
        printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
        sleep "$STDIO_WAIT"
    } | "$EVALHUB_MCP_BIN" >"$INIT_STDOUT" 2>"$INIT_STDERR"
) &
_pid=$!
( sleep "$STDIO_TIMEOUT" && kill "$_pid" 2>/dev/null ) &
_wd=$!
wait "$_pid" 2>/dev/null || true
kill "$_wd" 2>/dev/null || true
wait "$_wd" 2>/dev/null || true
INIT_RESPONSE=$(cat "$INIT_STDOUT" 2>/dev/null || true)

if echo "$INIT_RESPONSE" | grep -q '"result"'; then
    pass "Step 2 — Server responds to initialize request"

    if echo "$INIT_RESPONSE" | grep -q '"tools"'; then
        pass "Step 2 — Server advertises tools capability"
    else
        fail "Step 2 — Server does not advertise tools capability"
    fi

    if echo "$INIT_RESPONSE" | grep -q '"resources"'; then
        pass "Step 2 — Server advertises resources capability"
    else
        fail "Step 2 — Server does not advertise resources capability"
    fi

    if echo "$INIT_RESPONSE" | grep -q '"prompts"'; then
        pass "Step 2 — Server advertises prompts capability"
    else
        fail "Step 2 — Server does not advertise prompts capability"
    fi
else
    fail "Step 2 — Server did not return a valid initialize response"
    log "Response was: ${INIT_RESPONSE:-<empty>}"
    show_stderr_on_fail
fi

if command -v claude &>/dev/null; then
    log "Verifying registration via 'claude mcp list'..."
    MCP_LIST=$(claude mcp list 2>&1 || true)
    if echo "$MCP_LIST" | grep -qi "evalhub"; then
        pass "Step 2 — 'claude mcp list' shows evalhub entry"
    else
        fail "Step 2 — 'claude mcp list' does not show evalhub"
        log "Output: $MCP_LIST"
    fi
fi

###############################################################################
# Step 3 — Verify MCP Resources Are Accessible
###############################################################################
header "Step 3: Verify MCP Resources"

ALL_STDIO_OUTPUT=$(send_stdio "resources/list" '{}' 3)
RESOURCES_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)

if [[ -z "$RESOURCES_RESPONSE" ]]; then
    fail "Step 3 — resources/list returned no response"
    show_stderr_on_fail
else
    pass "Step 3 — resources/list returned a response"

    # server/version is always registered; others require a backend connection.
    if echo "$RESOURCES_RESPONSE" | grep -qi "server.version"; then
        pass "Step 3 — Resource 'server/version' found in resources/list"
    else
        fail "Step 3 — Resource 'server/version' NOT found in resources/list"
    fi

    BACKEND_RESOURCES=("providers" "benchmarks" "collections" "jobs")
    for res in "${BACKEND_RESOURCES[@]}"; do
        if echo "$RESOURCES_RESPONSE" | grep -qi "$res"; then
            pass "Step 3 — Resource '${res}' found in resources/list"
        else
            skip "Step 3 — Resource '${res}' not in resources/list (requires backend at ${EVALHUB_BASE_URL})"
        fi
    done
fi

read_resource() {
    local uri="$1"
    local label="$2"
    local needs_backend="${3:-true}"
    local all_out resp
    all_out=$(send_stdio "resources/read" "{\"uri\":\"${uri}\"}" 10)
    resp=$(extract_response "$all_out" 10)

    if [[ -z "$resp" ]]; then
        if [[ "$needs_backend" == "true" ]]; then
            skip "Step 3 — Read resource ${label} (${uri}) — no response (backend may not be running)"
        else
            fail "Step 3 — Read resource ${label} (${uri}) — no response"
        fi
    elif echo "$resp" | grep -q '"error"'; then
        if [[ "$needs_backend" == "true" ]] && echo "$resp" | grep -qiE "connect|timeout|resolve|refused|ECONNREFUSED"; then
            skip "Step 3 — Read resource ${label} (${uri}) — backend not reachable"
        else
            fail "Step 3 — Read resource ${label} (${uri}) — error response"
            log "Response: $(echo "$resp" | head -1)"
        fi
    elif echo "$resp" | grep -q '"result"'; then
        pass "Step 3 — Read resource ${label} (${uri})"
    else
        fail "Step 3 — Read resource ${label} (${uri}) — unexpected response"
    fi
}

read_resource "evalhub://server/version" "Server Version" false
read_resource "evalhub://providers"      "Providers"
read_resource "evalhub://benchmarks"     "Benchmarks"
read_resource "evalhub://collections"    "Collections"
read_resource "evalhub://jobs"           "Jobs"

###############################################################################
# Step 4 — Verify MCP Tools Are Accessible
###############################################################################
header "Step 4: Verify MCP Tools"

ALL_STDIO_OUTPUT=$(send_stdio "tools/list" '{}' 3)
TOOLS_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)

declare -a EXPECTED_TOOLS=(
    "submit_evaluation"
    "cancel_job"
    "get_job_status"
)

if [[ -z "$TOOLS_RESPONSE" ]]; then
    fail "Step 4 — tools/list returned no response"
    show_stderr_on_fail
else
    for tool in "${EXPECTED_TOOLS[@]}"; do
        if echo "$TOOLS_RESPONSE" | grep -qi "$tool"; then
            pass "Step 4 — Tool '${tool}' found in tools/list"
        else
            skip "Step 4 — Tool '${tool}' not in tools/list (requires backend at ${EVALHUB_BASE_URL})"
        fi
    done

    for tool in "${EXPECTED_TOOLS[@]}"; do
        if echo "$TOOLS_RESPONSE" | python3 -c "
import sys, json
line = sys.stdin.read().strip()
try:
    msg = json.loads(line)
    if 'result' in msg and 'tools' in msg['result']:
        for t in msg['result']['tools']:
            if t.get('name') == '${tool}':
                schema = t.get('inputSchema', {})
                if schema.get('properties'):
                    print('HAS_SCHEMA')
                    sys.exit(0)
except: pass
print('NO_SCHEMA')
" 2>/dev/null | grep -q "HAS_SCHEMA"; then
            pass "Step 4 — Tool '${tool}' has parameter schema"
        else
            skip "Step 4 — Tool '${tool}' schema check (requires backend)"
        fi
    done
fi

###############################################################################
# Step 5 — Verify MCP Prompts Are Accessible
###############################################################################
header "Step 5: Verify MCP Prompts"

ALL_STDIO_OUTPUT=$(send_stdio "prompts/list" '{}' 3)
PROMPTS_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)

declare -a EXPECTED_PROMPTS=(
    "edd_workflow"
    "evaluate_model"
    "compare_runs"
)

if [[ -z "$PROMPTS_RESPONSE" ]]; then
    fail "Step 5 — prompts/list returned no response"
    show_stderr_on_fail
else
    for prompt in "${EXPECTED_PROMPTS[@]}"; do
        if echo "$PROMPTS_RESPONSE" | grep -qi "$prompt"; then
            pass "Step 5 — Prompt '${prompt}' found in prompts/list"
        else
            skip "Step 5 — Prompt '${prompt}' not in prompts/list (requires backend at ${EVALHUB_BASE_URL})"
        fi
    done
fi

ALL_STDIO_OUTPUT=$(send_stdio "prompts/get" \
    '{"name":"edd_workflow","arguments":{"application_type":"rag"}}' 3)
PROMPT_GET_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)
if [[ -n "$PROMPT_GET_RESPONSE" ]] && echo "$PROMPT_GET_RESPONSE" | grep -q '"messages"'; then
    pass "Step 5 — prompts/get edd_workflow(application_type=rag) returns messages"
else
    skip "Step 5 — prompts/get edd_workflow — not available (requires backend)"
fi

###############################################################################
# Step 6 — Verify Autocompletion
###############################################################################
header "Step 6: Verify Autocompletion"

ALL_STDIO_OUTPUT=$(send_stdio "completion/complete" \
    '{"ref":{"type":"ref/resource","uri":"evalhub://benchmarks/{id}"},"argument":{"name":"id","value":""}}' 3)
COMPLETE_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)
if [[ -n "$COMPLETE_RESPONSE" ]] && echo "$COMPLETE_RESPONSE" | grep -q '"completion"'; then
    pass "Step 6 — Autocompletion returns completion results for benchmarks"
else
    skip "Step 6 — Autocompletion for benchmarks — not available (requires backend)"
fi

ALL_STDIO_OUTPUT=$(send_stdio "completion/complete" \
    '{"ref":{"type":"ref/resource","uri":"evalhub://providers/{id}"},"argument":{"name":"id","value":""}}' 3)
COMPLETE_PROVIDERS_RESPONSE=$(extract_response "$ALL_STDIO_OUTPUT" 3)
if [[ -n "$COMPLETE_PROVIDERS_RESPONSE" ]] && echo "$COMPLETE_PROVIDERS_RESPONSE" | grep -q '"completion"'; then
    pass "Step 6 — Autocompletion returns completion results for providers"
else
    skip "Step 6 — Autocompletion for providers — not available (requires backend)"
fi

###############################################################################
# Summary
###############################################################################
header "Part 1 Summary: stdio Transport"
TOTAL=$((PASS + FAIL + SKIP))
echo -e "  ${GREEN}Passed:  ${PASS}${NC}"
echo -e "  ${RED}Failed:  ${FAIL}${NC}"
echo -e "  ${YELLOW}Skipped: ${SKIP}${NC}"
echo -e "  Total:   ${TOTAL}"
echo ""

if [[ $SKIP -gt 0 ]]; then
    echo -e "  ${YELLOW}Note:${NC} Skipped tests require a running EvalHub backend at ${EVALHUB_BASE_URL}"
    echo -e "        Resources, tools, and prompts (except server/version) are only"
    echo -e "        registered when the backend is reachable."
    echo ""
fi

echo "Detailed results written to: ${RESULTS_FILE}"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
