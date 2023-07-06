#!/usr/bin/env bash

set -e

source hack/common.sh

OPM_VERSION="v1.28.0"
case "$(uname -m)" in
	x86_64)
		OPM_ARCH="amd64"
		;;
	aarch64)
		OPM_ARCH="arm64"
		;;
	*)
		OPM_ARCH="$(uname -m)"
		;;
esac
OPM_OS=$(echo -n "${OS_TYPE}" | awk '{print tolower($0)}')
OPM_PLATFORM="${OPM_OS}-${OPM_ARCH}-opm"
OPM_URL="https://github.com/operator-framework/operator-registry/releases/download/${OPM_VERSION}/${OPM_PLATFORM}"
OPM_BIN="opm-${OPM_VERSION}"
OPM="${OUTDIR_TOOLS}/${OPM_BIN}"
OPERATOR_REGISTRY_TMPDIR="${OUTDIR_TOOLS}/operator-registry"

if [ -x "${OPM}" ]; then
	# Case 1: already has opm binray
	echo "Using opm cached at ${OPM}"
elif [ "${OPM_ARCH}" != "amd64" ]; then
	# Case 2: image does not exist, need to build from source
	echo "Building opm from source under ${OPERATOR_REGISTRY_TMPDIR} for ${OPM_OS}-${OPM_ARCH}"
	mkdir -p "${OPERATOR_REGISTRY_TMPDIR}"
	pushd "${OPERATOR_REGISTRY_TMPDIR}"
	git clone https://github.com/operator-framework/operator-registry .
	git checkout "${OPM_VERSION}"
	make
	popd
	cp "${OPERATOR_REGISTRY_TMPDIR}/bin/opm" "${OPM}"
	rm -rf "${OPERATOR_REGISTRY_TMPDIR}"
else
	# Case 3: use public image
	echo "Downloading opm ${OPM_VERSION} for ${OS_TYPE}"
	mkdir -p "${OUTDIR_TOOLS}"
	curl -JL "${OPM_URL}" -o "${OPM}"
	chmod +x "${OPM}"
fi
