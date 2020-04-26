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
rm -rf $OUTDIR_TEMPLATES
mkdir -p $OUTDIR_TEMPLATES
mkdir -p $OUTDIR_CRDS $OUTDIR_BUNDLEMANIFESTS
mkdir -p $OUTDIR_TOOLS

# ==== DUMP NOOBAA YAMLS ====
noobaa_dump_crds_cmd="crd yaml"
noobaa_dump_csv_cmd="olm csv yaml"
echo "Dumping Noobaa csv using command: $IMAGE_RUN_CMD $NOOBAA_IMAGE $noobaa_dump_csv_cmd"
($IMAGE_RUN_CMD "$NOOBAA_IMAGE" "$noobaa_dump_csv_cmd") | tee $NOOBAA_CSV
echo "Dumping Noobaa crds using command: $IMAGE_RUN_CMD $NOOBAA_IMAGE $noobaa_dump_crds_cmd"
($IMAGE_RUN_CMD "$NOOBAA_IMAGE" "$noobaa_dump_crds_cmd") | tee $OUTDIR_CRDS/noobaa-crd.yaml

# ==== DUMP ROOK YAMLS ====
rook_template_dir="/etc/ceph-csv-templates"
rook_csv_template="rook-ceph-ocp.vVERSION.clusterserviceversion.yaml.in"
rook_crds_dir=$rook_template_dir/crds
crd_list=$(mktemp)
echo "Dumping rook csv using command: $IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $rook_template_dir/$rook_csv_template"
$IMAGE_RUN_CMD --entrypoint=cat "$ROOK_IMAGE" $rook_template_dir/$rook_csv_template | tee $ROOK_CSV
echo "Listing rook crds using command: $IMAGE_RUN_CMD --entrypoint=ls $ROOK_IMAGE -1 $rook_crds_dir/"
$IMAGE_RUN_CMD --entrypoint=ls "$ROOK_IMAGE" -1 $rook_crds_dir/ | tee "$crd_list"
# shellcheck disable=SC2013
for i in $(cat "$crd_list"); do
        # shellcheck disable=SC2059
	crd_file=$(printf ${rook_crds_dir}/"$i" | tr -d '[:space:]')
	echo "Dumping rook crd $crd_file using command: $IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $crd_file"
	($IMAGE_RUN_CMD --entrypoint=cat "$ROOK_IMAGE" "$crd_file") | tee $OUTDIR_CRDS/"$(basename "$crd_file")"
done;
rm -f "$crd_list"

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
TMP_CSV_VERSION=9999.9999.9999
gen_args="generate csv --csv-version=$TMP_CSV_VERSION"
if [ -n "$CSV_REPLACES_VERSION" ]; then
	gen_args="$gen_args --from-version=$CSV_REPLACES_VERSION"
fi
$OPERATOR_SDK $gen_args
OCS_TMP_CSV="deploy/olm-catalog/ocs-operator/${TMP_CSV_VERSION}/ocs-operator.v${TMP_CSV_VERSION}.clusterserviceversion.yaml"
mv $OCS_TMP_CSV $OCS_CSV
# Make variables templated for csv-merger tool
if [ "$OS_TYPE" == "Darwin" ]; then
	sed -i '' "s/$TMP_CSV_VERSION/{{.OcsOperatorCsvVersion}}/g" $OCS_CSV
	sed -i '' "s/REPLACE_IMAGE/{{.OcsOperatorImage}}/g" $OCS_CSV
else
	sed -i "s/$TMP_CSV_VERSION/{{.OcsOperatorCsvVersion}}/g" $OCS_CSV
	sed -i "s/REPLACE_IMAGE/{{.OcsOperatorImage}}/g" $OCS_CSV
fi
cp deploy/crds/* $OUTDIR_CRDS/
cp deploy/bundlemanifests/*.yaml $OUTDIR_BUNDLEMANIFESTS/

echo "Manifests sourced into $OUTDIR_TEMPLATES directory"

rm -rf "$(dirname $OCS_TMP_CSV)"
