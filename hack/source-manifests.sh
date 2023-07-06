#!/usr/bin/env bash

# example: ROOK_IMAGE=build-e858f56d/ceph-amd64:latest NOOBAA_IMAGE=noobaa/noobaa-operator:1.1.0 OCS_IMAGE=placeholder CSV_VERSION=1.1.1 hack/generate-manifests.sh

set -e

source hack/common.sh
source hack/operator-sdk-common.sh

function help_txt() {
	echo "Environment Variables"
	echo "    NOOBAA_IMAGE:         (required) The noobaa operator container image to integrate with"
	echo "    ROOK_IMAGE:           (required) The rook operator container image to integrate with"
	echo ""
	echo "Example usage:"
	echo "    NOOBAA_IMAGE=<image> ROOK_IMAGE=<image> $0"
}

# check required env vars
if [ -z "$NOOBAA_IMAGE" ] || [ -z "$ROOK_IMAGE" ]; then
	help_txt
	echo ""
	echo "ERROR: Missing required environment variables"
	exit 1
fi

# always start fresh and remove any previous artifacts that may exist.
mkdir -p $OUTDIR_TEMPLATES
mkdir -p $OUTDIR_CRDS
mkdir -p $OUTDIR_TOOLS

# ==== DUMP NOOBAA YAMLS ====
function dump_noobaa_csv() {
	noobaa_dump_crds_cmd="crd yaml"
	noobaa_dump_csv_cmd="olm csv"
	noobaa_crds_outdir="$OUTDIR_CRDS/noobaa"
	rm -rf $NOOBAA_CSV
	rm -rf $noobaa_crds_outdir
	mkdir -p $noobaa_crds_outdir

	echo "Dumping Noobaa csv using command: $IMAGE_RUN_CMD --entrypoint=/usr/local/bin/noobaa-operator $NOOBAA_IMAGE $noobaa_dump_csv_cmd"
	# shellcheck disable=SC2086
	($IMAGE_RUN_CMD --entrypoint=/usr/local/bin/noobaa-operator "$NOOBAA_IMAGE" $noobaa_dump_csv_cmd) > $NOOBAA_CSV
	echo "Dumping Noobaa crds using command: $IMAGE_RUN_CMD --entrypoint=/usr/local/bin/noobaa-operator $NOOBAA_IMAGE $noobaa_dump_crds_cmd"
	# shellcheck disable=SC2086
	($IMAGE_RUN_CMD --entrypoint=/usr/local/bin/noobaa-operator "$NOOBAA_IMAGE" $noobaa_dump_crds_cmd) > $noobaa_crds_outdir/noobaa-crd.yaml
}

# ==== DUMP ROOK YAMLS ====
function dump_rook_csv() {
	rook_template_dir="/etc/ceph-csv-templates"
	rook_csv_template="rook-ceph.clusterserviceversion.yaml"
	rook_crds_dir=$rook_template_dir/ceph
	rook_crds_outdir="$OUTDIR_CRDS/rook"
	rm -rf $ROOK_CSV
	rm -rf $rook_crds_outdir
	mkdir -p $rook_crds_outdir

	crd_list=$(mktemp)
	echo "Dumping rook csv using command: $IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $rook_template_dir/$rook_csv_template"
	$IMAGE_RUN_CMD --entrypoint=cat "$ROOK_IMAGE" $rook_template_dir/$rook_csv_template > $ROOK_CSV
	echo "Listing rook crds using command: $IMAGE_RUN_CMD --entrypoint=ls $ROOK_IMAGE -1 $rook_crds_dir/"
	$IMAGE_RUN_CMD --entrypoint=ls "$ROOK_IMAGE" -1 $rook_crds_dir/ > "$crd_list"
	# shellcheck disable=SC2013
	for i in $(cat "$crd_list"); do
	        # shellcheck disable=SC2059
		crd_file=$(printf ${rook_crds_dir}/"$i" | tr -d '[:space:]')
		echo "Dumping rook crd $crd_file using command: $IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $crd_file"
		($IMAGE_RUN_CMD --entrypoint=cat "$ROOK_IMAGE" "$crd_file") > $rook_crds_outdir/"$(basename "$crd_file")"
	done;
	rm -f "$crd_list"
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
	$KUSTOMIZE build config/manifests/ocs-operator | $OPERATOR_SDK generate bundle -q --overwrite=false --output-dir deploy/ocs-operator --kustomize-dir config/manifests/ocs-operator --package ocs-operator --version "$CSV_VERSION" --extra-service-accounts=ocs-metrics-exporter
	mv deploy/ocs-operator/manifests/*clusterserviceversion.yaml $OCS_CSV
	cp config/crd/bases/* $ocs_crds_outdir
}

if [ -z "$OPENSHIFT_BUILD_NAMESPACE" ] && [ -z "$SKIP_CSV_DUMP" ]; then
	source hack/docker-common.sh
	dump_noobaa_csv
	dump_rook_csv
fi

gen_ocs_csv

echo "Manifests sourced into $OUTDIR_TEMPLATES directory"

mv bundle.Dockerfile Dockerfile.bundle
