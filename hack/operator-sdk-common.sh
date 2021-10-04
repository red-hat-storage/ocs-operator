#!/bin/bash

# shellcheck disable=SC2034
# don't fail on unused variables - this is for sourcing


# Align with installation instructions from:
# https://sdk.operatorframework.io/docs/installation/
OPERATOR_SDK_ARCH=$(uname -m)
OPERATOR_SDK_OS=$(uname | awk '{print tolower($0)}')
# This hack was changed in newer operator-sdk versions
if [ "$OPERATOR_SDK_OS" == "linux" ]; then
       OPERATOR_SDK_OS="linux-gnu"
elif [ "$OPERATOR_SDK_OS" == "darwin" ]; then
       OPERATOR_SDK_OS="apple-darwin"
fi

OPERATOR_SDK_URL="${OPERATOR_SDK_URL:-https://github.com/operator-framework/operator-sdk/releases/download}"
OPERATOR_SDK_VERSION="${OPERATOR_SDK_VERSION:-v1.2.0}"
OPERATOR_SDK_DL_URL_FULL="${OPERATOR_SDK_URL}/${OPERATOR_SDK_VERSION}/operator-sdk-${OPERATOR_SDK_VERSION}-${OPERATOR_SDK_ARCH}-${OPERATOR_SDK_OS}"
OPERATOR_SDK="${OUTDIR_TOOLS}/operator-sdk-${OPERATOR_SDK_VERSION}"
