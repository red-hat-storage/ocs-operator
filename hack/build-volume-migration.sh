#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh


${IMAGE_BUILD_CMD} build -f tools/volume-migration/Dockerfile -t "${VOLUME_MIGRATION_FULL_IMAGE_NAME}" . \
    --build-arg="GOOS=${GOOS}" --build-arg="GOARCH=${GOARCH}" --build-arg="LDFLAGS=${LDFLAGS}"
