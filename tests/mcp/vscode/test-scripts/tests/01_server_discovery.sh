#!/usr/bin/env bash
# SD-01 through SD-04: Server Discovery & Connection
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "Server Discovery ($TRANSPORT)"

# ---------- SD-01: Server launches / connects ---------------------------------
sd01() {
  local id="SD-01" desc="Server launches and connects via $TRANSPORT"
  if mcp_setup_transport "$TRANSPORT"; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "transport setup or initialize failed"
  fi
}

# ---------- SD-02: HTTP server accepts connection (HTTP-only) -----------------
sd02() {
  local id="SD-02" desc="HTTP server accepts SSE/HTTP connections"
  if [[ "$TRANSPORT" != "http" ]]; then
    test_skip "$id" "$desc" "stdio transport — not applicable"
    return
  fi
  if assert_port_open "$EVALHUB_HTTP_HOST" "$EVALHUB_HTTP_PORT"; then
    local health
    health=$(curl -sf --max-time "$HTTP_TIMEOUT" \
      "${_MCP_HTTP_ENDPOINT}" \
      -H "Content-Type: application/json" \
      -H "Accept: application/json, text/event-stream" \
      -d '{"jsonrpc":"2.0","id":999,"method":"ping","params":{}}' 2>/dev/null || echo '{"error":"no_response"}')
    if echo "$health" | jq -e '.error' >/dev/null 2>&1; then
      test_fail "$id" "$desc" "HTTP endpoint returned error: $health"
    else
      test_pass "$id" "$desc"
    fi
  else
    test_fail "$id" "$desc" "port not open"
  fi
}

# ---------- SD-03: Server metadata visible ------------------------------------
sd03() {
  local id="SD-03" desc="Server metadata (name, version, capabilities) visible"
  local result
  result=$(mcp_initialize)

  if assert_json_has_key "$result" "result"; then
    local has_name has_version has_caps
    has_name=$(echo "$result" | jq -r '.result.serverInfo.name // empty' 2>/dev/null)
    has_version=$(echo "$result" | jq -r '.result.serverInfo.version // empty' 2>/dev/null)
    has_caps=$(echo "$result" | jq -e '.result.capabilities' >/dev/null 2>&1 && echo "yes" || echo "")

    if [[ -n "$has_name" && -n "$has_caps" ]]; then
      test_pass "$id" "$desc — name=$has_name version=${has_version:-unknown}"
    else
      test_fail "$id" "$desc" "missing serverInfo.name or capabilities in: $result"
    fi
  else
    test_fail "$id" "$desc" "no result in initialize response"
  fi
}

# ---------- SD-04: Reconnection after server restart (HTTP only) ---------------
sd04() {
  local id="SD-04" desc="Reconnection after server restart"
  if [[ "$TRANSPORT" != "http" ]]; then
    test_skip "$id" "$desc" "requires HTTP transport for restart test"
    return
  fi

  # Baseline: confirm we can call the server
  local before
  before=$(mcp_list_resources)
  if ! assert_json_has_result "$before"; then
    test_fail "$id" "$desc" "could not reach server before restart"
    return
  fi

  printf "       (Restart the server process manually within %ds)\n" "${SERVER_STARTUP_WAIT:-3}"
  test_skip "$id" "$desc" "manual step — restart server and re-run to verify"
}

# --- Run ----------------------------------------------------------------------
sd01
sd02
sd03
sd04
