#!/bin/bash

source hack/common.sh

echo "dumping debug information"

echo "Running ocs-must-gather"
OCS_MUST_GATHER_DIR="ocs-must-gather"
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
  OCS_MUST_GATHER_DIR="/tmp/artifacts/ocs-must-gather"
fi
mkdir -p $OCS_MUST_GATHER_DIR
${OCS_OC_PATH} adm must-gather --image "$MUST_GATHER_FULL_IMAGE_NAME" --dest-dir "$OCS_MUST_GATHER_DIR"

echo "Running ocp-must-gather"
OCP_MUST_GATHER_DIR="ocp-must-gather"
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
  OCP_MUST_GATHER_DIR="/tmp/artifacts/ocp-must-gather"
fi
mkdir -p $OCP_MUST_GATHER_DIR
${OCS_OC_PATH} --insecure-skip-tls-verify adm must-gather --dest-dir "$OCP_MUST_GATHER_DIR"
