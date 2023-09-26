#!/usr/bin/env bash

set -e

source hack/common.sh

${OPERATOR_SDK} bundle validate "$(dirname $OCS_FINAL_DIR)" --verbose
