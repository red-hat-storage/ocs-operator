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
cat <<EOF | oc apply -f -
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
  ROOK_CSI_DISABLE_DRIVER: "true"
  ROOK_CSI_ENABLE_CEPHFS: "false"
EOF

# This ensures that we don't face issues related to OCP catalogsources with below error
# It is safe to disable these catalogsources as we are not using them anywhere
# ERROR: "failed to list bundles: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 172.30.172.195:50051: connect: connection refused"
oc patch operatorhub.config.openshift.io/cluster -p='{"spec":{"sources":[{"disabled":true,"name":"community-operators"},{"disabled":true,"name":"redhat-marketplace"},{"disabled":true,"name":"redhat-operators"},{"disabled":true,"name":"certified-operators"}]}}' --type=merge

"$OPERATOR_SDK" run bundle "$ROOK_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CEPH_CSI_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CSI_ADDONS_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$NOOBAA_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

oc wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment \
    ceph-csi-controller-manager \
    ocs-client-operator-controller-manager \
    noobaa-operator \
    ocs-operator \
    rook-ceph-operator \
    ux-backend-server
