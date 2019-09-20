#!/bin/bash
#
# Steps:
# Deploys the OCS cluster using the given image tag in quay.io
# Upgrade the OCS cluster to the latest build in openshift-ci or latest version of the image
# Verify the upgrade was successful 
#
# Usage examples:
# # Upgrade from lastest version in quay to the latest version ( or the latest build in openshift-ci)
# make upgrade-test
#
# # Upgrade from the latest version in quay.io to the given UPGRADE_VERSION
# UPGRADE_VERSION_REGISTRY_NAMESPACE=sacharya UPGRADE_VERSION_IMAGE_TAG=0.0.3 UPGRADE_VERSION_IMAGE_REGISTRY=quay.io UPGRADE_VERSION_SUBSCRIBE_CHANNEL=alpha make upgrade-test
#
# # Upgrade from the given version to the latest version in quay.io ( or the latest build in openshift-ci)
# REGISTRY_NAMESPACE=sacharya IMAGE_TAG=0.0.1 IMAGE_REGISTRY=quay.io make upgrade-test
#

set -e

source hack/common.sh

IMAGE_REGISTRY="${IMAGE_REGISTRY:-quay.io}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE:-}"
IMAGE_NAME="ocs-registry"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"
SUBSCRIBE_CHANNEL="${SUBSCRIBE_CHANNEL:alpha}"
DEPLOY_MANIFEST=$OUTDIR_CLUSTER_DEPLOY_MANIFESTS/deploy-with-olm.yaml

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
	timeout=1600
	while [ -z "$(oc get deployments -n openshift-storage | grep "${i} ")" ]; do
		echo "Waiting for deployment ${i} to be created..."
		sleep $sample
		current_time=$((current_time + sample))
		if [[ $current_time -gt $timeout ]]; then
			# On failure, dump debug info about the ocs-catalogsource
			echo "----------- dumping all pods for debug ----------------"
			oc get pods --all-namespaces
			echo "----------- dumping all pods for debug ----------------"
			echo ""
			echo "ERROR: Timed out waiting for deployment ${i} to be created"
			exit 1
		fi
	done
	echo "Deployment ${i} has been created"
done

for i in ${deployments[@]}; do
	echo "Waiting for deployment $i to become available"
	oc wait -n openshift-storage deployment ${i} --for condition=Available --timeout 12m
done

csv_name=$(oc get csvs -n openshift-storage | grep ocs-operator | head -1 | awk '{print $1}')
echo "Installed CSV $csv_name"

echo "Success! Deployed $FULL_IMAGE_NAME"
echo "Starting the upgrade process now"

# This image should point to the latest version in quay.io
# Or the latest build in openshift-ci environment.
UPGRADE_VERSION_IMAGE_REGISTRY="${UPGRADE_VERSION_IMAGE_REGISTRY:-quay.io}"
UPGRADE_VERSION_REGISTRY_NAMESPACE="${UPGRADE_VERSION_REGISTRY_NAMESPACE:-}"
UPGRADE_VERSION_IMAGE_NAME="ocs-registry"
UPGRADE_VERSION_IMAGE_TAG="${UPGRADE_VERSION_IMAGE_TAG:-latest}"
UPGRADE_VERSION_FULL_IMAGE_NAME="${UPGRADE_VERSION_IMAGE_REGISTRY}/${UPGRADE_VERSION_REGISTRY_NAMESPACE}/${UPGRADE_VERSION_IMAGE_NAME}:${UPGRADE_VERSION_IMAGE_TAG}"
UPGRADE_VERSION_SUBSCRIBE_CHANNEL="${UPGRADE_VERSION_SUBSCRIBE_CHANNEL:alpha}"

# Override the image name when this is invoked from openshift ci
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	UPGRADE_VERSION_FULL_IMAGE_NAME="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:ocs-registry"
	echo "Openshift CI detected, deploying using image $UPGRADE_VERSION_FULL_IMAGE_NAME"
fi

