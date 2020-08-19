#!/bin/bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}
go build -i -ldflags="-s -w" -o ${OUTDIR_BIN}/ocs-operator ./cmd/manager
go build -i -ldflags="-s -w" -o ${OUTDIR_BIN}/metrics-exporter ./metrics/main.go
