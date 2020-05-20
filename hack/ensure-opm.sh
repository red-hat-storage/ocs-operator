#!/bin/bash

set -e

source hack/common.sh

OPM_VERSION="v1.12.3"
OPM_PLATFORM="linux-amd64-opm"
OS_TYPE=$(uname)
if [ "$OS_TYPE" == "Darwin" ]; then
	OPM_PLATFORM=darwin-amd64-opm
fi
OPM_URL="https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/${OPM_PLATFORM}"
OPM_BIN="opm-${OPM_VERSION}"
OPM="${OUTDIR_TOOLS}/${OPM_BIN}"
if [ ! -x "${OPM}" ]; then
	echo "Downloading opm ${OPM_VERSION} for ${OS_TYPE}"
	mkdir -p ${OUTDIR_TOOLS}
	curl -JL ${OPM_URL} -o ${OPM}
	chmod +x ${OPM}
else
	echo "Using opm cached at ${OPM}"
fi