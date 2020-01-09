#!/bin/bash

set -e

source hack/common.sh

GOBIN="${GOBIN:-$GOPATH/bin}"
GINKGO=$GOBIN/ginkgo

if ! [ -x "$GINKGO" ]; then
	echo "Retrieving ginkgo and gomega build dependencies"
	go get github.com/onsi/ginkgo/ginkgo
	go get github.com/onsi/gomega/...
else
	echo "GINKO binary found at $GINKGO"
fi


"$GOBIN"/ginkgo build negativetests/

mkdir -p $OUTDIR_BIN
mv negativetests/negativetests.test $OUTDIR_BIN/negativetests
