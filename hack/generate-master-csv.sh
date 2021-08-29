#!/bin/bash

set -e

latest_master_version() {
  URL=https://registry.hub.docker.com/v2/repositories/$1/tags/
  FILTER=${2:-.}
  VERSION=$(curl -s -S "$URL" | jq '."results"[]["name"]' | grep "$FILTER" | sort -nr | head -n1 | tr -d '"')
  echo "$VERSION"
}

CEPH_PATTERN="ceph\/daemon-base:.*$"
CEPH_VERSION=$(latest_master_version ceph/daemon-base)
sed -i "s/${CEPH_PATTERN}/ceph\/daemon-base:${CEPH_VERSION}/" "$CSV_FILE"

NOOBAA_CORE_PATTERN="noobaa\/noobaa-core:[0-9.-]*$"
NOOBAA_CORE_VERSION=$(latest_master_version 'noobaa/noobaa-core' master)
sed -i "s/${NOOBAA_CORE_PATTERN}/noobaa\/noobaa-core:${NOOBAA_CORE_VERSION}/" "$CSV_FILE"

ROOK_PATTERN="rook\/ceph:v[a-zA-Z0-9.-]*$"
ROOK_VERSION="master"
sed -i "s/${ROOK_PATTERN}/rook\/ceph:${ROOK_VERSION}/" "${CSV_FILE}"
