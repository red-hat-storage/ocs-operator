#!/usr/bin/env bash

set -e

source hack/common.sh

OPERATOR_SDK_DL_URL="https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/operator-sdk_${HOST_OS}_${HOST_ARCH}"

mkdir -p "${LOCALBIN}"

if [ ! -x "${OPERATOR_SDK}" ]; then
	echo "Installing operator-sdk CLI at ${OPERATOR_SDK}"
	curl -JL "${OPERATOR_SDK_DL_URL}" -o "${OPERATOR_SDK}"
	chmod +x "${OPERATOR_SDK}"
else
	echo "Using operator-sdk CLI present at ${OPERATOR_SDK}"
fi
