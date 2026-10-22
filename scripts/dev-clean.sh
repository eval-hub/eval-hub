#!/usr/bin/env bash

# Remove the EvalHub development deployment from its Docker-backed kind cluster.
# By default the kind cluster is retained; --delete-cluster removes it entirely.
set -Eeuo pipefail
umask 077

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-evalhub-dev}"
DELETE_CLUSTER="${DELETE_CLUSTER:-false}"
KUBECONFIG_DIR="${XDG_CONFIG_HOME:-${HOME}/.config}/evalhub/kubeconfigs"
PRIVATE_KUBECONFIG="${KUBECONFIG_DIR}/${KIND_CLUSTER_NAME}.yaml"
KUBECONFIG_FILE=""
WORK_DIR=""
REGISTRY_NAME="evalhub-registry-${KIND_CLUSTER_NAME}"
REGISTRY_ALIAS="evalhub-reg-$(printf '%s' "$KIND_CLUSTER_NAME" | cksum | awk '{print $1}')"
OWNERSHIP_ANNOTATION="io.evalhub.kind-smoke.cluster"
NAMESPACE="system"

log() {
  printf '[%s] [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "$1" "$2"
}

fail() {
  log FAIL "$1" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage:
  make dev-clean [KIND_CLUSTER_NAME=name]
  make dev-clean KIND_CLUSTER_NAME=name DELETE_CLUSTER=true
  scripts/dev-clean.sh [--delete-cluster]

By default, removes the EvalHub development resources and local image registry
owned by this script, plus evaluation workloads labeled for evalhub-local, while
keeping the kind cluster and its private kubeconfig.
Pass DELETE_CLUSTER=true (or --delete-cluster) to remove the named kind cluster
as well. Keeping the cluster still deletes active and completed evaluation-job
workloads belonging to the evalhub-local service. Host images and Docker build
cache are retained.

Inputs (also accepted as environment variables):
  KIND_CLUSTER_NAME  kind cluster to clean (default: evalhub-dev)
  DELETE_CLUSTER     true to delete that cluster too (default: false)

The system namespace is retained to avoid deleting unrelated resources that may
have been added there. Deployment resources are removed only when their ownership
annotation matches this cluster; evaluation workloads are selected by the
evalhub-local instance labels. A local registry container is removed only if its
Docker label identifies it as belonging to the selected cluster.
USAGE
}

kind_cmd() {
  env -u KIND_EXPERIMENTAL_PROVIDER kind "$@"
}

cluster_exists() {
  local clusters
  clusters="$(kind_cmd get clusters 2>/dev/null)" || fail "Could not list kind clusters."
  printf '%s\n' "$clusters" | grep -Fqx -- "$KIND_CLUSTER_NAME"
}

resource_exists() {
  local resource="$1" name="$2" namespace="${3:-}" result
  local -a namespace_args=()
  [[ -z "$namespace" ]] || namespace_args=(-n "$namespace")
  result="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" "$name" "${namespace_args[@]}" \
    --ignore-not-found -o name 2>/dev/null)" || fail "Could not query $resource/$name."
  [[ -n "$result" ]]
}

