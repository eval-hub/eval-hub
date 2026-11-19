#!/usr/bin/env bash

# Build the curated adapter images for this machine's architecture and push them
# to a registry the caller owns, so a kind cluster can run them natively.
#
# The curated community images are published for linux/amd64 only. Running them
# on an Apple silicon machine means qemu-user-static emulation inside the podman
# machine, which is slow and, for some adapters, fatal: the Inspect and LM
# Evaluation Harness images abort their Python subprocesses with
# `qemu: uncaught target signal 11 (Segmentation fault)`. Building the same
# sources for the host architecture and pushing them to a registry removes the
# emulation, and the printed ADAPTER_IMAGES value replaces the published
# references a deploy or a benchmark sweep would otherwise pull.
#
# The build recipes (source checkout, Containerfile, image name) are the ones
# dev-deploy uses, from scripts/lib/adapter-build.sh, so a pushed image is the
# image dev-deploy would have built for itself.
set -Eeuo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EVALHUB_REPO="$(cd "${SCRIPT_DIR}/.." && pwd)"
EVALHUB_REPO_PARENT="$(dirname "${EVALHUB_REPO}")"

ADAPTERS="${ADAPTERS:-followbench}"
ADAPTER_IMAGE_REGISTRY="${ADAPTER_IMAGE_REGISTRY:-}"
ADAPTER_IMAGE_TAG="${ADAPTER_IMAGE_TAG:-latest}"
CONTAINER_RUNTIME="${CONTAINER_RUNTIME:-}"
DEV_REPO_PARENT_PATHS="${DEV_REPO_PARENT_PATHS:-${HOME}/code}"
CONTRIB_REPO=""
LMES_REPO=""
REGISTRY_PREFIX=""
SELECTED_ADAPTERS=()

# Runtime selection helpers, shared with the other dev scripts.
. "${SCRIPT_DIR}/lib/dev-runtime.sh"
# Adapter build recipes, shared with dev-deploy.
. "${SCRIPT_DIR}/lib/adapter-build.sh"

# Diagnostics go to stderr: stdout carries only the ADAPTER_IMAGES value, so a
# caller can substitute this script's output the way it substitutes
# scripts/adapter-images.sh.
log() {
  printf '[%s] [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "$1" "$2" >&2
}

fail() {
  log FAIL "$1"
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: make adapter-images-push [ADAPTERS=value ADAPTER_IMAGE_REGISTRY=value ...]

Builds the curated adapter images for this machine's architecture and pushes
them to a registry the caller owns, then prints the ADAPTER_IMAGES value for the
pushed images. The images come from the sibling source checkouts dev-deploy
uses, with the same Containerfile and the same image names, so a deployment
published from them is identical to one built locally.

  podman login quay.io
  make adapter-images-push ADAPTER_IMAGE_REGISTRY=quay.io/<your-user>

  # then, with the printed value:
  make test-benchmarks-kind SKIP_BUILD=true ADAPTERS=all \
    ADAPTER_IMAGES='inspect=quay.io/<your-user>/community-inspect:latest,...'

Inputs (also accepted as environment variables):
  ADAPTERS                 adapters to build (default: followbench; the make
                           target defaults to all), as 'all' or a comma-separated
                           list of the curated adapters: inspect,
                           lm-evaluation-harness, lighteval, ruler, ifbench,
                           followbench. garak and garak-kfp are not curated
                           adapters and keep the operator-packaged image.
  ADAPTER_IMAGE_REGISTRY   registry and namespace to push into, as
                           <registry>[:port]/<namespace> with a single namespace
                           and no tag or digest, for example quay.io/<your-user>.
                           The adapter image name becomes the repository, so
                           inspect is pushed as .../community-inspect:<tag>.
                           Defaults to quay.io/<the account the container
                           runtime is logged in to>.
  ADAPTER_IMAGE_TAG        tag for the pushed images (default: latest). Use a
                           fixed tag, for example the git revision, for a run you
                           need to reproduce.
  CONTAINER_RUNTIME        podman or docker (default: the first one reachable)
  DEV_REPO_PARENT_PATHS    colon-separated parent directories searched for the
                           eval-hub-contrib and lm-evaluation-harness checkouts
                           after the sibling of this checkout (default: ~/code)

The build targets the architecture the container runtime reports, not the host
shell, and passes it as --platform, so an arm64 podman machine builds arm64
natively and never falls back to emulation. Nothing is pulled: a missing source
checkout fails the run with the paths it searched.

Pushing needs credentials for the registry (podman login quay.io), and a Quay
repository that does not exist yet needs repository auto-creation enabled on the
account, or must be created first.
USAGE
}

