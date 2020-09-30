#!/bin/bash

set -e

source hack/common.sh

cp $PROMETHEUS_RULES_PATH/*rules*.yaml $OUTDIR
${IMAGE_BUILD_CMD} build -f build/Dockerfile -t "${OPERATOR_FULL_IMAGE_NAME}" build/
