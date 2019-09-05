#!/bin/bash

set -e

source hack/common.sh

echo "Deleting ocs-operator manifests"
# even though we alter the deploy-with-olm.yaml in cluster-deploy,
# the object names and namespaces remain the same, which allows us
# to clean the cluster using the original manifest.
oc delete --ignore-not-found -f deploy/deploy-with-olm.yaml

echo "Waiting on namespaces to disappear"
# We wait for the namespaces to disappear because that signals
# to us that the delete is finalized. Otherwise a 'cluster-deploy'
# might fail if all cluster artifacts haven't finished being removed.
managed_namespaces=(openshift-storage local-storage)
for i in ${managed_namespaces[@]}; do
	if [ -n "$(oc get namespace | grep "${i} ")" ]; then
		echo "Deleting namespace ${i}"
		oc delete --ignore-not-found namespace ${i}

		current_time=0
		sample=10
		timeout=120
		echo "Waiting for ${i} namespace to disappear ..."
		while [ -n "$(oc get namespace | grep "${i} ")" ]; do
			sleep $sample
			current_time=$((current_time + sample))
			if [[ $current_time -gt $timeout ]]; then
				exit 1
			fi
		done
	fi
done

# clean old
rm -rf $OUTDIR_CLUSTER_DEPLOY_MANIFESTS
