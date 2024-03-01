#!/usr/bin/env bash

# example: ROOK_IMAGE=build-e858f56d/ceph-amd64:latest OCS_IMAGE=placeholder CSV_VERSION=1.1.1 hack/generate-manifests.sh

set -e

source hack/common.sh

function help_txt() {
	echo "Environment Variables"
	echo "    ROOK_IMAGE:           (required) The rook operator container image to integrate with"
	echo ""
	echo "Example usage:"
	echo "    ROOK_IMAGE=<image> $0"
}

# check required env vars
if [ -z "$ROOK_IMAGE" ]; then
	help_txt
	echo ""
	echo "ERROR: Missing required environment variables"
	exit 1
fi

# always start fresh and remove any previous artifacts that may exist.
mkdir -p $OUTDIR_TEMPLATES
mkdir -p $OUTDIR_CRDS

# ==== DUMP ROOK YAMLS ====
function dump_rook_csv() {
	rook_template_dir="/etc/ceph-csv-templates"
	rook_csv_template="rook-ceph.clusterserviceversion.yaml"
	rook_crds_dir=$rook_template_dir/ceph
	rook_crds_outdir="$OUTDIR_CRDS/rook"
	tmp_container="rook-$(date +%S%N)"
	
	rm -rf $ROOK_CSV
	mkdir -p $rook_crds_outdir
	# The actual folder will be created by the cp below but we don't want the folder to exist
	rm -rf $rook_crds_outdir

	echo "Creating rook image to play with using command: $IMAGE_BUILD_CMD create --name=$tmp_container $ROOK_IMAGE"
	$IMAGE_BUILD_CMD create --name="$tmp_container" "$ROOK_IMAGE"
	echo "Dumping rook csv using command: $IMAGE_BUILD_CMD cp $tmp_container:$rook_template_dir/$rook_csv_template $ROOK_CSV"
	$IMAGE_BUILD_CMD cp "$tmp_container":$rook_template_dir/$rook_csv_template $ROOK_CSV
	echo "Dumping rook crds using command: $IMAGE_BUILD_CMD cp $tmp_container:$rook_crds_dir $rook_crds_outdir"
	$IMAGE_BUILD_CMD cp "$tmp_container":$rook_crds_dir $rook_crds_outdir
	echo "Cleaning up rook container with command: $IMAGE_BUILD_CMD rm $tmp_container"
	$IMAGE_BUILD_CMD rm "$tmp_container"
}

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
function gen_ocs_csv() {
	echo "Generating OpenShift Container Storage CSV"
	rm -rf "$(dirname $OCS_FINAL_DIR)"
	ocs_crds_outdir="$OUTDIR_CRDS/ocs"
	rm -rf $OUTDIR_TEMPLATES/manifests/ocs-operator.clusterserviceversion.yaml
	rm -rf $OCS_CSV
	rm -rf $ocs_crds_outdir
	mkdir -p $ocs_crds_outdir

	gen_args="generate kustomize manifests --input-dir config/manifests/ocs-operator --output-dir config/manifests/ocs-operator --package ocs-operator -q"
	# shellcheck disable=SC2086
	$OPERATOR_SDK $gen_args
	pushd config/manager
	$KUSTOMIZE edit set image ocs-dev/ocs-operator="$OCS_IMAGE"
	popd
	$KUSTOMIZE build config/manifests/ocs-operator | $OPERATOR_SDK generate bundle -q --overwrite=false --output-dir deploy/ocs-operator --kustomize-dir config/manifests/ocs-operator --package ocs-operator --version "$CSV_VERSION" --extra-service-accounts=ux-backend-server
	mv deploy/ocs-operator/manifests/*clusterserviceversion.yaml $OCS_CSV
	cp config/crd/bases/* $ocs_crds_outdir
}

if [ -z "$OPENSHIFT_BUILD_NAMESPACE" ] && [ -z "$SKIP_CSV_DUMP" ]; then
	source hack/docker-common.sh
	dump_rook_csv
fi

gen_ocs_csv

echo "Manifests sourced into $OUTDIR_TEMPLATES directory"

mv bundle.Dockerfile Dockerfile.bundle
