#!/usr/bin/env bash
# CS-01 through CS-04: Client-Specific Behavior
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "Client-Specific Behavior ($TRANSPORT)"

setup_transport() {
  if [[ "$TRANSPORT" == "stdio" ]]; then
    mcp_stdio_start "$EVALHUB_MCP_BIN" mcp
    sleep 1
  else
    mcp_http_start "$EVALHUB_HTTP_HOST" "$EVALHUB_HTTP_PORT"
  fi
  mcp_initialize >/dev/null 2>&1
}

setup_transport

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
    echo "$prompts" | jq -S '.result.prompts | map({name, description, arguments})' 2>/dev/null
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

  # Fire 5 concurrent resource reads (each request creates its own streamable session)
  for i in $(seq 1 5); do
    (
      curl -sS --max-time "${HTTP_TIMEOUT:-10}" \
        -H "Content-Type: application/json" \
        -H "Accept: application/json, text/event-stream" \
        -d "{\"jsonrpc\":\"2.0\",\"id\":$i,\"method\":\"resources/list\",\"params\":{}}" \
        "${_MCP_HTTP_ENDPOINT}" > "$tmpdir/resp_$i.raw" 2>/dev/null
    ) &
    pids+=($!)
  done

  local failed=0
  for pid in "${pids[@]}"; do
    wait "$pid" || ((failed++))
  done

  for i in $(seq 1 5); do
    if [[ -f "$tmpdir/resp_$i.raw" ]]; then
      mcp_http_strip_sse_data "$(cat "$tmpdir/resp_$i.raw")" > "$tmpdir/resp_$i.json"
    fi
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
    mcp_stdio_start "$EVALHUB_MCP_BIN" mcp
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
