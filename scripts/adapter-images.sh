#!/usr/bin/env bash

# Print the ADAPTER_IMAGES value for the adapters selected by ADAPTERS, built
# from the image reference each provider declares in config/providers. The
# result is one comma-separated list of adapter=image pairs on stdout, so it can
# be handed straight to dev-deploy:
#
#   make dev-deploy ADAPTERS=all ADAPTER_IMAGES="$(make adapter-images)"
#
# Nothing is pulled, built, or looked up against a registry: the pairs carry the
# tags the providers run (quay.io/evalhub/community-<adapter>:latest, and
# quay.io/opendatahub/ta-lmes-job:<tag> for lm-evaluation-harness), and a deploy
# pulls a pair only when the container runtime does not already have that
# reference, so a locally cached tag -- `latest` included -- is reused as-is. A
# provider that pins a digest is reproduced as pinned.
set -Eeuo pipefail
umask 077

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EVALHUB_REPO="$(cd "${SCRIPT_DIR}/.." && pwd)"

ADAPTERS="${ADAPTERS:-followbench}"
PROVIDERS_DIR="${ADAPTER_IMAGE_PROVIDERS_DIR:-${EVALHUB_REPO}/config/providers}"
# Adapters parse_adapters resolved, in the order they were given.
SELECTED_ADAPTERS=()

# Diagnostics go to stderr: stdout carries only the ADAPTER_IMAGES value.
log() {
  printf '[%s] [%s] %s\n' "$(date '+%Y-%m-%d %H:%M:%S%z')" "$1" "$2" >&2
}

fail() {
  log FAIL "$1"
  exit 1
}

usage() {
  cat <<'USAGE'
Usage: make adapter-images [ADAPTERS=value ...]

Prints the ADAPTER_IMAGES value for the adapters selected by ADAPTERS, using
the image reference each provider declares in config/providers:

  make adapter-images                     # all six curated adapters
  make adapter-images ADAPTERS=inspect,ruler

The output is a single comma-separated list on stdout, with no quoting, so it
can be substituted directly:

  make dev-deploy ADAPTERS=all ADAPTER_IMAGES="$(make adapter-images)"
  make test-benchmarks-kind ADAPTER_IMAGES="$(make adapter-images)"

Inputs (also accepted as environment variables):
  ADAPTERS                 adapters to emit (default: followbench), as 'all' or
                           a comma-separated list of the curated adapters:
                           inspect, lm-evaluation-harness, lighteval, ruler,
                           ifbench, followbench. The make target defaults to
                           ADAPTERS=all unless ADAPTERS is set on the command
                           line or in the environment.
  ADAPTER_IMAGE_PROVIDERS_DIR
                           directory holding the provider YAML definitions
                           (default: config/providers in this checkout)

The provider YAML is the source of truth for the image, so the pairs are the
references the providers run: quay.io/evalhub/community-<adapter>:latest for the
community adapters, and quay.io/opendatahub/ta-lmes-job:<tag> for
lm-evaluation-harness. The tags are copied as declared, so `latest` stays a
floating tag in the output; dev-deploy pulls a pair only when the container
runtime does not already have that reference, so a locally cached `latest` is
reused as-is instead of being refreshed from the registry. Pin a digest in the
provider YAML to make a pair immutable.

No network access, container runtime, or cluster is needed: nothing is pulled,
built, or resolved.
USAGE
}

trim_whitespace() {
  local value="$1"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

# provider_config_file maps an ADAPTERS name to the provider YAML that declares
# the image it runs. The names follow dev-deploy's ADAPTERS values, not the
# provider IDs, because ADAPTERS also selects the dev-deploy build.
provider_config_file() {
  case "$1" in
    inspect) printf 'inspect.yaml' ;;
    lm-evaluation-harness) printf 'lm_evaluation_harness.yaml' ;;
    lighteval) printf 'lighteval.yaml' ;;
    ruler) printf 'ruler.yaml' ;;
    ifbench) printf 'ifbench.yaml' ;;
    followbench) printf 'followbench.yaml' ;;
    *) return 1 ;;
  esac
}

