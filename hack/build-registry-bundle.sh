#!/bin/bash

set -e

source hack/common.sh

TMP_ROOT="$(dirname "${BASH_SOURCE[@]}")/.."
REPO_ROOT=$(readlink -e "${TMP_ROOT}" 2> /dev/null || perl -MCwd -e 'print Cwd::abs_path shift' "${TMP_ROOT}")

pushd "${REPO_ROOT}/deploy"
$IMAGE_BUILD_CMD build --no-cache -t "$CATALOG_FULL_IMAGE_NAME" -f Dockerfile .

echo
echo "Run '${IMAGE_BUILD_CMD} push ${CATALOG_FULL_IMAGE_NAME}' to push built container image to the registry."
popd
