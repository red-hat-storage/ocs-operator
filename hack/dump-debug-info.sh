#!/bin/bash
source hack/common.sh

echo "dumping debug information"
echo "--- PODS ----"
oc get pods -n openshift-storage
echo "---- PVs ----"
oc get pv
echo "--- StorageClasses ----"
oc get storageclass --all-namespaces
echo "--- StorageCluster ---"
oc get storagecluster --all-namespaces -o yaml
echo "--- CephCluster ---"
oc get cephcluster --all-namespaces -o yaml
echo "--- Noobaa ---"
oc get noobaa --all-namespaces -o yaml

echo "Running ocs-must-gather"
MUST_GATHER_DIR="ocs-must-gather"
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
  MUST_GATHER_DIR="/tmp/artifacts/ocs-must-gather"
fi
mkdir -p $MUST_GATHER_DIR
oc adm must-gather --image "$MUST_GATHER_FULL_IMAGE_NAME" --dest-dir "$MUST_GATHER_DIR"
