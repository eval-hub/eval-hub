#!/usr/bin/env bash

# Reclaim container runtime storage left behind by the local dev stack.
#
# `make dev-deploy` (and therefore `make test-benchmarks-kind`) builds several
# images on every run and pushes copies into the local registry and the kind
# node, so repeated runs accumulate dangling images, build containers, and
# unused volumes until an image build fails with "no space left on device".
# This script removes that unreferenced data for podman or Docker.
set -Eeuo pipefail

CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-}"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-evalhub-dev}"
ALL="${ALL:-false}"
VOLUMES="${VOLUMES:-false}"
DRY_RUN="${DRY_RUN:-false}"
MIN_FREE_GB="${MIN_FREE_GB:-15}"

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
  make clean-container-storage
  make clean-container-storage ALL=true VOLUMES=true
  scripts/clean-container-storage.sh [--all] [--volumes] [--dry-run]

Removes unreferenced container data from the runtime that backs the local dev
stack (podman or Docker): stopped containers, build containers, and dangling
images, plus unused volumes with VOLUMES=true and unused non-dangling images
with ALL=true. Images and volumes used by running containers are never removed,
so a running kind cluster keeps working.

Inputs (also accepted as environment variables):
  CONTAINER_RUNTIME  container engine to clean (podman or docker; default: the
                     engine that already owns KIND_CLUSTER_NAME, otherwise podman
                     when reachable, or docker under WSL)
  KIND_CLUSTER_NAME  cluster used to select that engine (default: evalhub-dev)
  ALL                true to also remove unused (non-dangling) images such as
                     cached base images and previous dev image tags (default:
                     false)
  VOLUMES            true to also remove unused volumes, including named ones
                     (default: false)
  DRY_RUN            true to print the commands instead of running them
                     (default: false)
  MIN_FREE_GB        warn when container storage still holds less free space
                     than this afterwards (default: 15)

Space held by a kind cluster is released only by `make dev-clean
DELETE_CLUSTER=true`: the node's image store and the local registry live in
volumes that the running containers keep in use. Temporary localhost:<port> tags
from `make dev-deploy` are removed by `make dev-clean`.
USAGE
}

# Return 0 when KIND_CLUSTER_NAME exists under the given container runtime,
# using the kind provider that runtime requires. A runtime that is not installed
# or not reachable cannot own a cluster, so it reports false.
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

# Select the runtime to clean. Set CONTAINER_RUNTIME to force a specific engine.
# Otherwise an existing KIND_CLUSTER_NAME decides the runtime, so cleanup targets
# the engine that backed the deployment even when CONTAINER_RUNTIME was only
# passed to dev-deploy. With no existing cluster, podman is preferred whenever
# it is installed and reachable, except under WSL where docker is preferred
# instead (matching detect_container_runtime in dev-deploy.sh).
detect_container_runtime() {
  local runtime
  local -a preferred_runtimes=()
  if uname -r | grep -qiE 'microsoft|wsl'; then
    preferred_runtimes=(docker podman)
  else
    preferred_runtimes=(podman docker)
  fi
  case "$CONTAINER_RUNTIME" in
    podman|docker)
      command -v "$CONTAINER_RUNTIME" >/dev/null 2>&1 ||
        fail "Required command not found: $CONTAINER_RUNTIME"
      "$CONTAINER_RUNTIME" info >/dev/null 2>&1 ||
        fail "$CONTAINER_RUNTIME is installed but not reachable. Start the $CONTAINER_RUNTIME daemon (or podman machine) and retry."
      ;;
    "")
      for runtime in "${preferred_runtimes[@]}"; do
        if kind_cluster_in_runtime "$runtime"; then
          CONTAINER_RUNTIME="$runtime"
          log INFO "Kind cluster '$KIND_CLUSTER_NAME' exists under $runtime; cleaning up with $runtime."
          break
        fi
      done
      if [[ -z "$CONTAINER_RUNTIME" ]]; then
        for runtime in "${preferred_runtimes[@]}"; do
          if command -v "$runtime" >/dev/null 2>&1 && "$runtime" info >/dev/null 2>&1; then
            CONTAINER_RUNTIME="$runtime"
            break
          fi
        done
      fi
      [[ -n "$CONTAINER_RUNTIME" ]] ||
        fail "Neither podman nor docker is reachable. Install podman (preferred; docker under WSL) and make sure it is running."
      ;;
    *)
      fail "CONTAINER_RUNTIME must be 'podman' or 'docker'."
      ;;
  esac
  log INFO "Using $CONTAINER_RUNTIME as the container runtime."
}

validate_inputs() {
  case "$ALL" in true|false) ;; *) fail "ALL must be true or false." ;; esac
  case "$VOLUMES" in true|false) ;; *) fail "VOLUMES must be true or false." ;; esac
  case "$DRY_RUN" in true|false) ;; *) fail "DRY_RUN must be true or false." ;; esac
  [[ "$MIN_FREE_GB" =~ ^[0-9]+$ ]] || fail "MIN_FREE_GB must be a whole number of GiB."
  [[ "$KIND_CLUSTER_NAME" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] ||
    fail "Invalid kind cluster name: $KIND_CLUSTER_NAME"
}

check_prerequisites() {
  local tool
  for tool in grep awk date uname df head; do
    command -v "$tool" >/dev/null 2>&1 || fail "Required command not found: $tool"
  done
  detect_container_runtime
}