# adapter_selected reports whether an adapter is already in the selection, so a
# duplicated ADAPTERS entry fails instead of emitting the same pair twice.
#
# The loop is indexed rather than `for selected in "${SELECTED_ADAPTERS[@]}"`
# because this runs before the first entry is recorded: bash 3.2, which is what a
# stock macOS `/bin/bash` provides, treats an empty `"${array[@]}"` as an unbound
# variable under `set -u` and would abort instead of reporting a duplicate. That
# would also break `make test-benchmarks-kind-latest`, which substitutes this
# script's output.
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
    requested_adapters=(inspect lm-evaluation-harness lighteval ruler ifbench followbench)
  else
    [[ -n "$ADAPTERS" ]] || fail "ADAPTERS must be 'all' or a comma-separated list of supported adapters."
    IFS=, read -r -a requested_adapters <<<"$ADAPTERS"
  fi

  SELECTED_ADAPTERS=()
  for requested in "${requested_adapters[@]}"; do
    adapter="$(trim_whitespace "$requested")"
    [[ -n "$adapter" ]] || fail "ADAPTERS contains an empty entry. Use one or more supported adapter names."
    provider_config_file "$adapter" >/dev/null ||
      fail "Unsupported adapter '$adapter' in ADAPTERS. Supported values: inspect, lm-evaluation-harness, lighteval, ruler, ifbench, followbench."
    adapter_selected "$adapter" && fail "ADAPTERS contains duplicate entry '$adapter'."
    SELECTED_ADAPTERS+=("$adapter")
  done
}

# provider_image prints the image reference the provider YAML declares for an
# adapter: the first `image:` value, without the surrounding quotes YAML allows
# and without a trailing YAML comment. A comment is not part of the scalar, and
# keeping it would emit a pair dev-deploy rejects; a '#' inside the quotes is
# data, so only an unquoted value loses its comment.
provider_image() {
  local adapter="$1" file scalar
  file="${PROVIDERS_DIR}/$(provider_config_file "$adapter")"
  [[ -f "$file" ]] || fail "Provider configuration not found for adapter '$adapter': $file"
  scalar="$(awk '
    /^[[:space:]]*image:[[:space:]]/ {
      sub(/^[[:space:]]*image:[[:space:]]*/, "")
      sub(/[[:space:]]+$/, "")
      print
      exit
    }' "$file")"
  [[ -n "$scalar" ]] || fail "Provider configuration has no runtime image for adapter '$adapter': $file"
  case "$scalar" in
    \"*)
      # Double-quoted scalar: the value ends at the closing quote, so anything
      # after it, including a comment, is not part of the reference.
      scalar="${scalar#\"}"
      scalar="${scalar%%\"*}"
      ;;
    \'*)
      # Single-quoted scalar: the value likewise ends at the closing quote.
      scalar="${scalar#\'}"
      scalar="${scalar%%\'*}"
      ;;
    *)
      # Plain scalar: YAML starts a comment only at whitespace followed by '#'.
      scalar="${scalar%%[[:space:]]#*}"
      ;;
  esac
  trim_whitespace "$scalar"
}

main() {
  local adapter reference
  local -a pairs=()

  if [[ "${1:-}" == --help || "${1:-}" == -h ]]; then
    usage
    return 0
  fi
  (($# == 0)) || fail "Unexpected argument: $1. Run make adapter-images-help for supported inputs."

  parse_adapters
  for adapter in "${SELECTED_ADAPTERS[@]}"; do
    reference="$(provider_image "$adapter")"
    pairs+=("$adapter=$reference")
    log PASS "$adapter -> $reference"
  done

  # Print the value only once every adapter resolved, so a caller substituting
  # the output never receives a truncated list.
  printf '%s\n' "$(IFS=,; printf '%s' "${pairs[*]}")"
}

main "$@"
