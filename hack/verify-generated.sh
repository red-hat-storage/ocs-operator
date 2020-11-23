#!/bin/bash

if [[ -n "$(git status --porcelain pkg/apis deploy/crds)" ]]; then
	git diff -u pkg/apis deploy/crds
	echo "uncommitted generated files. run 'make update-generated' and commit results."
	exit 1
fi
echo "Success: no out of source tree changes found for generated files"
