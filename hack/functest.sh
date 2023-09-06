#!/usr/bin/env bash

source hack/common.sh

"${LOCALBIN}/${GINKGO_TEST_SUITE}_tests" -ginkgo.v \
    	--ocs-catalog-image="${FILE_BASED_CATALOG_FULL_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--install-namespace="${INSTALL_NAMESPACE}" \
	--upgrade-from-ocs-catalog-image="${UPGRADE_FROM_OCS_REGISTRY_IMAGE}" \
	--upgrade-from-ocs-subscription-channel="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-operator-install="${OCS_OPERATOR_INSTALL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" "$@"

# shellcheck disable=SC2181
if [ $? -ne 0 ]; then
	echo "ERROR: Functest failed."
	exit 1
fi
