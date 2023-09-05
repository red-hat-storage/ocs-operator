#!/bin/bash

set -e

source hack/common.sh
source hack/ensure-opm.sh

[ -z "$CONTAINER_CLI" ] && { echo "Podman or Docker not found"; exit 1; }

echo "Did you push the bundle image? It must be pullable from '$IMAGE_REGISTRY'."
echo "Run '${CONTAINER_CLI} push ${BUNDLE_IMAGE} to push operator bundle to image registry.'"

${OPM} render --output=yaml "${BUNDLE_IMAGE}" > catalog/ocs-bundle.yaml
${OPM} render --output=yaml "${NOOBAA_BUNDLE_IMAGE}" > catalog/noobaa-bundle.yaml
${OPM} validate catalog
${OPM} generate dockerfile catalog

mv catalog.Dockerfile Dockerfile.catalog

${CONTAINER_CLI} build --platform="${GOOS}"/"${GOARCH}" --no-cache -t "${CATALOG_IMAGE}" -f Dockerfile.catalog .

echo "Run '${CONTAINER_CLI} push ${CATALOG_IMAGE}' to push operator catalog image to image registry."
