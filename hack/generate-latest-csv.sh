#!/bin/bash

set -e

source hack/common.sh

NOOBAA_VERSION="1.1.0"
ROOK_VERSION="v1.0.0-526.g3ece503"

# Current DEV version of the CSV
export CSV_VERSION=0.0.1

# Current dependency images our DEV CSV are pinned to
export ROOK_IMAGE=rook/ceph:$ROOK_VERSION
export NOOBAA_IMAGE=noobaa/noobaa-operator:$NOOBAA_VERSION
export CEPH_IMAGE=ceph/ceph:v14.2.2-20190828
export OCS_IMAGE=quay.io/ocs-dev/ocs-operator:latest

# Temporary CSI image until https://github.com/rook/rook/pull/3716 is available in a Rook image
# Built with https://github.com/ceph/ceph-csi/pull/566 reverted
export ROOK_CSI_CEPH_IMAGE=madhupr001/cephcsi:canary

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
if [ "$ROOK_VERSION" = "v1.0.0-526.g3ece503" ]; then
    sed -i "s/serviceAccountName: rbd-csi-provisioner-sa/serviceAccountName: rook-csi-rbd-provisioner-sa/g" $OUTDIR_TEMPLATES/rook-csv.yaml.in
fi

hack/generate-unified-csv.sh
