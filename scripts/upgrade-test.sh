#!/bin/bash
set -euo pipefail

if [[ -f .env ]]; then
    set -a
    source .env
    set +a
fi

REPORT_DIR="${REPORT_DIR:-test-reports}"
UPGRADE_STATE_JSON="${UPGRADE_STATE_JSON:-${REPORT_DIR}/upgrade-state.json}"

REQUIRED_VARS=(SERVER_URL AUTH_TOKEN X_TENANT MODEL_URL MODEL_NAME)

check_required_vars() {
    local missing=()
    for var in "${REQUIRED_VARS[@]}"; do
        if [[ -z "${!var:-}" ]]; then
            missing+=("$var")
        fi
    done
    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: required environment variables are not set: ${missing[*]}" >&2
        exit 1
    fi
}

usage() {
    cat <<EOF
Usage: $(basename "$0") [PHASE...]

Run upgrade test phases via make targets. When called with no arguments,
runs all four phases in order (equivalent to 'make run-atris-upgrade').

Phases:
  pre-upgrade             Create evaluation jobs on the source version
  post-upgrade-verify     Verify resources survived the upgrade
  post-upgrade            Run new evaluations on the target version
  post-upgrade-cleanup    Clean up all test resources
  all                     Run all phases in order (default)

Environment variables (required):
  SERVER_URL              EvalHub API base URL
  AUTH_TOKEN              Bearer token for authentication
  X_TENANT                Tenant namespace
  MODEL_URL               Model inference endpoint URL
  MODEL_NAME              Model name

Environment variables (optional):
  MODEL_AUTH_SECRET_REF   Secret reference for model auth (omits model.auth if unset)
  REPORT_DIR              Report output directory (default: test-reports)
  UPGRADE_STATE_JSON      State file path (default: \$REPORT_DIR/upgrade-state.json)
  X_USER                  User identity header (default: upgrade-test-user)

Examples:
  $(basename "$0")                          # run all phases
  $(basename "$0") pre-upgrade              # run only pre-upgrade
  $(basename "$0") post-upgrade-verify post-upgrade  # run two phases
EOF
}

run_phase() {
    local phase="$1"
    local junit_xml="${REPORT_DIR}/${phase}.xml"

    echo "==> Running phase: ${phase}"
    JUNIT_XML="${junit_xml}" \
    UPGRADE_STATE_JSON="${UPGRADE_STATE_JSON}" \
        make "run-${phase}"
    echo ""
}

ALL_PHASES=(pre-upgrade post-upgrade-verify post-upgrade post-upgrade-cleanup)

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

check_required_vars

phases=("$@")
if [[ ${#phases[@]} -eq 0 || ( ${#phases[@]} -eq 1 && "${phases[0]}" == "all" ) ]]; then
    phases=("${ALL_PHASES[@]}")
fi

is_valid_phase() {
    local needle="$1"
    for p in "${ALL_PHASES[@]}"; do
        [[ "$p" == "$needle" ]] && return 0
    done
    return 1
}

for phase in "${phases[@]}"; do
    if ! is_valid_phase "${phase}"; then
        echo "ERROR: unknown phase '${phase}'" >&2
        usage >&2
        exit 1
    fi
done

cleanup_pending=false
for phase in "${phases[@]}"; do
    if [[ "$phase" == "post-upgrade-cleanup" ]]; then
        cleanup_pending=true
        break
    fi
done

run_cleanup_on_exit() {
    local exit_status=$?
    if [[ "$cleanup_pending" == true ]]; then
        cleanup_pending=false
        echo "==> Running post-upgrade-cleanup (exit trap)"
        run_phase "post-upgrade-cleanup" || true
    fi
    exit "$exit_status"
}

if [[ "$cleanup_pending" == true ]]; then
    trap run_cleanup_on_exit EXIT
fi

for phase in "${phases[@]}"; do
    if [[ "$phase" == "post-upgrade-cleanup" ]]; then
        continue
    fi
    run_phase "${phase}"
done

if [[ "$cleanup_pending" == true ]]; then
    cleanup_pending=false
    run_phase "post-upgrade-cleanup"
fi

echo "==> All requested phases complete"
echo "    Reports: ${REPORT_DIR}/"
echo "    State:   ${UPGRADE_STATE_JSON}"
