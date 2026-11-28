#!/usr/bin/env bash

# Build recipes for the curated adapter images, shared by dev-deploy (which
# builds them into the kind-local registry) and build-adapter-images (which
# builds them for this machine and pushes them to a registry the caller owns).
#
# Keeping the curated list, the image names, and the source Containerfile of each
# adapter in one place is what makes the two paths interchangeable: an image
# pushed by build-adapter-images is the image dev-deploy would have built, under
# the name the provider ConfigMaps expect, so an ADAPTER_IMAGES override needs no
# other change.
#
# Sourced by scripts/dev-deploy.sh and scripts/build-adapter-images.sh. The caller
# sets CONTRIB_REPO and LMES_REPO (see find_repo_checkout in each script) before
# calling anything that resolves a source path, and provides log() and fail(),
# which the helpers use.

# curated_adapters prints the adapters ADAPTERS=all selects, in the order they
# are built, one per line.
curated_adapters() {
  printf '%s\n' inspect lm-evaluation-harness lighteval ruler ifbench followbench
}

# curated_adapter_list prints the curated adapters as one comma-separated phrase
# for diagnostics, so an unsupported value is reported with the same list.
curated_adapter_list() {
  printf 'inspect, lm-evaluation-harness, lighteval, ruler, ifbench, followbench'
}

# curated_adapter_supported reports whether $1 is a curated adapter. The values
# follow dev-deploy's ADAPTERS names, not the provider IDs, because the same
# selection also decides which source checkout is needed.
curated_adapter_supported() {
  case "$1" in
    inspect|lm-evaluation-harness|lighteval|ruler|ifbench|followbench) return 0 ;;
    *) return 1 ;;
  esac
}

# adapter_image_name prints the image name every stage uses for an adapter. It is
# the single mapping shared by the local build, the prebuilt override, and the
# registry destination, so a pushed image keeps the path a deployment publishes.
adapter_image_name() {
  case "$1" in
    lm-evaluation-harness) printf 'community-lmes' ;;
    *) printf 'community-%s' "$1" ;;
  esac
}

# adapter_build_context prints the build context of an adapter's Containerfile.
adapter_build_context() {
  case "$1" in
    lm-evaluation-harness) printf '%s' "$LMES_REPO" ;;
    *) printf '%s/adapters/%s' "$CONTRIB_REPO" "$1" ;;
  esac
}

# adapter_containerfile prints the Containerfile that builds an adapter image.
adapter_containerfile() {
  case "$1" in
    lm-evaluation-harness) printf '%s/Dockerfile.lmes-job' "$LMES_REPO" ;;
    *) printf '%s/adapters/%s/Containerfile' "$CONTRIB_REPO" "$1" ;;
  esac
}

# build_adapter_image builds one curated adapter image for linux/$2 and tags it
# as $3. Only the LM Evaluation Harness image selects its wheel set from
# TARGETOS/TARGETARCH; the contrib adapters take no build arguments.
#
# --platform is always explicit and always the target the caller resolved from
# the container runtime, so an arm64 machine builds arm64 natively instead of
# producing an image its own kind nodes have to emulate.
build_adapter_image() {
  local adapter="$1" arch="$2" image="$3" context dockerfile
  context="$(adapter_build_context "$adapter")"
  dockerfile="$(adapter_containerfile "$adapter")"
  [[ -f "$dockerfile" ]] || fail "Containerfile not found: $dockerfile"
  log INFO "Building $image from $context"
  case "$adapter" in
    lm-evaluation-harness)
      "$CONTAINER_RUNTIME" build --platform "linux/${arch}" \
        --build-arg TARGETOS=linux --build-arg "TARGETARCH=${arch}" \
        --tag "$image" --file "$dockerfile" "$context"
      ;;
    *)
      "$CONTAINER_RUNTIME" build --platform "linux/${arch}" \
        --tag "$image" --file "$dockerfile" "$context"
      ;;
  esac
  log PASS "Built $image"
}
