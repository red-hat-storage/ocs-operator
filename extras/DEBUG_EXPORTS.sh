#!/bin/bash
set -x
export OPERATOR_NAME=ocs-operator
export WATCH_NAMESPACE=openshift-storage
export KUBERNETES_CONFIG=${KUBECONFIG}
set +x
echo Run the operator with: go run cmd/manager/main.go
