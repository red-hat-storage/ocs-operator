#!/usr/bin/env bash

set -e

source hack/ensure-operator-sdk.sh
source hack/docker-common.sh

if [ "$FUSION" == "true" ]; then
    ./"${OPERATOR_SDK}" bundle validate "$(dirname $FCS_FINAL_DIR)" --verbose
else
    ./"${OPERATOR_SDK}" bundle validate "$(dirname $OCS_FINAL_DIR)" --verbose
fi
