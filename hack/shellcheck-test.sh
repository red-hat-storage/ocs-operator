#!/usr/bin/env bash

# Check for shell syntax & style.

source hack/common.sh

test_syntax() {
        bash -n "${1}"
}
test_shellcheck() {
        if [[ "${SHELLCHECK}" ]]; then
                # only look for the "flag" on comment lines
                # without this we can match our own code :-)
                if grep -q '^#.*OCS-OPERATOR-SKIP-SHELLCHECK' "${1}"; then
                        return 0
                fi
                #shell check -x -e SC2181,SC2029,SC1091,SC1090,SC2012 "${1}"
                "${SHELLCHECK}" -x -e SC2181 "${1}"
        else
                return 0
        fi
}

SCRIPT_DIR="$(cd "$(dirname "${0}")" && pwd)"
BASE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SHELLCHECK="${OUTDIR_TOOLS}/shellcheck"

if [ ! -f "${SHELLCHECK}" ]; then
        SC_VERSION="stable"
        echo "Shellcheck not found, installing shellcheck... (${SC_VERSION?})" >&2

        if [ "$OS_TYPE" == "Darwin" ]; then
                SC_SOURCE="https://github.com/koalaman/shellcheck/releases/download/${SC_VERSION?}/shellcheck-${SC_VERSION?}.darwin.x86_64.tar.xz"
        else
                SC_SOURCE="https://github.com/koalaman/shellcheck/releases/download/${SC_VERSION?}/shellcheck-${SC_VERSION?}.linux.x86_64.tar.xz"
        fi

        curl -JL "${SC_SOURCE}" | tar -xJv
        mkdir -p ${OUTDIR_TOOLS}
        cp -f "shellcheck-${SC_VERSION}/shellcheck" "${SHELLCHECK}"

        if [ -f "$SHELLCHECK" ]; then
                rm -rf "./shellcheck-${SC_VERSION?}"
        else
                unset SHELLCHECK
        fi
fi


cd "${BASE_DIR}" || exit 2
SCRIPTS=$(find . \( -path "*/vendor/*" -o -path "*/build/*" -o -path "*/_cache/*" \) -prune -o -name "*~" -prune -o -name "*.swp" -prune -o -type f -exec grep -l -e '^#!/usr/bin/env bash$' {} \;)

failed=0
for script in ${SCRIPTS}; do
        err=0
        test_syntax "${script}"
        if [[ $? -ne 0 ]]; then
                err=1
                echo "detected syntax issues in ${script}}" >&2
        fi
        test_shellcheck "${script}"
        if [[ $? -ne 0 ]]; then
                err=1
                echo "detected shellcheck issues in ${script}" >&2
        fi
        if [[ $err -ne 0 ]]; then
                ((failed+=err))
        else
                echo "${script}: ok" >&2
        fi
done

echo "${failed} scripts with errors were found"

exit "${failed}"

