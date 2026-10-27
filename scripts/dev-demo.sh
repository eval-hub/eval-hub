#!/usr/bin/env bash

# Exercise the EvalHub API on an already deployed Docker-backed kind cluster.
# This creates a demo collection, a completed evaluation job, and a mock-auth Secret.
set -Eeuo pipefail
umask 077

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-evalhub-dev}"
EVALHUB_HOST_PORT="${EVALHUB_HOST_PORT:-18443}"
DEMO_SOURCE_COLLECTION_ID="${DEMO_SOURCE_COLLECTION_ID:-instruction-output-v1}"
DEMO_JOB_TIMEOUT_SECONDS="${DEMO_JOB_TIMEOUT_SECONDS:-900}"
KUBECONFIG_FILE="${EVALHUB_KUBECONFIG_FILE:-${XDG_CONFIG_HOME:-${HOME}/.config}/evalhub/kubeconfigs/${KIND_CLUSTER_NAME}.yaml}"
API_BASE_URL="${EVALHUB_API_BASE_URL:-https://127.0.0.1:${EVALHUB_HOST_PORT}}"
API_BASE_URL="${API_BASE_URL%/}"
NAMESPACE=system
SERVICE_ACCOUNT=evalhub-local-service
DEMO_SECRET=evalhub-kind-demo-auth
OWNERSHIP_ANNOTATION=io.evalhub.kind-smoke.cluster
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
Usage: make dev-demo [VARIABLE=value ...]

Runs a small API workflow against an already deployed EvalHub kind cluster:
  1. Lists system collections and recent jobs.
  2. Clones DEMO_SOURCE_COLLECTION_ID with only one FollowBench example.
  3. Submits the cloned collection to the local mock target and judge, then polls
     until the evaluation reaches a terminal state.

The created collection and job remain available for inspection. The script also
creates a fake-credential Secret for the mock services; make dev-clean removes it.

Inputs (also accepted as environment variables):
  KIND_CLUSTER_NAME             kind cluster (default: evalhub-dev)
  EVALHUB_HOST_PORT             local API port (default: 18443)
  EVALHUB_API_BASE_URL          API URL override (default: https://127.0.0.1:<port>)
  EVALHUB_KUBECONFIG_FILE       kubeconfig override (default: private dev-deploy kubeconfig)
  DEMO_SOURCE_COLLECTION_ID     source system collection (default: instruction-output-v1)
  DEMO_JOB_TIMEOUT_SECONDS      job polling timeout (default: 900)
  API_TOKEN                     optional caller token; otherwise a short-lived
                                token is minted for evalhub-local-service.

A supplied API_TOKEN must be authorized for collection and evaluation APIs.
The token is never printed. The API uses the local kind cluster's self-signed TLS
certificate, which this script accepts for localhost requests only.
USAGE
}

