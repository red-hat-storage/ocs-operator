#!/bin/bash

set -e

source hack/common.sh

# shellcheck disable=SC2046
go test -ldflags="${LDFLAGS}" -v -cover $(go list ./... | grep -v "functest")