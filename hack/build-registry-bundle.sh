#!/bin/bash

IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE:-}"
CONTAINER_NAME="ocs-registry"
CONTAINER_TAG="${CONTAINER_TAG:-latest}"
CONTAINER_BUILD_CMD="${CONTAINER_BUILD_CMD:-docker}"
FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${CONTAINER_NAME}:${CONTAINER_TAG}"

if [ -z "${REGISTRY_NAMESPACE}" ]; then
    echo "Please set REGISTRY_NAMESPACE"
    echo "   REGISTRY_NAMESPACE=<your-quay-username> ./hack/build-registry-bundle.sh"
    echo "   make bundle-registry REGISTRY_NAMESPACE=<your-quay-username>"
    exit 1
fi

TMP_ROOT="$(dirname "${BASH_SOURCE[@]}")/.."
REPO_ROOT=$(readlink -e "${TMP_ROOT}" 2> /dev/null || perl -MCwd -e 'print Cwd::abs_path shift' "${TMP_ROOT}")

pushd "${REPO_ROOT}/deploy"
$CONTAINER_BUILD_CMD build --no-cache -t "$FULL_IMAGE_NAME" -f Dockerfile .

printf "\nRun '${CONTAINER_BUILD_CMD} push ${FULL_IMAGE_NAME}' to push built container image to the registry.\n"
popd
