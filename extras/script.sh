#!/usr/bin/env bash

/dlv \
  --accept-multiclient \
  --api-version=2 \
  --headless=true \
  --listen=:40000 \
  --log \
  exec /usr/local/bin/ocs-operator \
  -- \
  --enable-leader-election \
  --health-probe-bind-address=:8081
