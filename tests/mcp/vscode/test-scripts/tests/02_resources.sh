#!/usr/bin/env bash
# RES-01 through RES-09: MCP Resources
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../common/mcp_client.sh"
source "$SCRIPT_DIR/../common/test_framework.sh"

TRANSPORT="${1:-stdio}"

test_suite "MCP Resources ($TRANSPORT)"

# --- Helper: read a resource and validate it's non-empty ----------------------
check_resource() {
  local id="$1" desc="$2" uri="$3" array_path="${4:-.result.contents}"
  local result
  result=$(mcp_read_resource "$uri")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    if assert_json_array_not_empty "$result" "$array_path"; then
      test_pass "$id" "$desc"
    else
      test_fail "$id" "$desc" "resource returned empty contents for $uri"
    fi
  else
    test_fail "$id" "$desc" "error reading $uri: $(echo "$result" | jq -c '.error // .')"
  fi
}

mcp_setup_transport "$TRANSPORT" || exit 1

# ---------- RES-01: List all providers ----------------------------------------
check_resource "RES-01" "List all providers" "evalhub://providers"

# ---------- RES-02: List all benchmarks ---------------------------------------
check_resource "RES-02" "List all benchmarks" "evalhub://benchmarks"

# ---------- RES-03: Filter benchmarks by label --------------------------------
res03() {
  local id="RES-03" desc="Filter benchmarks by label ($TEST_BENCHMARK_LABEL)"
  local result
  result=$(mcp_read_resource "evalhub://benchmarks?label=${TEST_BENCHMARK_LABEL}")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local count
    count=$(echo "$result" | jq '.result.contents | length' 2>/dev/null || echo 0)
    if [[ "$count" -gt 0 ]]; then
      test_pass "$id" "$desc — $count benchmarks returned"
    else
      test_fail "$id" "$desc" "no benchmarks with label $TEST_BENCHMARK_LABEL"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
res03

# ---------- RES-04: List collections -----------------------------------------
check_resource "RES-04" "List evaluation collections" "evalhub://collections"

# ---------- RES-05: List jobs -------------------------------------------------
check_resource "RES-05" "List evaluation jobs" "evalhub://jobs"

# ---------- RES-06: Filter jobs by status -------------------------------------
res06() {
  local id="RES-06" desc="Filter jobs by status (running)"
  local result
  result=$(mcp_read_resource "evalhub://jobs?status=running")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
res06

# ---------- RES-08: Get server version ----------------------------------------
res08() {
  local id="RES-08" desc="Get server version resource"
  local result
  result=$(mcp_read_resource "evalhub://server/version")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    local version
    version=$(echo "$result" | jq -r '.result.contents[0].text // empty' 2>/dev/null)
    if [[ -n "$version" ]]; then
      test_pass "$id" "$desc — version=$version"
    else
      test_fail "$id" "$desc" "version resource returned but content empty"
    fi
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
res08

# ---------- RES-09: Get individual item by ID ---------------------------------
res09() {
  local id="RES-09" desc="Get individual benchmark by ID"
  if [[ -z "${TEST_BENCHMARK_ID:-}" ]]; then
    test_skip "$id" "$desc" "TEST_BENCHMARK_ID not set"
    return
  fi
  local result
  result=$(mcp_read_resource "evalhub://benchmarks/${TEST_BENCHMARK_ID}")

  if assert_json_has_result "$result" && assert_json_no_error "$result"; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "error: $(echo "$result" | jq -c '.error // .')"
  fi
}
res09

# ---------- Resource listing completeness -------------------------------------
test_suite "MCP Resources — Discovery ($TRANSPORT)"

res_discovery() {
  local id="RES-ALL" desc="resources/list returns all expected resource types"
  local result
  result=$(mcp_list_resources)

  if ! assert_json_has_result "$result"; then
    test_fail "$id" "$desc" "resources/list failed"
    return
  fi

  local uris
  uris=$(echo "$result" | jq -r '.result.resources[].uri' 2>/dev/null || echo "")
  local missing=""
  for expected in "evalhub://providers" "evalhub://benchmarks" "evalhub://collections" "evalhub://jobs" "evalhub://server/version"; do
    if ! echo "$uris" | grep -q "$expected"; then
      missing="$missing $expected"
    fi
  done

  if [[ -z "$missing" ]]; then
    test_pass "$id" "$desc"
  else
    test_fail "$id" "$desc" "missing resources:$missing"
  fi
}
res_discovery
