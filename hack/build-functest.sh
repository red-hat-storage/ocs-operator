#!/bin/bash

set -e

source hack/common.sh

suite="${GINKGO_TEST_SUITE:-ocs}"
GOBIN="${GOBIN:-$GOPATH/bin}"
GINKGO=$GOBIN/ginkgo

if ! [ -x "$GINKGO" ]; then
	echo "Retrieving ginkgo and gomega build dependencies"
	go install -v github.com/onsi/ginkgo/v2/ginkgo@latest
	go install -v github.com/onsi/gomega/...
else
	echo "GINKO binary found at $GINKGO"
fi


"$GOBIN"/ginkgo build "functests/${suite}/"

mkdir -p $OUTDIR_BIN
mv "functests/${suite}/${suite}.test" "${OUTDIR_BIN}/${suite}_tests"
