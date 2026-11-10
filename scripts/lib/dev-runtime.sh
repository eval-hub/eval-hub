#!/usr/bin/env bash

# Container runtime selection shared by the dev scripts.
#
# dev-deploy records the engine that backs KIND_CLUSTER_NAME next to the private
# kubeconfig, and every dev script resolves the runtime from that record instead
# of probing both engines. These helpers resolve the engine, query kind with the
# matching provider, and read or write the record, so the four scripts share one
# implementation instead of four near-copies.
#
# Sourced by scripts/dev-deploy.sh, scripts/dev-clean.sh, scripts/dev-demo.sh and
# scripts/clean-container-storage.sh. The caller sets:
#
#   KIND_CLUSTER_NAME   cluster whose engine is resolved
#   RUNTIME_STATE_FILE  file holding that engine (see recorded_runtime)
#   CONTAINER_RUNTIME   explicit engine from the environment, or empty
#
# and provides log() and fail(), which the helpers use.

# kind_cmd runs kind with the provider the selected runtime needs.
kind_cmd() {
  if [[ "${CONTAINER_RUNTIME:-}" == podman ]]; then
    KIND_EXPERIMENTAL_PROVIDER=podman kind "$@"
  else
    env -u KIND_EXPERIMENTAL_PROVIDER kind "$@"
  fi
}

# canonical_runtime resolves a runtime name to the engine that actually runs it.
# The podman-docker package installs a `docker` wrapper that execs podman, so both
# names reach one engine and that wrapper reports itself through --version. The
# kind provider environment and the registry push flags depend on knowing it is
# really podman: treating the shim as docker would run kind with the docker
# provider and push to the plain-HTTP local registry without --tls-verify=false.
canonical_runtime() {
  local runtime="$1"
  if [[ "$runtime" == docker ]] && command -v docker >/dev/null 2>&1 &&
    docker --version 2>/dev/null | grep -qi podman; then
    printf 'podman\n'
  else
    printf '%s\n' "$runtime"
  fi
}

# kind_cluster_in_runtime returns 0 when KIND_CLUSTER_NAME exists under the given
# container runtime, using the kind provider that runtime requires. A runtime that
# is not installed or not reachable cannot own a cluster, so it reports false.
kind_cluster_in_runtime() {
  local runtime="$1" clusters
  command -v kind >/dev/null 2>&1 || return 1
  command -v "$runtime" >/dev/null 2>&1 || return 1
  "$runtime" info >/dev/null 2>&1 || return 1
  case "$runtime" in
    podman)
      clusters="$(KIND_EXPERIMENTAL_PROVIDER=podman kind get clusters 2>/dev/null || true)"
      ;;
    docker)
      clusters="$(env -u KIND_EXPERIMENTAL_PROVIDER kind get clusters 2>/dev/null || true)"
      ;;
    *)
      return 1
      ;;
  esac
  grep -Fqx -- "$KIND_CLUSTER_NAME" <<<"$clusters"
}

# ensure_runtime_owns_existing_cluster fails when the other engine lists
# KIND_CLUSTER_NAME and the selected one does not. kind only lists the clusters of
# the engine it is pointed at, so a command that continues under the selected
# engine would miss the running cluster: a deploy creates a second cluster with
# the same name and overwrites the private kubeconfig of the one that is running,
# and a cleanup reports the cluster absent, drops its record, and still removes
# registry state on an engine the user did not deploy with. The optional second
# argument names what the caller is about to do: "deploy" (the default), "clean",
# or "recorded" when the engine comes from the record, which fails with the stale
# record to remove instead of an "Unset CONTAINER_RUNTIME" hint.
ensure_runtime_owns_existing_cluster() {
  local runtime other purpose="${2:-deploy}"
  runtime="$(canonical_runtime "$1")"
  # A cluster of this name under the selected engine is used as-is, so the other
  # engine owning one too changes nothing: this run creates no cluster and
  # rewrites no kubeconfig, and a cleanup removes only the selected engine's
  # registry container and image tags.
  kind_cluster_in_runtime "$runtime" && return 0
  case "$runtime" in
    podman) other=docker ;;
    docker) other=podman ;;
    *) return 0 ;;
  esac
  kind_cluster_in_runtime "$other" || return 0
  if [[ "$purpose" == recorded ]]; then
    fail "Kind cluster '$KIND_CLUSTER_NAME' is recorded as $runtime-backed in $RUNTIME_STATE_FILE, but $runtime no longer runs it and $other does. Remove the stale record (rm -- '$RUNTIME_STATE_FILE') to reuse the $other cluster with this script, or remove it and run 'make dev-clean CONTAINER_RUNTIME=$other DELETE_CLUSTER=true' to delete that cluster."
  fi
  if [[ "$purpose" == clean ]]; then
    fail "Kind cluster '$KIND_CLUSTER_NAME' exists under $other, not $runtime. Unset CONTAINER_RUNTIME to clean up with the engine that owns the cluster, or set CONTAINER_RUNTIME=$other."
  fi
  fail "Kind cluster '$KIND_CLUSTER_NAME' exists under $other, not $runtime. Unset CONTAINER_RUNTIME to reuse the engine that owns the cluster, or set CONTAINER_RUNTIME=$other."
}

