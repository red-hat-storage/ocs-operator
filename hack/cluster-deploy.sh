#!/bin/bash

set -e

source hack/common.sh

IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE:-}"
IMAGE_NAME="ocs-registry"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"
DEPLOY_MANIFEST=$OUTDIR_CLUSTER_DEPLOY_MANIFESTS/deploy-with-olm.yaml

# Override the image name when this is invoked from openshift ci
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	FULL_IMAGE_NAME="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:ocs-registry"
	echo "Openshift CI detected, deploying using image $FULL_IMAGE_NAME"
fi

mkdir -p $OUTDIR_CLUSTER_DEPLOY_MANIFESTS

cp deploy/deploy-with-olm.yaml $DEPLOY_MANIFEST

sed -i "s|image\: .*/ocs-registry:.*$|image: ${FULL_IMAGE_NAME}|g" $DEPLOY_MANIFEST

echo "Applying ocs-operator manifests"
oc apply -f $DEPLOY_MANIFEST

# Wait for ocs-operator, rook operator and noobaa operator to come online
deployments=(ocs-operator rook-ceph-operator noobaa-operator)

for i in ${deployments[@]}; do
	current_time=0
	sample=10
	timeout=600
	while [ -z "$(oc get deployments -n openshift-storage | grep "${i} ")" ]; do
		echo "Waiting for deployment ${i} to be created..."
		sleep $sample
		current_time=$((current_time + sample))
		if [[ $current_time -gt $timeout ]]; then
			echo "Timed out waiting for deployment ${i} to be created"
			exit 1
		fi
	done
	echo "Deployment ${i} has been created"
done

for i in ${deployments[@]}; do
	echo "Waiting for deployment $i to become available"
	oc wait -n openshift-storage deployment ${i} --for condition=Available --timeout 12m
done

echo "Success!"
