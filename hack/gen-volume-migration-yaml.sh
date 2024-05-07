#!/usr/bin/env bash

set -e

source hack/common.sh
source hack/docker-common.sh


${IMAGE_BUILD_CMD} run --rm -t "${VOLUME_MIGRATION_FULL_IMAGE_NAME}" /usr/local/bin/volume-migration dump-yaml
