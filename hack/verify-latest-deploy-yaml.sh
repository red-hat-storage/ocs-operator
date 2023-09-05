#!/bin/bash

set -e

source hack/common.sh

hack/gen-deployment-yaml.sh

if [[ -n "$(git status --porcelain ${DEPLOY_YAML_PATH})" ]]; then
	git diff -u ${DEPLOY_YAML_PATH}
	echo "uncommitted ${DEPLOY_YAML_PATH} changes. run 'make gen-latest-deploy-yaml' and commit results."
	exit 1
fi

echo "Success: no out of source tree changes found for ${DEPLOY_YAML_PATH}"