# Free space, in KiB, of the filesystem that holds container storage. Podman on
# macOS and Docker Desktop keep storage inside a VM, so the runtime's own report
# is used when the path is not visible from the host; returning non-zero only
# suppresses the free-space hints.
storage_free_kb() {
  local free path=""
  case "$CONTAINER_RUNTIME" in
    podman)
      if podman machine info >/dev/null 2>&1 &&
        [[ -n "$(podman machine list --format '{{.Name}}' 2>/dev/null | head -n 1)" ]]; then
        free="$(podman machine ssh 'df -Pk /' 2>/dev/null | tail -n 1 | awk '{print $4}')" || return 1
        [[ "$free" =~ ^[0-9]+$ ]] || return 1
        printf '%s' "$free"
        return 0
      fi
      path="$(podman info --format '{{.Store.GraphRoot}}' 2>/dev/null || true)"
      ;;
    docker)
      path="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || true)"
      ;;
  esac
  [[ -n "$path" && -d "$path" ]] || return 1
  free="$(df -Pk "$path" 2>/dev/null | tail -n 1 | awk '{print $4}')" || return 1
  [[ "$free" =~ ^[0-9]+$ ]] || return 1
  printf '%s' "$free"
}

free_kb_to_gib() {
  awk -v kb="$1" 'BEGIN {printf "%.1f GiB", kb / 1048576}'
}

report_storage() {
  local label="$1" free
  log INFO "$label"
  "$CONTAINER_RUNTIME" system df || log WARN "Could not report $CONTAINER_RUNTIME storage usage."
  if free="$(storage_free_kb)"; then
    log INFO "Free space left in container storage: $(free_kb_to_gib "$free")."
  fi
}

# Volume pruning removes any volume no container references, so a stopped kind
# node or local registry would lose its data. Running containers are never
# affected, so only stopped ones are reported here.
warn_about_stopped_containers() {
  local name stopped
  [[ "$VOLUMES" == true ]] || return 0
  stopped="$("$CONTAINER_RUNTIME" ps --all --filter status=exited --filter status=created \
    --format '{{.Names}}' 2>/dev/null || true)"
  [[ -n "$stopped" ]] || return 0
  log WARN "Stopped containers found; volumes only they reference would be removed:"
  while IFS= read -r name; do
    [[ -n "$name" ]] && log WARN "  $name"
  done <<<"$stopped"
  log WARN "Start them first (for example 'make dev-deploy') or omit VOLUMES=true to keep those volumes."
}

# Podman's --build also removes build containers left behind by an interrupted
# (for example disk-full) image build.
build_prune_args() {
  local -a args=(system prune)
  [[ "$ALL" == true ]] && args+=(-a)
  [[ "$CONTAINER_RUNTIME" == podman ]] && args+=(--build)
  args+=(-f)
  [[ "$VOLUMES" == true ]] && args+=(--volumes)
  printf '%s\n' "${args[@]}"
}

run_prune() {
  local arg
  local -a args=()
  while IFS= read -r arg; do
    args+=("$arg")
  done < <(build_prune_args)
  if [[ "$DRY_RUN" == true ]]; then
    log INFO "DRY_RUN=true; would run: $CONTAINER_RUNTIME ${args[*]}"
    if [[ "$VOLUMES" == true && "$CONTAINER_RUNTIME" == docker ]]; then
      log INFO "DRY_RUN=true; would run: docker volume prune -f"
    fi
    return 0
  fi
  log INFO "Running: $CONTAINER_RUNTIME ${args[*]}"
  "$CONTAINER_RUNTIME" "${args[@]}" || fail "$CONTAINER_RUNTIME ${args[*]} failed."
  # Docker's system prune only reclaims anonymous volumes; unused named volumes
  # need an explicit prune, which is what podman's --volumes already covers.
  if [[ "$VOLUMES" == true && "$CONTAINER_RUNTIME" == docker ]]; then
    log INFO "Running: docker volume prune -f"
    docker volume prune -f || fail "docker volume prune -f failed."
  fi
}

# Warn when an image build would still run out of space, and point at the fix for
# the runtime in use.
check_remaining_space() {
  local free min_kb
  free="$(storage_free_kb)" || return 0
  min_kb=$((MIN_FREE_GB * 1048576))
  if ((free < min_kb)); then
    log WARN "Only $(free_kb_to_gib "$free") free; image builds need at least ${MIN_FREE_GB} GiB of headroom."
    if [[ "$CONTAINER_RUNTIME" == podman ]] && podman machine info >/dev/null 2>&1; then
      log WARN "Free more space, or grow the podman machine disk (podman machine stop; podman machine set --disk-size 200; podman machine start) and grow the guest filesystem afterwards (growpart, xfs_growfs)."
    else
      log WARN "Free more space, or give $CONTAINER_RUNTIME more disk."
    fi
    return 0
  fi
  log PASS "$(free_kb_to_gib "$free") free in container storage."
}

main() {
  local arg
  for arg in "$@"; do
    case "$arg" in
      --help|-h) usage; return 0 ;;
      --all) ALL=true ;;
      --volumes) VOLUMES=true ;;
      --dry-run) DRY_RUN=true ;;
      *) fail "Unexpected argument: $arg. Run make clean-container-storage-help for supported options." ;;
    esac
  done

  validate_inputs
  check_prerequisites
  report_storage "Container storage before cleanup:"
  warn_about_stopped_containers
  run_prune
  if [[ "$DRY_RUN" == false ]]; then
    report_storage "Container storage after cleanup:"
  fi
  log INFO "Space held by kind cluster '$KIND_CLUSTER_NAME' is released only by 'make dev-clean DELETE_CLUSTER=true'."
  check_remaining_space
}

main "$@"
