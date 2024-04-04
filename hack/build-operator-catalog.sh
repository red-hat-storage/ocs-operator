#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh
source hack/ensure-opm.sh

echo
echo "Did you push the bundle image? It must be pullable from '$IMAGE_REGISTRY'."
echo "Run '${IMAGE_BUILD_CMD} push ${BUNDLE_FULL_IMAGE_NAME}'"
echo
${OPM} render --output=yaml "${BUNDLE_FULL_IMAGE_NAME}" > catalog/ocs-bundle.yaml
${OPM} render --output=yaml "${OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME}" > catalog/ocs-client-bundle.yaml
${OPM} render --output=yaml ${NOOBAA_BUNDLE_FULL_IMAGE_NAME} > catalog/noobaa-bundle.yaml
${OPM} render --output=yaml ${ROOK_BUNDLE_FULL_IMAGE_NAME} > catalog/rook-ceph-bundle.yaml
${OPM} validate catalog
${OPM} generate dockerfile catalog

mv catalog.Dockerfile Dockerfile.catalog
${IMAGE_BUILD_CMD} build --platform="${GOOS}"/"${GOARCH}" --no-cache -t "${FILE_BASED_CATALOG_FULL_IMAGE_NAME}" -f Dockerfile.catalog .

echo "Run '${IMAGE_BUILD_CMD} push ${FILE_BASED_CATALOG_FULL_IMAGE_NAME}' to push operator catalog image to image registry."
