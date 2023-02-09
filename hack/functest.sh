#!/usr/bin/env bash

source hack/common.sh

"$OUTDIR_BIN/${GINKGO_TEST_SUITE}_tests" -ginkgo.v \
    	--ocs-catalog-image="${FILE_BASED_CATALOG_FULL_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--upgrade-from-ocs-catalog-image="${UPGRADE_FROM_OCS_REGISTRY_IMAGE}" \
	--upgrade-from-ocs-subscription-channel="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-operator-install="${OCS_OPERATOR_INSTALL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" "$@"

if [ $? -ne 0 ]; then
	echo "ERROR: Functest failed."
	exit 1
fi
