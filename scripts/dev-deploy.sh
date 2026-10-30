#!/usr/bin/env bash

# Build local EvalHub/operator/provider images and deploy a development stack
# to a kind cluster backed by podman or Docker. Job/API behavior checks live in tests/e2e.
set -Eeuo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EVALHUB_REPO="$(cd "${SCRIPT_DIR}/.." && pwd)"
EVALHUB_REPO_PARENT="$(dirname "$EVALHUB_REPO")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-evalhub-dev}"
IMAGE_TAG="${IMAGE_TAG:-kind-${KIND_CLUSTER_NAME}}"
SKIP_BUILD="${SKIP_BUILD:-false}"
AIRGAP="${AIRGAP:-false}"
ADAPTERS="${ADAPTERS:-all}"
CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-}"
EVALHUB_HOST_PORT="${EVALHUB_HOST_PORT:-18443}"
EVALHUB_NODE_PORT="${EVALHUB_NODE_PORT:-30443}"
EVALHUB_HOST_BIND_ADDRESS="${EVALHUB_HOST_BIND_ADDRESS:-127.0.0.1}"
DEV_REPO_PARENT_PATHS="${DEV_REPO_PARENT_PATHS:-${HOME}/code}"
CONTRIB_REPO=""
OPERATOR_REPO=""
LMES_REPO=""
SELECTED_ADAPTERS=()
SELECTED_ADAPTER_IMAGES=()
CALICO_VERSION="v3.33.0"
KUBECONFIG_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/evalhub/kubeconfigs"
KUBECONFIG_FILE="${KUBECONFIG_DIR}/${KIND_CLUSTER_NAME}.yaml"
WORK_DIR=""
REGISTRY_NAME=""
REGISTRY_ALIAS=""
REGISTRY_PORT=""
TEMP_REGISTRY_TAGS=()
PORT_FORWARD_PID=""
KIND_CLUSTER_CREATED=false
API_HOST_ACCESS_PORT="${EVALHUB_HOST_PORT}"

log() {
  printf '[%s] [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "$1" "$2"
}

fail() {
  log FAIL "$1" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: make dev-deploy [VARIABLE=value ...]

Builds and deploys EvalHub, the operator, the local adapter images, and mock
target/judge services to a kind cluster backed by podman or Docker.
All system collections packaged by the operator are discovered automatically.
The cluster and deployment are retained. No evaluation jobs are submitted.

Inputs (also accepted as environment variables):
  KIND_CLUSTER_NAME       kind cluster to create or reuse (default: evalhub-dev)
  IMAGE_TAG               tag for all locally built images (default: kind-<cluster>)
  SKIP_BUILD=true         reuse locally built images with IMAGE_TAG for EvalHub,
                          the operator, and selected adapters
  ADAPTERS=all             comma-separated adapter images to build and use
                          (default: all): inspect, lm-evaluation-harness, followbench
  AIRGAP=true              create/use a Calico kind cluster and deny external
                          egress from system-namespace evaluation jobs (default: false)
  CONTAINER_RUNTIME        container engine for image builds, the local registry,
                           and the kind nodes (podman or docker; default: podman
                           when reachable, otherwise docker)
  Repeated builds use the container runtime's layer cache; the EvalHub build date is stable
  for a source commit so a no-change deploy does not rebuild Go binaries.
  Temporary localhost:<port> registry tags are removed after each successful push.
  EVALHUB_HOST_PORT       localhost API port (default: 18443)
  EVALHUB_NODE_PORT       kind NodePort for the API (default: 30443)
  EVALHUB_HOST_BIND_ADDRESS host bind address (default: 127.0.0.1; loopback only)
  DEV_REPO_PARENT_PATHS   colon-separated parent directories to search after
                          the sibling check (default: ~/code)
                          Each repo is searched as a sibling of EvalHub first,
                          then as <parent>/<repo> in the listed order.

New clusters map the API NodePort to the host port using
EVALHUB_HOST_BIND_ADDRESS. The default 127.0.0.1 binds only on loopback; use
0.0.0.0 only when access from other host interfaces is needed. Existing clusters
are reused only when their mapping matches the requested bind address and host
port; mappings cannot be changed without recreating the cluster. For registry
mirror support, a reused cluster must use a kind node image from v0.27.0 or newer.

A narrowly scoped NetworkPolicy allows the kind container network gateway to reach
the EvalHub API on port 8443. This is required because the operator's default
ingress policy otherwise blocks host requests forwarded through the NodePort.

Air-gap mode is an evaluation-pod egress simulation, not a host or whole-cluster
firewall. New clusters disable kindnet and install the pinned Calico v3.33.0 so
network policies are enforced. Existing clusters must already have Calico and
must not have kindnet; the CNI cannot be changed in place by this script.
The policy selects evaluation-job pods in namespace system; use X-Tenant: system
for test requests. It permits DNS and traffic to pods in system, including EvalHub
and the local target/judge mocks. Stage benchmark assets on a PVC before
submitting jobs. Only selected adapter images are built and wired to the local
registry. Unselected providers retain their operator-packaged image references;
in air-gap mode, those images are not guaranteed to be available for jobs.

The private kubeconfig is written under XDG_CONFIG_HOME (default ~/.config),
at evalhub/kubeconfigs/<cluster>.yaml.
Use it explicitly: KUBECONFIG=<printed-path> kubectl get pods -n system
USAGE
}

remove_temporary_registry_tag() {
  local image_ref="$1" pending
  local -a remaining=()
  if ! "$CONTAINER_RUNTIME" image rm "$image_ref" >/dev/null 2>&1; then
    log WARN "Could not remove temporary local registry tag $image_ref."
    return 0
  fi
  for pending in "${TEMP_REGISTRY_TAGS[@]}"; do
    [[ "$pending" == "$image_ref" ]] || remaining+=("$pending")
  done
  TEMP_REGISTRY_TAGS=("${remaining[@]}")
  log INFO "Removed temporary local registry tag $image_ref."
}

remove_pending_registry_tags() {
  local image_ref
  for image_ref in "${TEMP_REGISTRY_TAGS[@]}"; do
    "$CONTAINER_RUNTIME" image rm "$image_ref" >/dev/null 2>&1 || true
  done
  TEMP_REGISTRY_TAGS=()
}

cleanup() {
  local status=$?
  trap - EXIT
  remove_pending_registry_tags
  if [[ -n "$PORT_FORWARD_PID" ]]; then
    kill "$PORT_FORWARD_PID" >/dev/null 2>&1 || true
    wait "$PORT_FORWARD_PID" 2>/dev/null || true
  fi
  if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then
    rm -rf -- "$WORK_DIR"
  fi
  if [[ -n "$REGISTRY_NAME" ]] && "$CONTAINER_RUNTIME" container inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
    log INFO "Local registry remains running for future job pods: $REGISTRY_NAME"
  fi
  exit "$status"
}

find_repo_checkout() {
  local repo_name="$1" required_file="$2" candidate parent searched
  local -a search_parents=()
  local sibling="${EVALHUB_REPO_PARENT}/${repo_name}"
  searched="$sibling"
  if [[ -f "$sibling/$required_file" ]]; then
    printf '%s\n' "$sibling"
    return 0
  fi

  IFS=: read -r -a search_parents <<<"$DEV_REPO_PARENT_PATHS"
  for parent in "${search_parents[@]}"; do
    [[ -n "$parent" ]] || continue
    parent="${parent%/}"
    candidate="${parent}/${repo_name}"
    searched+=", $candidate"
    if [[ -f "$candidate/$required_file" ]]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done

  fail "Could not find repository '$repo_name' (requires $required_file). Searched: $searched. Set DEV_REPO_PARENT_PATHS to a colon-separated list of parent directories."
}

need_command() {
  command -v "$1" >/dev/null 2>&1 || fail "Required command not found: $1"
}

