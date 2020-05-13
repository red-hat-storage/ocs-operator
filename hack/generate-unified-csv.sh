#!/bin/bash

set -e

source hack/common.sh

OCS_FINAL_DIR="deploy/olm-catalog/ocs-operator/manifests"
CSV_MERGER="tools/csv-merger/csv-merger"
(cd tools/csv-merger/ && go build)

function help_txt() {
	echo "Environment Variables"
	echo "    OCS_IMAGE:            (required) The ocs operator container image to integrate with"
	echo "    NOOBAA_IMAGE:         (required) The noobaa operator container image to integrate with"
	echo "    NOOBAA_CORE_IMAGE:    (required) The noobaa core container image to integrate with"
	echo "    NOOBAA_DB_IMAGE: 		(required) DB container image that is used by noobaa"
	echo "    ROOK_IMAGE:           (required) The rook operator container image to integrate with"
	echo "    CEPH_IMAGE:           (required) The ceph daemon container image to be deployed with storage clusters"
	echo "    CSV_VERSION:          (required) The ocs-operator csv version that will be generated"
	echo "    REPLACES_CSV_VERSION       (optional) The ocs-operator csv version this new csv will be updating"
	echo "    SKIP_RANGE                 (optional) The skip range value set for this csv"
	echo "    ROOK_CSI_CEPH_IMAGE        (optional) Sets custom image env var on the rook deployment spec"
	echo "    ROOK_CSI_REGISTRAR_IMAGE   (optional) Sets custom image env var on the rook deployment spec"
	echo "    ROOK_CSI_RESIZER_IMAGE     (optional) Sets custom image env var on the rook deployment spec"
	echo "    ROOK_CSI_PROVISIONER_IMAGE (optional) Sets custom image env var on the rook deployment spec"
	echo "    ROOK_CSI_SNAPSHOTTER_IMAGE (optional) Sets custom image env var on the rook deployment spec"
	echo "    ROOK_CSI_ATTACHER_IMAGE    (optional) Sets custom image env var on the rook deployment spec"
	echo ""
	echo "Example usage:"
	echo "    NOOBAA_IMAGE=<image> ROOK_IMAGE=<image> CSV_VERSION=<version> $0"
}

# check required env vars
if [ -z "$NOOBAA_IMAGE" ] || [ -z "$NOOBAA_CORE_IMAGE" ] || [ -z "$NOOBAA_DB_IMAGE" ] || \
   [ -z "$ROOK_IMAGE" ] || [ -z "$CSV_VERSION" ] || [ -z "$OCS_IMAGE" ] || [ -z "$CEPH_IMAGE" ]; then
	help_txt
	echo ""
	echo "ERROR: Missing required environment variables"
	exit 1
fi

hack/source-manifests.sh

# Merge component-level operators into ocs CSV
$CSV_MERGER \
	--csv-version="$CSV_VERSION" \
	--replaces-csv-version="$REPLACES_CSV_VERSION" \
	--skip-range="$SKIP_RANGE" \
	--rook-csv-filepath=$ROOK_CSV \
	--noobaa-csv-filepath=$NOOBAA_CSV \
	--ocs-csv-filepath=$OCS_CSV \
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
	--crds-directory="$OUTDIR_CRDS" \
	--manifests-directory=$OUTDIR_BUNDLEMANIFESTS \
	--olm-bundle-directory="$OCS_FINAL_DIR"

