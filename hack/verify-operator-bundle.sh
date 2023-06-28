#!/usr/bin/env bash

set -e

source hack/ensure-operator-sdk.sh

if [ "$FUSION" == "true" ]; then
    ./"${OPERATOR_SDK}" bundle validate "$(dirname $ICS_FINAL_DIR)" --verbose
else
    ./"${OPERATOR_SDK}" bundle validate "$(dirname $OCS_FINAL_DIR)" --verbose
fi
