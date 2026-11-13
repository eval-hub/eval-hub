#!/usr/bin/env bash

# Exercise the temporary collection-generation API fixture on an already
# deployed EvalHub kind cluster. This script does not build or deploy images.
set -Eeuo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${REPO_ROOT}/scripts/lib/dev-runtime.sh"

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-evalhub-dev}"
CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-}"
EVALHUB_HOST_PORT="${EVALHUB_HOST_PORT:-18443}"
KUBECONFIG_FILE="${EVALHUB_KUBECONFIG_FILE:-${XDG_CONFIG_HOME:-${HOME}/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.yaml}"
RUNTIME_STATE_FILE="${XDG_CONFIG_HOME:-${HOME}/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.runtime"
API_BASE_URL="${EVALHUB_API_BASE_URL:-https://127.0.0.1:${EVALHUB_HOST_PORT}}"
API_BASE_URL="${API_BASE_URL%/}"
NAMESPACE=system
SERVICE_ACCOUNT=evalhub-local-service
WORK_DIR=""
CURL_CONFIG=""

log() {
  printf '[%s] [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "$1" "$2"
}

fail() {
  log FAIL "$1" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: ./examples/generation/demo.sh [--help]

Exercises the temporary collection-generation mock API against an already
deployed EvalHub kind cluster. It checks the documented 503 response when the
mock switch is absent, submits a generation with a caller-supplied model,
polls its Location, and checks delete/unknown-ID behavior. It creates no
collection or evaluation job and does not build, push, or deploy images.

Inputs (also accepted as environment variables):
  KIND_CLUSTER_NAME             kind cluster (default: evalhub-dev)
  EVALHUB_HOST_PORT             localhost NodePort (default: 18443)
  EVALHUB_API_BASE_URL          override (HTTPS localhost only)
  EVALHUB_KUBECONFIG_FILE       private dev-deploy kubeconfig override
  CONTAINER_RUNTIME             podman or docker; defaults to the recorded
                                runtime/cluster owner
  API_TOKEN                     optional authorized token; otherwise a
                                short-lived token is minted for
                                system/evalhub-local-service

The API is accessed through the kind localhost NodePort, not port-forwarding.
The token is never printed and is removed from the environment after curl's
private temporary config is written. The service/API branch and compatible
operator endpoint/RBAC mapping must already be deployed.
USAGE
}

cleanup() {
  local status=$?
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf -- "$WORK_DIR"
  fi
  exit "$status"
}

need_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"
}

validate_inputs() {
  [[ "$KIND_CLUSTER_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail "Invalid kind cluster name: $KIND_CLUSTER_NAME"
  ((${#KIND_CLUSTER_NAME} <= 63)) || fail "KIND_CLUSTER_NAME must be at most 63 characters."
  [[ "$EVALHUB_HOST_PORT" =~ ^[0-9]{1,5}$ ]] &&
    ((10#$EVALHUB_HOST_PORT >= 1 && 10#$EVALHUB_HOST_PORT <= 65535)) ||
    fail "EVALHUB_HOST_PORT must be a TCP port from 1 to 65535."
  [[ "$API_BASE_URL" =~ ^https://(127\.0\.0\.1|localhost)(:[0-9]{1,5})?$ ]] ||
    fail "EVALHUB_API_BASE_URL must be an HTTPS localhost URL, for example https://127.0.0.1:18443."
}

# This is the read-only runtime policy used by dev-demo: follow a valid recorded
# owner, otherwise query the engine that actually lists this cluster.
detect_container_runtime() {
  resolve_runtime_decision query
  case "$RESOLVED_EVIDENCE" in
    explicit-invalid) fail "CONTAINER_RUNTIME must be 'podman' or 'docker'." ;;
    explicit-missing)
      need_command "$RESOLVED_RUNTIME"
      CONTAINER_RUNTIME="$RESOLVED_RUNTIME"
      ;;
    explicit-mismatch)
      log WARN "Kind cluster '$KIND_CLUSTER_NAME' was deployed with $RESOLVED_RECORDED; using the recorded runtime."
      CONTAINER_RUNTIME="$RESOLVED_RECORDED"
      ;;
    explicit-mismatch-stale|explicit-mismatch-gone)
      log WARN "The explicit runtime does not own kind cluster '$KIND_CLUSTER_NAME'; using '$RESOLVED_RUNTIME' for the read-only query."
      CONTAINER_RUNTIME="$RESOLVED_RUNTIME"
      ;;
    explicit-recorded-stale|explicit-other-owns)
      log WARN "The explicit runtime does not own kind cluster '$KIND_CLUSTER_NAME'; using its owner '$RESOLVED_OWNER' for the read-only query."
      CONTAINER_RUNTIME="$RESOLVED_OWNER"
      ;;
    recorded-stale)
      log WARN "The recorded runtime no longer lists kind cluster '$KIND_CLUSTER_NAME'; using its current owner '$RESOLVED_OWNER'."
      CONTAINER_RUNTIME="$RESOLVED_OWNER"
      ;;
    recorded-gone) CONTAINER_RUNTIME="$(canonical_runtime docker)" ;;
    owner|explicit-owned|explicit-no-owner|explicit-recorded-unverified|recorded-owned|recorded-stopped)
      CONTAINER_RUNTIME="$RESOLVED_RUNTIME"
      ;;
    no-owner) CONTAINER_RUNTIME="$(canonical_runtime docker)" ;;
    *) fail "Unhandled runtime decision: $RESOLVED_EVIDENCE" ;;
  esac
  log INFO "Using $CONTAINER_RUNTIME for kind queries."
}

