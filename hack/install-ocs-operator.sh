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
  ROOK_CSI_DISABLE_DRIVER: "true"
EOF

patch_ocs_client_operator_config_configmap() {
    while true; do
        if oc get cm ocs-client-operator-config -n openshift-storage; then
            oc patch cm ocs-client-operator-config -n openshift-storage --type merge -p '{"data":{"disableInstallPlanAutoApproval":"true"}}'
            sleep 2
        fi
    done
}

# This ensures that we don't face issues related to OCP catalogsources with below error
# It is safe to disable these catalogsources as we are not using them anywhere
# ERROR: "failed to list bundles: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 172.30.172.195:50051: connect: connection refused"
oc patch operatorhub.config.openshift.io/cluster -p='{"spec":{"sources":[{"disabled":true,"name":"community-operators"},{"disabled":true,"name":"redhat-marketplace"},{"disabled":true,"name":"redhat-operators"},{"disabled":true,"name":"certified-operators"}]}}' --type=merge

"$OPERATOR_SDK" run bundle "$ROOK_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$NOOBAA_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

# These bundles are dependencies of ocs-client-operator
"$OPERATOR_SDK" run bundle "$CSI_ADDONS_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$CEPH_CSI_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$RECIPE_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$SNAPSHOT_CONTROLLER_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

# Patch the ConfigMap in the background because an empty ConfigMap is initially created by the OLM as it is part of the ocs-client-operator bundle.
# Patching is done in the background to ensure that it happens immediately after creation, preventing the operator from running with the default
# configuration and approving the install plans.
# We cannot create the ConfigMap early in the process because OLM overwrites it with an empty one later in the cycle.
patch_ocs_client_operator_config_configmap &
# Get the process ID (PID) of the background process
bg_pid=$!
# Trap to kill the process when the script exits
trap 'kill $bg_pid' EXIT
"$OPERATOR_SDK" run bundle "$OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
"$OPERATOR_SDK" run bundle "$BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"
