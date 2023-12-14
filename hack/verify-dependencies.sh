#!/usr/bin/env bash

paths=('go.mod' 'go.sum' 'vendor/' 'api/go.mod' 'api/go.sum' 'api/vendor/')

if [[ -n "$(git status --porcelain "${paths[@]}")" ]]; then
	git diff -u "${paths[@]}"
	echo "uncommitted dependency files. run 'make deps-update' and commit results."
	exit 1
fi
echo "Success: no out of source tree changes found for dependency files"
