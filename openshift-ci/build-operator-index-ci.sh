#!/bin/bash

set -e

source hack/common.sh
source hack/docker-common.sh
source hack/ensure-opm.sh

BUNDLE_IMGS="${BUNDLE_IMGS:-${NOOBAA_BUNDLE_FULL_IMAGE_NAME}}"

cd openshift-ci
"../${OPM}" index add --bundles "${BUNDLE_IMGS}" --tag "${INDEX_FULL_IMAGE_NAME}" --generate
