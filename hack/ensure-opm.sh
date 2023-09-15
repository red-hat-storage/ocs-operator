#!/usr/bin/env bash

set -e

source hack/common.sh

OPM_DL_URL="https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/${GOHOSTOS}-${GOHOSTARCH}-opm"
mkdir -p "${LOCALBIN}"

if [ ! -x "${OPM}" ]; then
	echo "Installing opm CLI at ${OPM}"
	curl -JL "${OPM_DL_URL}" -o "${OPM}"
	chmod +x "${OPM}"
else
	echo "Using opm CLI present at ${OPM}"
fi
