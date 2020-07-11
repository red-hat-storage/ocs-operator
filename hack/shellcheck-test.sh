#!/bin/bash
  
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
                shellcheck -x -e SC2181 "${1}"
        else
                return 0
        fi
}

SHELLCHECK="$(command -v shellcheck 2>/dev/null)"

SCRIPT_DIR="$(cd "$(dirname "${0}")" && pwd)"

BASE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [[ -z "${SHELLCHECK}" ]]; then
        echo "warning: could not find shellcheck ... installing shellcheck" >&2
        scversion="stable"
        FILE="${OUTDIR_TOOLS}/shellcheck"
        wget -qO- "https://storage.googleapis.com/shellcheck/shellcheck-${scversion?}.linux.x86_64.tar.xz" | tar -xJv
        cp -f "shellcheck-${scversion}/shellcheck" "${FILE}"
        if [ -f "$FILE" ]; then
                SHELLCHECK="${FILE}"
        fi
fi

cd "${BASE_DIR}" || exit 2
SCRIPTS=$(find . \( -path "*/vendor/*" -o -path "*/build/*" -o -path "*/_cache/*" \) -prune -o -name "*~" -prune -o -name "*.swp" -prune -o -type f -exec grep -l -e '^#!/bin/bash$' {} \;)

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

exit ${failed}

