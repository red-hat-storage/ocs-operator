#!/bin/bash

IMAGE_RUN_CMD="${IMAGE_RUN_CMD:-docker run --rm -it}"

OUTDIR="build/_output"
OUTDIR_BIN="build/_output/bin"
OUTDIR_OCS_CI="build/_output/ocs-ci-testsuite"
OUTDIR_TEMPLATES="$OUTDIR/csv-templates"
OUTDIR_CRDS="$OUTDIR_TEMPLATES/crds"
OUTDIR_BUNDLEMANIFESTS="$OUTDIR_TEMPLATES/bundlemanifests"
OUTDIR_TOOLS="$OUTDIR/tools"
OUTDIR_CLUSTER_DEPLOY_MANIFESTS="$OUTDIR/cluster-deploy-manifests"

DEPLOY_YAML_PATH="deploy/deploy-with-olm.yaml"

REDHAT_OCS_CI_DEFAULT_TEST_EXPRESSION="TestOSCBasics or TestPvCreation or TestRawBlockPV or TestReclaimPolicy or TestCreateSCSameName or TestBasicPVCOperations or TestVerifyAllFieldsInScYamlWithOcDescribe"
REDHAT_OCS_CI_REPO="${REDHAT_OCS_CI_REPO:-https://github.com/red-hat-storage/ocs-ci}"
REDHAT_OCS_CI_HASH="${REDHAT_OCS_CI_HASH:-}"
REDHAT_OCS_CI_TEST_EXPRESSION="${REDHAT_OCS_CI_TEST_EXPRESSION:-$REDHAT_OCS_CI_DEFAULT_TEST_EXPRESSION}"
REDHAT_OCS_CI_PYTHON_BINARY="${REDHAT_OCS_CI_PYTHON_BINARY:-python3.7}"
REDHAT_OCS_CI_FORCE_TOOL_POD_INSTALL="${REDHAT_OCS_CI_FORCE_TOOL_POD_INSTALL:-false}"

# This env var allows developers to point to a custom oc tool that isn't in $PATH
# defaults to just using the 'oc' binary provided in $PATH
OCS_OC_PATH="${OCS_OC_PATH:-oc}"

NOOBAA_CSV="$OUTDIR_TEMPLATES/noobaa-csv.yaml"
ROOK_CSV="$OUTDIR_TEMPLATES/rook-csv.yaml.in"
OCS_CSV="$OUTDIR_TEMPLATES/ocs-operator.csv.yaml.in"

DEFAULT_IMAGE_REGISTRY="quay.io"
DEFAULT_REGISTRY_NAMESPACE="ocs-dev"
DEFAULT_IMAGE_TAG="latest"

IMAGE_REGISTRY="${IMAGE_REGISTRY:-${DEFAULT_IMAGE_REGISTRY}}"
REGISTRY_NAMESPACE="${REGISTRY_NAMESPACE:-}"
IMAGE_NAME="ocs-registry"
IMAGE_TAG="${IMAGE_TAG:-latest}"
FULL_IMAGE_NAME="${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"

default_local_storage_image="quay.io/gnufied/local-registry:v4.2.0"
LOCAL_STORAGE_IMAGE_NAME="${LOCAL_STORAGE_IMAGE:-$default_local_storage_image}"

OCS_CLUSTER_UNINSTALL="${OCS_CLUSTER_UNINSTALL:-true}"

# Override the image name when this is invoked from openshift ci
if [ -n "$OPENSHIFT_BUILD_NAMESPACE" ]; then
	FULL_IMAGE_NAME="registry.svc.ci.openshift.org/${OPENSHIFT_BUILD_NAMESPACE}/stable:ocs-registry"
	echo "Openshift CI detected, deploying using image $FULL_IMAGE_NAME"
fi
