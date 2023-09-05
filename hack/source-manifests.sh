#!/bin/bash

set -e

source hack/common.sh

[ -z "$CONTAINER_CLI" ] && { echo "Podman or Docker not found"; exit 1; }

rm -rf "$CSV_TEMPLATES_DIR"
mkdir -p "$CSV_TEMPLATES_DIR"
mkdir -p "$CRDS_DIR"

# ==== DUMP NOOBAA YAMLS ====
function dump_noobaa_csv() {
	noobaa_dump_csv_cmd="olm csv"
	noobaa_dump_crds_cmd="crd yaml"

	echo "Dumping Noobaa CSV"
	# shellcheck disable=SC2086
	(${CONTAINER_CLI} run --rm --platform="${GOOS}"/"${GOARCH}" --entrypoint=/usr/local/bin/noobaa-operator "$NOOBAA_IMAGE" $noobaa_dump_csv_cmd) > $NOOBAA_CSV

	mkdir -p "$NOOBAA_CRDS"
	echo "Dumping Noobaa CRDs"
	# shellcheck disable=SC2086
	(${CONTAINER_CLI} run --rm --platform="${GOOS}"/"${GOARCH}" --entrypoint=/usr/local/bin/noobaa-operator "$NOOBAA_IMAGE" $noobaa_dump_crds_cmd) > $NOOBAA_CRDS/noobaa-crd.yaml
}

# ==== DUMP ROOK YAMLS ====
function dump_rook_csv() {
	rook_template_dir="/etc/ceph-csv-templates"
	rook_csv=$rook_template_dir/rook-ceph.clusterserviceversion.yaml
	rook_crds_dir=$rook_template_dir/ceph

	echo "Dumping Rook CSV"
	${CONTAINER_CLI} run --rm --platform="${GOOS}"/"${GOARCH}" --entrypoint=cat "$ROOK_IMAGE" $rook_csv > $ROOK_CSV

	crd_list=$(mktemp)
	echo "Listing Rook CRDs"
	${CONTAINER_CLI} run --rm --platform="${GOOS}"/"${GOARCH}" --entrypoint=ls "$ROOK_IMAGE" -1 $rook_crds_dir/ > "$crd_list"

	mkdir -p "$ROOK_CRDS"
	echo "Dumping Rook CRDs"
	# shellcheck disable=SC2013
	for i in $(cat "$crd_list"); do
		# shellcheck disable=SC2059
		crd_file=$(printf ${rook_crds_dir}/"$i" | tr -d '[:space:]')
		(${CONTAINER_CLI} run --rm --platform="${GOOS}"/"${GOARCH}" --entrypoint=cat "$ROOK_IMAGE" "$crd_file") > $ROOK_CRDS/"$(basename "$crd_file")"
	done;
	rm -f "$crd_list"
}

# ==== DUMP OCS YAMLS ====
# Generate an OCS CSV using the operator-sdk.
# This is the base CSV everything else gets merged into later on.
function gen_ocs_csv() {
	echo "Generating OpenShift Container Storage CSV"
	rm -rf "$(dirname $MANIFESTS_DIR)"
	$OPERATOR_SDK generate kustomize manifests --input-dir config/manifests/ocs-operator --output-dir config/manifests/ocs-operator --package ocs-operator -q

	pushd config/manager
	$KUSTOMIZE edit set image ocs-dev/ocs-operator="$OCS_IMAGE"
	popd
	$KUSTOMIZE build config/manifests/ocs-operator | $OPERATOR_SDK generate bundle -q --overwrite=false --output-dir deploy/ocs-operator --kustomize-dir config/manifests/ocs-operator --package ocs-operator --version "$VERSION" --extra-service-accounts=ocs-metrics-exporter

	mv $MANIFESTS_DIR/*clusterserviceversion.yaml $OCS_CSV

	mkdir -p $OCS_CRDS
	cp config/crd/bases/* $OCS_CRDS
	
	mv bundle.Dockerfile Dockerfile.bundle
}

if [ -z "$OPENSHIFT_BUILD_NAMESPACE" ] && [ -z "$SKIP_CSV_DUMP" ]; then
	dump_noobaa_csv
	dump_rook_csv
fi

gen_ocs_csv

echo "Manifests sourced into bundle directory"
