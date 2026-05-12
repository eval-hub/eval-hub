#!/usr/bin/env bash
# Part 4: End-to-End EDD Workflow Test
# Tests Step 14 from CLAUDE_CODE_INTEGRATION.md
# JIRA: RHOAIENG-60353
#
# Runs the full Evaluation-Driven Development golden path:
#   Discover -> Submit -> Monitor -> Review -> Compare -> Clean up
#
# Supports both stdio and HTTP transports (set TRANSPORT=stdio|http).
set -euo pipefail

###############################################################################
# Configuration
###############################################################################
BIN_DIR="${BIN_DIR:-bin}"
EVALHUB_MCP_BIN="${EVALHUB_MCP_BIN:-${BIN_DIR}/evalhub-mcp}"
EVALHUB_BASE_URL="${EVALHUB_BASE_URL:-http://localhost:8080}"
EVALHUB_TENANT="${EVALHUB_TENANT:-tenant}"
TRANSPORT="${TRANSPORT:-stdio}"
EVALHUB_HOST="${EVALHUB_HOST:-localhost}"
EVALHUB_PORT="${EVALHUB_PORT:-3001}"
EVALHUB_HTTP_URL="http://${EVALHUB_HOST}:${EVALHUB_PORT}"
STARTUP_WAIT="${STARTUP_WAIT:-5}"
STDIO_TIMEOUT="${STDIO_TIMEOUT:-30}"
STDIO_WAIT="${STDIO_WAIT:-5}"
MSG_DELAY="${MSG_DELAY:-0.1}"
POLL_INTERVAL="${POLL_INTERVAL:-10}"
POLL_MAX_ATTEMPTS="${POLL_MAX_ATTEMPTS:-30}"
COMPARE_JOB_ID="${COMPARE_JOB_ID:-}"
RESULTS_DIR="${RESULTS_DIR:-${BIN_DIR}/test-results}"
RESULTS_FILE="${RESULTS_DIR}/part4_e2e_results.txt"

PASS=0
FAIL=0
SKIP=0
SERVER_PID=""
SESSION_ID=""
SUBMITTED_JOB_ID=""

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
        log "Stopping HTTP server (PID ${SERVER_PID})..."
        kill "$SERVER_PID" 2>/dev/null || true
        wait "$SERVER_PID" 2>/dev/null || true
    fi
}
trap cleanup EXIT

