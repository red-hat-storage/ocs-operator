#!/bin/bash

set -e

source hack/common.sh
source hack/docker-common.sh

${IMAGE_BUILD_CMD} build -f metrics/Dockerfile -t "${METRICS_EXPORTER_FULL_IMAGE_NAME}" .
