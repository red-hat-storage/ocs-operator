#!/bin/bash

oc delete -f pod_helper.yaml

# Add Ready nodes to the list
nodes=$(oc get nodes -l cluster.ocs.openshift.io/openshift-storage='' --no-headers | awk '/\yReady\y/{print $1}')

for node in ${nodes}; do
    pod_name=$(oc get pods -n openshift-storage | grep "${node//./}-debug" | awk '{print $1}')
    oc delete pod -l  "$pod_name" "${node//./}"-debug='ready'
done
