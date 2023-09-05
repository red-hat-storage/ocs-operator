#!/bin/bash

set -e

source hack/common.sh

"$OCS_OC_PATH" get namespace "$INSTALL_NAMESPACE" >/dev/null 2>&1 && echo "Namespace $INSTALL_NAMESPACE already exists." || \
    "$OCS_OC_PATH" create namespace "$INSTALL_NAMESPACE" && echo "Namespace $INSTALL_NAMESPACE created."

"$OPERATOR_SDK" run bundle "$NOOBAA_BUNDLE_IMAGE" --timeout=10m --security-context-config restricted -n "$INSTALL_NAMESPACE"

"$OCS_OC_PATH" wait --timeout=5m --for condition=Available -n "$INSTALL_NAMESPACE" deployment noobaa-operator
