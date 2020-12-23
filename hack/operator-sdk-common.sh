#!/bin/bash

# shellcheck disable=SC2034
# don't fail on unused variables - this is for sourcing

OPERATOR_SDK_URL="${OPERATOR_SDK_URL:-https://github.com/operator-framework/operator-sdk/releases/download}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v1.2.0}"
OPERATOR_SDK_PLATFORM="x86_64-linux-gnu"
if [ "$OS_TYPE" == "Darwin" ]; then
	OPERATOR_SDK_PLATFORM="x86_64-apple-darwin"
fi
OPERATOR_SDK_BIN="operator-sdk-${OPERATOR_SDK_VERSION}-${OPERATOR_SDK_PLATFORM}"
OPERATOR_SDK="${OUTDIR_TOOLS}/operator-sdk-${OPERATOR_SDK_VERSION}"
