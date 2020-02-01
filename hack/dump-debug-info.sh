#!/bin/bash

source hack/common.sh

echo "dumping debug information"
echo "--- PODS ----"
${OCS_OC_PATH} get pods -n openshift-storage
echo "---- PVs ----"
${OCS_OC_PATH} get pv
echo "--- StorageClasses ----"
${OCS_OC_PATH} get storageclass --all-namespaces
echo "--- StorageCluster ---"
${OCS_OC_PATH} get storagecluster --all-namespaces -o yaml
echo "--- CephCluster ---"
${OCS_OC_PATH} get cephcluster --all-namespaces -o yaml
echo "--- Noobaa ---"
${OCS_OC_PATH} get noobaa --all-namespaces -o yaml

echo "Running ocs-must-gather"
MUST_GATHER_DIR="ocs-must-gather"
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
  MUST_GATHER_DIR="/tmp/artifacts/ocs-must-gather"
fi
mkdir -p $MUST_GATHER_DIR
${OCS_OC_PATH} adm must-gather --image "$MUST_GATHER_FULL_IMAGE_NAME" --dest-dir "$MUST_GATHER_DIR"
