#!/bin/bash

set -e

source hack/common.sh
source hack/ensure-opm.sh

echo
echo "Did you push the bundle image? It must be pullable from '$IMAGE_REGISTRY'."
echo "Run '${IMAGE_BUILD_CMD} push ${BUNDLE_FULL_IMAGE_NAME}'"
echo
${OPM} index add --bundles "${BUNDLE_FULL_IMAGE_NAME}" --tag "${INDEX_FULL_IMAGE_NAME}"

echo
echo "Run '${IMAGE_BUILD_CMD} push ${INDEX_FULL_IMAGE_NAME}' to push operator index to image registry."