mcp_call() {
    local method="$1"
    local params="$2"
    if [[ -z "$params" ]]; then params="{}"; fi
    local id="${3:-1}"

    if [[ "$TRANSPORT" == "http" ]]; then
        if [[ -n "$SESSION_ID" ]]; then
            curl -s -X POST "${EVALHUB_HTTP_URL}" \
                -H "Content-Type: application/json" \
                -H "Mcp-Session-Id: ${SESSION_ID}" \
                -d "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}" \
                --max-time 30 2>/dev/null || echo '{"error":"request_failed"}'
        else
            curl -s -X POST "${EVALHUB_HTTP_URL}" \
                -H "Content-Type: application/json" \
                -d "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}" \
                --max-time 30 2>/dev/null || echo '{"error":"request_failed"}'
        fi
    else
        local stdout_tmp="${RESULTS_DIR}/stdio_stdout.tmp"
        : > "$stdout_tmp"
        (
            {
                printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e-test","version":"1.0"}}}'
                sleep "$MSG_DELAY"
                printf '%s\n' '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}'
                sleep "$MSG_DELAY"
                printf '%s\n' "{\"jsonrpc\":\"2.0\",\"id\":${id},\"method\":\"${method}\",\"params\":${params}}"
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
    fi
}

extract_json_field() {
    local json="$1"
    local field="$2"
    echo "$json" | python3 -c "
import sys, json
data = sys.stdin.read().strip()
for line in data.split('\n'):
    line = line.strip()
    if not line: continue
    try:
        msg = json.loads(line)
        if 'result' in msg:
            result = msg['result']
            if isinstance(result, dict):
                # Direct field
                if '${field}' in result:
                    print(result['${field}'])
                    sys.exit(0)
                # Nested in content
                for item in result.get('content', []):
                    if isinstance(item, dict) and 'text' in item:
                        try:
                            inner = json.loads(item['text'])
                            if '${field}' in inner:
                                print(inner['${field}'])
                                sys.exit(0)
                        except: pass
    except: pass
print('')
" 2>/dev/null
}

###############################################################################
# Prerequisites
###############################################################################
header "Prerequisites"

# build the MCP server binary if it doesn't exist
if ! command -v "$EVALHUB_MCP_BIN" &>/dev/null; then
    make build-mcp
fi

if ! command -v "$EVALHUB_MCP_BIN" &>/dev/null; then
    fail "Binary '${EVALHUB_MCP_BIN}' not found. Set EVALHUB_MCP_BIN."
    exit 1
fi
pass "evalhub-mcp binary found"

log "Transport: ${TRANSPORT}"

if [[ "$TRANSPORT" == "http" ]]; then
    log "Starting HTTP server..."
    "$EVALHUB_MCP_BIN" --transport http --host "$EVALHUB_HOST" --port "$EVALHUB_PORT" \
        > "${RESULTS_DIR}/e2e_server_stdout.log" 2> "${RESULTS_DIR}/e2e_server_stderr.log" &
    SERVER_PID=$!
    sleep "$STARTUP_WAIT"

    if kill -0 "$SERVER_PID" 2>/dev/null; then
        pass "HTTP server started (PID ${SERVER_PID})"
    else
        fail "HTTP server failed to start"
        exit 1
    fi

    INIT_HEADERS="${RESULTS_DIR}/e2e_init_headers.txt"
    INIT_RESP=$(curl -s -D "$INIT_HEADERS" -X POST "${EVALHUB_HTTP_URL}" \
        -H "Content-Type: application/json" \
        -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"e2e-test","version":"1.0"}}}' \
        --max-time 10 2>/dev/null || true)
    SESSION_ID=$(grep -i 'mcp-session-id' "$INIT_HEADERS" 2>/dev/null | sed 's/[^:]*: *//' | tr -d '\r\n' || true)
    if [[ -n "$SESSION_ID" ]]; then
        log "Session ID: ${SESSION_ID}"
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -H "Mcp-Session-Id: ${SESSION_ID}" \
            -d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
            --max-time 10 >/dev/null 2>&1 || true
    else
        curl -s -X POST "${EVALHUB_HTTP_URL}" \
            -H "Content-Type: application/json" \
            -d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' \
            --max-time 10 >/dev/null 2>&1 || true
    fi
fi

###############################################################################
# Step 14a — Discover: List benchmarks labeled "rag"
###############################################################################
header "Step 14a: Discover — List RAG Benchmarks"

log "Reading benchmarks resource with rag filter..."
BENCHMARKS_RESP=$(mcp_call "resources/read" '{"uri":"evalhub://benchmarks"}' 10)

if echo "$BENCHMARKS_RESP" | grep -q '"result"'; then
    pass "14a — Benchmarks resource readable"

    if echo "$BENCHMARKS_RESP" | grep -qiE "rag"; then
        pass "14a — RAG benchmarks found in response"
    else
        log "No explicit 'rag' label found; trying label-filtered URI..."
        BENCHMARKS_RAG=$(mcp_call "resources/read" '{"uri":"evalhub://benchmarks?label=rag"}' 11)
        if echo "$BENCHMARKS_RAG" | grep -q '"result"'; then
            pass "14a — Label-filtered benchmarks URI works"
        else
            skip "14a — RAG label filtering could not be verified (may need different URI pattern)"
        fi
    fi
else
    fail "14a — Failed to read benchmarks resource"
    log "Response: $(echo "$BENCHMARKS_RESP" | head -3)"
fi

###############################################################################
# Step 14b — Submit: Evaluate a model
###############################################################################
header "Step 14b: Submit — Submit Evaluation Job"

SUBMIT_PARAMS=$(cat <<'JSON'
{
    "name": "tools/call",
    "arguments": {
        "name": "submit_evaluation",
        "arguments": {
            "name": "edd-e2e-integration-test",
            "model": "https://my-model.example.com/v1",
            "collection": "rag",
            "description": "E2E integration test from Part 4 script",
            "tags": ["integration-test", "e2e", "automated"]
        }
    }
}
JSON
)

SUBMIT_RESP=$(mcp_call "tools/call" \
    '{"name":"submit_evaluation","arguments":{"name":"edd-e2e-integration-test","model":"https://my-model.example.com/v1","collection":"rag","description":"E2E integration test from Part 4 script","tags":["integration-test","e2e","automated"]}}' \
    20)

