#!/bin/bash

set -e

source hack/common.sh

CSV_CHECKSUM="tools/csv-checksum/csv-checksum"
(cd tools/csv-checksum/ && go build)

NOOBAA_VERSION="1.1.0"
ROOK_VERSION="v1.0.0-586.g8abcf37.dirty "

export CSV_CHECKSUM_OUTFILE="hack/latest-csv-checksum.md5"

# Current DEV version of the CSV
export CSV_VERSION=0.0.1

# Current dependency images our DEV CSV are pinned to
export ROOK_IMAGE=rook/ceph:$ROOK_VERSION
export NOOBAA_IMAGE=noobaa/noobaa-operator:$NOOBAA_VERSION
export CEPH_IMAGE=ceph/ceph:v14.2.2-20190828
export OCS_IMAGE=quay.io/ocs-dev/ocs-operator:latest

echo "=== Generating DEV CSV with the following vars ==="
echo -e "\tCSV_VERSION=$CSV_VERSION"
echo -e "\tROOK_IMAGE=$ROOK_IMAGE"
echo -e "\tNOOBAA_IMAGE=$NOOBAA_IMAGE"
echo -e "\tCEPH_IMAGE=$CEPH_IMAGE"
echo -e "\tOCS_IMAGE=$OCS_IMAGE"

if [ -z "${CSV_CHECKSUM_ONLY}" ]; then
	hack/source-manifests.sh
fi

if [ -z "${CSV_CHECKSUM_ONLY}" ]; then
	hack/generate-unified-csv.sh
fi

echo "Generating MD5 Checksum for CSV with version $CSV_VERSION"
$CSV_CHECKSUM \
	--csv-version=$CSV_VERSION \
	--replaces-csv-version=$REPLACES_CSV_VERSION \
	--rook-image=$ROOK_IMAGE \
	--ceph-image=$CEPH_IMAGE \
	--rook-csi-ceph-image=$ROOK_CSI_CEPH_IMAGE \
	--rook-csi-registrar-image=$ROOK_CSI_REGISTRAR_IMAGE \
	--rook-csi-provisioner-image=$ROOK_CSI_PROVISIONER_IMAGE \
	--rook-csi-snapshotter-image=$ROOK_CSI_SNAPSHOTTER_IMAGE \
	--rook-csi-attacher-image=$ROOK_CSI_ATTACHER_IMAGE \
	--noobaa-image=$NOOBAA_IMAGE \
	--ocs-image=$OCS_IMAGE \
	--checksum-outfile=$CSV_CHECKSUM_OUTFILE
