#!/usr/bin/env bash

# Check for shell syntax & style.

source hack/common.sh

mkdir -p "${LOCALBIN}"

if [[ ${HOST_ARCH} == "amd64" ]]; then
        SHELLCHECK_ARCH="x86_64"
elif [[ ${HOST_ARCH} == "arm64" ]]; then
        SHELLCHECK_ARCH="aarch64"
fi

SHELLCHECK_TAR="shellcheck-${SHELLCHECK_VERSION}.${HOST_OS}.${SHELLCHECK_ARCH}.tar.gz"
SHELLCHECK_DL_URL="https://github.com/vscode-shellcheck/shellcheck-binaries/releases/download/${SHELLCHECK_VERSION}/${SHELLCHECK_TAR}"

#install shellcheck
if [ ! -x "${SHELLCHECK}" ] || [ "v$(${SHELLCHECK} --version | awk 'FNR == 2 {print $2}')" != "${SHELLCHECK_VERSION}" ]; then
        echo "Installing shellcheck at ${SHELLCHECK}"
        curl -LO "${SHELLCHECK_DL_URL}"
        tar -xf "${SHELLCHECK_TAR}" -C "${LOCALBIN}" --exclude=*.txt
        rm -f "${SHELLCHECK_TAR}"
else
        echo "shellcheck already present at ${SHELLCHECK}"
fi

SCRIPTS=$(find . \( -path "*/vendor/*" -o -path "*/build/*" -o -path "*/_cache/*" \) -prune -o -name "*~" -prune -o -name "*.swp" -prune -o -type f -exec grep -l -e '^#!/usr/bin/env bash$' {} \;)

failed=0
for script in ${SCRIPTS}; do
        if ! (bash -n "${script}"); then
                ((failed++))
        elif ! ("${SHELLCHECK}" -x "${script}"); then
                ((failed++))
        fi
done

echo "${failed} scripts with errors were found"

exit "${failed}"

