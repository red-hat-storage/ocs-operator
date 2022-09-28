#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh

$IMAGE_BUILD_CMD build --no-cache -f build/Dockerfile.bundle -t "$BUNDLE_FULL_IMAGE_NAME"  .

echo
echo "Run '${IMAGE_BUILD_CMD} push ${BUNDLE_FULL_IMAGE_NAME}' to push operator bundle to image registry."
