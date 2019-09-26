#!/bin/bash

# example: ROOK_IMAGE=build-e858f56d/ceph-amd64:latest NOOBAA_IMAGE=noobaa/noobaa-operator:1.1.0 OCS_IMAGE=placeholder CSV_VERSION=1.1.1 hack/generate-manifests.sh

set -e

source hack/common.sh

# For consistency, we're locking into a specific
# operator-sdk cli build for generating the initial
# ocs-operator csv that we build upon
OS_TYPE=$(uname)
OPERATOR_SDK_URL="https://github.com/operator-framework/operator-sdk/releases/download/v0.8.2/operator-sdk-v0.8.2-x86_64-linux-gnu"
OPERATOR_SDK_VERSION="operator-sdk-v0.8.2-x86_64-linux-gnu"
if [ $OS_TYPE=="Darwin" ]; then
	OPERATOR_SDK_URL="https://github.com/operator-framework/operator-sdk/releases/download/v0.8.2/operator-sdk-v0.8.2-x86_64-apple-darwin"
	OPERATOR_SDK_VERSION="operator-sdk-v0.8.2-x86_64-apple-darwin"
fi
OPERATOR_SDK="$OUTDIR_TOOLS/$OPERATOR_SDK_VERSION"

function help_txt() {
	echo "Environment Variables"
	echo "    NOOBAA_IMAGE:         (required) The noobaa operator container image to integrate with"
	echo "    ROOK_IMAGE:           (required) The rook operator container image to integrate with"
	echo ""
	echo "Example usage:"
	echo "    NOOBAA_IMAGE=<image> ROOK_IMAGE=<image> $0"
}

# check required env vars
if [ -z $NOOBAA_IMAGE ] || [ -z $ROOK_IMAGE ]; then
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

if [ ! -x $OPERATOR_SDK ]; then
        curl -JL $OPERATOR_SDK_URL -o $OPERATOR_SDK
        chmod 0755 $OPERATOR_SDK
fi

# ==== DUMP NOOBAA YAMLS ====
noobaa_dump_crds_cmd="crd yaml"
noobaa_dump_csv_cmd="olm csv yaml"
($IMAGE_RUN_CMD $NOOBAA_IMAGE $noobaa_dump_csv_cmd) > $NOOBAA_CSV
($IMAGE_RUN_CMD $NOOBAA_IMAGE $noobaa_dump_crds_cmd) > $OUTDIR_CRDS/noobaa-crd.yaml

# ==== DUMP ROOK YAMLS ====
rook_template_dir="/etc/ceph-csv-templates"
rook_csv_template="rook-ceph-ocp.vVERSION.clusterserviceversion.yaml.in"
rook_crds_dir=$rook_template_dir/crds
crd_list=$(mktemp)
$IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $rook_template_dir/$rook_csv_template > $ROOK_CSV
$IMAGE_RUN_CMD --entrypoint=ls $ROOK_IMAGE -1 $rook_crds_dir/ > $crd_list
for i in $(cat $crd_list); do
	crd_file=$(printf ${rook_crds_dir}/$i | tr -d '[:space:]')
	($IMAGE_RUN_CMD --entrypoint=cat $ROOK_IMAGE $crd_file) > $OUTDIR_CRDS/$(basename $crd_file)
done;
rm -f $crd_list

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
TMP_CSV_VERSION=9999.9999.9999
gen_args="olm-catalog gen-csv --csv-version=$TMP_CSV_VERSION"
if [ -n "$CSV_REPLACES_VERSION" ]; then
	gen_args="$gen_args --from-version=$CSV_REPLACES_VERSION"
fi
$OPERATOR_SDK $gen_args 2>/dev/null
OCS_TMP_CSV="deploy/olm-catalog/ocs-operator/${TMP_CSV_VERSION}/ocs-operator.v${TMP_CSV_VERSION}.clusterserviceversion.yaml"
mv $OCS_TMP_CSV $OCS_CSV
# Make variables templated for csv-merger tool
if [ $OS_TYPE=="Darwin" ]; then
	sed -i '' "s/$TMP_CSV_VERSION/{{.OcsOperatorCsvVersion}}/g" $OCS_CSV
	sed -i '' "s/REPLACE_IMAGE/{{.OcsOperatorImage}}/g" $OCS_CSV
else 
	sed -i "s/$TMP_CSV_VERSION/{{.OcsOperatorCsvVersion}}/g" $OCS_CSV
	sed -i "s/REPLACE_IMAGE/{{.OcsOperatorImage}}/g" $OCS_CSV
fi
cp deploy/crds/* $OUTDIR_CRDS/
cp deploy/bundlemanifests/*.yaml $OUTDIR_BUNDLEMANIFESTS/

echo "Manifests sourced into $OUTDIR_TEMPLATES directory"

rm -rf $(dirname $OCS_TMP_CSV)
