#!/bin/bash

set -e

source hack/common.sh

${IMAGE_BUILD_CMD} build -f build/Dockerfile.build -t "${OPERATOR_FULL_IMAGE_NAME}" .
