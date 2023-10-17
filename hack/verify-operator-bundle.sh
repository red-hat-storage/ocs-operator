#!/usr/bin/env bash

set -e

source hack/ensure-operator-sdk.sh

${OPERATOR_SDK} bundle validate "$(dirname "$BUNDLE_MANIFESTS_DIR")" --verbose
