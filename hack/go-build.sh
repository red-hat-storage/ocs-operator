#!/bin/bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}
go build -tags 'netgo osusergo' -ldflags="-s -w" -o ${OUTDIR_BIN}/ocs-operator ./main.go
go build -tags 'netgo osusergo' -ldflags="-s -w" -o ${OUTDIR_BIN}/metrics-exporter ./metrics/main.go
