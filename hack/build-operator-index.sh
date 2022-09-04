#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh
source hack/ensure-opm.sh

BUNDLE_IMGS="${BUNDLE_IMGS:-${BUNDLE_FULL_IMAGE_NAME},${NOOBAA_BUNDLE_FULL_IMAGE_NAME}}"

echo
echo "Did you push the bundle image? It must be pullable from '$IMAGE_REGISTRY'."
echo "Run '${IMAGE_BUILD_CMD} push ${BUNDLE_FULL_IMAGE_NAME}'"
echo
${OPM} index add --container-tool "${IMAGE_BUILD_CMD}" --bundles "${BUNDLE_IMGS}" --tag "${INDEX_FULL_IMAGE_NAME}"

echo
echo "Run '${IMAGE_BUILD_CMD} push ${INDEX_FULL_IMAGE_NAME}' to push operator index to image registry."
