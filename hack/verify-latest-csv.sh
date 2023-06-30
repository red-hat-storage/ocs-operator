#!/usr/bin/env bash

source hack/common.sh

set -e

CSV="$(find "${OCS_FINAL_DIR}"/ocs-operator.*.yaml)"

NOT_FOUND=""
for LATEST_IMAGE in "${LATEST_ROOK_IMAGE}" "${LATEST_NOOBAA_CORE_IMAGE}"  "${LATEST_NOOBAA_DB_IMAGE}" "${LATEST_CEPH_IMAGE}"
do
	grep -q ${LATEST_IMAGE} "${CSV}" || NOT_FOUND="${NOT_FOUND} ${LATEST_IMAGE}"
done

if [[ -n "${NOT_FOUND}" ]];then
	echo "latest CSV has not been generated"
        echo "Missing images: ${NOT_FOUND}"
	exit 1
fi

if [[ -n "$(git status --porcelain deploy/csv-templates deploy/ocs-operator config/crd/bases )" ]]; then
	git diff -u deploy/csv-templates deploy/ocs-operator config/crd/bases
	echo "uncommitted CSV changes. run 'make gen-latest-csv' and commit results."
	exit 1
fi

echo "Success: no out of source tree changes found for CSV"
