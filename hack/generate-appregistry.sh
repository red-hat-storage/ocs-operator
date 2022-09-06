#!/usr/bin/env bash

set -e

source hack/common.sh

APPREGISRTY_DIR="build/_output/appregistry/olm-catalog/ocs-operator"
rm -rf ${APPREGISRTY_DIR}
mkdir -p "${APPREGISRTY_DIR}/${CSV_VERSION}"
cp -r ${OCS_FINAL_DIR}/* "${APPREGISRTY_DIR}/${CSV_VERSION}/"
mv "${APPREGISRTY_DIR}/${CSV_VERSION}/ocs-operator.clusterserviceversion.yaml" "${APPREGISRTY_DIR}/${CSV_VERSION}/ocs-operator.v${CSV_VERSION}.clusterserviceversion.yaml"
cat > ${APPREGISRTY_DIR}/ocs-operator.package.yaml <<EOF
channels:
- name: alpha
  currentCSV: ocs-operator.v${CSV_VERSION}
defaultChannel: alpha
packageName: ocs-operator
EOF