# Select the container runtime used for image builds, the local registry, and
# the kind nodes. Podman is preferred whenever it is installed and reachable;
# Docker is the fallback. Set CONTAINER_RUNTIME to force a specific runtime.
detect_container_runtime() {
  case "$CONTAINER_RUNTIME" in
    podman|docker)
      need_command "$CONTAINER_RUNTIME"
      "$CONTAINER_RUNTIME" info >/dev/null 2>&1 ||
        fail "$CONTAINER_RUNTIME is installed but not reachable. Start the $CONTAINER_RUNTIME daemon (or podman machine) and retry."
      ;;
    "")
      if command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
        CONTAINER_RUNTIME=podman
      elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
        CONTAINER_RUNTIME=docker
      else
        fail "Neither podman nor docker is reachable. Install podman (preferred) or docker and make sure it is running."
      fi
      ;;
    *)
      fail "CONTAINER_RUNTIME must be 'podman' or 'docker'."
      ;;
  esac
  log INFO "Using $CONTAINER_RUNTIME as the container runtime for builds, the local registry, and kind."
}

kind_cmd() {
  if [[ "${CONTAINER_RUNTIME:-}" == podman ]]; then
    KIND_EXPERIMENTAL_PROVIDER=podman kind "$@"
  else
    env -u KIND_EXPERIMENTAL_PROVIDER kind "$@"
  fi
}

trim_whitespace() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

adapter_selected() {
  local requested
  for requested in "${SELECTED_ADAPTERS[@]}"; do
    [[ "$requested" == "$1" ]] && return 0
  done
  return 1
}

parse_adapters() {
  local requested adapter image
  local -a requested_adapters=()

  ADAPTERS="$(trim_whitespace "$ADAPTERS")"
  if [[ "$ADAPTERS" == all ]]; then
    requested_adapters=(inspect followbench lm-evaluation-harness)
  else
    [[ -n "$ADAPTERS" ]] || fail "ADAPTERS must be 'all' or a comma-separated list of supported adapters."
    IFS=, read -r -a requested_adapters <<<"$ADAPTERS"
  fi

  SELECTED_ADAPTERS=()
  SELECTED_ADAPTER_IMAGES=()
  for requested in "${requested_adapters[@]}"; do
    adapter="$(trim_whitespace "$requested")"
    case "$adapter" in
      inspect) image=community-inspect ;;
      followbench) image=community-followbench ;;
      lm-evaluation-harness) image=community-lmes ;;
      "") fail "ADAPTERS contains an empty entry. Use inspect, followbench, or lm-evaluation-harness." ;;
      *) fail "Unsupported adapter '$adapter' in ADAPTERS. Supported values: inspect, followbench, lm-evaluation-harness." ;;
    esac
    adapter_selected "$adapter" && fail "ADAPTERS contains duplicate entry '$adapter'."
    SELECTED_ADAPTERS+=("$adapter")
    SELECTED_ADAPTER_IMAGES+=("$image")
  done
}