# If the image and channel are the same, no need to proceed with upgrade
if [ $FULL_IMAGE_NAME == $UPGRADE_VERSION_FULL_IMAGE_NAME ] && [ $SUBSCRIBE_CHANNEL == $UPGRADE_VERSION_SUBSCRIBE_CHANNEL ]; then
	echo "Detected registry image is the same as currently deployed image: $FULL_IMAGE_NAME"
	echo "Please provide a different registry image."
	exit 1
fi

echo "Updating catalogsource to use image: $UPGRADE_VERSION_FULL_IMAGE_NAME"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: CatalogSource
metadata:
  name: ocs-catalogsource
  namespace: openshift-marketplace
spec:
  sourceType: grpc
  image: $UPGRADE_VERSION_FULL_IMAGE_NAME
  displayName: Openshift Container Storage
  publisher: Red Hat
EOF

echo "Updating subscription to use channel: $UPGRADE_VERSION_SUBSCRIBE_CHANNEL"
cat <<EOF | oc apply -f -
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: ocs-subscription
  namespace: openshift-storage
spec:
  channel: $UPGRADE_VERSION_SUBSCRIBE_CHANNEL
  name: ocs-operator
  source: ocs-catalogsource
  sourceNamespace: openshift-marketplace
EOF

# Detect the new version of CSV ie. ocs-operator with a different version number
echo "Waiting for CSV not named $csv_name to appear"
retries=150
until [[ $retries == 0 || $new_csv_name ]]; do
    echo "Waiting for CSV to appear"
    new_csv_name=$(oc get csvs -n openshift-storage | grep ocs-operator | awk '{print $1}' | grep -v $csv_name 2>/dev/null || echo "")
    sleep 10
    retries=$((retries - 1))
done
if [ $retries == 0 ]; then
    echo "Error: cannot find any new ocs-operator CSV"
    oc get csvs -n openshift-storage
    exit 1
fi
echo "Detected new CSV: $new_csv_name"

# Wait till the CSV is successfully installed
retries=150
until [[ $retries == 0 || $new_csv_phase == "Succeeded" ]]; do
    new_csv_phase=$(oc get csv -n openshift-storage $new_csv_name -o jsonpath='{.status.phase}')
    if [[ $new_csv_phase != "$csv_phase" ]]; then
        csv_phase=$new_csv_phase
        echo "CSV $new_csv_name phase: $csv_phase"
    fi
    sleep 10
    retries=$((retries - 1))
done
if [ $retries == 0 ]; then
    echo "Error: CSV ocs-operator failed to reach phase succeeded"
    exit 1
fi

echo "Verify that subscription channel is updated after upgrade"
channel=$(oc get subscription ocs-subscription -n openshift-storage -o jsonpath='{.spec.channel}')
if [ $channel != $UPGRADE_VERSION_SUBSCRIBE_CHANNEL ]; then
    echo "Error: Detected channel $channel for subscription. Expected $UPGRADE_VERSION_SUBSCRIBE_CHANNEL"
    exit 1
else
    echo "ocs-subscription is subscribed to channel: $channel"
fi

echo "Verify that the pods are ready"
OCS_CATALOGSOURCE_POD=$(oc get pods -l olm.catalogSource=ocs-catalogsource -n openshift-marketplace | grep ocs-catalogsource | head -1 | awk '{print $1}')
oc wait pod $OCS_CATALOGSOURCE_POD --for condition=Ready -n openshift-marketplace --timeout 5m

OCS_OPERATOR_POD=$(oc get pods -n openshift-storage | grep ocs-operator | head -1 | awk '{ print $1 }')
oc wait pod $OCS_OPERATOR_POD --for condition=Ready -n openshift-storage --timeout 12m

echo "Verify that deployment image is updated after upgrade"
oc get deployment ocs-operator -n openshift-storage -o jsonpath='{.spec.template.spec.containers[0].image}'
echo ""
echo "Verify that subscription's currentCSV and installedCSV have moved to new version after upgrade"
oc get subscription ocs-subscription -n openshift-storage -o jsonpath='{.status.currentCSV}'
echo ""
oc get subscription ocs-subscription -n openshift-storage -o jsonpath='{.status.installedCSV}'
echo ""
echo "Success! Upgraded to $UPGRADE_VERSION_FULL_IMAGE_NAME"
