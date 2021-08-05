#!/usr/bin/env bash

/dlv \
  --accept-multiclient \
  --api-version=2 \
  --headless=true \
  --listen=:40000 \
  --log \
  attach 1 &