validate_inputs() {
  case "$SKIP_BUILD" in true|false) ;; *) fail "SKIP_BUILD must be true or false." ;; esac
  case "$AIRGAP" in true|false) ;; *) fail "AIRGAP must be true or false." ;; esac
  parse_adapters
  [[ "$KIND_CLUSTER_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail "Invalid kind cluster name: $KIND_CLUSTER_NAME"
  ((${#KIND_CLUSTER_NAME} <= 63)) || fail "KIND_CLUSTER_NAME must be at most 63 characters."
  [[ "$IMAGE_TAG" =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$ ]] ||
    fail "IMAGE_TAG must be 1-128 characters and contain only letters, digits, underscores, dots, or hyphens."
  [[ "$EVALHUB_HOST_PORT" =~ ^[0-9]{1,5}$ ]] &&
    ((10#$EVALHUB_HOST_PORT >= 1 && 10#$EVALHUB_HOST_PORT <= 65535)) ||
    fail "EVALHUB_HOST_PORT must be a TCP port from 1 to 65535."
  [[ "$EVALHUB_NODE_PORT" =~ ^[0-9]{1,5}$ ]] &&
    ((10#$EVALHUB_NODE_PORT >= 30000 && 10#$EVALHUB_NODE_PORT <= 32767)) ||
    fail "EVALHUB_NODE_PORT must be a Kubernetes NodePort from 30000 to 32767."
  case "$EVALHUB_HOST_BIND_ADDRESS" in
    0.0.0.0|127.0.0.1) ;;
    *) fail "EVALHUB_HOST_BIND_ADDRESS must be 0.0.0.0 or 127.0.0.1." ;;
  esac
}

check_prerequisites() {
  local tool
  for tool in kind kubectl git curl openssl python3 awk sed grep base64 cp mktemp tr env cksum sort; do
    need_command "$tool"
  done
  detect_container_runtime
  log PASS "$CONTAINER_RUNTIME is reachable; it will build images and back kind."
  log INFO "$(kind version 2>&1 | head -n 1)"
  log INFO "$(kubectl version --client 2>&1 | head -n 1)"
}

check_repository() {
  local label="$1" path="$2" required="$3" override="${4:-}"
  if [[ ! -d "$path" ]]; then
    if [[ -n "$override" ]]; then
      fail "$label checkout was not found at $path. Set $override to its checkout path."
    fi
    fail "$label checkout was not found at $path."
  fi
  [[ -f "$path/$required" ]] || fail "$label checkout is missing $required: $path"
  log INFO "$label source: $path ($(git -C "$path" branch --show-current 2>/dev/null || printf 'detached HEAD'))"
  if [[ -n "$(git -C "$path" status --porcelain 2>/dev/null || true)" ]]; then
    log INFO "$label has local changes; the image build will use the current working tree."
  fi
}

preflight_existing_airgap_cluster() {
  local preflight_kubeconfig
  [[ "$AIRGAP" == true ]] || return 0
  if ! kind_cmd get clusters 2>/dev/null | grep -Fqx -- "$KIND_CLUSTER_NAME"; then
    return 0
  fi

  preflight_kubeconfig="$WORK_DIR/airgap-preflight.kubeconfig"
  kind_cmd export kubeconfig --name "$KIND_CLUSTER_NAME" --kubeconfig "$preflight_kubeconfig" >/dev/null
  if kubectl --kubeconfig "$preflight_kubeconfig" -n kube-system get daemonset kindnet >/dev/null 2>&1; then
    fail "Existing kind cluster '$KIND_CLUSTER_NAME' uses kindnet, which does not enforce NetworkPolicy. Use a new cluster name or delete and recreate it with AIRGAP=true."
  fi
  kubectl --kubeconfig "$preflight_kubeconfig" -n kube-system get daemonset calico-node >/dev/null 2>&1 ||
    fail "Existing kind cluster '$KIND_CLUSTER_NAME' has no Calico CNI. Use a new cluster name or delete and recreate it with AIRGAP=true."
  log PASS "Existing cluster has Calico and can enforce the air-gap egress policy."
}

ensure_calico_cni() {
  local manifest_url
  if kubectl --kubeconfig "$KUBECONFIG_FILE" -n kube-system get daemonset calico-node >/dev/null 2>&1; then
    if kubectl --kubeconfig "$KUBECONFIG_FILE" -n kube-system get daemonset kindnet >/dev/null 2>&1; then
      fail "Cluster '$KIND_CLUSTER_NAME' has both Calico and kindnet. Recreate it with AIRGAP=true so Calico is the only CNI."
    fi
    log INFO "Using the existing Calico CNI in kind cluster '$KIND_CLUSTER_NAME'."
  else
    [[ "$KIND_CLUSTER_CREATED" == true ]] ||
      fail "Existing cluster '$KIND_CLUSTER_NAME' has no Calico CNI. Use a new cluster name or delete and recreate it with AIRGAP=true."
    manifest_url="https://raw.githubusercontent.com/projectcalico/calico/${CALICO_VERSION}/manifests/calico.yaml"
    log INFO "Installing Calico ${CALICO_VERSION} into the new kind cluster."
    curl --fail --location --silent --show-error "$manifest_url" -o "$WORK_DIR/calico.yaml" ||
      fail "Could not download the pinned Calico manifest from $manifest_url."
    kubectl --kubeconfig "$KUBECONFIG_FILE" create -f "$WORK_DIR/calico.yaml" ||
      fail "Could not install Calico into kind cluster '$KIND_CLUSTER_NAME'."
  fi

  kubectl --kubeconfig "$KUBECONFIG_FILE" -n kube-system rollout status \
    daemonset/calico-node --timeout=300s || fail "Calico node agents did not become Ready."
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n kube-system rollout status \
    deployment/calico-kube-controllers --timeout=300s || fail "Calico controllers did not become Ready."
  log PASS "Calico is Ready and kind NetworkPolicy enforcement is available."
}

ensure_cluster() {
  local node mapping kind_config networking_config
  mkdir -p -- "$KUBECONFIG_DIR"
  [[ ! -L "$KUBECONFIG_FILE" ]] || fail "Refusing to overwrite kubeconfig symlink: $KUBECONFIG_FILE"
  if [[ -e "$KUBECONFIG_FILE" ]]; then
    local existing_context
    existing_context="$(kubectl --kubeconfig "$KUBECONFIG_FILE" config current-context 2>/dev/null || true)"
    [[ "$existing_context" == "kind-$KIND_CLUSTER_NAME" ]] ||
      fail "Kubeconfig path already exists but is not for kind-$KIND_CLUSTER_NAME: $KUBECONFIG_FILE"
  fi
  if kind_cmd get clusters 2>/dev/null | grep -Fqx -- "$KIND_CLUSTER_NAME"; then
    log INFO "Reusing existing kind cluster: $KIND_CLUSTER_NAME"
    kind_cmd export kubeconfig --name "$KIND_CLUSTER_NAME" --kubeconfig "$KUBECONFIG_FILE"
  else
    log INFO "Creating ${CONTAINER_RUNTIME}-backed kind cluster: $KIND_CLUSTER_NAME"
    kind_config="$WORK_DIR/kind-cluster.yaml"
    networking_config=""
    if [[ "$AIRGAP" == true ]]; then
      networking_config=$'networking:\n  disableDefaultCNI: true\n  podSubnet: "192.168.0.0/16"\n'
    fi
    cat >"$kind_config" <<YAML
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
${networking_config}
containerdConfigPatches:
  - |-
    [plugins."io.containerd.grpc.v1.cri".registry]
      config_path = "/etc/containerd/certs.d"
nodes:
  - role: control-plane
    extraPortMappings:
      - containerPort: $EVALHUB_NODE_PORT
        hostPort: $EVALHUB_HOST_PORT
        listenAddress: $EVALHUB_HOST_BIND_ADDRESS
        protocol: TCP
YAML
    kind_cmd create cluster --name "$KIND_CLUSTER_NAME" --config "$kind_config" \
      --kubeconfig "$KUBECONFIG_FILE" --wait 180s --retain
    KIND_CLUSTER_CREATED=true
  fi
  chmod 600 "$KUBECONFIG_FILE"
  if [[ "$AIRGAP" == true ]]; then
    ensure_calico_cni
  fi
  kubectl --kubeconfig "$KUBECONFIG_FILE" wait --for=condition=Ready nodes --all --timeout=180s
  kubectl --kubeconfig "$KUBECONFIG_FILE" get nodes
  local found_mapping="" existing_mappings=""
  while IFS= read -r node; do
    [[ -n "$node" ]] || continue
    mapping="$("$CONTAINER_RUNTIME" port "$node" "${EVALHUB_NODE_PORT}/tcp" 2>/dev/null || true)"
    [[ -n "$existing_mappings" || -z "$mapping" ]] || existing_mappings="$mapping"
    mapping="$(printf '%s\n' "$mapping" | awk -v bind_address="${EVALHUB_HOST_BIND_ADDRESS}:" 'index($0, bind_address) == 1 {print; exit}')"
    if [[ -n "$mapping" ]]; then
      found_mapping="$mapping"
      API_HOST_ACCESS_PORT="${mapping##*:}"
      break
    fi
  done < <(kind_cmd get nodes --name "$KIND_CLUSTER_NAME")
  if [[ -n "$found_mapping" ]]; then
    log PASS "EvalHub NodePort is mapped at ${found_mapping}."
  else
    if [[ -n "$existing_mappings" ]]; then
      fail "Existing kind cluster maps NodePort $EVALHUB_NODE_PORT at '${existing_mappings}', but this run expects '${EVALHUB_HOST_BIND_ADDRESS}:${EVALHUB_HOST_PORT}'. Port mappings are fixed at cluster creation; recreate the cluster to apply the requested mapping."
    fi
    fail "Kind cluster did not publish NodePort $EVALHUB_NODE_PORT at ${EVALHUB_HOST_BIND_ADDRESS}:${EVALHUB_HOST_PORT}."
  fi
  log PASS "kind cluster API is reachable and its nodes are Ready."
}

image_ref() {
  printf 'localhost/evalhub/%s:%s' "$1" "$IMAGE_TAG"
}

build_image() {
  local name="$1" context="$2" dockerfile="$3" target_arch="$4"
  local image build_date
  image="$(image_ref "$name")"
  [[ -f "$dockerfile" ]] || fail "Containerfile not found: $dockerfile"
  log INFO "Building $image from $context"
  case "$name" in
    eval-hub)
      # A stable source timestamp keeps repeated local deployments from
      # invalidating the Go build layers solely because the wall clock changed.
      build_date="$(git -C "$EVALHUB_REPO" show -s --format=%cI HEAD)"
      "$CONTAINER_RUNTIME" build --platform "linux/${target_arch}" \
        --build-arg "TARGETARCH=${target_arch}" \
        --build-arg BUILD_NUMBER=kind-local \
        --build-arg "BUILD_DATE=${build_date}" \
        --build-arg "GIT_HASH=$(git -C "$EVALHUB_REPO" rev-parse --short HEAD)" \
        --tag "$image" --file "$dockerfile" "$context"
      ;;
    trustyai-service-operator)
      "$CONTAINER_RUNTIME" build --platform "linux/${target_arch}" \
        --build-arg TARGETOS=linux --build-arg "TARGETARCH=${target_arch}" \
        --build-arg CI_CONTAINER_VERSION=kind-local \
        --tag "$image" --file "$dockerfile" "$context"
      ;;
    community-lmes)
      "$CONTAINER_RUNTIME" build --platform "linux/${target_arch}" \
        --build-arg TARGETOS=linux --build-arg "TARGETARCH=${target_arch}" \
        --tag "$image" --file "$dockerfile" "$context"
      ;;
    *)
      "$CONTAINER_RUNTIME" build --platform "linux/${target_arch}" --tag "$image" --file "$dockerfile" "$context"
      ;;
  esac
  log PASS "Built $image"
}

build_images() {
  local target_arch name image adapter
  local -a image_names=(eval-hub trustyai-service-operator)
  image_names+=("${SELECTED_ADAPTER_IMAGES[@]}")
  case "$(uname -m)" in
    x86_64) target_arch=amd64 ;;
    aarch64|arm64) target_arch=arm64 ;;
    *) fail "Unsupported build architecture: $(uname -m)" ;;
  esac
  if [[ "$SKIP_BUILD" == true ]]; then
    log INFO "SKIP_BUILD=true; verifying core and selected adapter image tags exist locally."
    for name in "${image_names[@]}"; do
      image="$(image_ref "$name")"
      "$CONTAINER_RUNTIME" image inspect "$image" >/dev/null 2>&1 || fail "Required local image is missing: $image"
      log PASS "Found $image"
    done
    return
  fi

  build_image eval-hub "$EVALHUB_REPO" "$EVALHUB_REPO/Containerfile" "$target_arch"
  build_image trustyai-service-operator "$OPERATOR_REPO" \
    "$OPERATOR_REPO/Dockerfile" "$target_arch"
  for adapter in "${SELECTED_ADAPTERS[@]}"; do
    case "$adapter" in
      inspect)
        build_image community-inspect "$CONTRIB_REPO/adapters/inspect" \
          "$CONTRIB_REPO/adapters/inspect/Containerfile" "$target_arch"
        ;;
      followbench)
        build_image community-followbench "$CONTRIB_REPO/adapters/followbench" \
          "$CONTRIB_REPO/adapters/followbench/Containerfile" "$target_arch"
        ;;
      lm-evaluation-harness)
        build_image community-lmes "$LMES_REPO" \
          "$LMES_REPO/Dockerfile.lmes-job" "$target_arch"
        ;;
    esac
  done
}

