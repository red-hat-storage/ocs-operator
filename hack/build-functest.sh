#!/usr/bin/env bash

set -e

source hack/common.sh

mkdir -p "${LOCALBIN}"

if ! [ -x "$GINKGO" ]; then
	echo "Installing GINKGO binary at $GINKGO"
	GOBIN=${LOCALBIN} go install github.com/onsi/ginkgo/v2/ginkgo
else
	echo "GINKO binary found at $GINKGO"
fi

"${GINKGO}" build --ldflags "${LDFLAGS}" "functests/${GINKGO_TEST_SUITE}/"

mv "functests/${GINKGO_TEST_SUITE}/${GINKGO_TEST_SUITE}.test" "${LOCALBIN}/${GINKGO_TEST_SUITE}_tests"
