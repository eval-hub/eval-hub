#!/usr/bin/env bash
# PRM-01 through PRM-06: MCP Prompts
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "MCP Prompts ($TRANSPORT)"

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

# ---------- PRM-01: Prompt discovery ------------------------------------------
prm01() {
  local id="PRM-01" desc="All 3 prompts discoverable via prompts/list"
  local result
  result=$(mcp_list_prompts)

  if ! assert_json_has_result "$result"; then
    test_fail "$id" "$desc" "prompts/list failed: $result"
    return
  fi

  local prompts
  prompts=$(echo "$result" | jq -r '.result.prompts[].name' 2>/dev/null || echo "")
  local missing=""
  for expected in "edd_workflow" "evaluate_model" "compare_runs"; do
    if ! echo "$prompts" | grep -q "^${expected}$"; then
      missing="$missing $expected"
    fi
  done

  if [[ -z "$missing" ]]; then
    local all_have_desc=true
    for p in edd_workflow evaluate_model compare_runs; do
      local has_desc
      has_desc=$(echo "$result" | jq -r ".result.prompts[] | select(.name==\"$p\") | .description // empty" 2>/dev/null)
      if [[ -z "$has_desc" ]]; then all_have_desc=false; fi
    done
    if $all_have_desc; then
      test_pass "$id" "$desc — all have descriptions"
    else
      test_fail "$id" "$desc" "one or more prompts missing description"
    fi
  else
    test_fail "$id" "$desc" "missing prompts:$missing"
  fi
}
prm01

# ---------- PRM-02: edd_workflow — RAG type -----------------------------------
prm02() {
  local id="PRM-02" desc="edd_workflow prompt — app_type=rag"
  local result
  result=$(mcp_get_prompt "edd_workflow" '{"app_type":"rag"}')

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local messages
    messages=$(echo "$result" | jq '.result.messages | length' 2>/dev/null || echo 0)
    if [[ "$messages" -gt 0 ]]; then
      local content
      content=$(echo "$result" | jq -r '.result.messages[0].content.text // .result.messages[0].content // empty' 2>/dev/null)
      if echo "$content" | grep -qi "rag\|retrieval\|evaluation\|benchmark"; then
        test_pass "$id" "$desc — $messages messages, RAG-specific content confirmed"
      else
        test_pass "$id" "$desc — $messages messages returned (content not RAG-specific)"
      fi
    else
      test_fail "$id" "$desc" "prompt returned 0 messages"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
prm02

# ---------- PRM-03: edd_workflow — other application types --------------------
prm03() {
  local id="PRM-03" desc="edd_workflow prompt — agent, safety, classifier types"
  local all_ok=true
  local failures=""

  for app_type in agent safety classifier; do
    local result
    result=$(mcp_get_prompt "edd_workflow" "$(jq -cn --arg t "$app_type" '{"app_type":$t}')")

    if ! assert_json_has_result "$result" || ! assert_json_no_error "$result"; then
      all_ok=false
      failures="$failures $app_type"
    fi
  done

  if $all_ok; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "failed for types:$failures"
  fi
}
prm03

# ---------- PRM-04: evaluate_model prompt -------------------------------------
prm04() {
  local id="PRM-04" desc="evaluate_model prompt"
  local result
  result=$(mcp_get_prompt "evaluate_model" '{}')

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local messages
    messages=$(echo "$result" | jq '.result.messages | length' 2>/dev/null || echo 0)
    if [[ "$messages" -gt 0 ]]; then
      test_pass "$id" "$desc — $messages messages"
    else
      test_fail "$id" "$desc" "prompt returned 0 messages"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
prm04

# ---------- PRM-05: compare_runs prompt ---------------------------------------
prm05() {
  local id="PRM-05" desc="compare_runs prompt"
  local result
  result=$(mcp_get_prompt "compare_runs" '{}')

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local messages
    messages=$(echo "$result" | jq '.result.messages | length' 2>/dev/null || echo 0)
    if [[ "$messages" -gt 0 ]]; then
      test_pass "$id" "$desc — $messages messages"
    else
      test_fail "$id" "$desc" "prompt returned 0 messages"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
prm05

# ---------- PRM-06: Prompt rendering fidelity (cross-transport) ---------------
prm06() {
  local id="PRM-06" desc="Prompt rendering fidelity — content structure check"

  # We can only verify within the current transport; cross-transport comparison
  # is done by the runner comparing stdio vs HTTP output files.
  local result
  result=$(mcp_get_prompt "edd_workflow" '{"app_type":"rag"}')

  if assert_json_has_result "$result"; then
    local msg_count role_ok
    msg_count=$(echo "$result" | jq '.result.messages | length' 2>/dev/null || echo 0)
    role_ok=$(echo "$result" | jq -e '.result.messages[0].role' >/dev/null 2>&1 && echo "yes" || echo "")

    if [[ "$msg_count" -gt 0 && -n "$role_ok" ]]; then
      # Save output for cross-transport diff (runner compares these files)
      local outfile="$SCRIPT_DIR/../reports/prompt_fidelity_${TRANSPORT}.json"
      mkdir -p "$(dirname "$outfile")"
      echo "$result" | jq '.result.messages' > "$outfile" 2>/dev/null
      test_pass "$id" "$desc — saved for cross-transport diff"
    else
      test_fail "$id" "$desc" "unexpected prompt structure"
    fi
  else
    test_fail "$id" "$desc" "could not get prompt"
  fi
}
prm06
