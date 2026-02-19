#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh

${IMAGE_BUILD_CMD} build -f metrics/Dockerfile -t "${METRICS_EXPORTER_FULL_IMAGE_NAME}" . \
    --build-arg="LDFLAGS=${LDFLAGS}" --build-arg="GO_ARCH=${TARGET_ARCH}" --platform="${TARGET_OS}"/"${TARGET_ARCH}"
