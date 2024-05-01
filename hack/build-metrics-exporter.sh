#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh

pushd metrics
${IMAGE_BUILD_CMD} build --no-cache -f Dockerfile -t "${METRICS_EXPORTER_FULL_IMAGE_NAME}" . \
    --build-arg="GOOS=${GOOS}" --build-arg="GOARCH=${GOHOSTARCH}" --build-arg="LDFLAGS=${LDFLAGS}"
popd