trim_whitespace() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

# find_repo_checkout prints the checkout of a sibling repository, looking next to
# this checkout first and then under each DEV_REPO_PARENT_PATHS entry. It matches
# dev-deploy, so both scripts build from the same source tree.
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

# adapter_selected reports whether an adapter is already in the selection, so a
# duplicated ADAPTERS entry fails instead of building the same image twice.
#
# The loop is indexed rather than `for selected in "${SELECTED_ADAPTERS[@]}"`
# because this runs before the first entry is recorded: bash 3.2, which is what a
# stock macOS `/bin/bash` provides, treats an empty `"${array[@]}"` as an unbound
# variable under `set -u` and would abort instead of reporting a duplicate.
adapter_selected() {
  local index
  for ((index = 0; index < ${#SELECTED_ADAPTERS[@]}; index++)); do
    [[ "${SELECTED_ADAPTERS[${index}]}" == "$1" ]] && return 0
  done
  return 1
}

parse_adapters() {
  local requested adapter
  local -a requested_adapters=()

  ADAPTERS="$(trim_whitespace "$ADAPTERS")"
  if [[ "$ADAPTERS" == all ]]; then
    while IFS= read -r adapter; do
      requested_adapters+=("$adapter")
    done < <(curated_adapters)
  else
    [[ -n "$ADAPTERS" ]] || fail "ADAPTERS must be 'all' or a comma-separated list of supported adapters."
    IFS=, read -r -a requested_adapters <<<"$ADAPTERS"
  fi

  SELECTED_ADAPTERS=()
  for requested in "${requested_adapters[@]}"; do
    adapter="$(trim_whitespace "$requested")"
    case "$adapter" in
      "") fail "ADAPTERS contains an empty entry. Use one or more supported adapter names." ;;
      *) curated_adapter_supported "$adapter" ||
        fail "Unsupported adapter '$adapter' in ADAPTERS. Supported values: $(curated_adapter_list)." ;;
    esac
    adapter_selected "$adapter" && fail "ADAPTERS contains duplicate entry '$adapter'."
    SELECTED_ADAPTERS+=("$adapter")
  done
}

# select_runtime picks the engine used for the build and the push. No cluster is
# involved, so an explicit CONTAINER_RUNTIME wins and otherwise the first
# reachable engine in the dev scripts' preference order is used.
select_runtime() {
  if [[ -n "$CONTAINER_RUNTIME" ]]; then
    case "$CONTAINER_RUNTIME" in
      podman|docker) ;;
      *) fail "CONTAINER_RUNTIME must be podman or docker: $CONTAINER_RUNTIME" ;;
    esac
    runtime_is_reachable "$CONTAINER_RUNTIME" ||
      fail "CONTAINER_RUNTIME=$CONTAINER_RUNTIME is not installed or not reachable."
  else
    CONTAINER_RUNTIME="$(first_reachable_runtime)" ||
      fail "Neither podman nor docker is reachable. Install podman (or docker) and make sure it is running."
  fi
  CONTAINER_RUNTIME="$(canonical_runtime "$CONTAINER_RUNTIME")"
}

# require_repositories resolves only the checkouts the selection needs, so
# pushing one contrib adapter does not require the LM Evaluation Harness fork.
require_repositories() {
  local adapter
  for adapter in "${SELECTED_ADAPTERS[@]}"; do
    case "$adapter" in
      lm-evaluation-harness)
        [[ -n "$LMES_REPO" ]] || LMES_REPO="$(find_repo_checkout lm-evaluation-harness Dockerfile.lmes-job)"
        ;;
      *)
        [[ -n "$CONTRIB_REPO" ]] ||
          CONTRIB_REPO="$(find_repo_checkout eval-hub-contrib "adapters/${adapter}/Containerfile")"
        ;;
    esac
  done
}

# normalize_arch maps a runtime-reported or host architecture to a GOARCH name,
# and fails for anything it cannot map.
normalize_arch() {
  case "$1" in
    x86_64|amd64) printf 'amd64' ;;
    aarch64|arm64|arm64/v8) printf 'arm64' ;;
    *) return 1 ;;
  esac
}

