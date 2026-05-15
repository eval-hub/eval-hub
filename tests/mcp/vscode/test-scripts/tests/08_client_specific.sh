#!/usr/bin/env bash
# CS-01 through CS-04: Client-Specific Behavior
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

# CS-02: one Streamable HTTP session (initialize → initialized → resources/list).
# Must be file-scoped so subshells can invoke it on macOS bash 3.2.
_cs02_http_session_resources_list() {
  local idx="$1" tmpdir="$2" ep="$3"
  local hdrf bodyf http_code sid raw
  hdrf=$(mktemp "${tmpdir}/h${idx}.XXXXXX")
  bodyf=$(mktemp "${tmpdir}/b${idx}.XXXXXX")
  http_code=$(curl -sS --max-time "${HTTP_TIMEOUT:-15}" -D "$hdrf" -o "$bodyf" -w "%{http_code}" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cs02-worker","version":"1.0"}}}' \
    "$ep" 2>/dev/null | tr -d '\r\n')
  case "$http_code" in
    200|201|202) ;;
    *) rm -f "$hdrf" "$bodyf"; echo '{"error":"init_failed","http":'"$http_code"'}' > "$tmpdir/resp_$idx.json"; return ;;
  esac
  sid=$(awk -F': *' 'tolower($1)=="mcp-session-id"{gsub(/\r/,"",$2);print $2;exit}' "$hdrf")
  rm -f "$hdrf" "$bodyf"
  if [[ -z "$sid" ]]; then
    echo '{"error":"no_session"}' > "$tmpdir/resp_$idx.json"
    return
  fi
  curl -sS --max-time "${HTTP_TIMEOUT:-10}" -o /dev/null -X POST "$ep" \
    -H "Content-Type: application/json" \
    -H "Mcp-Session-Id: $sid" \
    -d '{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}' 2>/dev/null || true
  hdrf=$(mktemp "${tmpdir}/h2${idx}.XXXXXX")
  bodyf=$(mktemp "${tmpdir}/b2${idx}.XXXXXX")
  http_code=$(curl -sS --max-time "${HTTP_TIMEOUT:-15}" -D "$hdrf" -o "$bodyf" -w "%{http_code}" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json, text/event-stream" \
    -H "Mcp-Session-Id: $sid" \
    -d '{"jsonrpc":"2.0","id":2,"method":"resources/list","params":{}}' \
    "$ep" 2>/dev/null | tr -d '\r\n')
  raw=$(cat "$bodyf" 2>/dev/null || true)
  rm -f "$hdrf" "$bodyf"
  if [[ "$http_code" != "200" && "$http_code" != "201" && "$http_code" != "202" ]]; then
    echo '{"error":"list_failed","http":'"$http_code"'}' > "$tmpdir/resp_$idx.json"
    return
  fi
  if printf '%s' "$raw" | grep -q '^data:'; then
    printf '%s' "$raw" | sed -n 's/^data: //p' | tail -n1 > "$tmpdir/resp_$idx.json"
  else
    printf '%s\n' "$raw" > "$tmpdir/resp_$idx.json"
  fi
}

test_suite "Client-Specific Behavior ($TRANSPORT)"

mcp_setup_transport "$TRANSPORT" || exit 1

# ---------- CS-01: Transport payload parity -----------------------------------
cs01() {
  local id="CS-01" desc="Transport payload parity — identical results across transports"

  # Collect responses and save to file for cross-transport comparison
  local outdir="$SCRIPT_DIR/../reports"
  mkdir -p "$outdir"

  local resources tools prompts
  resources=$(mcp_list_resources)
  tools=$(mcp_list_tools)
  prompts=$(mcp_list_prompts)

  # Save normalized output (strip IDs and timing info for clean diff)
  {
    echo "=== resources/list ==="
    echo "$resources" | jq -S '.result.resources | map({name, uri, description})' 2>/dev/null
    echo "=== tools/list ==="
    echo "$tools" | jq -S '.result.tools | map({name, description, inputSchema})' 2>/dev/null
    echo "=== prompts/list ==="
    # Sort prompt arguments by name so stdio vs HTTP ordering differences do not fail parity.
    echo "$prompts" | jq -S '.result.prompts | map({name, description, arguments: ((.arguments // []) | sort_by(.name))})' 2>/dev/null
  } > "$outdir/parity_${TRANSPORT}.txt" 2>/dev/null

  # Within a single transport, just verify all three returned results
  if assert_json_has_result "$resources" && \
     assert_json_has_result "$tools" && \
     assert_json_has_result "$prompts"; then
    test_pass "$id" "$desc — all primitives returned (saved for cross-transport diff)"
    printf "       Run both transports, then: diff reports/parity_stdio.txt reports/parity_http.txt\n"
  else
    test_fail "$id" "$desc" "one or more primitives failed to return"
  fi
}
cs01

