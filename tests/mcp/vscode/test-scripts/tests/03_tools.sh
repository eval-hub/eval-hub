#!/usr/bin/env bash
# TOOL-01 through TOOL-07: MCP Tools
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "MCP Tools ($TRANSPORT)"

mcp_setup_transport "$TRANSPORT" || exit 1

# ---------- TOOL-01: Tool discovery -------------------------------------------
tool01() {
  local id="TOOL-01" desc="All 3 tools discoverable via tools/list"
  local result
  result=$(mcp_list_tools)

  if ! assert_json_has_result "$result"; then
    test_fail "$id" "$desc" "tools/list failed: $result"
    return
  fi

  local tools
  tools=$(echo "$result" | jq -r '.result.tools[].name' 2>/dev/null || echo "")
  local missing=""
  for expected in "submit_evaluation" "cancel_job" "get_job_status"; do
    if ! echo "$tools" | grep -q "^${expected}$"; then
      missing="$missing $expected"
    fi
  done

  if [[ -z "$missing" ]]; then
    # Verify each tool has inputSchema
    local schemas_ok=true
    for t in submit_evaluation cancel_job get_job_status; do
      local has_schema
      has_schema=$(echo "$result" | jq -e ".result.tools[] | select(.name==\"$t\") | .inputSchema" >/dev/null 2>&1 && echo "yes" || echo "")
      if [[ -z "$has_schema" ]]; then
        schemas_ok=false
      fi
    done
    if $schemas_ok; then
      test_pass "$id" "$desc — all have inputSchema"
    else
      test_fail "$id" "$desc" "one or more tools missing inputSchema"
    fi
  else
    test_fail "$id" "$desc" "missing tools:$missing"
  fi
}
tool01

# ---------- TOOL-02: submit_evaluation — basic --------------------------------
_SUBMITTED_JOB_ID=""

tool02() {
  local id="TOOL-02" desc="submit_evaluation — basic benchmark + model endpoint"
  if [[ -z "${TEST_BENCHMARK_ID:-}" || -z "${TEST_MODEL_ENDPOINT:-}" ]]; then
    test_skip "$id" "$desc" "TEST_BENCHMARK_ID or TEST_MODEL_ENDPOINT not set"
    return
  fi

  local args
  args=$(jq -cn \
    --arg bench "$TEST_BENCHMARK_ID" \
    --arg model "$TEST_MODEL_ENDPOINT" \
    '{"benchmarks":[$bench],"model_endpoint":$model,"name":"integration-test-basic"}')

  local result
  result=$(mcp_call_tool "submit_evaluation" "$args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    _SUBMITTED_JOB_ID=$(echo "$result" | jq -r '.result.content[0].text // empty' 2>/dev/null | jq -r '.job_id // .id // empty' 2>/dev/null || echo "")
    if [[ -n "$_SUBMITTED_JOB_ID" ]]; then
      test_pass "$id" "$desc — job_id=$_SUBMITTED_JOB_ID"
    else
      test_pass "$id" "$desc — submitted (could not extract job_id from response)"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
tool02

# ---------- TOOL-03: submit_evaluation — with collection ----------------------
tool03() {
  local id="TOOL-03" desc="submit_evaluation — using collection"
  if [[ -z "${TEST_COLLECTION_ID:-}" || -z "${TEST_MODEL_ENDPOINT:-}" ]]; then
    test_skip "$id" "$desc" "TEST_COLLECTION_ID or TEST_MODEL_ENDPOINT not set"
    return
  fi

  local args
  args=$(jq -cn \
    --arg coll "$TEST_COLLECTION_ID" \
    --arg model "$TEST_MODEL_ENDPOINT" \
    '{"collection":$coll,"model_endpoint":$model,"name":"integration-test-collection"}')

  local result
  result=$(mcp_call_tool "submit_evaluation" "$args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
tool03

# ---------- TOOL-04: submit_evaluation — full params --------------------------
tool04() {
  local id="TOOL-04" desc="submit_evaluation — full params (name, description, tags, config)"
  if [[ -z "${TEST_BENCHMARK_ID:-}" || -z "${TEST_MODEL_ENDPOINT:-}" ]]; then
    test_skip "$id" "$desc" "TEST_BENCHMARK_ID or TEST_MODEL_ENDPOINT not set"
    return
  fi

  local args
  args=$(jq -cn \
    --arg bench "$TEST_BENCHMARK_ID" \
    --arg model "$TEST_MODEL_ENDPOINT" \
    '{
      "benchmarks": [$bench],
      "model_endpoint": $model,
      "name": "integration-test-full",
      "description": "Full-param test from automated harness",
      "tags": ["integration-test", "automated"],
      "experiment_config": {"max_samples": 10}
    }')

  local result
  result=$(mcp_call_tool "submit_evaluation" "$args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
tool04

# ---------- TOOL-05: get_job_status -------------------------------------------
tool05() {
  local id="TOOL-05" desc="get_job_status — poll a submitted job"
  local job_id="${_SUBMITTED_JOB_ID:-${TEST_JOB_ID:-}}"
  if [[ -z "$job_id" ]]; then
    test_skip "$id" "$desc" "no job_id available (submit may have been skipped)"
    return
  fi

  local args
  args=$(jq -cn --arg jid "$job_id" '{"job_id":$jid}')

  local result
  result=$(mcp_call_tool "get_job_status" "$args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local status_text
    status_text=$(echo "$result" | jq -r '.result.content[0].text // empty' 2>/dev/null)
    test_pass "$id" "$desc — response received"
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
tool05

# ---------- TOOL-06: cancel_job -----------------------------------------------
tool06() {
  local id="TOOL-06" desc="cancel_job — cancel a submitted job"
  local job_id="${_SUBMITTED_JOB_ID:-${TEST_JOB_ID:-}}"
  if [[ -z "$job_id" ]]; then
    test_skip "$id" "$desc" "no job_id available"
    return
  fi

  local args
  args=$(jq -cn --arg jid "$job_id" '{"job_id":$jid}')

  local result
  result=$(mcp_call_tool "cancel_job" "$args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc — job_id=$job_id"
  else
    local err
    err=$(echo "$result" | jq -r '.error.message // .error // "unknown"' 2>/dev/null)
    if echo "$err" | grep -qi "already\|completed\|cancelled"; then
      test_pass "$id" "$desc — job already finished (acceptable)"
    else
      test_fail "$id" "$desc" "error: $err"
    fi
  fi
}
tool06

# ---------- TOOL-07: Tool parameter validation --------------------------------
tool07() {
  local id="TOOL-07" desc="Tool parameter validation — missing required fields"

  local result
  result=$(mcp_call_tool "submit_evaluation" '{}')

  if assert_json_has_error "$result"; then
    test_pass "$id" "$desc — server returned validation error"
  else
    local is_error_content
    is_error_content=$(echo "$result" | jq -r '.result.isError // empty' 2>/dev/null)
    if [[ "$is_error_content" == "true" ]]; then
      test_pass "$id" "$desc — tool returned isError=true"
    else
      test_fail "$id" "$desc" "expected validation error, got success: $(echo "$result" | jq -c .)"
    fi
  fi
}
tool07