registry_image() {
  printf 'localhost:%s/evalhub/%s:%s' "$REGISTRY_PORT" "$1" "$IMAGE_TAG"
}

tag_and_push() {
  local source="$1" destination="$2"
  "$CONTAINER_RUNTIME" image inspect "$source" >/dev/null 2>&1 || fail "Image missing before registry push: $source"
  "$CONTAINER_RUNTIME" image tag "$source" "$destination"
  TEMP_REGISTRY_TAGS+=("$destination")
  "$CONTAINER_RUNTIME" image push "$destination"
  remove_temporary_registry_tag "$destination"
}

ensure_registry() {
  local alias network_json port_mapping attempt
  REGISTRY_NAME="evalhub-registry-${KIND_CLUSTER_NAME}"
  alias="evalhub-reg-$(printf '%s' "$KIND_CLUSTER_NAME" | cksum | awk '{print $1}')"
  REGISTRY_ALIAS="$alias"
  "$CONTAINER_RUNTIME" network inspect kind >/dev/null 2>&1 || fail "Container network 'kind' does not exist in $CONTAINER_RUNTIME. Confirm the named cluster was created by $CONTAINER_RUNTIME-backed kind."

  if "$CONTAINER_RUNTIME" container inspect "$REGISTRY_NAME" >/dev/null 2>&1; then
    local owner
    owner="$("$CONTAINER_RUNTIME" container inspect --format '{{ index .Config.Labels "io.evalhub.dev.cluster" }}' "$REGISTRY_NAME" 2>/dev/null || true)"
    if [[ -z "$owner" ]]; then
      owner="$("$CONTAINER_RUNTIME" container inspect --format '{{ index .Config.Labels "io.evalhub.kind-cluster" }}' "$REGISTRY_NAME" 2>/dev/null || true)"
    fi
    [[ "$owner" == "$KIND_CLUSTER_NAME" ]] || fail "Container $REGISTRY_NAME exists but is not marked as this script's registry."
    [[ "$("$CONTAINER_RUNTIME" container inspect --format '{{.State.Status}}' "$REGISTRY_NAME")" == running ]] ||
      "$CONTAINER_RUNTIME" container start "$REGISTRY_NAME" >/dev/null
  else
    "$CONTAINER_RUNTIME" image inspect docker.io/library/registry:2 >/dev/null 2>&1 || "$CONTAINER_RUNTIME" image pull docker.io/library/registry:2
    "$CONTAINER_RUNTIME" run --detach --name "$REGISTRY_NAME" \
      --label "io.evalhub.dev.cluster=${KIND_CLUSTER_NAME}" \
      --restart unless-stopped --publish 127.0.0.1::5000 docker.io/library/registry:2
  fi

  network_json="$("$CONTAINER_RUNTIME" container inspect --format '{{json .NetworkSettings.Networks}}' "$REGISTRY_NAME")"
  if [[ "$network_json" != *'"kind":'* ]]; then
    "$CONTAINER_RUNTIME" network connect --alias "$REGISTRY_ALIAS" kind "$REGISTRY_NAME"
  elif [[ "$network_json" != *"$REGISTRY_ALIAS"* ]]; then
    "$CONTAINER_RUNTIME" network disconnect kind "$REGISTRY_NAME"
    "$CONTAINER_RUNTIME" network connect --alias "$REGISTRY_ALIAS" kind "$REGISTRY_NAME"
  fi
  port_mapping="$("$CONTAINER_RUNTIME" port "$REGISTRY_NAME" 5000/tcp | awk '/^127\.0\.0\.1:/ {print; exit}')"
  [[ "$port_mapping" == 127.0.0.1:* ]] || fail "Could not determine localhost port for registry container $REGISTRY_NAME."
  REGISTRY_PORT="${port_mapping##*:}"
  [[ "$REGISTRY_PORT" =~ ^[0-9]+$ ]] || fail "Invalid registry port: $REGISTRY_PORT"
  for attempt in {1..30}; do
    curl --silent --fail "http://127.0.0.1:${REGISTRY_PORT}/v2/" >/dev/null 2>&1 && break
    sleep 1
  done
  curl --silent --fail "http://127.0.0.1:${REGISTRY_PORT}/v2/" >/dev/null ||
    fail "Local registry did not become reachable at 127.0.0.1:${REGISTRY_PORT}."
  log PASS "Local registry is ready on port $REGISTRY_PORT and joined to the kind network."
}

publish_images() {
  local name source destination
  local -a image_names=(eval-hub trustyai-service-operator)
  image_names+=("${SELECTED_ADAPTER_IMAGES[@]}")
  for name in "${image_names[@]}"; do
    source="$(image_ref "$name")"
    destination="$(registry_image "$name")"
    log INFO "Publishing $name to the cluster-local registry."
    tag_and_push "$source" "$destination"
  done
  local mock_source=docker.io/library/python:3.12-slim proxy_source=quay.io/opendatahub/odh-kube-rbac-proxy:odh-stable
  "$CONTAINER_RUNTIME" image inspect "$mock_source" >/dev/null 2>&1 || "$CONTAINER_RUNTIME" image pull "$mock_source"
  tag_and_push "$mock_source" "$(registry_image kind-openai-mock)"
  "$CONTAINER_RUNTIME" image inspect "$proxy_source" >/dev/null 2>&1 || "$CONTAINER_RUNTIME" image pull "$proxy_source"
  tag_and_push "$proxy_source" "localhost:${REGISTRY_PORT}/opendatahub/odh-kube-rbac-proxy:odh-stable"
}

configure_registry_mirrors() {
  local hosts_file node
  hosts_file="$WORK_DIR/hosts.toml"
  printf '[host."http://%s:5000"]\n  capabilities = ["pull", "resolve"]\n' "$REGISTRY_ALIAS" >"$hosts_file"
  chmod 644 "$hosts_file"
  while IFS= read -r node; do
    [[ -n "$node" ]] || continue
    log INFO "Configuring the local registry mirror on kind node $node."
    "$CONTAINER_RUNTIME" exec "$node" mkdir -p "/etc/containerd/certs.d/localhost:${REGISTRY_PORT}"
    "$CONTAINER_RUNTIME" cp "$hosts_file" "$node:/etc/containerd/certs.d/localhost:${REGISTRY_PORT}/hosts.toml" >/dev/null
  done < <(kind_cmd get nodes --name "$KIND_CLUSTER_NAME")
  log PASS "Configured containerd to resolve localhost:${REGISTRY_PORT} through the local registry."
}

replace_env_value() {
  local file="$1" key="$2" value="$3" output="$WORK_DIR/params.env.tmp"
  awk -v key="$key" -v value="$value" '
    BEGIN { found = 0 }
    index($0, key "=") == 1 { print key "=" value; found = 1; next }
    { print }
    END { if (!found) exit 1 }
  ' "$file" >"$output" || fail "Image parameter $key was not found in $file."
  mv -- "$output" "$file"
}

set_lmes_provider_image_pull_policy() {
  local file="$1" output="$WORK_DIR/provider-lmes.yaml.tmp"
  awk '
    index($0, "image: $(lmes-pod-image)") > 0 {
      print
      match($0, /[^ ]/)
      print substr($0, 1, RSTART - 1) "image_pull_policy: always"
      found = 1
      next
    }
    { print }
    END { if (!found) exit 1 }
  ' "$file" >"$output" || fail "Could not set LMES provider image_pull_policy in $file."
  mv -- "$output" "$file"
}

