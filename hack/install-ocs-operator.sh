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
  CSI_DISABLE_HOLDER_PODS: "true"
  ROOK_CSI_DISABLE_DRIVER: "false"
EOF


patch_ocs_client_operator_config_configmap() {
    while true; do
        if oc get cm ocs-client-operator-config -n openshift-storage; then
            oc patch cm ocs-client-operator-config -n openshift-storage --type merge -p '{"data":{"DEPLOY_CSI":"false"}}'
            sleep 2
        fi
    done
}


"$OPERATOR_SDK" run bundle "$ROOK_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CSI_ADDONS_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CEPH_CSI_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
# Patch the ConfigMap in the background because an empty ConfigMap is initially created by the OLM as it is part of the ocs-client-operator bundle.
# Patching is done in the background to ensure that it happens immediately after creation, preventing the operator from running with the default
# configuration and deploying the CSI. This approach allows us to stop the CSI deployment by patching the ConfigMap as soon as it's created.
# We cannot create the ConfigMap early in the process because OLM overwrites it with an empty one later in the cycle.
patch_ocs_client_operator_config_configmap &
# Get the process ID (PID) of the background process
bg_pid=$!
# Trap to kill the process when the script exits
trap 'kill $bg_pid' EXIT

"$OPERATOR_SDK" run bundle "$OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$NOOBAA_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

oc wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment \
    ceph-csi-controller-manager \
    csi-addons-controller-manager \
    noobaa-operator \
    ocs-client-operator-console \
    ocs-client-operator-controller-manager \
    ocs-operator \
    rook-ceph-operator \
    ux-backend-server
