#!/bin/bash

RESTART_EXIT_CODE=42

while true; do
    ./usr/local/bin/ocs-operator --enable-leader-election --health-probe-bind-address=:8081
    EXIT_CODE=$?
    if [ $EXIT_CODE -ne $RESTART_EXIT_CODE ]; then
      exit $EXIT_CODE
    fi
done
