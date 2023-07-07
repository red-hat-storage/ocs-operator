#!/usr/bin/env bash

set -e

source hack/common.sh

mkdir -p ${LOCALBIN}

if ! [ -x "$GINKGO" ]; then
	echo "Installing GINKGO binary at $GINKGO"
	GOBIN=${LOCALBIN} go install -v github.com/onsi/ginkgo/v2/ginkgo
else
	echo "GINKO binary found at $GINKGO"
fi

"${GINKGO}" build "functests/${GINKGO_TEST_SUITE}/"

mv "functests/${GINKGO_TEST_SUITE}/${GINKGO_TEST_SUITE}.test" "${LOCALBIN}/${GINKGO_TEST_SUITE}_tests"

"${LOCALBIN}/${GINKGO_TEST_SUITE}_tests" -ginkgo.v \
    --ocs-catalog-image="${FILE_BASED_CATALOG_FULL_IMAGE_NAME}" \
	--ocs-subscription-channel="${OCS_SUBSCRIPTION_CHANNEL}" \
	--install-namespace="${INSTALL_NAMESPACE}" \
	--upgrade-from-ocs-catalog-image="${UPGRADE_FROM_OCS_REGISTRY_IMAGE}" \
	--upgrade-from-ocs-subscription-channel="${UPGRADE_FROM_OCS_SUBSCRIPTION_CHANNEL}" \
	--ocs-operator-install="${OCS_OPERATOR_INSTALL}" \
	--ocs-cluster-uninstall="${OCS_CLUSTER_UNINSTALL}" "$@"

if [ $? -ne 0 ]; then
	echo "ERROR: Functest failed."
	exit 1
fi
