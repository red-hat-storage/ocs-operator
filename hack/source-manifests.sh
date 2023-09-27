#!/usr/bin/env bash

set -e

source hack/common.sh

# check required env vars
if [ -z "$CSV_VERSION" ] || [ -z "$OCS_IMAGE" ] || [ -z "$ROOK_IMAGE" ]; then
	echo "ERROR: Missing required environment variables"
	exit 1
fi

# ==== DUMP ROOK YAMLS ====
function dump_rook_csv() {
	rook_template_dir="/etc/ceph-csv-templates"
	rook_csv=$rook_template_dir/rook-ceph.clusterserviceversion.yaml
	rook_crds_dir=$rook_template_dir/ceph
	rm -rf "$ROOK_CSV"
	rm -rf "$ROOK_CRDS"
	mkdir -p "$ROOK_CRDS"

	echo "Dumping Rook CSV"
	$IMAGE_BUILD_CMD run --rm --entrypoint=cat --platform="$GOOS"/"$GOARCH" "$ROOK_IMAGE" $rook_csv > $ROOK_CSV

	crd_list=$(mktemp)
	echo "Listing Rook CRDs"
	$IMAGE_BUILD_CMD run --rm --entrypoint=ls --platform="$GOOS"/"$GOARCH" "$ROOK_IMAGE" -1 $rook_crds_dir/ > "$crd_list"

	mkdir -p "$ROOK_CRDS"
	# shellcheck disable=SC2013
	for i in $(cat "$crd_list"); do
		# shellcheck disable=SC2059
		crd_file=$(printf ${rook_crds_dir}/"$i" | tr -d '[:space:]')
		echo "Dumping Rook CRD $crd_file"
		($IMAGE_BUILD_CMD run --rm --entrypoint=cat --platform="$GOOS"/"$GOARCH" "$ROOK_IMAGE" "$crd_file") > $ROOK_CRDS/"$(basename "$crd_file")"
	done;
	rm -f "$crd_list"
}

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
function gen_ocs_csv() {
	echo "Generating OpenShift Container Storage CSV"
	rm -rf "$(dirname $BUNDLE_MANIFESTS_DIR)"
	rm -rf "$OCS_CSV"
	rm -rf "$OCS_CRDS"
	mkdir -p "$OCS_CRDS"

	$OPERATOR_SDK generate kustomize manifests --package ocs-operator -q
	pushd config/manager
	$KUSTOMIZE edit set image ocs-dev/ocs-operator="$OCS_IMAGE"
	popd
	$KUSTOMIZE build config/manifests | $OPERATOR_SDK generate bundle -q --package ocs-operator --version "$CSV_VERSION" --extra-service-accounts=ocs-metrics-exporter
	
	mv $BUNDLE_MANIFESTS_DIR/*clusterserviceversion.yaml $OCS_CSV
	cp config/crd/bases/* $OCS_CRDS
}

if [ -z "$OPENSHIFT_BUILD_NAMESPACE" ] && [ -z "$SKIP_CSV_DUMP" ]; then
	source hack/docker-common.sh
	dump_rook_csv
fi

gen_ocs_csv

echo "Manifests sourced successfully."

mv bundle.Dockerfile Dockerfile.bundle
