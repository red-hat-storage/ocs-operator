#!/usr/bin/env bash

set -o nounset
set -o errexit
set -o pipefail

source hack/common.sh

NAMESPACE=$(oc get ns "$INSTALL_NAMESPACE" -o jsonpath="{.metadata.name}" 2>/dev/null || true)
if [[ -n "$NAMESPACE" ]]; then
    echo "Namespace \"$NAMESPACE\" exists"
else
    echo "Namespace \"$INSTALL_NAMESPACE\" does not exist: creating it"
    oc create ns "$INSTALL_NAMESPACE"
fi


# Ensure ocs-operator-config configmap for rook to come up before ocs-operator
cat <<EOF | oc create -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: ocs-operator-config
  namespace: openshift-storage
data:
  ROOK_CURRENT_NAMESPACE_ONLY: "true"
  CSI_CLUSTER_NAME: "test"
  CSI_ENABLE_TOPOLOGY: "test"
  CSI_TOPOLOGY_DOMAIN_LABELS: "test"
  ROOK_CSI_ENABLE_NFS: "false"
  ROOK_CSI_DISABLE_DRIVER: "false"
  ROOK_CSI_ENABLE_CEPHFS: "true"
EOF

"$OPERATOR_SDK" run bundle "$ROOK_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CEPH_CSI_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$NOOBAA_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

oc wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment \
    ceph-csi-controller-manager \
    noobaa-operator \
    ocs-operator \
    rook-ceph-operator \
    ux-backend-server
