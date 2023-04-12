#!/bin/bash

set -e

source hack/common.sh

LDFLAGS="-X github.com/red-hat-storage/ocs-operator/version.Version=${CSV_VERSION}"

# shellcheck disable=SC2046
go test -ldflags="${LDFLAGS}" -v -cover $(go list ./... | grep -v "functest")