make_certificate() {
  local directory="$1" name="$2" common_name="$3" sans="$4" config
  config="$directory/$name.cnf"
  mkdir -p -- "$directory"
  printf '%s\n' \
    '[ req ]' 'prompt = no' 'distinguished_name = dn' 'x509_extensions = v3_req' \
    '[ dn ]' "CN = $common_name" \
    '[ v3_req ]' 'basicConstraints = critical,CA:TRUE' \
    'keyUsage = critical,digitalSignature,keyEncipherment,keyCertSign' \
    'extendedKeyUsage = serverAuth' "subjectAltName = $sans" >"$config"
  openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
    -keyout "$directory/$name.key" -out "$directory/$name.crt" \
    -config "$config" -extensions v3_req >/dev/null 2>&1 ||
    fail "Could not generate temporary TLS material for $common_name."
  chmod 600 "$directory/$name.key"
}

check_resource_ownership() {
  local resource="$1" name="$2" namespace="${3:-}" owner
  local -a namespace_args=()
  [[ -z "$namespace" ]] || namespace_args=(-n "$namespace")
  if kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" "$name" "${namespace_args[@]}" >/dev/null 2>&1; then
    owner="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" "$name" "${namespace_args[@]}" \
      -o 'go-template={{if .metadata.annotations}}{{index .metadata.annotations "io.evalhub.kind-smoke.cluster"}}{{end}}')"
    [[ "$owner" == "$KIND_CLUSTER_NAME" ]] ||
      fail "$resource/$name already exists and is not marked as this script's deployment; refusing to modify it."
  fi
}

prepare_namespace_and_certificates() {
  local cert_dir="$WORK_DIR/certs"
  local binding role
  local -a bindings=(
    manager-auth-delegator evalhub-collections-access-binding evalhub-job-config-binding
    evalhub-jobs-writer-binding evalhub-manager-rolebinding evalhub-mlflow-access-binding
    evalhub-mlflow-jobs-access-binding evalhub-providers-access-binding proxy-rolebinding
    tls-profile-reader-binding
  )
  local -a roles=(
    evalhub-auth-reviewer-role evalhub-collections-access evalhub-events
    evalhub-hardware-profiles-reader evalhub-job-config evalhub-jobs-writer
    evalhub-manager-role evalhub-mlflow-access evalhub-mlflow-jobs-access
    evalhub-model-secret evalhub-providers-access metrics-reader proxy-role
    tls-profile-reader
  )
  check_resource_ownership namespace system
  check_resource_ownership crd evalhubs.trustyai.opendatahub.io
  check_resource_ownership deployment trustyai-service-operator-controller-manager system
  check_resource_ownership serviceaccount trustyai-service-operator-controller-manager system
  check_resource_ownership role trustyai-service-operator-leader-election-role system
  check_resource_ownership rolebinding trustyai-service-operator-leader-election-rolebinding system
  for binding in "${bindings[@]}"; do
    check_resource_ownership clusterrolebinding "$binding"
    check_resource_ownership clusterrolebinding "trustyai-service-operator-$binding"
  done
  for role in "${roles[@]}"; do
    check_resource_ownership clusterrole "$role"
    check_resource_ownership clusterrole "trustyai-service-operator-$role"
  done
  make_certificate "$cert_dir" webhook \
    trustyai-service-operator-webhook-service.system.svc \
    'DNS:trustyai-service-operator-webhook-service.system.svc,DNS:trustyai-service-operator-webhook-service.system.svc.cluster.local'
  make_certificate "$cert_dir" evalhub \
    evalhub-local.system.svc \
    'DNS:evalhub-local.system.svc,DNS:evalhub-local.system.svc.cluster.local'

  if kubectl --kubeconfig "$KUBECONFIG_FILE" get namespace system >/dev/null 2>&1; then
    kubectl --kubeconfig "$KUBECONFIG_FILE" create namespace system --dry-run=client -o yaml |
      kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - >/dev/null
  else
    kubectl --kubeconfig "$KUBECONFIG_FILE" create namespace system >/dev/null
    kubectl --kubeconfig "$KUBECONFIG_FILE" annotate namespace system \
      "io.evalhub.kind-smoke.created-by=dev-deploy" --overwrite >/dev/null
  fi
  kubectl --kubeconfig "$KUBECONFIG_FILE" annotate namespace system \
    "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system create secret tls webhook-server-cert \
    --cert="$cert_dir/webhook.crt" --key="$cert_dir/webhook.key" --dry-run=client -o yaml |
    kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system annotate secret webhook-server-cert \
    "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system create secret tls evalhub-local-tls \
    --cert="$cert_dir/evalhub.crt" --key="$cert_dir/evalhub.key" --dry-run=client -o yaml |
    kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system annotate secret evalhub-local-tls \
    "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system create configmap evalhub-local-service-ca \
    --from-file=service-ca.crt="$cert_dir/evalhub.crt" --dry-run=client -o yaml |
    kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - >/dev/null
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system annotate configmap evalhub-local-service-ca \
    "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite >/dev/null
  log PASS "Prepared temporary webhook and EvalHub HTTPS certificates for kind."
}

render_operator_overlay() {
  local config_root="$WORK_DIR/operator-config" params overlay manifest
  mkdir -p -- "$config_root"
  cp -a "$OPERATOR_REPO/config/." "$config_root/"
  params="$config_root/base/params.env"
  replace_env_value "$params" trustyaiOperatorImage "$(registry_image trustyai-service-operator)"
  replace_env_value "$params" evalHubImage "$(registry_image eval-hub)"
  if adapter_selected inspect; then
    replace_env_value "$params" evalhub-provider-inspect-image "$(registry_image community-inspect)"
  fi
  if adapter_selected followbench; then
    replace_env_value "$params" evalhub-provider-followbench-image "$(registry_image community-followbench)"
  fi
  if adapter_selected lm-evaluation-harness; then
    replace_env_value "$params" lmes-pod-image "$(registry_image community-lmes)"
    set_lmes_provider_image_pull_policy \
      "$config_root/configmaps/evalhub/provider-lm-evaluation-harness.yaml"
  fi
  replace_env_value "$params" kube-rbac-proxy "localhost:${REGISTRY_PORT}/opendatahub/odh-kube-rbac-proxy:odh-stable"

  overlay="$config_root/overlays/evalhub-kind"
  mkdir -p -- "$overlay"
  cat >"$overlay/kustomization.yaml" <<'YAML'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: system
resources:
  - ../evalhub-only
components:
  - ../../components/webhook
namePrefix: trustyai-service-operator-
patches:
  - target:
      group: monitoring.coreos.com
      version: v1
      kind: ServiceMonitor
      name: service-monitor
    patch: |-
      $patch: delete
      apiVersion: monitoring.coreos.com/v1
      kind: ServiceMonitor
      metadata:
        name: service-monitor
  - target:
      group: rbac.authorization.k8s.io
      version: v1
      kind: ClusterRoleBinding
      name: manager-auth-delegator
    patch: |-
      - op: add
        path: /subjects/0/namespace
        value: system
  - target:
      group: apps
      version: v1
      kind: Deployment
      name: controller-manager
    patch: |-
      apiVersion: apps/v1
      kind: Deployment
      metadata:
        name: controller-manager
      spec:
        template:
          spec:
            containers:
              - name: manager
                imagePullPolicy: Always
YAML
  cat >>"$overlay/kustomization.yaml" <<YAML
commonAnnotations:
  io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
YAML

  manifest="$WORK_DIR/operator-manifests.yaml"
  log INFO "Rendering an EvalHub-only operator overlay in a temporary copy of the operator config."
  kubectl kustomize "$overlay" >"$manifest"
  grep -Fq "image: $(registry_image trustyai-service-operator)" "$manifest" ||
    fail "Rendered operator deployment does not use the local operator image."
  grep -Fq "evalHubImage: $(registry_image eval-hub)" "$manifest" ||
    fail "Rendered operator config does not use the local EvalHub image."
  if adapter_selected inspect; then
    grep -Fq "image: $(registry_image community-inspect)" "$manifest" ||
      fail "Rendered provider config does not use the local Inspect image."
  fi
  if adapter_selected followbench; then
    grep -Fq "image: $(registry_image community-followbench)" "$manifest" ||
      fail "Rendered provider config does not use the local FollowBench image."
  fi
  if adapter_selected lm-evaluation-harness; then
    grep -Fq "image: $(registry_image community-lmes)" "$manifest" ||
      fail "Rendered LMES provider config does not use the locally built image."
    grep -Fq 'image_pull_policy: always' "$manifest" ||
      fail "Rendered LMES provider config does not pull the current local image."
  fi
  grep -Fq "kube-rbac-proxy: localhost:${REGISTRY_PORT}/opendatahub/odh-kube-rbac-proxy:odh-stable" "$manifest" ||
    fail "Rendered operator config does not use the local kube-rbac-proxy image."
  grep -Fq 'name: trustyai-service-operator-controller-manager' "$manifest" ||
    fail "Rendered overlay lacks the expected operator deployment."
  grep -Fq 'imagePullPolicy: Always' "$manifest" ||
    fail "Rendered operator deployment does not pull the current local operator image."
  grep -Fq 'name: trustyai-service-operator-webhook-service' "$manifest" ||
    fail "Rendered overlay lacks the conversion webhook service."
  log PASS "The temporary operator overlay renders all local image references."
}

