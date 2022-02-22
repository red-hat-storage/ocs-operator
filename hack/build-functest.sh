#!/bin/bash

set -e

source hack/common.sh

suite="${GINKGO_TEST_SUITE:-ocs}"
GOBIN="${GOBIN:-$GOPATH/bin}"
GINKGO=$GOBIN/ginkgo

if ! [ -x "$GINKGO" ]; then
	echo "Retrieving ginkgo and gomega build dependencies"
	# TODO: Move to `go install` while upgrading to Go 1.16
	# Currently, `go install` is unable to install ginkgo which
	# causes build failures during E2E tests. The workaround is
	# to install ginkgo and gomega using `go get` and turn off
	# the modules so that it doesn't update go.mod and go.sum files
	GO111MODULE=off go get github.com/onsi/ginkgo/ginkgo
	GO111MODULE=off go get github.com/onsi/gomega/...
else
	echo "GINKO binary found at $GINKGO"
fi

LDFLAGS="-X github.com/red-hat-storage/ocs-operator/version.Version=${VERSION}"

"$GOBIN"/ginkgo build --ldflags "${LDFLAGS}" "functests/${suite}/"

mkdir -p $OUTDIR_BIN
mv "functests/${suite}/${suite}.test" "${OUTDIR_BIN}/${suite}_tests"