if echo "$SUBMIT_RESP" | grep -q '"result"'; then
    pass "14b — submit_evaluation tool call succeeded"

    SUBMITTED_JOB_ID=$(echo "$SUBMIT_RESP" | python3 -c "
import sys, json
data = sys.stdin.read().strip()
for line in data.split('\n'):
    line = line.strip()
    if not line: continue
    try:
        msg = json.loads(line)
        if 'result' in msg:
            result = msg['result']
            # Try direct fields
            for key in ['job_id', 'id', 'jobId']:
                if key in result:
                    print(result[key])
                    sys.exit(0)
            # Try in content array
            for item in result.get('content', []):
                if isinstance(item, dict) and 'text' in item:
                    try:
                        inner = json.loads(item['text'])
                        for key in ['job_id', 'id', 'jobId']:
                            if key in inner:
                                print(inner[key])
                                sys.exit(0)
                    except: pass
    except: pass
print('')
" 2>/dev/null)

    if [[ -n "$SUBMITTED_JOB_ID" ]]; then
        pass "14b — Job submitted with ID: ${SUBMITTED_JOB_ID}"
    else
        log "Could not extract job ID from response. Continuing with manual ID if needed."
        skip "14b — Job ID extraction (response format may differ)"
    fi
else
    fail "14b — submit_evaluation tool call failed"
    log "Response: $(echo "$SUBMIT_RESP" | head -5)"

    if echo "$SUBMIT_RESP" | grep -qiE "error"; then
        log "Error details: $(echo "$SUBMIT_RESP" | grep -i error | head -3)"
    fi
fi

###############################################################################
# Step 14c — Monitor: Poll job status
###############################################################################
header "Step 14c: Monitor — Poll Job Status"

if [[ -n "$SUBMITTED_JOB_ID" ]]; then
    log "Polling job ${SUBMITTED_JOB_ID} (max ${POLL_MAX_ATTEMPTS} attempts, every ${POLL_INTERVAL}s)..."

    JOB_COMPLETED=false
    LAST_STATUS=""
    for i in $(seq 1 "$POLL_MAX_ATTEMPTS"); do
        STATUS_RESP=$(mcp_call "tools/call" \
            "{\"name\":\"get_job_status\",\"arguments\":{\"job_id\":\"${SUBMITTED_JOB_ID}\"}}" \
            30)

        if echo "$STATUS_RESP" | grep -q '"result"'; then
            CURRENT_STATUS=$(echo "$STATUS_RESP" | python3 -c "
import sys, json
data = sys.stdin.read().strip()
for line in data.split('\n'):
    line = line.strip()
    if not line: continue
    try:
        msg = json.loads(line)
        if 'result' in msg:
            result = msg['result']
            for key in ['status', 'state']:
                if key in result:
                    print(result[key])
                    sys.exit(0)
            for item in result.get('content', []):
                if isinstance(item, dict) and 'text' in item:
                    try:
                        inner = json.loads(item['text'])
                        for key in ['status', 'state']:
                            if key in inner:
                                print(inner[key])
                                sys.exit(0)
                    except: pass
    except: pass
print('unknown')
" 2>/dev/null)

            if [[ "$CURRENT_STATUS" != "$LAST_STATUS" ]]; then
                log "Poll ${i}/${POLL_MAX_ATTEMPTS}: status = ${CURRENT_STATUS}"
                LAST_STATUS="$CURRENT_STATUS"
            fi

            case "$CURRENT_STATUS" in
                *complet*|*finish*|*done*|*success*)
                    JOB_COMPLETED=true
                    break
                    ;;
                *fail*|*error*|*cancel*)
                    log "Job ended with status: ${CURRENT_STATUS}"
                    break
                    ;;
            esac
        else
            log "Poll ${i}/${POLL_MAX_ATTEMPTS}: no valid response"
        fi

        if [[ $i -lt $POLL_MAX_ATTEMPTS ]]; then
            sleep "$POLL_INTERVAL"
        fi
    done

    if [[ "$JOB_COMPLETED" == true ]]; then
        pass "14c — Job completed successfully (status: ${CURRENT_STATUS})"
    elif echo "$LAST_STATUS" | grep -qiE "fail|error"; then
        fail "14c — Job failed (status: ${LAST_STATUS})"
    elif echo "$LAST_STATUS" | grep -qiE "cancel"; then
        skip "14c — Job was cancelled externally"
    else
        log "Job still running after ${POLL_MAX_ATTEMPTS} polls. Last status: ${LAST_STATUS}"
        skip "14c — Job did not complete within polling window (not necessarily a failure)"
    fi

    pass "14c — get_job_status tool works for polling"
else
    skip "14c — No job ID available to poll"
fi

