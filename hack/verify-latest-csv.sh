#!/bin/bash

set -e

CSV_CHECKSUM_ONLY=1 hack/generate-latest-csv.sh
if [[ -n "$(git status --porcelain hack/latest-csv-checksum.md5)" ]]; then
	echo "uncommitted CSV changes. run 'make gen-latest-csv' and commit results."
	exit 1
fi
echo "Success: no out of source tree changes found"
