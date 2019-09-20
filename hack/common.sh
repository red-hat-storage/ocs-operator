#!/bin/bash

IMAGE_RUN_CMD="${IMAGE_RUN_CMD:-docker run --rm -it}"

OUTDIR="build/_output"
OUTDIR_BIN="build/_output/bin"
OUTDIR_OCS_CI="build/_output/ocs-ci-testsuite"
OUTDIR_TEMPLATES="$OUTDIR/csv-templates"
OUTDIR_CRDS="$OUTDIR_TEMPLATES/crds"
OUTDIR_BUNDLEMANIFESTS="$OUTDIR_TEMPLATES/bundlemanifests"
OUTDIR_TOOLS="$OUTDIR/tools"
OUTDIR_CLUSTER_DEPLOY_MANIFESTS="$OUTDIR/cluster-deploy-manifests"

REDHAT_OCS_CI_REPO="https://github.com/red-hat-storage/ocs-ci"
REDHAT_OCS_CI_HASH="e84e06c42cbf3121137fbd94476e2c88aa62d520"
REDHAT_OCS_CI_TEST_EXPRESSION="TestOSCBasics or TestPvCreation or TestRawBlockPV or TestReclaimPolicy or TestCreateSCSameName or TestBasicPVCOperations or TestVerifyAllFieldsInScYamlWithOcDescribe"
REDHAT_OCS_CI_PYTHON_BINARY="python3.7"

NOOBAA_CSV="$OUTDIR_TEMPLATES/noobaa-csv.yaml"
ROOK_CSV="$OUTDIR_TEMPLATES/rook-csv.yaml.in"
OCS_CSV="$OUTDIR_TEMPLATES/ocs-operator.csv.yaml.in"