# recorded_runtime prints the engine dev-deploy recorded for KIND_CLUSTER_NAME, if
# any. The record keeps a redeploy, dev-clean, dev-demo, and
# clean-container-storage on the engine that backs the cluster without probing
# both engines for it.
recorded_runtime() {
  local recorded
  [[ -f "$RUNTIME_STATE_FILE" ]] || return 1
  recorded="$(head -n 1 "$RUNTIME_STATE_FILE" 2>/dev/null | tr -d '[:space:]')"
  case "$recorded" in
    podman|docker) printf '%s\n' "$recorded" ;;
    *) return 1 ;;
  esac
}

# record_runtime stores the engine that backs KIND_CLUSTER_NAME. Call it once the
# cluster exists, so a failed deploy does not claim an engine for a cluster it
# never created.
record_runtime() {
  local tmp
  # Write through a private temporary file and rename it into place, so a reader
  # never sees a partial record and a pre-placed symlink at a predictable path
  # cannot redirect the write. mktemp creates the file in the record's own
  # directory (same filesystem, so the rename stays atomic) with mode 600.
  tmp="$(mktemp "${RUNTIME_STATE_FILE}.tmp.XXXXXX")" ||
    fail "Could not create a temporary file for $RUNTIME_STATE_FILE."
  # Keep the record private whatever the caller's umask is, and remove the
  # temporary file if writing or renaming it fails.
  if ! printf '%s\n' "$CONTAINER_RUNTIME" >"$tmp" ||
    ! chmod 600 "$tmp" ||
    ! mv -f -- "$tmp" "$RUNTIME_STATE_FILE"; then
    rm -f -- "$tmp"
    fail "Could not record $CONTAINER_RUNTIME in $RUNTIME_STATE_FILE."
  fi
  log INFO "Recorded $CONTAINER_RUNTIME as the container runtime of kind cluster '$KIND_CLUSTER_NAME' in $RUNTIME_STATE_FILE."
}

# runtime_is_installed returns 0 when the CLI of the given runtime is installed. A
# record naming an engine that is no longer installed cannot describe a live
# cluster, so a caller ignores or drops that record instead of repeating a failure
# the user can only clear by editing the state file.
runtime_is_installed() {
  local engine
  engine="$(canonical_runtime "$1")"
  command -v "$engine" >/dev/null 2>&1
}

# runtime_is_reachable returns 0 when the given runtime is installed and answers.
# Unlike use_runtime it never exits, so a caller can fall back to another engine
# when the recorded one is stopped, uninstalled, or belongs to an unmounted podman
# machine.
runtime_is_reachable() {
  local engine
  engine="$(canonical_runtime "$1")"
  runtime_is_installed "$engine" || return 1
  "$engine" info >/dev/null 2>&1
}

# fallback_runtime_order prints the engine preference used when neither the record
# nor an existing cluster decides, one name per line: podman before docker, except
# under WSL where Docker is preferred, because WSL2 podman answers 'podman info'
# but kind node startup fails during systemd/cgroup setup.
fallback_runtime_order() {
  if uname -r 2>/dev/null | grep -qiE 'microsoft|wsl'; then
    printf 'docker\npodman\n'
  else
    printf 'podman\ndocker\n'
  fi
}

# use_runtime makes ENGINE the selected runtime after checking that it exists and
# is reachable. The optional second argument names what the runtime is used for,
# so the log line matches the script ("for builds, the local registry, and kind").
use_runtime() {
  local engine scope="${2:-}"
  engine="$(canonical_runtime "$1")"
  command -v "$engine" >/dev/null 2>&1 || fail "Required command not found: $engine"
  "$engine" info >/dev/null 2>&1 ||
    fail "$engine is installed but not reachable. Start the $engine daemon (or podman machine) and retry."
  CONTAINER_RUNTIME="$engine"
  if [[ -n "$scope" ]]; then
    log INFO "Using $CONTAINER_RUNTIME as the container runtime $scope."
  else
    log INFO "Using $CONTAINER_RUNTIME as the container runtime."
  fi
}
