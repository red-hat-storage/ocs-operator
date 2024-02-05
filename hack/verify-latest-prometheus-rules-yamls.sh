#!/usr/bin/env bash

if [[ -n "$(git status --porcelain metrics/deploy)" ]]; then
	git diff -u metrics/deploy
	echo "uncommitted prometheus rules changs. run 'make gen-latest-prometheus-rules-yamls' and commit results."
	exit 1
fi
echo "Success: no out of source tree changes found for prometheus rules files"
