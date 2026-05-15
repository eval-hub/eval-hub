#!/usr/bin/env bash
# ERR-01 through ERR-06: Error Scenarios
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "Error Scenarios ($TRANSPORT)"

# ---------- ERR-01: Server not running (HTTP only) ----------------------------
err01() {
  local id="ERR-01" desc="Server not running — actionable error"
  if [[ "$TRANSPORT" != "http" ]]; then
    test_skip "$id" "$desc" "stdio transport — server is launched by client"
    return
  fi

  # Try to connect to a port where nothing is running
  local dead_port=39999
  mcp_http_start "localhost" "$dead_port"
  local result
  result=$(mcp_request "resources/list" '{}')

  if echo "$result" | grep -q "http_request_failed\|error\|timeout\|connection_refused"; then
    test_pass "$id" "$desc — connection failure detected"
  else
    test_fail "$id" "$desc" "expected connection error, got: $result"
  fi

  # Restore proper HTTP config for subsequent tests
  mcp_http_start "$EVALHUB_HTTP_HOST" "$EVALHUB_HTTP_PORT"
  mcp_initialize >/dev/null || true
}
err01

# ---------- ERR-02: Invalid auth token ----------------------------------------
err02() {
  local id="ERR-02" desc="Invalid auth token — clear authentication error"

  if [[ "$TRANSPORT" == "stdio" ]]; then
    # Start a new server process with bad token
    local orig_token="$EVALHUB_TOKEN"
    export EVALHUB_TOKEN="${INVALID_TOKEN:-bad-token-xxx}"

    local tmpdir
    tmpdir=$(mktemp -d)
    local stdin_fifo="$tmpdir/stdin"
    local stdout_fifo="$tmpdir/stdout"
    mkfifo "$stdin_fifo" "$stdout_fifo"

    env EVALHUB_TOKEN="${INVALID_TOKEN:-bad-token-xxx}" \
      EVALHUB_TENANT="${EVALHUB_TENANT:-tenant}" \
      EVALHUB_BASE_URL="${EVALHUB_BASE_URL:-http://localhost:8080}" \
      "$EVALHUB_MCP_BIN" < "$stdin_fifo" > "$stdout_fifo" 2>"$tmpdir/stderr" &
    local pid=$!
    exec 5>"$stdin_fifo"
    exec 6<"$stdout_fifo"

    # Initialize
    printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}\n' >&5
    timeout "${STDIO_TIMEOUT:-10}" head -n1 <&6 >/dev/null 2>&1 || true
    printf '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}\n' >&5 || true

    # Try to list providers (should fail with auth error)
    if ! kill -0 "$pid" 2>/dev/null; then
      test_fail "$id" "$desc" "server process died before resources/read"
      export EVALHUB_TOKEN="$orig_token"
      exec 5>&- 2>/dev/null || true
      exec 6<&- 2>/dev/null || true
      rm -rf "$tmpdir"
      return
    fi
    printf '{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"evalhub://providers"}}\n' >&5
    local result
    result=$(timeout "${STDIO_TIMEOUT:-10}" head -n1 <&6 2>/dev/null || echo '{"error":"timeout"}')

    exec 5>&- 2>/dev/null || true
    exec 6<&- 2>/dev/null || true
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -rf "$tmpdir"

    export EVALHUB_TOKEN="$orig_token"

    if echo "$result" | grep -qi "auth\|unauthorized\|401\|forbidden\|token\|credential"; then
      test_pass "$id" "$desc — auth error surfaced"
    elif echo "$result" | grep -qi "error"; then
      test_pass "$id" "$desc — error returned (check message specificity)"
    else
      test_fail "$id" "$desc" "no auth error in response: $result"
    fi
  else
    # HTTP: send request with bad auth header
    local result raw
    raw=$(curl -sS --max-time "${HTTP_TIMEOUT:-10}" \
      -H "Content-Type: application/json" \
      -H "Accept: application/json, text/event-stream" \
      -H "Authorization: Bearer ${INVALID_TOKEN:-bad-token}" \
      -d '{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"evalhub://providers"}}' \
      "${_MCP_HTTP_ENDPOINT}" 2>/dev/null || true)
    result=$(mcp_http_strip_sse_data "$raw")

    if echo "$result" | grep -qi "auth\|unauthorized\|401\|forbidden\|token"; then
      test_pass "$id" "$desc — auth error surfaced"
    elif echo "$result" | grep -qi "error"; then
      test_pass "$id" "$desc — error returned (verify message clarity)"
    else
      test_fail "$id" "$desc" "no auth error: $result"
    fi
  fi
}
err02

