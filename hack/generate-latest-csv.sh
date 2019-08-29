#!/bin/bash

set -e

source hack/common.sh

#sha for noobaa-operator:1.1.0
NOOBAA_SHA="sha256:d6bfdf02d72e763211a89a692f19a8c25bcc26e0d0cd04bda186333bb47894dd"

# sha for rook/ceph:v1.0.0-526.g3ece503
ROOK_SHA="sha256:addf210104fd4e222b11529f139fb4e12c44707c7b7d46920cdf46e25a3f6a42"

# Current DEV version of the CSV
export CSV_VERSION=0.0.1

# Current dependency images our DEV CSV are pinned to
export ROOK_IMAGE=rook/ceph@$ROOK_SHA
export NOOBAA_IMAGE=noobaa/noobaa-operator@$NOOBAA_SHA
export CEPH_IMAGE=ceph/ceph:v14.2.2-20190828
export OCS_IMAGE=quay.io/ocs-dev/ocs-operator:latest

echo "=== Generating DEV CSV with the following vars ==="
echo -e "\tCSV_VERSION=$CSV_VERSION"
echo -e "\tROOK_IMAGE=$ROOK_IMAGE"
echo -e "\tNOOBAA_IMAGE=$NOOBAA_IMAGE"
echo -e "\tCEPH_IMAGE=$CEPH_IMAGE"
echo -e "\tOCS_IMAGE=$OCS_IMAGE"

hack/source-manifests.sh

# TODO remove this once we update rook/ceph image
# This addresses an issue that is already fixed in rook upstream
# One of the ServiceAccounts we're sourcing from a specific rook
# build is incorrect.
if [ "$ROOK_SHA" = "sha256:addf210104fd4e222b11529f139fb4e12c44707c7b7d46920cdf46e25a3f6a42" ]; then
    sed -i "s/serviceAccountName: rbd-csi-provisioner-sa/serviceAccountName: rook-csi-rbd-provisioner-sa/g" $OUTDIR_TEMPLATES/rook-csv.yaml.in
fi

hack/generate-unified-csv.sh
