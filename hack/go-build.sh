#!/usr/bin/env bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}

LDFLAGS="-s -w -X github.com/red-hat-storage/ocs-operator/controllers/defaults.IsUnsupportedCephVersionAllowed=${OCS_ALLOW_UNSUPPORTED_CEPH_VERSION} \
	-X github.com/red-hat-storage/ocs-operator/version.Version=${CSV_VERSION}"

go build -tags 'netgo osusergo' -ldflags="${LDFLAGS}" -o ${OUTDIR_BIN}/ocs-operator ./main.go
go build -tags 'netgo osusergo' -ldflags="${LDFLAGS}" -o ${OUTDIR_BIN}/metrics-exporter ./metrics/main.go
go build -tags 'netgo osusergo' -ldflags="${LDFLAGS}" -o ${OUTDIR_BIN}/provider-api ./services/provider/main.go
