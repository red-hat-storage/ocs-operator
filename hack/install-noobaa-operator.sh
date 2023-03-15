#!/bin/bash

set -o nounset
set -o errexit
set -o pipefail

INSTALL_NAMESPACE=${OO_INSTALL_NAMESPACE:-'openshift-storage'}
CATALOG_IMAGE=${CATALOG_IMAGE:-'quay.io/ocs-dev/ocs-operator-catalog:noobaa-v5.13.0'}

CATALOG=$(oc -n openshift-marketplace get catsrc ocs-catalog -o jsonpath="{.metadata.name}" 2>/dev/null || true)
if [[ -n "$CATALOG" ]]; then
    echo "CatalogSource \"$CATALOG\" exists"
else
    #echo "CatalogSource \"ocs-catalog\" does not exist: creating it"
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ocs-catalog
  namespace: openshift-marketplace
spec:
  displayName: OCS Catalog
  image: $CATALOG_IMAGE
  publisher: Red Hat
  sourceType: grpc
EOF
fi

NAMESPACE=$(oc get ns "$INSTALL_NAMESPACE" -o jsonpath="{.metadata.name}" 2>/dev/null || true)
if [[ -n "$NAMESPACE" ]]; then
    echo "Namespace \"$NAMESPACE\" exists"
else
    #echo "Namespace \"$INSTALL_NAMESPACE\" does not exist: creating it"
oc apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: $INSTALL_NAMESPACE
EOF
fi

OPERATORGROUP=$(oc -n "$INSTALL_NAMESPACE" get operatorgroup -o jsonpath="{.items[*].metadata.name}" || true)
if [[ -n "$OPERATORGROUP" ]]; then
    echo "OperatorGroup \"$OPERATORGROUP\" exists"
else
    #echo "OperatorGroup does not exist: creating it"
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: ocs-operatorgroup
  namespace: $INSTALL_NAMESPACE
spec:
  targetNamespaces: [$INSTALL_NAMESPACE]
EOF
fi

SUB=$(oc -n "$INSTALL_NAMESPACE" get sub -o jsonpath="{.items[?(@.spec.name=='noobaa-operator')].metadata.name}" || true)
if [[ -n "$SUB" ]]; then
    echo "Subscription \"$SUB\" exists"
else
    #echo "Subscription \"noobaa-operator\" does not exist: creating it"
    SUB="noobaa-operator"
oc apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: noobaa-operator
  namespace: $INSTALL_NAMESPACE
spec:
  installPlanApproval: Automatic
  name: noobaa-operator
  source: ocs-catalog
  sourceNamespace: openshift-marketplace
EOF
fi

for _ in {1..60}; do
    IP=$(oc -n "$INSTALL_NAMESPACE" get sub "$SUB" -o jsonpath="{.status.installplan.name}" || true)
    if [[ -n "$IP" ]]; then
        echo "Approving installplan \"$IP\""
        oc -n "$INSTALL_NAMESPACE" patch installplan "$IP" --type merge --patch '{"spec":{"approved":true}}'
        break
    fi
    sleep 10
done

for _ in {1..60}; do
    CSV=$(oc -n "$INSTALL_NAMESPACE" get sub "$SUB" -o jsonpath="{.status.installedCSV}" || true)
    if [[ -n "$CSV" ]]; then
        if [[ "$(oc -n "$INSTALL_NAMESPACE" get csv "$CSV" -o jsonpath='{.status.phase}')" == "Succeeded" ]]; then
            echo "ClusterServiceVersion \"$CSV\" is ready"
            exit 0
        fi
    fi
    sleep 10
done

echo "Timed out waiting for noobaa CSV to become ready"
oc -n "$INSTALL_NAMESPACE" get sub "$SUB" -o yaml
exit 1
