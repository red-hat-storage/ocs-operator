#!/bin/bash

source hack/common.sh

GOLANGCI_LINT_DL_SCRIPT_URL="https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh"

mkdir -p "${LOCALBIN}"

if [ ! -x "${GOLANGCI_LINT}" ] || [ "v$(${GOLANGCI_LINT} --version | awk '{print $4}')" != "${GOLANGCI_LINT_VERSION}" ]; then
  echo "Installing golangci-lint at ${GOLANGCI_LINT}"
  curl -sSfL "${GOLANGCI_LINT_DL_SCRIPT_URL}" | sh -s -- -b "${LOCALBIN}" "${GOLANGCI_LINT_VERSION}"
else
  echo "Using golangci-lint present at ${GOLANGCI_LINT}"
fi

echo "Running golangci-lint"
GOLANGCI_LINT_CACHE=/tmp/golangci-cache "${GOLANGCI_LINT}" run
exit