discover_system_collections() {
  local configmaps collection_names configmap_name collection_name duplicate_names

  configmaps="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get configmaps \
    --selector='trustyai.opendatahub.io/evalhub-collection-type=system' \
    -o go-template='{{range .items}}{{.metadata.name}}{{"\t"}}{{index .metadata.labels "trustyai.opendatahub.io/evalhub-collection-name"}}{{"\n"}}{{end}}')" ||
    fail "Could not list the operator-packaged system collection ConfigMaps."
  [[ -n "$configmaps" ]] || fail "No operator-packaged system collection ConfigMaps were found in namespace system."

  collection_names=""
  while IFS=$'\t' read -r configmap_name collection_name; do
    [[ -n "$configmap_name" ]] || continue
    [[ -n "$collection_name" ]] ||
      fail "System collection ConfigMap $configmap_name is missing the collection-name label."
    [[ "$collection_name" =~ ^[A-Za-z0-9]([A-Za-z0-9_.-]*[A-Za-z0-9])?$ ]] ||
      fail "System collection ConfigMap $configmap_name has an unsafe collection name: $collection_name"
    collection_names+="${collection_name}"$'\n'
  done <<<"$configmaps"
  [[ -n "$collection_names" ]] || fail "No valid system collection names were discovered."

  duplicate_names="$(printf '%s' "$collection_names" | LC_ALL=C sort | awk 'seen[$0]++ { print }')"
  [[ -z "$duplicate_names" ]] ||
    fail "Multiple operator ConfigMaps advertise the same system collection name(s): ${duplicate_names//$'\n'/, }"

  printf '%s' "$collection_names" | LC_ALL=C sort -u
}

install_operator_and_evalhub() {
  local cert_bundle config_root configmaps name owner collection_names collections_yaml collection_name collection_count
  config_root="$WORK_DIR/operator-config"
  log INFO "Applying the EvalHub-only operator overlay to namespace system."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -k "$config_root/overlays/evalhub-kind"
  local operator_pull_policy
  operator_pull_policy="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get \
    deployment/trustyai-service-operator-controller-manager \
    -o 'jsonpath={.spec.template.spec.containers[?(@.name=="manager")].imagePullPolicy}')"
  [[ "$operator_pull_policy" == Always ]] ||
    fail "The local operator deployment must use imagePullPolicy Always; got ${operator_pull_policy:-unset}."
  kubectl --kubeconfig "$KUBECONFIG_FILE" annotate crd evalhubs.trustyai.opendatahub.io \
    "io.evalhub.kind-smoke.cluster=$KIND_CLUSTER_NAME" --overwrite >/dev/null
  configmaps="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get configmaps \
    -o go-template='{{range .items}}{{.metadata.name}}{{"\t"}}{{if .metadata.annotations}}{{index .metadata.annotations "io.evalhub.kind-smoke.cluster"}}{{end}}{{"\n"}}{{end}}')"
  while IFS=$'\t' read -r name owner; do
    [[ "$owner" == "$KIND_CLUSTER_NAME" ]] || continue
    case "$name" in
      evalhub-collection-*|evalhub-provider-*)
        log INFO "Removing stale script-managed unprefixed catalog ConfigMap $name."
        kubectl --kubeconfig "$KUBECONFIG_FILE" -n system delete configmap "$name" --ignore-not-found >/dev/null
        ;;
    esac
  done <<<"$configmaps"
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
    deployment/trustyai-service-operator-controller-manager --timeout=300s
  log PASS "Operator deployment is available."

  cert_bundle="$(base64 <"$WORK_DIR/certs/webhook.crt" | tr -d '\n')"
  kubectl --kubeconfig "$KUBECONFIG_FILE" patch crd evalhubs.trustyai.opendatahub.io \
    --type=json --patch "[{\"op\":\"add\",\"path\":\"/spec/conversion/webhook/clientConfig/caBundle\",\"value\":\"$cert_bundle\"}]" >/dev/null
  log PASS "Configured the CRD conversion webhook to trust the temporary certificate."

  collection_names="$(discover_system_collections)"
  collections_yaml=""
  collection_count=0
  while IFS= read -r collection_name; do
    [[ -n "$collection_name" ]] || continue
    collections_yaml+="    - \"${collection_name}\""$'\n'
    collection_count=$((collection_count + 1))
  done <<<"$collection_names"
  ((collection_count > 0)) || fail "No packaged system collections are available for the EvalHub instance."
  collections_yaml="${collections_yaml%$'\n'}"
  log PASS "Discovered $collection_count packaged system collections for the EvalHub instance."

  cat >"$WORK_DIR/evalhub.yaml" <<YAML
apiVersion: trustyai.opendatahub.io/v1
kind: EvalHub
metadata:
  name: evalhub-local
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  tenancy: single
  database:
    type: sqlite
  providers:
    - garak
    - garak-kfp
    - lm-evaluation-harness
    - inspect
    - lighteval
    - ruler
    - ifbench
    - followbench
  # Set the full discovered list explicitly so the CRD's static default list
  # does not limit collections available in this development deployment.
  collections:
${collections_yaml}
YAML
  log INFO "Creating the local SQLite-backed EvalHub instance."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f "$WORK_DIR/evalhub.yaml"
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system wait \
    --for=jsonpath='{.status.ready}'=True evalhub/evalhub-local --timeout=300s
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status deployment/evalhub-local --timeout=300s
  log PASS "EvalHub custom resource and service deployment are Ready."
}

restart_workloads_on_redeploy() {
  local deployment
  if [[ "$KIND_CLUSTER_CREATED" == true ]]; then
    log INFO "New kind cluster; workloads already started from the freshly built images."
    return
  fi

  for deployment in trustyai-service-operator-controller-manager evalhub-local; do
    log INFO "Restarting $deployment so the reused kind cluster runs the current local image contents."
    kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout restart "deployment/$deployment"
    kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
      "deployment/$deployment" --timeout=300s
  done
  log PASS "Reused-cluster operator and EvalHub workloads now use the current local images."
}

install_host_api_service() {
  check_resource_ownership service evalhub-local-nodeport system
  log INFO "Creating the localhost-development NodePort service for EvalHub."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - <<YAML
apiVersion: v1
kind: Service
metadata:
  name: evalhub-local-nodeport
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  type: NodePort
  selector:
    app: eval-hub
    instance: evalhub-local
    component: api
  ports:
    - name: https
      protocol: TCP
      port: 8443
      targetPort: https
      nodePort: $EVALHUB_NODE_PORT
YAML
  log PASS "EvalHub NodePort service is configured on port $EVALHUB_NODE_PORT."
}

