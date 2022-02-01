#!/bin/bash

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
rm -rf "$(dirname $OCS_FINAL_DIR)"
mkdir -p "$(dirname $OCS_FINAL_DIR)"
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

	echo "Dumping Noobaa csv and crds"
	${OCS_OC_PATH} image extract --confirm "${NOOBAA_IMAGE}" --path /usr/local/bin/noobaa-operator:./
	chmod +x ./noobaa-operator
	./noobaa-operator $noobaa_dump_csv_cmd > $NOOBAA_CSV
	./noobaa-operator $noobaa_dump_crds_cmd > $noobaa_crds_outdir/noobaa-crd.yaml
	rm -f ./noobaa-operator
}

# ==== DUMP ROOK YAMLS ====
function dump_rook_csv() {
	rook_template_dir_parent="/etc"
	rook_template_dir="ceph-csv-templates"
	rook_csv_template="rook-ceph-ocp.vVERSION.clusterserviceversion.yaml.in"
	rook_crds_dir=$rook_template_dir/crds
	rook_crds_outdir="$OUTDIR_CRDS/rook"
	rm -rf $ROOK_CSV
	rm -rf $rook_crds_outdir
	mkdir -p $rook_crds_outdir

	temp_dir=$(mktemp -d)

	echo "Dumping rook csv and crds"
	${OCS_OC_PATH} image extract ${ROOK_IMAGE} --path ${rook_template_dir_parent}/${rook_template_dir}:${temp_dir}

	if [ -z "$(ls -A ${temp_dir})" ]; then
		echo "[FATAL] Failed to extract template dir from Rook image"
		rm -rf ${temp_dir}
		exit 1
	fi

	cat ${temp_dir}/${rook_template_dir}/${rook_csv_template} > $ROOK_CSV

	for i in $(ls ${temp_dir}/${rook_crds_dir}); do
		cat ${temp_dir}/${rook_crds_dir}/${i} > $rook_crds_outdir/"$(basename "$i")"
	done
	rm -rf ${temp_dir}
}

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
function gen_ocs_csv() {
	ocs_crds_outdir="$OUTDIR_CRDS/ocs"
	rm -rf $OUTDIR_TEMPLATES/manifests/ocs-operator.clusterserviceversion.yaml
	rm -rf $OCS_CSV
	rm -rf $ocs_crds_outdir
	mkdir -p $ocs_crds_outdir

	gen_args="generate kustomize manifests -q"
	# shellcheck disable=SC2086
	$OPERATOR_SDK $gen_args
	pushd config/manager
	$KUSTOMIZE edit set image ocs-dev/ocs-operator="$OCS_IMAGE"
	popd
	$KUSTOMIZE build config/manifests | $OPERATOR_SDK generate bundle -q --overwrite=false --version "$CSV_VERSION"
	mv bundle/manifests/*clusterserviceversion.yaml $OCS_CSV
	cp config/crd/bases/* $ocs_crds_outdir
}

dump_noobaa_csv
dump_rook_csv
gen_ocs_csv

echo "Manifests sourced into $OUTDIR_TEMPLATES directory"


mv bundle/manifests $OCS_FINAL_DIR
mv bundle/metadata "$(dirname $OCS_FINAL_DIR)"/metadata
rm -rf bundle
rm bundle.Dockerfile
