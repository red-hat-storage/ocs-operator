#!/bin/bash

set -e

source hack/common.sh
source hack/docker-common.sh
source hack/generate-appregistry.sh

$IMAGE_BUILD_CMD build --no-cache -t "$CATALOG_FULL_IMAGE_NAME" -f build/Dockerfile.registry .
echo
echo "Run '${IMAGE_BUILD_CMD} push ${CATALOG_FULL_IMAGE_NAME}' to push ocs-registry to ${IMAGE_REGISTRY}."
