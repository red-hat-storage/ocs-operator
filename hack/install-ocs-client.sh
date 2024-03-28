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

"$OPERATOR_SDK" run bundle "$OCS_CLIENT_BUNDLE_FULL_IMAGE_NAME" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE" --index-image "$CSI_ADDONS_CATALOG_FULL_IMAGE_NAME"

oc wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment ocs-client-operator-controller-manager
oc wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment csi-addons-controller-manager
