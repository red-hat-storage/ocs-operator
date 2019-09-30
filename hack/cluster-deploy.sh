#!/bin/bash

set -e

source hack/common.sh

(cd tools/cluster-deploy/ && go build)

CLUSTER_DEPLOY="tools/cluster-deploy/cluster-deploy"

$CLUSTER_DEPLOY --ocs-registry-image="${FULL_IMAGE_NAME}" --local-storage-registry-image="${LOCAL_STORAGE_IMAGE_NAME}"

