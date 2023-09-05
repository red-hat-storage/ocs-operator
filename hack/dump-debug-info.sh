#!/bin/bash

source hack/common.sh

set -e

echo "dumping debug information"

echo "Running ocs-must-gather to ${OCS_MUST_GATHER_DIR}"
mkdir -p "$OCS_MUST_GATHER_DIR"
${OCS_OC_PATH} adm must-gather --image "$MUST_GATHER_IMAGE" --dest-dir "$OCS_MUST_GATHER_DIR"

echo "Running ocp-must-gather to ${OCP_MUST_GATHER_DIR}"
mkdir -p "$OCP_MUST_GATHER_DIR"
${OCS_OC_PATH} --insecure-skip-tls-verify adm must-gather --dest-dir "$OCP_MUST_GATHER_DIR"
