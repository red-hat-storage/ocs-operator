#!/usr/bin/env bash
set -e

if [ -z "$GOPATH" ]; then
    exit 1
fi

#shellcheck source=/dev/null
source "$GOPATH/src/github.com/openshift/ocs-operator/hack/common.sh"

# must-gather func-tests
must_gather_output_dir="$GOPATH/src/github.com/openshift/ocs-operator/must-gather-test"
# Cleaning all existing dump
rm -rf "${must_gather_output_dir}"
echo "Triggering ocs-must-gather"
${OCS_OC_PATH} adm must-gather --image="${MUST_GATHER_FULL_IMAGE_NAME}" --dest-dir="${must_gather_output_dir}"
"$GOPATH"/src/github.com/openshift/ocs-operator/must-gather/functests/integration/command_output_test.sh "${must_gather_output_dir}"

echo "All OCS-must-gather tests passed successfully"

# Cleaning the dump
rm -rf "${must_gather_output_dir}"
