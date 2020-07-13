#!/bin/bash

RETVAL=0
GENERATED_FILES="zz_generated.*.go"
GOLINT="golang.org/x/lint/golint"

go get "${GOLINT}"
for file in $(find . -path ./vendor -prune -o -type f -name '*.go' -print | grep -E -v "$GENERATED_FILES"); do
	go run "${GOLINT}" -set_exit_status "$file"
	if [[ $? -ne 0 ]]; then
		RETVAL=1
	fi
done
exit $RETVAL

