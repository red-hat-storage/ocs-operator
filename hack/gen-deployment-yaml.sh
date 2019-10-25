#!/bin/bash

set -e

source hack/common.sh

# use the default image name when generating deploy-with-olm.yaml
DEPLOY_YAML_IMAGE_NAME="${DEFAULT_IMAGE_REGISTRY}/${DEFAULT_REGISTRY_NAMESPACE}/${IMAGE_NAME}:${IMAGE_TAG}"

(cd tools/cluster-deploy/ && go build)

CLUSTER_DEPLOY="tools/cluster-deploy/cluster-deploy"

$CLUSTER_DEPLOY --ocs-registry-image="${DEPLOY_YAML_IMAGE_NAME}" --local-storage-registry-image="${LOCAL_STORAGE_IMAGE_NAME}" --yaml-output-path=${DEPLOY_YAML_PATH}
