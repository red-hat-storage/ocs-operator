#!/bin/bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}

LDFLAGS="-X github.com/openshift/ocs-operator/controllers/defaults.IsUnsupportedCephVersionAllowed=${OCS_ALLOW_UNSUPPORTED_CEPH_VERSION} \
	-X github.com/openshift/ocs-operator/version.Version=${VERSION}"

go build -tags 'netgo osusergo' -ldflags="${LDFLAGS}" -gcflags="all=-N -l" -o ${OUTDIR_BIN}/ocs-operator ./main.go
go build -tags 'netgo osusergo' -ldflags="${LDFLAGS}" -gcflags="all=-N -l" -o ${OUTDIR_BIN}/metrics-exporter ./metrics/main.go
