#!/usr/bin/env bash
# =============================================================================
# EvalHub MCP Integration Test Runner
#
# Usage:
#   ./run_tests.sh                    # Run all tests, both transports
#   ./run_tests.sh stdio              # Run all tests, stdio only
#   ./run_tests.sh http               # Run all tests, HTTP only
#   ./run_tests.sh stdio 02           # Run resources tests, stdio only
#   ./run_tests.sh http 03,06         # Run tools + errors, HTTP only
#
# Environment:
#   Copy test.env.example to test.env and fill in values before running.
#   For HTTP tests, start the server first:
#     evalhub-mcp --transport http --host localhost --port 3001
#
# Options:
#   FAIL_FAST=true ./run_tests.sh     # Stop on first failure
#   VERBOSE=true ./run_tests.sh       # Show raw JSON responses
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/common/mcp_client.sh"

# --- Load configuration ------------------------------------------------------
if [[ -f "$SCRIPT_DIR/test.env" ]]; then
  set -a
  source "$SCRIPT_DIR/test.env"
  set +a
else
  echo "ERROR: test.env not found. Copy test.env.example to test.env and fill in values."
  exit 1
fi

# --- Validate prerequisites ---------------------------------------------------
check_prereqs() {
  local missing=""
  for cmd in jq curl timeout mkfifo; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
      missing="$missing $cmd"
    fi
  done
  if [[ -n "$missing" ]]; then
    echo "ERROR: Missing required commands:$missing"
    exit 1
  fi

  if [[ -n "${EVALHUB_MCP_BIN:-}" && "${EVALHUB_MCP_BIN}" != /* ]]; then
    local repo_root
    repo_root="$(cd "$SCRIPT_DIR/../../../.." && pwd)"
    EVALHUB_MCP_BIN="${repo_root}/${EVALHUB_MCP_BIN#./}"
    export EVALHUB_MCP_BIN
  fi

  if [[ ! -x "${EVALHUB_MCP_BIN:-}" ]]; then
    echo "ERROR: EVALHUB_MCP_BIN (${EVALHUB_MCP_BIN:-not set}) is not executable."
    echo "       Set it in test.env or ensure the binary exists."
    exit 1
  fi
}

# --- Parse arguments ----------------------------------------------------------
TRANSPORTS="${1:-stdio,http}"
SUITES="${2:-01,02,03,04,05,06,07,08}"

IFS=',' read -ra TRANSPORT_LIST <<< "$TRANSPORTS"
IFS=',' read -ra SUITE_LIST <<< "$SUITES"

# --- Map suite numbers to files -----------------------------------------------
# macOS /bin/bash is 3.2 — no associative arrays (declare -A). Map with a function; use 10# when
# parsing suite ids so leading zeros (e.g. 08) are decimal, not invalid octal in arithmetic.
suite_file_for_index() {
  case "$1" in
    1) echo "01_server_discovery.sh" ;;
    2) echo "02_resources.sh" ;;
    3) echo "03_tools.sh" ;;
    4) echo "04_prompts.sh" ;;
    5) echo "05_autocompletion.sh" ;;
    6) echo "06_error_scenarios.sh" ;;
    7) echo "07_e2e_workflow.sh" ;;
    8) echo "08_client_specific.sh" ;;
    *) echo "" ;;
  esac
}

# --- Run ----------------------------------------------------------------------
main() {
  check_prereqs

  mkdir -p "$SCRIPT_DIR/reports"

  echo "============================================================"
  echo " EvalHub MCP Integration Test Suite"
  echo " Date:       $(date '+%Y-%m-%d %H:%M:%S')"
  echo " Binary:     $EVALHUB_MCP_BIN"
  echo " Backend:    ${EVALHUB_BASE_URL:-not set}"
  echo " Transports: ${TRANSPORTS}"
  echo " Suites:     ${SUITES}"
  echo "============================================================"

  local overall_exit=0

  for transport in "${TRANSPORT_LIST[@]}"; do
    transport=$(echo "$transport" | xargs)  # trim whitespace

    echo ""
    echo "############################################################"
    echo "# Transport: $transport"
    echo "############################################################"

    # For HTTP, verify the server is reachable before running tests
    if [[ "$transport" == "http" ]]; then
      if ! mcp_http_wait_ready "${EVALHUB_HTTP_HOST:-localhost}" "${EVALHUB_HTTP_PORT:-3001}"; then
        echo ""
        echo "WARNING: HTTP server not reachable at ${EVALHUB_HTTP_HOST:-localhost}:${EVALHUB_HTTP_PORT:-3001}"
        echo "         Start the server first:  ${EVALHUB_MCP_BIN} --transport http --host ${EVALHUB_HTTP_HOST:-localhost} --port ${EVALHUB_HTTP_PORT:-3001}"
        echo "         Skipping HTTP tests."
        echo ""
        continue
      fi
    fi

    for suite_num in "${SUITE_LIST[@]}"; do
      suite_num=$(echo "$suite_num" | xargs)
      local suite_file=""
      if [[ "$suite_num" =~ ^[0-9]+$ ]]; then
        local suite_idx=$((10#$suite_num))
        suite_file=$(suite_file_for_index "$suite_idx")
      fi
      if [[ -z "$suite_file" ]]; then
        echo "WARNING: Unknown suite number '$suite_num', skipping."
        continue
      fi

      local test_file="$SCRIPT_DIR/tests/$suite_file"
      if [[ ! -f "$test_file" ]]; then
        echo "WARNING: Test file $test_file not found, skipping."
        continue
      fi

      bash "$test_file" "$transport" || overall_exit=1
    done
  done

  # --- Cross-transport parity check -------------------------------------------
  if [[ -f "$SCRIPT_DIR/reports/parity_stdio.txt" && -f "$SCRIPT_DIR/reports/parity_http.txt" ]]; then
    echo ""
    echo "=== Cross-Transport Parity Check ==="
    if diff -q "$SCRIPT_DIR/reports/parity_stdio.txt" "$SCRIPT_DIR/reports/parity_http.txt" >/dev/null 2>&1; then
      echo "  PASS: stdio and HTTP payloads are identical"
    else
      echo "  WARN: stdio and HTTP payloads differ. Diff:"
      diff --unified=3 "$SCRIPT_DIR/reports/parity_stdio.txt" "$SCRIPT_DIR/reports/parity_http.txt" | head -30
      echo "  (see full diff in reports/)"
    fi
  fi

  # --- Prompt fidelity check --------------------------------------------------
  if [[ -f "$SCRIPT_DIR/reports/prompt_fidelity_stdio.json" && -f "$SCRIPT_DIR/reports/prompt_fidelity_http.json" ]]; then
    echo ""
    echo "=== Prompt Fidelity Check ==="
    if diff -q "$SCRIPT_DIR/reports/prompt_fidelity_stdio.json" "$SCRIPT_DIR/reports/prompt_fidelity_http.json" >/dev/null 2>&1; then
      echo "  PASS: Prompt content identical across transports"
    else
      echo "  WARN: Prompt content differs across transports"
    fi
  fi

  echo ""
  echo "============================================================"
  echo " Test run complete. Reports in: $SCRIPT_DIR/reports/"
  echo "============================================================"

  return $overall_exit
}

main
