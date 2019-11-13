#!/bin/bash

set -e

source hack/common.sh

CSV_CHECKSUM="tools/csv-checksum/csv-checksum"
(cd tools/csv-checksum/ && go build)

export CSV_CHECKSUM_OUTFILE="hack/latest-csv-checksum.md5"

# Current DEV version of the CSV
export CSV_VERSION=0.0.1

# Current dependency images our DEV CSV are pinned to
export ROOK_IMAGE=${ROOK_IMAGE:-"rook/ceph:v1.1.4-27.gf20c056"}
export NOOBAA_IMAGE=${NOOBAA_IMAGE:-"noobaa/noobaa-operator:2.0.7"}
export NOOBAA_CORE_IMAGE=${NOOBAA_CORE_IMAGE:-"noobaa/noobaa-core:5.2.7"}
export NOOBAA_DB_IMAGE=${NOOBAA_DB_IMAGE:-"centos/mongodb-36-centos7"}
export CEPH_IMAGE=${CEPH_IMAGE:-"ceph/ceph:v14.2"}
export OCS_IMAGE=${OCS_IMAGE:-"quay.io/ocs-dev/ocs-operator:latest"}

echo "=== Generating DEV CSV with the following vars ==="
echo -e "\tCSV_VERSION=$CSV_VERSION"
echo -e "\tROOK_IMAGE=$ROOK_IMAGE"
echo -e "\tNOOBAA_IMAGE=$NOOBAA_IMAGE"
echo -e "\tNOOBAA_CORE_IMAGE=$NOOBAA_CORE_IMAGE"
echo -e "\tNOOBAA_DB_IMAGE=$NOOBAA_DB_IMAGE"
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
	--noobaa-core-image=$NOOBAA_CORE_IMAGE \
	--noobaa-db-image=$NOOBAA_DB_IMAGE \
	--ocs-image=$OCS_IMAGE \
	--checksum-outfile=$CSV_CHECKSUM_OUTFILE
