#!/bin/bash

set -e

source hack/common.sh

go test -ldflags="${LDFLAGS}" -v -cover "$(go list ./... | grep -v "functest")"
