#!/bin/bash

source hack/common.sh

$OUTDIR_BIN/functests --ocs-registry-image="${CATALOG_FULL_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--upgrade-from-ocs-registry-image="${UPGRADE_FROM_OCS_REGISTRY_IMAGE}" \
	--upgrade-from-ocs-subscription-channel="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" "$@"

if [ $? -ne 0 ]; then
	hack/dump-debug-info.sh
	echo "ERROR: Functest failed."
	exit 1
fi
