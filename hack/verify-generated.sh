#!/bin/bash

if [[ -n "$(git status --porcelain api config/crd/bases)" ]]; then
 	git diff -u api config/crd/bases 
	echo "uncommitted generated files. run 'make update-generated' and commit results."
	exit 1
fi

echo "Success: no out of source tree changes found for generated files"
