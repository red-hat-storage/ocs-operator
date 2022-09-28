#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh
source hack/ensure-opm.sh

BUNDLE_IMGS="${BUNDLE_IMGS:-${BUNDLE_FULL_IMAGE_NAME} ${NOOBAA_BUNDLE_FULL_IMAGE_NAME}}"
OPM_RENDER_OPTS="${OPM_RENDER_OPTS:-}"

echo
echo "Did you push the bundle image? It must be pullable from $IMAGE_REGISTRY"
echo "Run '${IMAGE_BUILD_CMD} push ${BUNDLE_FULL_IMAGE_NAME}'"
echo
$OPM render --output=yaml ${BUNDLE_IMGS} $OPM_RENDER_OPTS > deploy/catalog/bundle.yaml
$OPM validate deploy/catalog
$IMAGE_BUILD_CMD build -f build/Dockerfile.catalog -t "$CATALOG_FULL_IMAGE_NAME" .
echo
echo "Run '${IMAGE_BUILD_CMD} push ${CATALOG_FULL_IMAGE_NAME}' to push operator catalog to image registry."
