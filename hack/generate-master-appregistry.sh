#!/bin/bash

set -e

CSV_VERSION=4.8.0
source hack/generate-appregistry.sh

CSV_FILE=build/_output/appregistry/olm-catalog/ocs-operator/${CSV_VERSION}/ocs-operator.v${CSV_VERSION}.clusterserviceversion.yaml

latest_master_version() {
  URL=https://registry.hub.docker.com/v2/repositories/$1/tags/
  FILTER=${2:-.}
  VERSION=$(curl -s -S "$URL" | jq '."results"[]["name"]' | grep "$FILTER" | sort -nr | head -n1 | tr -d '"')
  echo "$VERSION"
}

CEPH_PATTERN="\(travisn\|ceph\)\/ceph:.*$"
CEPH_VERSION=$(latest_master_version ceph/ceph)
sed -i "s/${CEPH_PATTERN}/ceph\/ceph:${CEPH_VERSION}/" $CSV_FILE

NOOBAA_CORE_PATTERN="noobaa\/noobaa-core:[0-9.-]*$"
NOOBAA_CORE_VERSION=$(latest_master_version 'noobaa/noobaa-core' master)
sed -i "s/${NOOBAA_CORE_PATTERN}/noobaa\/noobaa-core:${NOOBAA_CORE_VERSION}/" $CSV_FILE

NOOBAA_OPERATOR_PATTERN="noobaa\/noobaa-operator:[0-9.-]*$"
NOOBAA_OPERATOR_VERSION=$(latest_master_version noobaa/noobaa-operator master)
sed -i "s/${NOOBAA_OPERATOR_PATTERN}/noobaa\/noobaa-operator:${NOOBAA_OPERATOR_VERSION}/" $CSV_FILE

ROOK_PATTERN="rook\/ceph:v[0-9.-]*$"
ROOK_VERSION="master"
sed -i "s/${ROOK_PATTERN}/rook\/ceph:${ROOK_VERSION}/" ${CSV_FILE}
