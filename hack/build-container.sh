#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh

${IMAGE_BUILD_CMD} build -f build/Dockerfile -t "${OPERATOR_FULL_IMAGE_NAME}" .
