#!/bin/bash

set -e

source hack/common.sh

${IMAGE_BUILD_CMD} build -f must-gather/Dockerfile -t ${MUST_GATHER_FULL_IMAGE_NAME} must-gather/