check_cluster() {
  local clusters context
  clusters="$(kind_cmd get clusters 2>/dev/null)" || fail "Could not list kind clusters."
  printf '%s\n' "$clusters" | grep -Fqx -- "$KIND_CLUSTER_NAME" ||
    fail "Kind cluster '$KIND_CLUSTER_NAME' was not found. Run make dev-deploy first."
  [[ -f "$KUBECONFIG_FILE" ]] || fail "Private kubeconfig not found: $KUBECONFIG_FILE"
  context="$(kubectl --kubeconfig "$KUBECONFIG_FILE" config current-context 2>/dev/null)" ||
    fail "Could not read the current context from $KUBECONFIG_FILE"
  [[ "$context" == "kind-$KIND_CLUSTER_NAME" ]] ||
    fail "Kubeconfig context '$context' does not target kind-$KIND_CLUSTER_NAME."
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" rollout status \
    deployment/evalhub-local --timeout=180s >/dev/null ||
    fail "EvalHub is not Ready in namespace '$NAMESPACE'. Run make dev-deploy first."
}

check_api_health() {
  local status
  status="$(curl --silent --show-error --insecure --connect-timeout 3 --max-time 15 \
    --output "$WORK_DIR/health.json" --write-out '%{http_code}' \
    "$API_BASE_URL/api/v1/health")" || fail "Could not connect to EvalHub at $API_BASE_URL."
  [[ "$status" == 200 ]] || fail "EvalHub health endpoint returned HTTP $status."
  log PASS "EvalHub health endpoint returned HTTP 200 through the localhost NodePort."
}

get_api_token() {
  if [[ -z "${API_TOKEN:-}" ]]; then
    log INFO "Minting a short-lived API token for $NAMESPACE/$SERVICE_ACCOUNT."
    API_TOKEN="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" \
      create token "$SERVICE_ACCOUNT" --duration=1h 2>/dev/null)" ||
      fail "Could not mint the API token. Set API_TOKEN to a token authorized for EvalHub APIs."
  fi
  [[ "$API_TOKEN" =~ ^[-A-Za-z0-9._~+/:=]+$ ]] ||
    fail "API_TOKEN contains characters unsupported by this local demo script."
  CURL_CONFIG="$WORK_DIR/curl.conf"
  {
    printf 'header = "Authorization: Bearer %s"\n' "$API_TOKEN"
    printf 'header = "X-Tenant: system"\n'
    printf 'header = "Content-Type: application/json"\n'
  } >"$CURL_CONFIG"
  chmod 600 "$CURL_CONFIG"
  unset API_TOKEN
}

api_request() {
  local method="$1" path="$2" expected="$3" response_file="$4" body_file="${5:-}" status
  local -a args=(
    --silent --show-error --insecure --connect-timeout 5 --max-time 30
    --config "$CURL_CONFIG" --request "$method"
    --dump-header "$WORK_DIR/headers" --output "$response_file" --write-out '%{http_code}'
  )
  if [[ -n "$body_file" ]]; then
    args+=(--data-binary "@$body_file")
  fi
  status="$(curl "${args[@]}" "$API_BASE_URL$path")" ||
    fail "Request $method $path could not reach EvalHub."
  if [[ "$status" != "$expected" ]]; then
    log FAIL "Request $method $path returned HTTP $status; expected $expected." >&2
    if [[ "$status" == 403 ]]; then
      log INFO "The operator must authorize and route the generation API paths for this service image." >&2
    fi
    if [[ -s "$response_file" ]]; then
      cat "$response_file" >&2
    fi
    exit 1
  fi
}

check_not_configured_response() {
  api_request GET "/api/v1/evaluations/collections/generation/550e8400-e29b-41d4-a716-446655440099" 503 "$WORK_DIR/not-configured.json"
  jq -e '.message_code == "collection_generation_not_configured"' "$WORK_DIR/not-configured.json" >/dev/null ||
    fail "No-mock response did not match the documented 503 error."
  log PASS "Omitting mock returns the documented 503 collection_generation_not_configured response."
}

