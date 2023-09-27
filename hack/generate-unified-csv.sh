#!/usr/bin/env bash

set -e

source hack/common.sh

CSV_MERGER="tools/csv-merger/csv-merger"

(cd tools/csv-merger/ && go build -ldflags="${LDFLAGS}")

# check required env vars
if [ -z "$CSV_VERSION" ] || [ -z "$OCS_IMAGE" ] || [ -z "$OCS_METRICS_EXPORTER_IMAGE" ] || \
[ -z "$ROOK_IMAGE" ] || [ -z "$CEPH_IMAGE" ] || [ -z "$NOOBAA_CORE_IMAGE" ] || [ -z "$NOOBAA_DB_IMAGE" ] \
; then
	echo "ERROR: Missing required environment variables"
	exit 1
fi

# Merge component-level operators into ocs CSV
$CSV_MERGER \
	--csv-version="$CSV_VERSION" \
	--replaces-csv-version="$REPLACES_CSV_VERSION" \
	--skip-range="$SKIP_RANGE" \
	--ocs-image="$OCS_IMAGE" \
	--ocs-metrics-exporter-image="$OCS_METRICS_EXPORTER_IMAGE" \
	--rook-image="$ROOK_IMAGE" \
	--ceph-image="$CEPH_IMAGE" \
	--rook-csiaddons-image="$ROOK_CSIADDONS_IMAGE" \
	--rook-csi-ceph-image="$ROOK_CSI_CEPH_IMAGE" \
	--rook-csi-registrar-image="$ROOK_CSI_REGISTRAR_IMAGE" \
	--rook-csi-resizer-image="$ROOK_CSI_RESIZER_IMAGE" \
	--rook-csi-provisioner-image="$ROOK_CSI_PROVISIONER_IMAGE" \
	--rook-csi-snapshotter-image="$ROOK_CSI_SNAPSHOTTER_IMAGE" \
	--rook-csi-attacher-image="$ROOK_CSI_ATTACHER_IMAGE" \
	--noobaa-core-image="$NOOBAA_CORE_IMAGE" \
	--noobaa-db-image="$NOOBAA_DB_IMAGE" \
	--ocs-must-gather-image="$OCS_MUST_GATHER_IMAGE" \
	--rook-csv-filepath=$ROOK_CSV \
	--ocs-csv-filepath=$OCS_CSV \
	--crds-directory="$CRDS_DIR" \
	--manifests-directory=$EXTRA_MANIFESTS_DIR \
	--olm-bundle-directory="$BUNDLE_MANIFESTS_DIR" \
	--timestamp="$TIMESTAMP" 