cleanup() {
  local status=$?
  trap - EXIT
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
  [[ "$DEMO_JOB_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] &&
    ((DEMO_JOB_TIMEOUT_SECONDS > 0)) ||
    fail "DEMO_JOB_TIMEOUT_SECONDS must be a positive integer."
  [[ -n "$DEMO_SOURCE_COLLECTION_ID" ]] || fail "DEMO_SOURCE_COLLECTION_ID must not be empty."
  [[ "$API_BASE_URL" =~ ^https://(127\.0\.0\.1|localhost)(:[0-9]{1,5})?$ ]] ||
    fail "EVALHUB_API_BASE_URL must be an HTTPS localhost URL, for example https://127.0.0.1:18443."
}

kind_cmd() {
  env -u KIND_EXPERIMENTAL_PROVIDER kind "$@"
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
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" rollout status \
    deployment/evalhub-kind-mock-target --timeout=180s >/dev/null ||
    fail "The mock target is not Ready. Run make dev-deploy first."
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" rollout status \
    deployment/evalhub-kind-mock-judge --timeout=180s >/dev/null ||
    fail "The mock judge is not Ready. Run make dev-deploy first."
}

check_api_health() {
  local status
  status="$(curl --silent --show-error --insecure --connect-timeout 3 --max-time 15 \
    --output "$WORK_DIR/health.json" --write-out '%{http_code}' \
    "$API_BASE_URL/api/v1/health")" || fail "Could not connect to EvalHub at $API_BASE_URL."
  [[ "$status" == 200 ]] || fail "EvalHub health endpoint returned HTTP $status."
  log PASS "EvalHub health endpoint is reachable at $API_BASE_URL."
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
  local method="$1" path="$2" response_file="$3" body_file="${4:-}" status
  local -a args=(
    --silent --show-error --insecure --connect-timeout 5 --max-time 30
    --config "$CURL_CONFIG" --request "$method"
    --output "$response_file" --write-out '%{http_code}'
  )
  if [[ -n "$body_file" ]]; then
    args+=(--data-binary "@$body_file")
  fi
  status="$(curl "${args[@]}" "$API_BASE_URL$path")" ||
    fail "Request $method $path could not reach EvalHub."
  if [[ ! "$status" =~ ^2[0-9]{2}$ ]]; then
    log FAIL "Request $method $path returned HTTP $status. Response:" >&2
    cat "$response_file" >&2
    exit 1
  fi
}

ensure_mock_secret() {
  local existing owner
  existing="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" \
    get secret "$DEMO_SECRET" --ignore-not-found -o name 2>/dev/null)" ||
    fail "Could not check for the demo mock-auth Secret."
  if [[ -n "$existing" ]]; then
    owner="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" \
      get secret "$DEMO_SECRET" \
      -o "go-template={{if .metadata.annotations}}{{index .metadata.annotations \"$OWNERSHIP_ANNOTATION\"}}{{end}}")" ||
      fail "Could not read ownership of Secret/$DEMO_SECRET."
    [[ "$owner" == "$KIND_CLUSTER_NAME" ]] ||
      fail "Secret/$DEMO_SECRET exists but is not owned by dev-deploy for cluster '$KIND_CLUSTER_NAME'; refusing to overwrite it."
  fi
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" apply -f - >/dev/null <<YAML
apiVersion: v1
kind: Secret
metadata:
  name: $DEMO_SECRET
  namespace: $NAMESPACE
  annotations:
    $OWNERSHIP_ANNOTATION: $KIND_CLUSTER_NAME
type: Opaque
stringData:
  api-key: target-test-token
  judge_api-key: judge-test-token
  judge_url: http://evalhub-kind-mock-judge.system.svc.cluster.local:8000/v1
YAML
  log PASS "Created or updated the owned fake-credential Secret for the mock models."
}

list_objects() {
  local collections_file="$WORK_DIR/collections.json" jobs_file="$WORK_DIR/jobs.json"
  api_request GET '/api/v1/evaluations/collections?scope=system&limit=100' "$collections_file"
  api_request GET '/api/v1/evaluations/jobs?limit=10' "$jobs_file"
  log INFO "System collections (ID<TAB>name):"
  jq -r '.items[]? | [(.resource.id // .id // ""), (.name // "")] | @tsv' "$collections_file"
  log INFO "Recent evaluation jobs (ID<TAB>name<TAB>state):"
  jq -r '.items[]? | [(.resource.id // .id // ""), (.name // ""), (.status.state // "unknown")] | @tsv' "$jobs_file"
  jq -e --arg id "$DEMO_SOURCE_COLLECTION_ID" \
    'any(.items[]?; (.resource.id // .id) == $id)' "$collections_file" >/dev/null ||
    fail "Source collection '$DEMO_SOURCE_COLLECTION_ID' was not found in the system collection list. Choose an ID shown above with DEMO_SOURCE_COLLECTION_ID=<id>."
}

create_demo_collection() {
  local source_id encoded_source
  DEMO_SUFFIX="$(date -u '+%Y%m%dT%H%M%SZ')-$$"
  DEMO_COLLECTION_NAME="kind-demo-$DEMO_SUFFIX"
  source_id="$DEMO_SOURCE_COLLECTION_ID"
  encoded_source="$(jq -rn --arg id "$source_id" '$id | @uri')"
  jq -n --arg name "$DEMO_COLLECTION_NAME" \
    --arg description 'Local kind API demo: one FollowBench example using mock target and judge services.' \
    '{
      name: $name,
      description: $description,
      benchmarks: [{
        id: "followbench",
        provider_id: "followbench",
        parameters: {
          judge_model: "kind-judge-model",
          num_examples: 1,
          max_tokens: 64,
          temperature: 0.0,
          request_timeout: 60
        }
      }]
    }' >"$WORK_DIR/clone.json"
  api_request POST "/api/v1/evaluations/collections/$encoded_source/clones" \
    "$WORK_DIR/clone-response.json" "$WORK_DIR/clone.json"
  DEMO_COLLECTION_ID="$(jq -er '.resource.id' "$WORK_DIR/clone-response.json")" ||
    fail "The clone response did not contain resource.id."
  log PASS "Cloned '$DEMO_SOURCE_COLLECTION_ID' as '$DEMO_COLLECTION_ID'."
}

submit_and_wait_for_job() {
  local started state elapsed
  DEMO_JOB_NAME="kind-demo-job-$DEMO_SUFFIX"
  jq -n --arg name "$DEMO_JOB_NAME" --arg collection_id "$DEMO_COLLECTION_ID" \
    '{
      name: $name,
      description: "One-example EvalHub API demo against the local kind mock models.",
      model: {
        url: "http://evalhub-kind-mock-target.system.svc.cluster.local:8000/v1",
        name: "kind-target-model",
        auth: {secret_ref: "evalhub-kind-demo-auth"}
      },
      collection: {
        id: $collection_id,
        benchmarks: [{
          id: "followbench",
          provider_id: "followbench",
          parameters: {
            judge_model: "kind-judge-model",
            num_examples: 1,
            max_tokens: 64,
            temperature: 0.0,
            request_timeout: 60
          }
        }]
      }
    }' >"$WORK_DIR/job.json"
  api_request POST '/api/v1/evaluations/jobs' \
    "$WORK_DIR/job-response.json" "$WORK_DIR/job.json"
  DEMO_JOB_ID="$(jq -er '.resource.id' "$WORK_DIR/job-response.json")" ||
    fail "The job response did not contain resource.id."
  log PASS "Submitted job '$DEMO_JOB_ID' for the cloned collection."

  started=$SECONDS
  while :; do
    api_request GET "/api/v1/evaluations/jobs/$DEMO_JOB_ID" "$WORK_DIR/job-status.json"
    state="$(jq -er '.status.state' "$WORK_DIR/job-status.json")" ||
      fail "Job '$DEMO_JOB_ID' response did not contain status.state."
    case "$state" in
      completed)
        jq '{job_id: .resource.id, name: .name, state: .status.state, benchmarks: [.results.benchmarks[]? | {id, provider_id, metrics}]}' \
          "$WORK_DIR/job-status.json"
        log PASS "Demo evaluation completed."
        return 0
        ;;
      failed|partially_failed|cancelled)
        jq '{job_id: .resource.id, name: .name, state: .status.state, message: .status.message, benchmarks: .status.benchmarks}' \
          "$WORK_DIR/job-status.json" >&2
        fail "Demo evaluation ended in state '$state'. Job ID: $DEMO_JOB_ID"
        ;;
      pending|running) ;;
      *) fail "Job '$DEMO_JOB_ID' returned unexpected state '$state'." ;;
    esac
    elapsed=$((SECONDS - started))
    ((elapsed < DEMO_JOB_TIMEOUT_SECONDS)) ||
      fail "Timed out after ${DEMO_JOB_TIMEOUT_SECONDS}s waiting for job '$DEMO_JOB_ID'; it remains available for inspection."
    log INFO "Job state is '$state'; waiting 5 seconds (${elapsed}s elapsed)."
    sleep 5
  done
}

main() {
  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    return 0
  fi
  (($# == 0)) || fail "Unexpected argument: $1. Run make dev-demo-help for supported inputs."
  validate_inputs
  for tool in kind kubectl curl jq env grep date chmod mktemp rm; do
    need_command "$tool"
  done
  WORK_DIR="$(mktemp -d /tmp/evalhub-dev-demo.XXXXXX)"
  trap cleanup EXIT
  log INFO "Using kind cluster '$KIND_CLUSTER_NAME' and kubeconfig '$KUBECONFIG_FILE'."
  check_cluster
  check_api_health
  get_api_token
  list_objects
  ensure_mock_secret
  create_demo_collection
  submit_and_wait_for_job
  log INFO "Demo collection ID: $DEMO_COLLECTION_ID"
  log INFO "Demo job ID: $DEMO_JOB_ID"
  log INFO "These API resources remain for inspection. make dev-clean removes the local deployment and mock-auth Secret."
}

main "$@"
