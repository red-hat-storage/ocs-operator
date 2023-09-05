#!/bin/bash

set -e

source hack/common.sh

[ -z "$CONTAINER_CLI" ] && { echo "Podman or Docker not found"; exit 1; }

${CONTAINER_CLI} build --build-arg="LDFLAGS=${LDFLAGS}" --platform="${GOOS}"/"${GOARCH}" --no-cache -f metrics/Dockerfile -t "${METRICS_EXPORTER_IMAGE}" .