# ---------- ERR-03: Backend unreachable ---------------------------------------
err03() {
  local id="ERR-03" desc="Backend unreachable — server starts, operations return error"

  if [[ "$TRANSPORT" == "stdio" ]]; then
    local orig_url="$EVALHUB_BASE_URL"
    export EVALHUB_BASE_URL="${UNREACHABLE_URL:-http://192.0.2.1:9999}"

    local tmpdir
    tmpdir=$(mktemp -d)
    local stdin_fifo="$tmpdir/stdin"
    local stdout_fifo="$tmpdir/stdout"
    mkfifo "$stdin_fifo" "$stdout_fifo"

    env EVALHUB_BASE_URL="${UNREACHABLE_URL:-http://192.0.2.1:9999}" \
      EVALHUB_TOKEN="${EVALHUB_TOKEN:-token}" \
      EVALHUB_TENANT="${EVALHUB_TENANT:-tenant}" \
      "$EVALHUB_MCP_BIN" < "$stdin_fifo" > "$stdout_fifo" 2>"$tmpdir/stderr" &
    local pid=$!
    exec 5>"$stdin_fifo"
    exec 6<"$stdout_fifo"

    sleep 1
    if ! kill -0 "$pid" 2>/dev/null; then
      test_fail "$id" "$desc" "server crashed on start with unreachable backend"
      export EVALHUB_BASE_URL="$orig_url"
      rm -rf "$tmpdir"
      return
    fi

    # Initialize (should succeed even with unreachable backend)
    printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}\n' >&5
    timeout "${STDIO_TIMEOUT:-10}" head -n1 <&6 >/dev/null 2>&1 || true
    printf '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}\n' >&5 || true

    # Try an operation (should return connectivity error)
    if ! kill -0 "$pid" 2>/dev/null; then
      test_fail "$id" "$desc" "server process died before resources/read"
      export EVALHUB_BASE_URL="$orig_url"
      exec 5>&- 2>/dev/null || true
      exec 6<&- 2>/dev/null || true
      rm -rf "$tmpdir"
      return
    fi
    printf '{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"evalhub://providers"}}\n' >&5
    local result
    result=$(timeout "${STDIO_TIMEOUT:-10}" head -n1 <&6 2>/dev/null || echo '{"error":"timeout"}')

    exec 5>&- 2>/dev/null || true
    exec 6<&- 2>/dev/null || true
    kill "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
    rm -rf "$tmpdir"

    export EVALHUB_BASE_URL="$orig_url"

    if echo "$result" | grep -qi "connect\|unreachable\|timeout\|refused\|error"; then
      test_pass "$id" "$desc — connectivity error returned"
    else
      test_fail "$id" "$desc" "expected connectivity error: $result"
    fi
  else
    test_skip "$id" "$desc" "requires restarting server with bad URL (manual test)"
  fi
}
err03

# ---------- ERR-04: Invalid binary path (stdio only) --------------------------
err04() {
  local id="ERR-04" desc="Invalid binary path — client-side error"
  if [[ "$TRANSPORT" != "stdio" ]]; then
    test_skip "$id" "$desc" "stdio-only test"
    return
  fi

  local fake_bin="/tmp/nonexistent-evalhub-mcp-binary-$$"
  if "$fake_bin" --version </dev/null >/dev/null 2>&1; then
    test_fail "$id" "$desc" "non-existent binary somehow succeeded"
  else
    test_pass "$id" "$desc — shell returns error for missing binary"
  fi
}
err04

# ---------- ERR-05: Server crash mid-operation (HTTP) -------------------------
err05() {
  local id="ERR-05" desc="Server crash mid-operation — client detects disconnect"
  if [[ "$TRANSPORT" != "http" ]]; then
    test_skip "$id" "$desc" "requires HTTP transport"
    return
  fi
  test_skip "$id" "$desc" "manual test — kill server during long operation"
}
err05

# ---------- ERR-06: Expired auth token mid-session ----------------------------
err06() {
  local id="ERR-06" desc="Expired auth token mid-session"
  test_skip "$id" "$desc" "requires token expiration infrastructure (manual test)"
}
err06
