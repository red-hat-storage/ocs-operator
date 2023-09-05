#!/bin/bash

set -e

source hack/common.sh

(cd tools/cluster-deploy/ && go build)

CLUSTER_DEPLOY="tools/cluster-deploy/cluster-deploy"

# we want to handle errors explicitly at this point in order to dump debug info
set +e

$CLUSTER_DEPLOY --ocs-catalog-image="${CATALOG_IMAGE}" --ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" --install-namespace="${INSTALL_NAMESPACE}"

# shellcheck disable=SC2181
if [ $? -ne 0 ]; then
	hack/dump-debug-info.sh
	echo "ERROR: cluster-deploy failed."
	exit 1
fi
