#!/bin/bash

if [[ -n "$(git status --porcelain pkg/apis deploy/crds)" ]]; then
	echo "uncommitted generated files. run 'make update-generated' and commit results."
	exit 1
fi
