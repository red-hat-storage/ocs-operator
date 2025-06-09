#!/usr/bin/env bash

for service in "${SERVICES[@]}"
do
   if [[ -n "$(git status --porcelain services/"${service}"/proto)" ]]; then
 	git diff -u services/"${service}"/proto
	echo "uncommitted protobuf files. run 'make gen-protobuf' and commit results."
	exit 1
   fi
done

echo "Success: no out of source tree changes found for protobuf files"
