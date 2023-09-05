#!/bin/bash

set -e

source hack/common.sh

[ -z "$CONTAINER_CLI" ] && { echo "Podman or Docker not found"; exit 1; }

${CONTAINER_CLI} build --platform="${GOOS}"/"${GOARCH}" --no-cache -t "$BUNDLE_IMAGE" -f Dockerfile.bundle .

echo "Run '${CONTAINER_CLI} push ${BUNDLE_IMAGE}' to push operator bundle to image registry."
