#!/bin/bash

source hack/common.sh

$OUTDIR_BIN/negativetests --ocs-registry-image="${FULL_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" "$@"
if [ $? -ne 0 ]; then
	hack/dump-debug-info.sh
	echo "ERROR: Negative tests failed."
	exit 1
fi