# runtime_arch prints the architecture the container runtime builds for, as a
# GOARCH name. Asking the runtime rather than `uname` is what keeps the build
# native on macOS: the host shell can be arm64 while a `podman machine --arch
# amd64` VM is x86_64, and `--platform linux/arm64` in that VM would silently run
# the whole build under emulation.
runtime_arch() {
  local probe arch
  # podman reports .Host.Arch; docker reports .Architecture. A template the
  # engine does not understand produces no usable value, so each probe is tried
  # until one maps to a known architecture.
  for probe in '{{.Host.Arch}}' '{{.Architecture}}'; do
    arch="$("$CONTAINER_RUNTIME" info --format "$probe" 2>/dev/null || true)"
    arch="$(trim_whitespace "$arch")"
    if normalize_arch "$arch" >/dev/null 2>&1; then
      normalize_arch "$arch"
      return 0
    fi
  done
  arch="$(uname -m)"
  if normalize_arch "$arch" >/dev/null 2>&1; then
    log WARN "Could not read the build architecture from $CONTAINER_RUNTIME; using the host architecture $(normalize_arch "$arch")."
    normalize_arch "$arch"
    return 0
  fi
  fail "Unsupported build architecture: $arch"
}

# validate_registry_prefix accepts <registry>[:port]/<namespace> with a single
# lowercase namespace component and no tag or digest. The adapter image name is
# the whole repository, which is the shape Quay, GitHub Container Registry and
# Harbor use for an account; a nested path there is a different namespace whose
# pushes are rejected, so it fails during validation instead of after the images
# are built.
validate_registry_prefix() {
  local prefix="$1"
  local domain='[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]+)?'
  local component='[a-z0-9]+(([._]|__|-+)[a-z0-9]+)*'
  [[ "$prefix" =~ ^(${domain}/)?${component}$ ]]
}

# registry_prefix resolves and validates the destination namespace. Without an
# explicit value the account the runtime is logged in to is used, so a personal
# Quay registry needs no hard-coded user name.
registry_prefix() {
  local prefix login
  prefix="$(trim_whitespace "$ADAPTER_IMAGE_REGISTRY")"
  if [[ -z "$prefix" ]]; then
    login="$("$CONTAINER_RUNTIME" login --get-login quay.io 2>/dev/null || true)"
    login="$(trim_whitespace "$login")"
    [[ -n "$login" ]] ||
      fail "ADAPTER_IMAGE_REGISTRY is not set and $CONTAINER_RUNTIME is not logged in to quay.io. Run '$CONTAINER_RUNTIME login quay.io', or pass ADAPTER_IMAGE_REGISTRY=<registry>/<namespace> (for example quay.io/<your-user>)."
    prefix="quay.io/${login}"
    log INFO "Using quay.io/$login, from $CONTAINER_RUNTIME's quay.io login, as the destination namespace."
  fi
  validate_registry_prefix "$prefix" ||
    fail "ADAPTER_IMAGE_REGISTRY must be <registry>[:port]/<namespace> with one lowercase namespace and no tag or digest: '$prefix'. For a personal Quay account use quay.io/<your-user>; each adapter image name becomes the repository under it."
  printf '%s\n' "$prefix"
}

# destination_image prints the reference an adapter is built and pushed as.
destination_image() {
  local adapter="$1"
  printf '%s/%s:%s' "$REGISTRY_PREFIX" "$(adapter_image_name "$adapter")" "$ADAPTER_IMAGE_TAG"
}

push_image() {
  local destination="$1" host
  host="${REGISTRY_PREFIX%%/*}"
  log INFO "Pushing $destination"
  "$CONTAINER_RUNTIME" image push "$destination" ||
    fail "Could not push $destination. Authenticate with '$CONTAINER_RUNTIME login $host', and make sure the repository exists in $REGISTRY_PREFIX and allows pushes (Quay needs repository auto-creation enabled, or the repository created first)."
  log PASS "Pushed $destination"
}

main() {
  local adapter arch destination
  local -a pairs=()

  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    return 0
  fi
  (($# == 0)) || fail "Unexpected argument: $1. Run make adapter-images-push-help for supported inputs."

  parse_adapters
  select_runtime
  require_repositories
  REGISTRY_PREFIX="$(registry_prefix)"
  arch="$(runtime_arch)"
  log INFO "Building ${#SELECTED_ADAPTERS[@]} adapter image(s) for linux/$arch and pushing to $REGISTRY_PREFIX with tag $ADAPTER_IMAGE_TAG."

  for adapter in "${SELECTED_ADAPTERS[@]}"; do
    destination="$(destination_image "$adapter")"
    build_adapter_image "$adapter" "$arch" "$destination"
    push_image "$destination"
    pairs+=("$adapter=$destination")
  done

  # Print the value only once every image is pushed, so a caller substituting the
  # output never receives a partial ADAPTER_IMAGES list.
  printf '%s\n' "$(IFS=,; printf '%s' "${pairs[*]}")"
}

main "$@"
