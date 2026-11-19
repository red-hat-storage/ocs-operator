#!/bin/bash

set -e

source hack/common.sh

# shellcheck disable=SC2046
go test -v -cover $(go list ./... | grep -v "functest")