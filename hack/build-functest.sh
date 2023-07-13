#!/usr/bin/env bash

set -e

source hack/common.sh

suite="${GINKGO_TEST_SUITE:-ocs}"
GOBIN="${GOBIN:-$GOPATH/bin}"
GINKGO=$GOBIN/ginkgo

if ! [ -x "$GINKGO" ]; then
	echo "Installing GINKGO"
	go install -v github.com/onsi/ginkgo/v2/ginkgo@latest
else
	echo "GINKO binary found at $GINKGO"
fi

"${GINKGO}" build --ldflags "${LDFLAGS}" "functests/${suite}/"

mkdir -p $OUTDIR_BIN
mv "functests/${suite}/${suite}.test" "${OUTDIR_BIN}/${suite}_tests"
