#!/bin/bash

set -e

"${OPERATOR_SDK}" bundle validate "$(dirname "$MANIFESTS_DIR")" --verbose
