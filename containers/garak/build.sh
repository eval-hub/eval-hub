#!/bin/bash
# Build script for Garak KFP container

set -e

# Configuration
IMAGE_NAME="${IMAGE_NAME:-quay.io/evalhub/garak-kfp}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE="${IMAGE_NAME}:${IMAGE_TAG}"
PLATFORM="${PLATFORM:-linux/amd64}"

echo "Building Garak KFP container image: ${FULL_IMAGE}"

# Change to script directory
cd "$(dirname "$0")"

# Build the image
podman build \
    --platform "${PLATFORM}" \
    -t "${FULL_IMAGE}" \
    -f Dockerfile \
    .

echo "Successfully built ${FULL_IMAGE}"

# Optionally push
if [ "${PUSH_IMAGE}" = "true" ]; then
    echo "Pushing image to registry..."
    podman push "${FULL_IMAGE}"
    echo "Successfully pushed ${FULL_IMAGE}"
fi

# Print usage info
echo ""
echo "To test the image locally:"
echo "  podman run --rm ${FULL_IMAGE} --help"
echo ""
echo "To push the image:"
echo "  podman push ${FULL_IMAGE}"
echo "  OR"
echo "  PUSH_IMAGE=true ./build.sh"
