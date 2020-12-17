#!/bin/bash

set -e

source hack/common.sh

mkdir -p ${OUTDIR_BIN}
go build -tags 'netgo osusergo' -ldflags="-s -w -X github.com/openshift/ocs-operator/pkg/controller/defaults.IsUnsupportedCephVersionAllowed=${OCS_ALLOW_UNSUPPORTED_CEPH_VERSION}" -o ${OUTDIR_BIN}/ocs-operator ./cmd/manager
go build -tags 'netgo osusergo' -ldflags="-s -w -X github.com/openshift/ocs-operator/pkg/controller/defaults.IsUnsupportedCephVersionAllowed=${OCS_ALLOW_UNSUPPORTED_CEPH_VERSION}" -o ${OUTDIR_BIN}/metrics-exporter ./metrics/main.go
