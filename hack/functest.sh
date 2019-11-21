#!/bin/bash

source hack/common.sh

$OUTDIR_BIN/functests --ocs-registry-image="${FULL_IMAGE_NAME}" \
	--local-storage-registry-image="${LOCAL_STORAGE_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--upgrade-from-ocs-registry-image="${UPGRADE_FROM_OCS_REGISTRY_IMAGE}" \
	--upgrade-from-local-storage-registry-image="${UPGRADE_FROM_LOCAL_STORAGE_REGISTRY_NAME}" \
	--upgrade-from-ocs-subscription-channel="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" $@
if [ $? -ne 0 ]; then
	hack/dump-debug-info.sh
	echo "ERROR: Functest failed."
	exit 1
fi

must-gather/functests/functests.sh
