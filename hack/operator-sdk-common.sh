#!/bin/bash

OPERATOR_SDK_URL="${OPERATOR_SDK_URL:-https://github.com/operator-framework/operator-sdk/releases/download}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v0.17.0}"
OPERATOR_SDK_PLATFORM="x86_64-linux-gnu"
OS_TYPE=$(uname)
if [ "$OS_TYPE" == "Darwin" ]; then
	OPERATOR_SDK_PLATFORM="x86_64-apple-darwin"
fi
OPERATOR_SDK_BIN="operator-sdk-${OPERATOR_SDK_VERSION}-${OPERATOR_SDK_PLATFORM}"
OPERATOR_SDK="${OUTDIR_TOOLS}/${OPERATOR_SDK_BIN}"