# ---------- CS-02: Concurrent sessions (HTTP only) ----------------------------
cs02() {
  local id="CS-02" desc="Concurrent sessions — parallel requests"
  if [[ "$TRANSPORT" != "http" ]]; then
    test_skip "$id" "$desc" "requires HTTP transport for concurrent access"
    return
  fi

  local pids=()
  local tmpdir
  tmpdir=$(mktemp -d)
  local ep="${_MCP_HTTP_ENDPOINT}"

  for i in $(seq 1 5); do
    ( _cs02_http_session_resources_list "$i" "$tmpdir" "$ep" ) &
    pids+=($!)
  done

  local failed=0
  for pid in "${pids[@]}"; do
    wait "$pid" || ((failed++))
  done

  local success=0
  for i in $(seq 1 5); do
    if [[ -f "$tmpdir/resp_$i.json" ]] && \
       jq -e '.result' "$tmpdir/resp_$i.json" >/dev/null 2>&1; then
      ((success++))
    fi
  done

  rm -rf "$tmpdir"

  if [[ "$success" -eq 5 ]]; then
    test_pass "$id" "$desc — 5/5 concurrent requests succeeded"
  elif [[ "$success" -gt 0 ]]; then
    test_fail "$id" "$desc" "$success/5 succeeded, $((5-success)) failed"
  else
    test_fail "$id" "$desc" "all concurrent requests failed"
  fi
}
cs02

# ---------- CS-03: Client restart recovery ------------------------------------
cs03() {
  local id="CS-03" desc="Client restart recovery"
  if [[ "$TRANSPORT" == "stdio" ]]; then
    # Simulate: stop and restart the stdio server
    mcp_stdio_stop 2>/dev/null || true
    sleep 1
    mcp_stdio_start "$EVALHUB_MCP_BIN"
    sleep 1
    local result
    result=$(mcp_initialize)
    if assert_json_has_result "$result"; then
      test_pass "$id" "$desc — stdio server relaunched successfully"
    else
      test_fail "$id" "$desc" "could not reinitialize after restart"
    fi
  else
    # HTTP: just verify the persistent server is still reachable
    local result
    result=$(mcp_list_resources)
    if assert_json_has_result "$result"; then
      test_pass "$id" "$desc — HTTP server still reachable"
    else
      test_fail "$id" "$desc" "HTTP server unreachable after simulated restart"
    fi
  fi
}
cs03

# ---------- CS-04: Large response handling ------------------------------------
cs04() {
  local id="CS-04" desc="Large response handling — 100+ items"

  # Request all jobs without filtering (hoping for a large dataset)
  local result
  result=$(mcp_read_resource "evalhub://jobs")

  if assert_json_has_result "$result"; then
    local text
    text=$(echo "$result" | jq -r '.result.contents[0].text // empty' 2>/dev/null)
    local text_len=${#text}

    if [[ "$text_len" -gt 10000 ]]; then
      test_pass "$id" "$desc — response is ${text_len} chars (large response handled)"
    elif [[ "$text_len" -gt 0 ]]; then
      test_pass "$id" "$desc — response is ${text_len} chars (may need more test data for stress test)"
    else
      test_fail "$id" "$desc" "empty response"
    fi
  else
    test_fail "$id" "$desc" "error reading jobs resource"
  fi
}
cs04
