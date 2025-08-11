#!/usr/bin/env bash

source hack/common.sh

set -e

echo "dumping debug information"

if [[ -n "${ARTIFACT_DIR:-}" ]]; then
    OCS_MUST_GATHER_DIR="${ARTIFACT_DIR}/${OCS_MUST_GATHER_DIR}"
fi

echo "Running ocs-must-gather to ${OCS_MUST_GATHER_DIR}"
mkdir -p "$OCS_MUST_GATHER_DIR"
${OCS_OC_PATH} adm must-gather --image "$LATEST_MUST_GATHER_IMAGE" --dest-dir "$OCS_MUST_GATHER_DIR"
echo "OCS must-gather completed successfully."
