#!/bin/bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}
${IMAGE_BUILD_CMD} build -v "$(pwd)/${OUTDIR_BIN}":/output -f build/Dockerfile.build .