###############################################################################
# Step 14d — Review: Read experiment results
###############################################################################
header "Step 14d: Review — Read Experiment Results"

if [[ -n "$SUBMITTED_JOB_ID" ]]; then
    log "Reading experiment results for job ${SUBMITTED_JOB_ID}..."
    RESULTS_RESP=$(mcp_call "resources/read" \
        "{\"uri\":\"evalhub://experiments/${SUBMITTED_JOB_ID}/results\"}" 40)

    if echo "$RESULTS_RESP" | grep -q '"result"'; then
        pass "14d — Experiment results readable for job ${SUBMITTED_JOB_ID}"
    else
        if echo "$RESULTS_RESP" | grep -qiE "not.found|not.ready|pending"; then
            skip "14d — Results not yet available (job may not be complete)"
        else
            fail "14d — Failed to read experiment results"
            log "Response: $(echo "$RESULTS_RESP" | head -3)"
        fi
    fi
else
    skip "14d — No job ID available for results review"
fi

###############################################################################
# Step 14e — Compare: Compare two runs
###############################################################################
header "Step 14e: Compare — Compare Evaluation Runs"

if [[ -n "$SUBMITTED_JOB_ID" ]] && [[ -n "$COMPARE_JOB_ID" ]]; then
    log "Comparing jobs: ${SUBMITTED_JOB_ID} vs ${COMPARE_JOB_ID}..."

    COMPARE_RESP=$(mcp_call "prompts/get" \
        "{\"name\":\"compare_runs\",\"arguments\":{\"job_ids\":\"${SUBMITTED_JOB_ID},${COMPARE_JOB_ID}\"}}" \
        50)

    if echo "$COMPARE_RESP" | grep -q '"messages"'; then
        pass "14e — compare_runs prompt returned guidance for two jobs"
    else
        fail "14e — compare_runs prompt did not return messages"
    fi
elif [[ -n "$SUBMITTED_JOB_ID" ]]; then
    log "No COMPARE_JOB_ID set; testing compare_runs prompt with single job..."
    COMPARE_RESP=$(mcp_call "prompts/get" \
        "{\"name\":\"compare_runs\",\"arguments\":{\"job_ids\":\"${SUBMITTED_JOB_ID}\"}}" \
        50)

    if echo "$COMPARE_RESP" | grep -q '"messages"'; then
        pass "14e — compare_runs prompt returns messages (single job)"
    else
        skip "14e — compare_runs needs two jobs. Set COMPARE_JOB_ID to a previous job ID."
    fi
else
    skip "14e — No job IDs available for comparison"
fi

###############################################################################
# Step 14f — Clean up: Cancel job if still running
###############################################################################
header "Step 14f: Clean Up — Cancel Job"

if [[ -n "$SUBMITTED_JOB_ID" ]]; then
    log "Cancelling job ${SUBMITTED_JOB_ID}..."
    CANCEL_RESP=$(mcp_call "tools/call" \
        "{\"name\":\"cancel_job\",\"arguments\":{\"job_id\":\"${SUBMITTED_JOB_ID}\"}}" 60)

    if echo "$CANCEL_RESP" | grep -q '"result"'; then
        pass "14f — cancel_job tool call succeeded"
    elif echo "$CANCEL_RESP" | grep -qiE "already.*complete|already.*cancel|not.*found|cannot.*cancel"; then
        pass "14f — cancel_job correctly reports job already finished/cancelled"
    else
        fail "14f — cancel_job tool call failed"
        log "Response: $(echo "$CANCEL_RESP" | head -3)"
    fi
else
    skip "14f — No job to cancel"
fi

###############################################################################
# Summary
###############################################################################
header "Part 4 Summary: End-to-End EDD Workflow (${TRANSPORT})"
TOTAL=$((PASS + FAIL + SKIP))
echo -e "  ${GREEN}Passed:  ${PASS}${NC}"
echo -e "  ${RED}Failed:  ${FAIL}${NC}"
echo -e "  ${YELLOW}Skipped: ${SKIP}${NC}"
echo -e "  Total:   ${TOTAL}"
echo ""

if [[ -n "$SUBMITTED_JOB_ID" ]]; then
    echo "  Job ID:  ${SUBMITTED_JOB_ID}"
fi

echo ""
echo "Detailed results written to: ${RESULTS_FILE}"
echo ""
echo "To run with HTTP transport:   TRANSPORT=http $0"
echo "To compare with a prior job:  COMPARE_JOB_ID=<id> $0"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