create_generation() {
  jq -n '{evaluation_goal:"Assess safe tool use and reasoning",model:{name:"fixture-generator",url:"https://generator.example/v1"},max_benchmarks:2,strictness:"moderate",generation_effort:"standard"}' \
    >"$WORK_DIR/generation-request.json"
  api_request POST '/api/v1/evaluations/collections/generation?mock=true' 202 \
    "$WORK_DIR/generation-create.json" "$WORK_DIR/generation-request.json"
  jq -e '.resource.id != null and .request.evaluation_goal == "Assess safe tool use and reasoning" and .request.model.name == "fixture-generator" and .request.model.url == "https://generator.example/v1" and .request.max_benchmarks == 2 and .request.strictness == "moderate" and .request.generation_effort == "standard" and .status.state == "pending" and has("result") == false' \
    "$WORK_DIR/generation-create.json" >/dev/null || fail "POST response did not match the pending CollectionGenerationResource contract."
  local location
  location="$(awk 'tolower($1) == "location:" { sub(/\r$/, "", $2); print $2; exit }' "$WORK_DIR/headers")"
  [[ "$location" == /api/v1/evaluations/collections/generation/*\?mock=true ]] ||
    fail "POST response Location is missing or is not a mock polling URL."
  log PASS "POST returned HTTP 202 with normalized request, pending state, and a Location polling URL."
  printf '%s\n' "$location" >"$WORK_DIR/location"
}

poll_generation() {
  local location
  location="$(<"$WORK_DIR/location")"
  api_request GET "$location" 200 "$WORK_DIR/generation-get.json"
  jq -e '
    .resource.id != null and .request.model.name == "fixture-generator" and .request.model.url == "https://generator.example/v1" and
    .request.generation_effort == "standard" and .status.state == "completed" and
    (.result.collection.name | length > 0) and (.result.collection.benchmarks | length > 0) and
    (.result.yaml | length > 0) and (.result.rationale | length > 0) and
    (.result.explanation.selected_benchmarks | length) == (.result.collection.benchmarks | length) and
    ([.result.collection.benchmarks[] | [.provider_id,.id]] == [.result.explanation.selected_benchmarks[] | [.provider_id,.id]]) and
    (.result.explanation.rejected_benchmarks | type) == "array" and (.result.actions | type) == "array" and
    (.status | has("progress") | not) and (.status | has("error") | not)
  ' "$WORK_DIR/generation-get.json" >/dev/null ||
    fail "GET response did not match the completed CollectionGenerationResource contract."
  log PASS "Location polling returned the completed proposal and aligned explanations."
  jq '{id:.resource.id,state:.status.state,collection:.result.collection.name,benchmarks:[.result.collection.benchmarks[] | {provider_id,id}]}' \
    "$WORK_DIR/generation-get.json"
}

check_delete_and_not_found() {
  local id path
  id="$(jq -er '.resource.id' "$WORK_DIR/generation-create.json")" || fail "POST response omitted resource.id."
  path="/api/v1/evaluations/collections/generation/$id"
  api_request DELETE "$path?mock=true" 204 "$WORK_DIR/delete.json"
  [[ ! -s "$WORK_DIR/delete.json" ]] || fail "DELETE 204 response unexpectedly contained a body."
  api_request GET "$path?mock=true" 200 "$WORK_DIR/after-delete.json"
  api_request DELETE "$path?mock=true" 204 "$WORK_DIR/delete-again.json"
  api_request GET '/api/v1/evaluations/collections/generation/not-a-fixture?mock=true' 404 "$WORK_DIR/not-found.json"
  log PASS "DELETE is the documented fixture no-op; unknown IDs return 404."
}

main() {
  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    return 0
  fi
  (($# == 0)) || fail "Unexpected argument: $1. Run ./examples/generation/demo.sh --help for supported inputs."
  validate_inputs
  for tool in kind kubectl curl jq grep awk date chmod mktemp rm; do
    need_command "$tool"
  done
  detect_container_runtime
  WORK_DIR="$(mktemp -d /tmp/evalhub-generation-demo.XXXXXX)"
  trap cleanup EXIT
  log INFO "Using kind cluster '$KIND_CLUSTER_NAME' and kubeconfig '$KUBECONFIG_FILE'."
  check_cluster
  check_api_health
  get_api_token
  check_not_configured_response
  create_generation
  poll_generation
  check_delete_and_not_found
  log PASS "Collection-generation API demo completed. No collection or evaluation job was created."
}

main "$@"
