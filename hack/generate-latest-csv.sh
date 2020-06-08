#!/bin/bash

set -e

source hack/common.sh

CSV_CHECKSUM="tools/csv-checksum/csv-checksum"
(cd tools/csv-checksum/ && go build)

export CSV_CHECKSUM_OUTFILE="hack/latest-csv-checksum.md5"

export SKIP_MINIMUM="0.0.1"
export SKIP_RANGE=">=${SKIP_MINIMUM} <${CSV_VERSION}"

# Current dependency images our DEV CSV are pinned to
export ROOK_IMAGE=${ROOK_IMAGE:-"quay.io/multi-arch/ocs4-rook-ceph-rhel8-operator:latest"}
export NOOBAA_IMAGE=${NOOBAA_IMAGE:-"quay.io/multi-arch/ocs4-mcg-rhel8-operator:4.3-2-ppc64le"}
export NOOBAA_CORE_IMAGE=${NOOBAA_CORE_IMAGE:-"quay.io/multi-arch/ocs4-mcg-core-rhel8:4.3-2-ppc64le"}
export NOOBAA_DB_IMAGE=${NOOBAA_DB_IMAGE:-"hub.docker.com/r/ibmcom/mongodb:3.6.0-ppc64le"}
export CEPH_IMAGE=${CEPH_IMAGE:-"quay.io/multi-arch/rhceph-rhceph-4-rhel8:latest"}
export OCS_IMAGE=${OCS_IMAGE:-"${IMAGE_REGISTRY}/${REGISTRY_NAMESPACE}/${OPERATOR_IMAGE_NAME}:${IMAGE_TAG}"}

echo "=== Generating DEV CSV with the following vars ==="
echo -e "\tCSV_VERSION=$CSV_VERSION"
echo -e "\tROOK_IMAGE=$ROOK_IMAGE"
echo -e "\tNOOBAA_IMAGE=$NOOBAA_IMAGE"
echo -e "\tNOOBAA_CORE_IMAGE=$NOOBAA_CORE_IMAGE"
echo -e "\tNOOBAA_DB_IMAGE=$NOOBAA_DB_IMAGE"
echo -e "\tCEPH_IMAGE=$CEPH_IMAGE"
echo -e "\tOCS_IMAGE=$OCS_IMAGE"

if [ -z "${CSV_CHECKSUM_ONLY}" ]; then
	hack/generate-unified-csv.sh
fi

echo "Generating MD5 Checksum for CSV with version $CSV_VERSION"
$CSV_CHECKSUM \
	--csv-version=$CSV_VERSION \
	--replaces-csv-version="$REPLACES_CSV_VERSION" \
	--rook-image="$ROOK_IMAGE" \
	--ceph-image="$CEPH_IMAGE" \
	--rook-csi-ceph-image="$ROOK_CSI_CEPH_IMAGE" \
	--rook-csi-registrar-image="$ROOK_CSI_REGISTRAR_IMAGE" \
	--rook-csi-resizer-image="$ROOK_CSI_RESIZER_IMAGE" \
	--rook-csi-provisioner-image="$ROOK_CSI_PROVISIONER_IMAGE" \
	--rook-csi-snapshotter-image="$ROOK_CSI_SNAPSHOTTER_IMAGE" \
	--rook-csi-attacher-image="$ROOK_CSI_ATTACHER_IMAGE" \
	--noobaa-image="$NOOBAA_IMAGE" \
	--noobaa-core-image="$NOOBAA_CORE_IMAGE" \
	--noobaa-db-image="$NOOBAA_DB_IMAGE" \
	--ocs-image="$OCS_IMAGE" \
	--checksum-outfile="$CSV_CHECKSUM_OUTFILE"
