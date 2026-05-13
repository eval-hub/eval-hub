#!/usr/bin/env bash
# Lightweight test framework with TAP-like output and JUnit XML report.

set -euo pipefail

_TESTS_RUN=0
_TESTS_PASSED=0
_TESTS_FAILED=0
_TESTS_SKIPPED=0
_CURRENT_SUITE=""
_JUNIT_ENTRIES=""
_REPORT_FILE=""
_FAIL_FAST="${FAIL_FAST:-false}"

COLOR_GREEN="\033[0;32m"
COLOR_RED="\033[0;31m"
COLOR_YELLOW="\033[0;33m"
COLOR_CYAN="\033[0;36m"
COLOR_RESET="\033[0m"

test_suite() {
  _CURRENT_SUITE="$1"
  printf "\n${COLOR_CYAN}=== %s ===${COLOR_RESET}\n" "$_CURRENT_SUITE"
}

test_pass() {
  local id="$1" desc="$2"
  _TESTS_RUN=$((_TESTS_RUN + 1))
  _TESTS_PASSED=$((_TESTS_PASSED + 1))
  printf "${COLOR_GREEN}  PASS${COLOR_RESET} [%s] %s\n" "$id" "$desc"
  _junit_add "$id" "$desc" "pass" ""
}

test_fail() {
  local id="$1" desc="$2" reason="${3:-}"
  _TESTS_RUN=$((_TESTS_RUN + 1))
  _TESTS_FAILED=$((_TESTS_FAILED + 1))
  printf "${COLOR_RED}  FAIL${COLOR_RESET} [%s] %s\n" "$id" "$desc"
  [[ -n "$reason" ]] && printf "       Reason: %s\n" "$reason"
  _junit_add "$id" "$desc" "fail" "$reason"
  if [[ "$_FAIL_FAST" == "true" ]]; then
    printf "${COLOR_RED}FAIL_FAST enabled — aborting.${COLOR_RESET}\n"
    test_report
    exit 1
  fi
}

test_skip() {
  local id="$1" desc="$2" reason="${3:-not applicable}"
  _TESTS_RUN=$((_TESTS_RUN + 1))
  _TESTS_SKIPPED=$((_TESTS_SKIPPED + 1))
  printf "${COLOR_YELLOW}  SKIP${COLOR_RESET} [%s] %s (%s)\n" "$id" "$desc" "$reason"
  _junit_add "$id" "$desc" "skip" "$reason"
}

# --- Assertions ----------------------------------------------------------------

assert_not_empty() {
  local value="$1" msg="${2:-value should not be empty}"
  [[ -n "$value" && "$value" != "null" ]]
}

assert_json_has_key() {
  local json="$1" key="$2"
  echo "$json" | jq -e ".$key" >/dev/null 2>&1
}

assert_json_has_result() {
  local json="$1"
  echo "$json" | jq -e '.result' >/dev/null 2>&1
}

assert_json_no_error() {
  local json="$1"
  ! echo "$json" | jq -e '.error' >/dev/null 2>&1
}

assert_json_has_error() {
  local json="$1"
  echo "$json" | jq -e '.error' >/dev/null 2>&1
}

assert_json_array_not_empty() {
  local json="$1" path="$2"
  local len
  len=$(echo "$json" | jq -r "$path | length" 2>/dev/null || echo 0)
  [[ "$len" -gt 0 ]]
}

assert_json_array_contains() {
  local json="$1" path="$2" field="$3" value="$4"
  echo "$json" | jq -e "$path | map(select(.$field == \"$value\")) | length > 0" >/dev/null 2>&1
}

assert_json_field_equals() {
  local json="$1" path="$2" expected="$3"
  local actual
  actual=$(echo "$json" | jq -r "$path" 2>/dev/null)
  [[ "$actual" == "$expected" ]]
}

assert_process_running() {
  local pid="$1"
  kill -0 "$pid" 2>/dev/null
}

assert_port_open() {
  local host="$1" port="$2"
  timeout 2 bash -c "echo >/dev/tcp/$host/$port" 2>/dev/null
}

# --- Reporting -----------------------------------------------------------------

_junit_add() {
  local id="$1" desc="$2" status="$3" message="$4"
  local xml_desc
  xml_desc=$(echo "$desc" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g')
  local xml_msg
  xml_msg=$(echo "$message" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g; s/"/\&quot;/g')

  local entry="    <testcase classname=\"${_CURRENT_SUITE}\" name=\"[${id}] ${xml_desc}\">"
  case "$status" in
    fail) entry="$entry<failure message=\"${xml_msg}\"/>" ;;
    skip) entry="$entry<skipped message=\"${xml_msg}\"/>" ;;
  esac
  entry="$entry</testcase>"
  _JUNIT_ENTRIES="${_JUNIT_ENTRIES}\n${entry}"
}

test_report() {
  local report_dir
  report_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/reports"
  mkdir -p "$report_dir"
  _REPORT_FILE="${report_dir}/test-results-$(date +%Y%m%dT%H%M%S).xml"

  cat > "$_REPORT_FILE" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<testsuites tests="${_TESTS_RUN}" failures="${_TESTS_FAILED}" skipped="${_TESTS_SKIPPED}">
  <testsuite name="evalhub-mcp-integration" tests="${_TESTS_RUN}" failures="${_TESTS_FAILED}" skipped="${_TESTS_SKIPPED}">
$(echo -e "$_JUNIT_ENTRIES")
  </testsuite>
</testsuites>
EOF

  printf "\n${COLOR_CYAN}=== Test Summary ===${COLOR_RESET}\n"
  printf "  Total:   %d\n" "$_TESTS_RUN"
  printf "  ${COLOR_GREEN}Passed:  %d${COLOR_RESET}\n" "$_TESTS_PASSED"
  printf "  ${COLOR_RED}Failed:  %d${COLOR_RESET}\n" "$_TESTS_FAILED"
  printf "  ${COLOR_YELLOW}Skipped: %d${COLOR_RESET}\n" "$_TESTS_SKIPPED"
  printf "  Report:  %s\n" "$_REPORT_FILE"

  if [[ $_TESTS_FAILED -gt 0 ]]; then
    return 1
  fi
  return 0
}
