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


scale_down_ocs_client_operator_deployments() {
    while true; do
        if oc get deployment ocs-client-operator-controller-manager ocs-client-operator-console -n "$INSTALL_NAMESPACE" >/dev/null 2>&1; then
            # Get the current replica count for the deployments
            current_replicas_manager=$(oc get deployment ocs-client-operator-controller-manager -n "$INSTALL_NAMESPACE" -o=jsonpath='{.spec.replicas}')
            current_replicas_console=$(oc get deployment ocs-client-operator-console -n "$INSTALL_NAMESPACE" -o=jsonpath='{.spec.replicas}')
            # Scale down only if the replica count is 1
            if [ "$current_replicas_manager" -eq 1 ] || [ "$current_replicas_console" -eq 1 ]; then
                oc scale deployments ocs-client-operator-controller-manager ocs-client-operator-console --replicas=0 -n "$INSTALL_NAMESPACE"
            fi
        fi
        sleep 1
    done
}


"$OPERATOR_SDK" run bundle "$ROOK_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CSI_ADDONS_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CEPH_CSI_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
# Normally, the ODF operator would handle scaling down the ocs-client-operator-controller-manager and ocs-client-operator-console deployments.
# However, in the ocs-operator CI environment, since the ODF operator is not running, we manually scale down these deployments within this script.
# We keep the process running in the background to ensure that if OLM attempts to bring the deployments back up, we can immediately scale them down again.
scale_down_ocs_client_operator_deployments &

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
