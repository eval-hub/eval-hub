#!/usr/bin/env bash
# AC-01 through AC-03: Autocompletion
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "Autocompletion ($TRANSPORT)"

mcp_setup_transport "$TRANSPORT" || exit 1

# ---------- AC-01: Resource URI autocompletion --------------------------------
ac01() {
  local id="AC-01" desc="Resource URI autocompletion"
  local result
  result=$(mcp_complete "ref/resource" "evalhub://benchmarks" "uri" "evalhub://bench")

  if assert_json_has_result "$result"; then
    local completions
    completions=$(echo "$result" | jq '.result.completion.values | length' 2>/dev/null || echo 0)
    if [[ "$completions" -gt 0 ]]; then
      test_pass "$id" "$desc — $completions suggestions returned"
    else
      # completion/complete may not be supported by all servers; mark as skip if method not found
      local err_code
      err_code=$(echo "$result" | jq -r '.error.code // empty' 2>/dev/null)
      if [[ "$err_code" == "-32601" ]]; then
        test_skip "$id" "$desc" "completion/complete not implemented"
      else
        test_fail "$id" "$desc" "0 completions returned"
      fi
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
ac01

# ---------- AC-02: Parameter value autocompletion -----------------------------
ac02() {
  local id="AC-02" desc="Parameter value autocompletion (label filter)"
  local result
  result=$(mcp_complete "ref/resource" "evalhub://benchmarks" "label" "ra")

  if assert_json_has_result "$result"; then
    local completions
    completions=$(echo "$result" | jq '.result.completion.values | length' 2>/dev/null || echo 0)
    if [[ "$completions" -gt 0 ]]; then
      local has_rag
      has_rag=$(echo "$result" | jq -r '.result.completion.values[]' 2>/dev/null | grep -c "rag" || echo 0)
      if [[ "$has_rag" -gt 0 ]]; then
        test_pass "$id" "$desc — 'rag' found in suggestions"
      else
        test_pass "$id" "$desc — $completions suggestions (rag not in list)"
      fi
    else
      local err_code
      err_code=$(echo "$result" | jq -r '.error.code // empty' 2>/dev/null)
      if [[ "$err_code" == "-32601" ]]; then
        test_skip "$id" "$desc" "completion/complete not implemented"
      else
        test_fail "$id" "$desc" "0 completions"
      fi
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
ac02

# ---------- AC-03: Tool argument autocompletion (schema-based) ----------------
ac03() {
  local id="AC-03" desc="Tool argument autocompletion (schema-based)"
  # Tool argument completion is primarily a client-side feature driven by
  # inputSchema. We verify the schema is rich enough for clients to use.
  local result
  result=$(mcp_list_tools)

  if ! assert_json_has_result "$result"; then
    test_fail "$id" "$desc" "tools/list failed"
    return
  fi

  local submit_schema
  submit_schema=$(echo "$result" | jq '.result.tools[] | select(.name=="submit_evaluation") | .inputSchema' 2>/dev/null)

  if [[ -z "$submit_schema" || "$submit_schema" == "null" ]]; then
    test_fail "$id" "$desc" "submit_evaluation has no inputSchema"
    return
  fi

  local has_props has_required has_descriptions
  has_props=$(echo "$submit_schema" | jq -e '.properties' >/dev/null 2>&1 && echo "yes" || echo "")
  has_required=$(echo "$submit_schema" | jq -e '.required' >/dev/null 2>&1 && echo "yes" || echo "")

  # Check at least one property has a description
  has_descriptions=$(echo "$submit_schema" | jq '[.properties[].description // empty] | length' 2>/dev/null || echo 0)

  if [[ -n "$has_props" && "$has_descriptions" -gt 0 ]]; then
    test_pass "$id" "$desc — schema has properties with descriptions"
  else
    test_fail "$id" "$desc" "schema missing properties or descriptions"
  fi
}
ac03
