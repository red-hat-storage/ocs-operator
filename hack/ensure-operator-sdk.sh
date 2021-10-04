#!/bin/bash

set -e

source hack/common.sh
source hack/operator-sdk-common.sh

if [ ! -x "${OPERATOR_SDK}" ]; then
	echo "Downloading operator-sdk: ${OPERATOR_SDK_DL_URL_FULL} --> ${OPERATOR_SDK}"
	mkdir -p "${OUTDIR_TOOLS}"
	curl -JL "${OPERATOR_SDK_DL_URL_FULL}" -o "${OPERATOR_SDK}"
	chmod +x "${OPERATOR_SDK}"
else
	echo "Using operator-sdk cached at ${OPERATOR_SDK}"
fi

# Ensure operator-sdk can run properly on local machine
"${OPERATOR_SDK}" version > /dev/null 2>&1 || echo "Bad operator-sdk binary: ${OPERATOR_SDK}"
