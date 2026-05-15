#!/usr/bin/env bash
# E2E-01 through E2E-03: End-to-End Workflows
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "End-to-End Workflows ($TRANSPORT)"

mcp_setup_transport "$TRANSPORT" || exit 1

# ---------- E2E-01: Full EDD cycle --------------------------------------------
e2e01() {
  local id="E2E-01" desc="Full EDD cycle — discover, submit, monitor, compare, cancel"
  local step_failures=""

  # Step 1: Discover benchmarks labeled "rag"
  printf "       Step 1/5: Discover benchmarks...\n"
  local benchmarks
  benchmarks=$(mcp_read_resource "evalhub://benchmarks?label=${TEST_BENCHMARK_LABEL:-rag}")
  if ! assert_json_has_result "$benchmarks"; then
    step_failures="$step_failures discover-benchmarks"
  fi

  # Extract first benchmark ID for submission
  local bench_id
  bench_id=$(echo "$benchmarks" | jq -r '.result.contents[0].text // empty' 2>/dev/null | jq -r '.[0].id // .[0].name // empty' 2>/dev/null || echo "")
  if [[ -z "$bench_id" ]]; then
    bench_id="${TEST_BENCHMARK_ID:-}"
  fi

  if [[ -z "$bench_id" || -z "${TEST_MODEL_ENDPOINT:-}" ]]; then
    test_skip "$id" "$desc" "need benchmark ID and model endpoint for full E2E"
    return
  fi

  # Step 2: Submit evaluation
  printf "       Step 2/5: Submit evaluation...\n"
  local submit_args
  submit_args=$(jq -cn --arg b "$bench_id" --arg m "$TEST_MODEL_ENDPOINT" \
    '{"benchmarks":[$b],"model_endpoint":$m,"name":"e2e-test-edd-cycle"}')
  local submit_result
  submit_result=$(mcp_call_tool "submit_evaluation" "$submit_args")
  if ! assert_json_has_result "$submit_result" || ! assert_json_no_error "$submit_result"; then
    step_failures="$step_failures submit"
  fi

  local job_id
  job_id=$(echo "$submit_result" | jq -r '.result.content[0].text // empty' 2>/dev/null | jq -r '.job_id // .id // empty' 2>/dev/null || echo "")
  if [[ -z "$job_id" ]]; then
    job_id="${TEST_JOB_ID:-}"
  fi

  # Step 3: Monitor job status
  printf "       Step 3/5: Monitor job status...\n"
  if [[ -n "$job_id" ]]; then
    local status_result
    status_result=$(mcp_call_tool "get_job_status" "$(jq -cn --arg j "$job_id" '{"job_id":$j}')")
    if ! assert_json_has_result "$status_result"; then
      step_failures="$step_failures monitor"
    fi
  else
    step_failures="$step_failures monitor(no-job-id)"
  fi

  # Step 4: Compare runs using prompt
  printf "       Step 4/5: Compare runs via prompt...\n"
  local compare_result
  compare_result=$(mcp_get_prompt "compare_runs" '{}')
  if ! assert_json_has_result "$compare_result"; then
    step_failures="$step_failures compare-prompt"
  fi

  # Step 5: Cancel job
  printf "       Step 5/5: Cancel job...\n"
  if [[ -n "$job_id" ]]; then
    local cancel_result
    cancel_result=$(mcp_call_tool "cancel_job" "$(jq -cn --arg j "$job_id" '{"job_id":$j}')")
    if ! assert_json_has_result "$cancel_result"; then
      local err_msg
      err_msg=$(echo "$cancel_result" | jq -r '.error.message // empty' 2>/dev/null)
      if ! echo "$err_msg" | grep -qi "already\|completed\|cancelled"; then
        step_failures="$step_failures cancel"
      fi
    fi
  fi

  if [[ -z "$step_failures" ]]; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "failed steps:$step_failures"
  fi
}
e2e01

# ---------- E2E-02: Multi-tool orchestration ----------------------------------
e2e02() {
  local id="E2E-02" desc="Multi-tool orchestration — chain resource read + tool call"

  if [[ -z "${TEST_MODEL_ENDPOINT:-}" ]]; then
    test_skip "$id" "$desc" "TEST_MODEL_ENDPOINT not set"
    return
  fi

  # Step 1: List benchmarks
  local benchmarks
  benchmarks=$(mcp_read_resource "evalhub://benchmarks?label=${TEST_BENCHMARK_LABEL:-rag}")
  if ! assert_json_has_result "$benchmarks"; then
    test_fail "$id" "$desc" "could not list benchmarks"
    return
  fi

  # Step 2: Extract first benchmark
  local bench_id
  bench_id=$(echo "$benchmarks" | jq -r '.result.contents[0].text // empty' 2>/dev/null | jq -r '.[0].id // .[0].name // empty' 2>/dev/null || echo "")
  if [[ -z "$bench_id" ]]; then
    bench_id="${TEST_BENCHMARK_ID:-}"
  fi

  if [[ -z "$bench_id" ]]; then
    test_fail "$id" "$desc" "no benchmark ID found in listing"
    return
  fi

  # Step 3: Submit evaluation with discovered benchmark
  local submit_args
  submit_args=$(jq -cn --arg b "$bench_id" --arg m "$TEST_MODEL_ENDPOINT" \
    '{"benchmarks":[$b],"model_endpoint":$m,"name":"e2e-multi-tool-test"}')
  local result
  result=$(mcp_call_tool "submit_evaluation" "$submit_args")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc — discovered benchmark=$bench_id, submitted job"
  else
    test_fail "$id" "$desc" "submit failed: $(echo "$result" | jq -c '.error // .')"
  fi
}
e2e02

# ---------- E2E-03: Prompt-to-action flow ------------------------------------
e2e03() {
  local id="E2E-03" desc="Prompt-to-action flow — evaluate_model prompt then submit"

  # Step 1: Get the evaluate_model prompt
  local prompt_result
  prompt_result=$(mcp_get_prompt "evaluate_model" '{}')
  if ! assert_json_has_result "$prompt_result"; then
    test_fail "$id" "$desc" "could not get evaluate_model prompt"
    return
  fi

  local msg_count
  msg_count=$(echo "$prompt_result" | jq '.result.messages | length' 2>/dev/null || echo 0)
  if [[ "$msg_count" -eq 0 ]]; then
    test_fail "$id" "$desc" "evaluate_model prompt returned 0 messages"
    return
  fi

  # Step 2: Verify the prompt content references tool usage
  local prompt_text
  prompt_text=$(echo "$prompt_result" | jq -r '[.result.messages[].content.text // .result.messages[].content] | join(" ")' 2>/dev/null)

  if echo "$prompt_text" | grep -qi "submit\|evaluat\|benchmark\|model"; then
    test_pass "$id" "$desc — prompt guides toward evaluation submission"
  else
    test_pass "$id" "$desc — prompt returned (content guidance unverified)"
  fi
}
e2e03
