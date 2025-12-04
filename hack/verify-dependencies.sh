#!/usr/bin/env bash

paths=('go.mod' 'go.sum' 'api/go.mod' 'api/go.sum' 'metrics/go.mod' 'metrics/go.sum' 'services/provider/api/go.mod' 'services/provider/api/go.sum' 'vendor/')

if [[ -n "$(git status --porcelain "${paths[@]}")" ]]; then
	git diff -u "${paths[@]}"
	echo "uncommitted dependency files. run 'make deps-update' and commit results."
	exit 1
fi
echo "Success: no out of source tree changes found for dependency files"
