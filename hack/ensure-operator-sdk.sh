#!/bin/bash

set -e

source hack/common.sh
source hack/operator-sdk-common.sh

if [ ! -x "${OPERATOR_SDK}" ]; then
	echo "Downloading operator-sdk ${OPERATOR_SDK_VERSION}-${OPERATOR_SDK_PLATFORM}"
	mkdir -p ${OUTDIR_TOOLS}
	curl -JL ${OPERATOR_SDK_URL}/${OPERATOR_SDK_VERSION}/${OPERATOR_SDK_BIN} -o ${OPERATOR_SDK}
	chmod +x ${OPERATOR_SDK}
else
	echo "Using operator-sdk cached at ${OPERATOR_SDK}"
fi