install_nodeport_ingress_policy() {
  local node gateway
  node="$(kind_cmd get nodes --name "$KIND_CLUSTER_NAME" | sed -n '1p')"
  [[ -n "$node" ]] || fail "Could not identify the kind node for the NodePort ingress policy."
  gateway="$("$CONTAINER_RUNTIME" inspect --format '{{range .NetworkSettings.Networks}}{{if .Gateway}}{{.Gateway}}{{"\n"}}{{end}}{{end}}' "$node" 2>/dev/null | sed -n '1p')"
  [[ "$gateway" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] ||
    fail "Could not determine the IPv4 gateway for kind node $node; cannot safely scope NodePort ingress."

  check_resource_ownership networkpolicy evalhub-local-nodeport-host-ingress system
  log INFO "Allowing NodePort traffic from kind network gateway $gateway to the EvalHub API only."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - <<YAML
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: evalhub-local-nodeport-host-ingress
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  podSelector:
    matchLabels:
      app: eval-hub
      instance: evalhub-local
      component: api
  policyTypes:
  - Ingress
  ingress:
  - from:
    - ipBlock:
        cidr: ${gateway}/32
    ports:
    - protocol: TCP
      port: 8443
YAML
  log PASS "Installed the kind-only ingress exception for the EvalHub NodePort."
}

install_mock_services() {
  local mock_image
  mock_image="$(registry_image kind-openai-mock)"
  log INFO "Deploying mock OpenAI-compatible target and judge endpoints."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - <<YAML
apiVersion: v1
kind: ConfigMap
metadata:
  name: evalhub-kind-openai-mock-code
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
data:
  mock.py: |
    import json
    import os
    import re
    from http.server import BaseHTTPRequestHandler, HTTPServer

    ROLE = os.environ["MOCK_ROLE"]
    MODEL = os.environ["MOCK_MODEL"]
    API_KEYS = {item.strip() for item in os.environ["MOCK_API_KEYS"].split(",") if item.strip()}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, _format, *_args):
            pass

        def reply(self, status, value):
            body = json.dumps(value).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path.rstrip("/").endswith("/models"):
                self.reply(200, {"object": "list", "data": [{"id": MODEL, "object": "model"}]})
            else:
                self.reply(404, {"error": {"message": "not found"}})

        def do_POST(self):
            token = self.headers.get("Authorization", "")
            if token not in {"Bearer " + key for key in API_KEYS}:
                self.reply(401, {"error": {"message": "mock rejected credential"}})
                print(json.dumps({"role": ROLE, "auth": "rejected"}), flush=True)
                return

            raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            request = json.loads(raw or b"{}")
            endpoint = self.path.split("?", 1)[0]
            if ROLE == "target" and endpoint.endswith("/v1/completions"):
                prompts = request.get("prompt", "")
                if not isinstance(prompts, list):
                    prompts = [prompts]
                choices = []
                prompt_tokens = 0
                for index, prompt_value in enumerate(prompts):
                    prompt = prompt_value if isinstance(prompt_value, str) else str(prompt_value)
                    # local-completions uses echo/logprobs for multiple-choice
                    # likelihood scoring. Return enough deterministic mock token
                    # scores for the client tokenizer; test scores are not meaningful.
                    token_count = max(2, len(prompt.encode("utf-8")) + 1)
                    prompt_tokens += token_count
                    choices.append({
                        "index": index,
                        "text": prompt + " ",
                        "logprobs": {
                            "tokens": ["mock"] * token_count,
                            "token_logprobs": [-0.1] * token_count,
                            "top_logprobs": [{"mock": -0.1}] * token_count,
                            "text_offset": list(range(token_count)),
                        },
                        "finish_reason": "stop",
                    })
                self.reply(200, {
                    "id": "cmpl-kind-mock",
                    "object": "text_completion",
                    "created": 0,
                    "model": request.get("model", MODEL),
                    "choices": choices,
                    "usage": {"prompt_tokens": prompt_tokens, "completion_tokens": 1, "total_tokens": prompt_tokens + 1},
                })
                print(json.dumps({"role": ROLE, "endpoint": "completions", "auth": "accepted"}), flush=True)
                return

            if ROLE == "judge":
                prompt = " ".join(str(message.get("content", "")) for message in request.get("messages", []))
                match = re.search(r"with\s+(\d+)\s+values", prompt, re.IGNORECASE)
                count = int(match.group(1)) if match else 1
                content = "[" + ", ".join('"YES"' for _ in range(count)) + "]"
            else:
                content = "I will follow the requested constraints."
            self.reply(200, {
                "id": "chatcmpl-kind-mock",
                "object": "chat.completion",
                "created": 0,
                "model": request.get("model", MODEL),
                "choices": [{"index": 0, "message": {"role": "assistant", "content": content}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
            })
            print(json.dumps({"role": ROLE, "auth": "accepted"}), flush=True)

    HTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: evalhub-kind-mock-target
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  replicas: 1
  selector:
    matchLabels:
      app: evalhub-kind-mock-target
  template:
    metadata:
      labels:
        app: evalhub-kind-mock-target
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: mock
          image: $mock_image
          imagePullPolicy: IfNotPresent
          command: ["python3", "-B", "/app/mock.py"]
          env:
            - name: MOCK_ROLE
              value: target
            - name: MOCK_MODEL
              value: kind-target-model
            - name: MOCK_API_KEYS
              value: target-test-token,judge-test-token
          ports:
            - name: http
              containerPort: 8000
          readinessProbe:
            httpGet:
              path: /v1/models
              port: http
            periodSeconds: 3
            timeoutSeconds: 2
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 128Mi
          volumeMounts:
            - name: mock-code
              mountPath: /app
              readOnly: true
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
      volumes:
        - name: mock-code
          configMap:
            name: evalhub-kind-openai-mock-code
---
apiVersion: v1
kind: Service
metadata:
  name: evalhub-kind-mock-target
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  selector:
    app: evalhub-kind-mock-target
  ports:
    - name: http
      port: 8000
      targetPort: http
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: evalhub-kind-mock-judge
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  replicas: 1
  selector:
    matchLabels:
      app: evalhub-kind-mock-judge
  template:
    metadata:
      labels:
        app: evalhub-kind-mock-judge
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: mock
          image: $mock_image
          imagePullPolicy: IfNotPresent
          command: ["python3", "-B", "/app/mock.py"]
          env:
            - name: MOCK_ROLE
              value: judge
            - name: MOCK_MODEL
              value: kind-judge-model
            - name: MOCK_API_KEYS
              value: judge-test-token
          ports:
            - name: http
              containerPort: 8000
          readinessProbe:
            httpGet:
              path: /v1/models
              port: http
            periodSeconds: 3
            timeoutSeconds: 2
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 128Mi
          volumeMounts:
            - name: mock-code
              mountPath: /app
              readOnly: true
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
      volumes:
        - name: mock-code
          configMap:
            name: evalhub-kind-openai-mock-code
---
apiVersion: v1
kind: Service
metadata:
  name: evalhub-kind-mock-judge
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  selector:
    app: evalhub-kind-mock-judge
  ports:
    - name: http
      port: 8000
      targetPort: http
YAML

  # The mock source is mounted from a ConfigMap, but Python has already loaded
  # the old module in a running process. Restart the target so completions
  # support is active after every dev-deploy.
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout restart deployment/evalhub-kind-mock-target
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status deployment/evalhub-kind-mock-target --timeout=180s
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status deployment/evalhub-kind-mock-judge --timeout=180s
  log PASS "Mock target and judge deployments are Ready."
}

check_service_health() {
  local service="$1" remote_port="$2" scheme="$3" path="$4" expected="$5"
  local local_port log_file http_code body attempt
  local_port="$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1]); s.close()')"
  log_file="$WORK_DIR/port-forward-${service}.log"
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system port-forward \
    --address 127.0.0.1 "service/$service" "${local_port}:${remote_port}" >"$log_file" 2>&1 &
  PORT_FORWARD_PID=$!

  http_code=""
  body=""
  for attempt in {1..10}; do
    if ! kill -0 "$PORT_FORWARD_PID" 2>/dev/null; then
      sed -n '1,80p' "$log_file" >&2 || true
      fail "Port-forward for service $service exited unexpectedly."
    fi
    # curl may not create its output file when the connection is refused while
    # kubectl port-forward is starting. Create it before each probe so reading
    # the body remains safe even when curl receives no response.
    : >"$WORK_DIR/health-body"
    if [[ "$scheme" == https ]]; then
      http_code="$(curl --silent --output "$WORK_DIR/health-body" --write-out '%{http_code}' \
        --insecure --connect-timeout 2 --max-time 4 \
        "https://127.0.0.1:${local_port}${path}" 2>/dev/null || true)"
    else
      http_code="$(curl --silent --output "$WORK_DIR/health-body" --write-out '%{http_code}' \
        --connect-timeout 2 --max-time 4 \
        "http://127.0.0.1:${local_port}${path}" 2>/dev/null || true)"
    fi
    body="$(<"$WORK_DIR/health-body")"
    if [[ "$http_code" == 200 && ( -z "$expected" || "$body" == *"$expected"* ) ]]; then
      break
    fi
    sleep 1
  done

  kill "$PORT_FORWARD_PID" >/dev/null 2>&1 || true
  wait "$PORT_FORWARD_PID" 2>/dev/null || true
  PORT_FORWARD_PID=""
  [[ "$http_code" == 200 ]] || fail "$service health endpoint returned HTTP ${http_code:-no response}."
  [[ -z "$expected" || "$body" == *"$expected"* ]] ||
    fail "$service health response did not contain the expected model identifier."
  log PASS "$service health endpoint returned HTTP 200."
}

check_evalhub_health() {
  local http_code attempt
  local body_file="$WORK_DIR/evalhub-health-body"
  log INFO "Checking EvalHub health through https://127.0.0.1:${API_HOST_ACCESS_PORT}."
  http_code=""
  for attempt in {1..5}; do
    : >"$body_file"
    http_code="$(curl --silent --output "$body_file" --write-out '%{http_code}' \
      --insecure --connect-timeout 2 --max-time 3 \
      "https://127.0.0.1:${API_HOST_ACCESS_PORT}/api/v1/health" 2>/dev/null || true)"
    if [[ "$http_code" == 200 ]]; then
      break
    fi
    sleep 1
  done
  if [[ "$http_code" == 200 ]]; then
    log PASS "EvalHub health endpoint returned HTTP 200 through the localhost NodePort."
    return
  fi

  fail "EvalHub health endpoint through NodePort at https://127.0.0.1:${API_HOST_ACCESS_PORT} returned HTTP ${http_code:-no response}."
}

configure_airgap_job_egress() {
  local policy_name=evalhub-dev-airgap-egress owner
  if [[ "$AIRGAP" != true ]]; then
    if kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get networkpolicy "$policy_name" >/dev/null 2>&1; then
      owner="$(kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get networkpolicy "$policy_name" \
        -o 'go-template={{if .metadata.annotations}}{{index .metadata.annotations "io.evalhub.kind-smoke.cluster"}}{{end}}')"
      [[ "$owner" == "$KIND_CLUSTER_NAME" ]] ||
        fail "NetworkPolicy/$policy_name exists and is not owned by this dev-deploy cluster; refusing to delete it."
      kubectl --kubeconfig "$KUBECONFIG_FILE" -n system delete networkpolicy "$policy_name"
      log INFO "Removed the script-managed air-gap policy because AIRGAP=false."
    fi
    return 0
  fi

  check_resource_ownership networkpolicy "$policy_name" system
  log INFO "Restricting evaluation-job egress in namespace system to cluster DNS and namespace system."
  kubectl --kubeconfig "$KUBECONFIG_FILE" apply -f - <<YAML
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: $policy_name
  namespace: system
  annotations:
    io.evalhub.kind-smoke.cluster: $KIND_CLUSTER_NAME
spec:
  podSelector:
    matchLabels:
      app: evalhub
      component: evaluation-job
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: system
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
YAML
  log PASS "External egress is denied for evaluation jobs; system services and DNS remain reachable."
}

verify_deployment() {
  log INFO "Checking the operator, EvalHub service, and mock service readiness."
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
    deployment/trustyai-service-operator-controller-manager --timeout=300s
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
    deployment/evalhub-local --timeout=300s
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
    deployment/evalhub-kind-mock-target --timeout=180s
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system rollout status \
    deployment/evalhub-kind-mock-judge --timeout=180s
  check_evalhub_health
  check_service_health evalhub-kind-mock-target 8000 http /v1/models kind-target-model
  check_service_health evalhub-kind-mock-judge 8000 http /v1/models kind-judge-model
  log PASS "All deployed components passed their readiness and basic health checks."
}

main() {
  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    return 0
  fi
  (($# == 0)) || fail "Unexpected argument: $1. Run make dev-deploy-help for supported inputs."
  validate_inputs
  trap cleanup EXIT
  log INFO "Deploying EvalHub development environment to kind cluster '$KIND_CLUSTER_NAME'."
  log INFO "Selected adapter frameworks: ${SELECTED_ADAPTERS[*]}"
  log INFO "This applies a CRD and cluster-scoped RBAC to this kind cluster, plus resources in namespace system."
  check_prerequisites
  check_repository eval-hub "$EVALHUB_REPO" Containerfile
  OPERATOR_REPO="$(find_repo_checkout trustyai-service-operator Dockerfile)"
  check_repository trustyai-service-operator "$OPERATOR_REPO" Dockerfile
  if adapter_selected inspect || adapter_selected followbench; then
    if adapter_selected inspect; then
      CONTRIB_REPO="$(find_repo_checkout eval-hub-contrib adapters/inspect/Containerfile)"
      check_repository eval-hub-contrib "$CONTRIB_REPO" adapters/inspect/Containerfile
    else
      CONTRIB_REPO="$(find_repo_checkout eval-hub-contrib adapters/followbench/Containerfile)"
      check_repository eval-hub-contrib "$CONTRIB_REPO" adapters/followbench/Containerfile
    fi
    if adapter_selected inspect && adapter_selected followbench; then
      check_repository eval-hub-contrib "$CONTRIB_REPO" adapters/followbench/Containerfile
    fi
  fi
  if adapter_selected lm-evaluation-harness; then
    LMES_REPO="$(find_repo_checkout lm-evaluation-harness Dockerfile.lmes-job)"
    check_repository lm-evaluation-harness "$LMES_REPO" Dockerfile.lmes-job
  fi

  WORK_DIR="$(mktemp -d /tmp/evalhub-dev-deploy.XXXXXX)"
  preflight_existing_airgap_cluster
  build_images
  ensure_cluster
  ensure_registry
  publish_images
  configure_registry_mirrors
  prepare_namespace_and_certificates
  render_operator_overlay
  install_operator_and_evalhub
  restart_workloads_on_redeploy
  install_host_api_service
  install_nodeport_ingress_policy
  install_mock_services
  configure_airgap_job_egress
  verify_deployment

  kubectl --kubeconfig "$KUBECONFIG_FILE" -n system get pods,services,evalhub
  log PASS "EvalHub development environment is deployed."
  log INFO "Dedicated kubeconfig: $KUBECONFIG_FILE"
  log INFO "Use it without changing your default context: KUBECONFIG='$KUBECONFIG_FILE' kubectl get pods -n system"
  log INFO "EvalHub API URL (local self-signed TLS): https://127.0.0.1:${API_HOST_ACCESS_PORT}"
  log INFO "No evaluation job was submitted; API behavior tests are intentionally separate."
}

main "$@"
