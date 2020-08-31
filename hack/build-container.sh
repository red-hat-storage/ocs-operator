#!/bin/bash

set -e

source hack/common.sh

cp $PROMETHEUS_RULE_PATH/*rules*.yaml $OUTDIR
${IMAGE_BUILD_CMD} build -f build/Dockerfile.build -t "${OPERATOR_FULL_IMAGE_NAME}" .
