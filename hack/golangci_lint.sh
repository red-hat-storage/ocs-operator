#!/bin/bash

set -e

source hack/common.sh

# check for golangci-lint in *.go files

echo "Installing golangci-lint"

mkdir -p ${OUTDIR_TOOLS}
curl -JL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b "${OUTDIR_TOOLS}" v1.45.2
chmod +x "${OUTDIR_TOOLS}"/golangci-lint

echo "Running golangci-lint"
GOLANGCI_LINT_CACHE=/tmp/golangci-cache "${OUTDIR_TOOLS}"/golangci-lint run ./...

exit
