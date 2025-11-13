#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh

pushd services/devicefinder
${IMAGE_BUILD_CMD} build -f Dockerfile -t "${DEVICEFINDER_FULL_IMAGE_NAME}" ../.. \
    --build-arg="LDFLAGS=${LDFLAGS}" --platform="${TARGET_OS}"/"${TARGET_ARCH}"
popd
