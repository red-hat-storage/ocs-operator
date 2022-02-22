#!/bin/bash

set -e

source hack/common.sh

LDFLAGS="-X github.com/red-hat-storage/ocs-operator/version.Version=${VERSION}"

go test -ldflags="${LDFLAGS}" -v -cover `go list ./... | grep -v "functest"`