resource_owner() {
  local resource="$1" name="$2" namespace="${3:-}"
  local -a namespace_args=()
  [[ -z "$namespace" ]] || namespace_args=(-n "$namespace")
  kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" "$name" "${namespace_args[@]}" \
    -o "go-template={{if .metadata.annotations}}{{index .metadata.annotations \"${OWNERSHIP_ANNOTATION}\"}}{{end}}" \
    2>/dev/null || true
}

registry_owner() {
  local owner
  owner="$(docker container inspect --format '{{ index .Config.Labels "io.evalhub.dev.cluster" }}' "$REGISTRY_NAME")" ||
    fail "Could not inspect local registry container $REGISTRY_NAME."
  if [[ -z "$owner" || "$owner" == '<no value>' ]]; then
    owner="$(docker container inspect --format '{{ index .Config.Labels "io.evalhub.kind-cluster" }}' "$REGISTRY_NAME")" ||
      fail "Could not inspect local registry container $REGISTRY_NAME."
  fi
  printf '%s' "$owner"
}

validate_inputs() {
  [[ "$KIND_CLUSTER_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail "Invalid kind cluster name: $KIND_CLUSTER_NAME"
  ((${#KIND_CLUSTER_NAME} <= 63)) || fail "KIND_CLUSTER_NAME must be at most 63 characters."
  case "$DELETE_CLUSTER" in true|false) ;; *) fail "DELETE_CLUSTER must be true or false." ;; esac
}

check_prerequisites() {
  local tool
  for tool in docker kind kubectl env grep awk cksum date mktemp chmod rm rmdir; do
    command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
  done
  docker info >/dev/null 2>&1 || fail "Docker daemon is not reachable. Run this from the terminal where Docker works."
}

check_registry_ownership() {
  local owner containers
  containers="$(docker container ls --all --format '{{.Names}}')" || fail "Could not list Docker containers."
  if ! printf '%s\n' "$containers" | grep -Fqx -- "$REGISTRY_NAME"; then
    return 0
  fi
  owner="$(registry_owner)"
  [[ "$owner" == "$KIND_CLUSTER_NAME" ]] ||
    fail "Container $REGISTRY_NAME exists but is not marked as this script's registry; refusing to remove it."
}

remove_registry() {
  local containers
  containers="$(docker container ls --all --format '{{.Names}}')" || fail "Could not list Docker containers."
  if printf '%s\n' "$containers" | grep -Fqx -- "$REGISTRY_NAME"; then
    log INFO "Removing script-managed local image registry $REGISTRY_NAME."
    docker container rm --force "$REGISTRY_NAME" >/dev/null
    log PASS "Removed the local registry container. Locally built images remain available in Docker."
  else
    log INFO "No local registry container exists for cluster $KIND_CLUSTER_NAME."
  fi
}

prepare_kubeconfig() {
  local context
  if [[ -e "$PRIVATE_KUBECONFIG" ]]; then
    [[ ! -L "$PRIVATE_KUBECONFIG" ]] || fail "Refusing to use kubeconfig symlink: $PRIVATE_KUBECONFIG"
    KUBECONFIG_FILE="$PRIVATE_KUBECONFIG"
    context="$(kubectl --kubeconfig "$KUBECONFIG_FILE" config current-context 2>/dev/null || true)"
    [[ "$context" == "kind-$KIND_CLUSTER_NAME" ]] ||
      fail "The private kubeconfig does not target kind-$KIND_CLUSTER_NAME: $PRIVATE_KUBECONFIG"
  else
    WORK_DIR="$(mktemp -d /tmp/evalhub-dev-clean.XXXXXX)"
    KUBECONFIG_FILE="$WORK_DIR/kubeconfig"
    log INFO "No private kubeconfig exists; exporting a temporary one for this cleanup."
    kind_cmd export kubeconfig --name "$KIND_CLUSTER_NAME" --kubeconfig "$KUBECONFIG_FILE"
    chmod 600 "$KUBECONFIG_FILE"
    context="$(kubectl --kubeconfig "$KUBECONFIG_FILE" config current-context 2>/dev/null || true)"
    [[ "$context" == "kind-$KIND_CLUSTER_NAME" ]] || fail "kind exported an unexpected kubeconfig context: $context"
  fi
  kubectl --kubeconfig "$KUBECONFIG_FILE" get nodes >/dev/null ||
    fail "Cannot reach kind cluster $KIND_CLUSTER_NAME with its private kubeconfig."
}

has_legacy_deployment_markers() {
  [[ "$(resource_owner namespace "$NAMESPACE")" == "$KIND_CLUSTER_NAME" ]] &&
    [[ "$(resource_owner deployment trustyai-service-operator-controller-manager "$NAMESPACE")" == "$KIND_CLUSTER_NAME" ]] &&
    [[ "$(resource_owner service evalhub-local-nodeport "$NAMESPACE")" == "$KIND_CLUSTER_NAME" ]]
}

validate_legacy_resource() {
  local resource="$1" name="$2" namespace="${3:-}" owner
  resource_exists "$resource" "$name" "$namespace" || return 0
  owner="$(resource_owner "$resource" "$name" "$namespace")"
  if [[ "$owner" == "$KIND_CLUSTER_NAME" ]]; then
    return 0
  fi
  [[ -z "$owner" ]] && has_legacy_deployment_markers ||
    fail "$resource/$name is not marked as this deployment; refusing to delete it."
  log INFO "Recognized legacy unannotated $resource/$name using the deployment's namespace, operator, and NodePort ownership markers."
}

validate_evalhub_instances() {
  local instances entry resource_owner_value
  kubectl --kubeconfig "$KUBECONFIG_FILE" get crd evalhubs.trustyai.opendatahub.io \
    >/dev/null 2>&1 || return 0
  instances="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get evalhubs --all-namespaces \
    -o "go-template={{range .items}}{{.metadata.namespace}}/{{.metadata.name}}{{\"\\t\"}}{{if .metadata.annotations}}{{index .metadata.annotations \"${OWNERSHIP_ANNOTATION}\"}}{{end}}{{\"\\n\"}}{{end}}")" ||
    fail "Could not list EvalHub custom resources before cleanup."
  while IFS=$'\t' read -r entry resource_owner_value; do
    [[ -n "$entry" ]] || continue
    [[ "$entry" == "$NAMESPACE/evalhub-local" ]] ||
      fail "Found another EvalHub resource ($entry). Refusing cleanup because deleting the operator/CRD could affect it."
    [[ "$resource_owner_value" == "$KIND_CLUSTER_NAME" ]] ||
      validate_legacy_resource evalhub evalhub-local "$NAMESPACE"
  done <<<"$instances"
}

delete_owned_resources() {
  local resource="$1" namespace="${2:-}" names name template
  local -a namespace_args=()
  [[ -z "$namespace" ]] || namespace_args=(-n "$namespace")
  template="{{range .items}}{{if .metadata.annotations}}{{if eq (index .metadata.annotations \"${OWNERSHIP_ANNOTATION}\") \"${KIND_CLUSTER_NAME}\"}}{{.metadata.name}}{{\"\\n\"}}{{end}}{{end}}{{end}}"
  names="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" "${namespace_args[@]}" \
    --ignore-not-found -o "go-template=$template")" ||
    fail "Could not list $resource resources for ownership-safe cleanup."
  while IFS= read -r name; do
    [[ -n "$name" ]] || continue
    log INFO "Deleting owned $resource/$name${namespace:+ in namespace $namespace}."
    kubectl --kubeconfig "$KUBECONFIG_FILE" delete "$resource" "$name" "${namespace_args[@]}" \
      --wait=true --timeout=180s >/dev/null
  done <<<"$names"
}

delete_legacy_resource_if_unannotated() {
  local resource="$1" name="$2" namespace="${3:-}" owner
  local -a namespace_args=()
  [[ -z "$namespace" ]] || namespace_args=(-n "$namespace")
  resource_exists "$resource" "$name" "$namespace" || return 0
  owner="$(resource_owner "$resource" "$name" "$namespace")"
  [[ "$owner" == "$KIND_CLUSTER_NAME" ]] && return 0
  [[ -z "$owner" ]] && has_legacy_deployment_markers ||
    fail "$resource/$name ownership changed during cleanup; refusing to delete it."
  log INFO "Deleting legacy unannotated $resource/$name."
  kubectl --kubeconfig "$KUBECONFIG_FILE" delete "$resource" "$name" "${namespace_args[@]}" \
    --wait=true --timeout=180s >/dev/null
}

delete_evalhub_resource() {
  local owner
  resource_exists evalhub evalhub-local "$NAMESPACE" || return 0
  owner="$(resource_owner evalhub evalhub-local "$NAMESPACE")"
  if [[ "$owner" != "$KIND_CLUSTER_NAME" ]]; then
    [[ -z "$owner" ]] && has_legacy_deployment_markers ||
      fail "EvalHub/evalhub-local ownership changed during cleanup; refusing to delete it."
  fi
  log INFO "Deleting EvalHub/evalhub-local and waiting for the operator to remove owned resources."
  kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" delete evalhub evalhub-local \
    --wait=true --timeout=180s >/dev/null
}

delete_evalhub_workloads() {
  local resource names item object_namespace object_name selector
  selector='app=evalhub,component=evaluation-job,evalhub_instance_name=evalhub-local,evalhub_instance_namespace=system'
  for resource in job networkpolicy configmap secret; do
    names="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get "$resource" --all-namespaces \
      --selector="$selector" --ignore-not-found \
      -o 'go-template={{range .items}}{{.metadata.namespace}}/{{.metadata.name}}{{"\n"}}{{end}}')" ||
      fail "Could not list $resource resources for EvalHub instance evalhub-local."
    while IFS=/ read -r object_namespace object_name; do
      [[ -n "$object_namespace" && -n "$object_name" ]] || continue
      log INFO "Deleting evalhub-local workload $resource/$object_name in namespace $object_namespace."
      if [[ "$resource" == job ]]; then
        kubectl --kubeconfig "$KUBECONFIG_FILE" delete "$resource" "$object_name" -n "$object_namespace" \
          --cascade=foreground --wait=true --timeout=180s >/dev/null
      else
        kubectl --kubeconfig "$KUBECONFIG_FILE" delete "$resource" "$object_name" -n "$object_namespace" \
          --wait=true --timeout=180s >/dev/null
      fi
    done <<<"$names"
  done
}

remove_registry_mirror_config() {
  local node nodes
  nodes="$(kind_cmd get nodes --name "$KIND_CLUSTER_NAME")" || fail "Could not list kind nodes for registry mirror cleanup."
  while IFS= read -r node; do
    [[ -n "$node" ]] || continue
    log INFO "Removing this deployment's registry mirror configuration from kind node $node."
    docker exec "$node" sh -c '
      alias="$1"
      for hosts_file in /etc/containerd/certs.d/localhost:*/hosts.toml; do
        [ -f "$hosts_file" ] || continue
        if grep -Fq -- "$alias" "$hosts_file"; then
          config_dir=${hosts_file%/*}
          rm -f -- "$hosts_file"
          rmdir -- "$config_dir" 2>/dev/null || true
        fi
      done
    ' sh "$REGISTRY_ALIAS"
  done <<<"$nodes"
}

clean_deployment_keep_cluster() {
  local remaining_evalhubs resource
  prepare_kubeconfig
  validate_evalhub_instances
  delete_evalhub_workloads

  if ! resource_exists namespace "$NAMESPACE"; then
    log INFO "Namespace '$NAMESPACE' does not exist; skipping namespaced deployment cleanup."
    for resource in validatingwebhookconfiguration mutatingwebhookconfiguration clusterrolebinding clusterrole crd; do
      delete_owned_resources "$resource"
    done
    remove_registry_mirror_config
    remove_registry
    log PASS "Removed the EvalHub development resources. Kind cluster '$KIND_CLUSTER_NAME' is retained."
    return
  fi

  # These fixed-name resources were created before the deployment script added
  # ownership annotations. They are safe to recognize only when the namespace,
  # operator Deployment, and host NodePort Service all carry this cluster's mark.
  validate_legacy_resource secret webhook-server-cert "$NAMESPACE"
  validate_legacy_resource secret evalhub-local-tls "$NAMESPACE"
  validate_legacy_resource configmap evalhub-local-service-ca "$NAMESPACE"
  validate_legacy_resource configmap evalhub-kind-openai-mock-code "$NAMESPACE"

  delete_evalhub_resource
  if resource_exists deployment evalhub-local "$NAMESPACE"; then
    log INFO "Waiting for the EvalHub controller to remove its owned service Deployment."
    kubectl --kubeconfig "$KUBECONFIG_FILE" -n "$NAMESPACE" wait \
      --for=delete deployment/evalhub-local --timeout=180s ||
      fail "The EvalHub Deployment remained after deleting the EvalHub custom resource."
  fi

  for resource in deployment service networkpolicy configmap secret serviceaccount rolebinding role; do
    delete_owned_resources "$resource" "$NAMESPACE"
  done

  delete_legacy_resource_if_unannotated secret webhook-server-cert "$NAMESPACE"
  delete_legacy_resource_if_unannotated secret evalhub-local-tls "$NAMESPACE"
  delete_legacy_resource_if_unannotated configmap evalhub-local-service-ca "$NAMESPACE"
  delete_legacy_resource_if_unannotated configmap evalhub-kind-openai-mock-code "$NAMESPACE"

  remaining_evalhubs=""
  if kubectl --kubeconfig "$KUBECONFIG_FILE" get crd evalhubs.trustyai.opendatahub.io \
    >/dev/null 2>&1; then
    remaining_evalhubs="$(kubectl --kubeconfig "$KUBECONFIG_FILE" get evalhubs --all-namespaces \
      -o 'go-template={{range .items}}{{.metadata.namespace}}/{{.metadata.name}}{{"\n"}}{{end}}')" ||
      fail "Could not verify that EvalHub resources were removed."
  fi
  if [[ -n "$remaining_evalhubs" ]]; then
    fail "EvalHub resource(s) remain after cleanup: ${remaining_evalhubs//$'\n'/, }. Cluster-scoped operator resources were retained."
  fi

  for resource in validatingwebhookconfiguration mutatingwebhookconfiguration clusterrolebinding clusterrole crd; do
    delete_owned_resources "$resource"
  done

  remove_registry_mirror_config
  remove_registry
  log PASS "Removed the EvalHub development deployment. Kind cluster '$KIND_CLUSTER_NAME' and its kubeconfig are retained."
  log INFO "Namespace '$NAMESPACE' is retained to avoid deleting resources not owned by this deployment."
  log INFO "Host images and Docker build cache are retained for faster redeployment."
}

delete_cluster() {
  local remove_kubeconfig=false context
  if [[ -e "$PRIVATE_KUBECONFIG" ]]; then
    if [[ -L "$PRIVATE_KUBECONFIG" ]]; then
      log WARN "The private kubeconfig path is a symlink; it will be preserved."
    else
      context="$(kubectl --kubeconfig "$PRIVATE_KUBECONFIG" config current-context 2>/dev/null || true)"
      if [[ "$context" == "kind-$KIND_CLUSTER_NAME" ]]; then
        remove_kubeconfig=true
      else
        log WARN "The kubeconfig at $PRIVATE_KUBECONFIG has a different context and will be preserved."
      fi
    fi
  fi

  if cluster_exists; then
    log INFO "Deleting the explicitly selected kind cluster '$KIND_CLUSTER_NAME'."
    kind_cmd delete cluster --name "$KIND_CLUSTER_NAME"
    log PASS "Deleted kind cluster '$KIND_CLUSTER_NAME'."
  else
    log INFO "Kind cluster '$KIND_CLUSTER_NAME' does not exist; nothing to delete."
  fi

  if [[ "$remove_kubeconfig" == true ]]; then
    rm -f -- "$PRIVATE_KUBECONFIG"
    log INFO "Removed this cluster's private kubeconfig."
  fi
  remove_registry
  log PASS "Cluster deletion is complete. Host images and Docker build cache are retained."
}

main() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      --help|-h) usage; return 0 ;;
      --delete-cluster) DELETE_CLUSTER=true ;;
      *) fail "Unexpected argument: $arg. Run make dev-clean-help for supported options." ;;
    esac
  done

  validate_inputs
  check_prerequisites
  check_registry_ownership
  trap 'status=$?; trap - EXIT; if [[ -n "$WORK_DIR" && -d "$WORK_DIR" ]]; then rm -f -- "$WORK_DIR/kubeconfig"; rmdir -- "$WORK_DIR" 2>/dev/null || true; fi; exit "$status"' EXIT

  if [[ "$DELETE_CLUSTER" == true ]]; then
    delete_cluster
  elif cluster_exists; then
    log INFO "Cleaning the EvalHub development deployment and retaining kind cluster '$KIND_CLUSTER_NAME'."
    clean_deployment_keep_cluster
  else
    log INFO "Kind cluster '$KIND_CLUSTER_NAME' does not exist; no Kubernetes resources need cleanup."
    remove_registry
  fi
}

main "$@"
