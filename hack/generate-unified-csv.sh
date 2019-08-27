#!/bin/bash

set -e

source hack/common.sh

OCS_FINAL_DIR="deploy/olm-catalog/ocs-operator/${CSV_VERSION}"
CSV_MERGER="tools/csv-merger/csv-merger"
(cd tools/csv-merger/ && go build)

function help_txt() {
	echo "Environment Variables"
	echo "    OCS_IMAGE:            (required) The ocs operator container image to integrate with"
	echo "    NOOBAA_IMAGE:         (required) The noobaa operator container image to integrate with"
	echo "    ROOK_IMAGE:           (required) The rook operator container image to integrate with"
	echo "    CSV_VERSION:          (required) The ocs-operator csv version that will be generated"
	echo "    REPLACES_CSV_VERSION  (optional) The ocs-operator csv version this new csv will be updating"
	echo ""
	echo "Example usage:"
	echo "    NOOBAA_IMAGE=<image> ROOK_IMAGE=<image> CSV_VERSION=<version> $0"
}

# check required env vars
if [ -z $NOOBAA_IMAGE ] || [ -z $ROOK_IMAGE ] || [ -z $CSV_VERSION ] || [ -z $OCS_IMAGE ]; then
	help_txt
	echo ""
	echo "ERROR: Missing required environment variables"
	exit 1
fi

if [ ! -d $OUTDIR_TEMPLATES ]; then
	echo "ERROR: no manifests found."
	echo "Run 'make source-manifests' in order to source component-level manifests"
fi

# Merge component-level operators into ocs CSV
$CSV_MERGER \
	--csv-version=$CSV_VERSION \
	--replaces-csv-version=$REPLACES_CSV_VERSION \
	--rook-csv-filepath=$ROOK_CSV \
	--noobaa-csv-filepath=$NOOBAA_CSV \
	--ocs-csv-filepath=$OCS_CSV \
	--rook-container-image=$ROOK_IMAGE \
	--noobaa-container-image=$NOOBAA_IMAGE \
	--ocs-container-image=$OCS_IMAGE \
	--crds-directory=$OUTDIR_CRDS \
	--olm-bundle-directory=$OCS_FINAL_DIR

