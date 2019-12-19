#!/usr/bin/env bash

# This test checks for empty output files of ceph commands

# Expect base collection path as an argument
BASE_PATH=$1

# Use PWD as base path if no argument is passed
if [ "${BASE_PATH}" = "" ]; then
    BASE_PATH=$(pwd)
fi

# finding the paths of ceph command output directories
# shellcheck disable=SC2044
for path in $(find "${BASE_PATH}" -type d -name must_gather_commands); do
    numberOfEmptyOutputFiles=$(find "${path}" -empty -type f | wc -l)
    if [ "${numberOfEmptyOutputFiles}" -ne "0" ]; then
        printf "The following files must not be empty : \n\n";
        find "${path}" -empty -type f 
        exit 1;
    fi

    jsonOutputPath=${path}/json_output
    numberOfEmptyOutputFiles=$(find "${jsonOutputPath}" -empty -type f | wc -l)
    if [ "${numberOfEmptyOutputFiles}" -ne "0" ]; then
        printf "The following files must not be empty : \n\n";
        find "${jsonOutputPath}" -empty -type f 
        exit 1;
    fi
done

