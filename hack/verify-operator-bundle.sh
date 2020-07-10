#!/bin/bash

set -e

source hack/common.sh
source hack/operator-sdk-common.sh

./"${OPERATOR_SDK}" bundle validate "$(dirname $OCS_FINAL_DIR)" -b "$IMAGE_BUILD_CMD" --verbose
