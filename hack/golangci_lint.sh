#!/usr/bin/env bash

source hack/common.sh

# check for golangci-lint in *.go files

mkdir -p ${OUTDIR_TOOLS}
LINT_BIN="${OUTDIR_TOOLS}/golangci-lint"
LINT_VER="1.51.1"

check_bin_exists() {
  which "${LINT_BIN}" >/dev/null 2>&1 && [[ "$(${LINT_BIN} --version)" == *"${LINT_VER}"* ]]
}

install_from_github() {
  SCRIPT="${OUTDIR_TOOLS}/golangci-lint_install.sh"
  URL="https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh"

  echo ""; echo "Downloading golangci-lint installation script: ${URL}"
  curl -JL -o "${SCRIPT}" "${URL}"
  chmod +x "${SCRIPT}"

  echo "Installing golangci-lint"
  "${SCRIPT}" -db "${OUTDIR_TOOLS}" "v${LINT_VER}"; RET=$?
  rm -f "${SCRIPT}"
  return ${RET}
}

install_from_downstream() {
  TGZ="${OUTDIR_TOOLS}/golangci-lint.tgz"
  URL="https://mirror2.openshift.com/pub/ci/x86_64/golangci-lint/golangci-lint-${LINT_VER}/golangci-lint-${LINT_VER}-linux-amd64.tar.gz"

  echo ""; echo "Upstream installation failed, trying downstream fallback: ${URL}"
  curl -JL -o "${TGZ}" "${URL}"
  tar -xzf "${TGZ}" -C "${OUTDIR_TOOLS}" --strip-components=1 "*/golangci-lint"; RET=$?
  rm -f "${TGZ}"
  return ${RET}
}

if ! (check_bin_exists || install_from_github || install_from_downstream); then
  echo "Failed to install golangci-lint"
  exit 1
fi

chmod +x "${OUTDIR_TOOLS}"/golangci-lint

echo ""; echo "Running golangci-lint"
GOLANGCI_LINT_CACHE=/tmp/golangci-cache "${OUTDIR_TOOLS}"/golangci-lint run ./...

exit
