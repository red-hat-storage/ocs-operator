#!/usr/bin/env bash

# shellcheck disable=SC2034
# disable unused variable warnings

if [ -z "$IMAGE_BUILD_CMD" ]; then
    IMAGE_BUILD_CMD=$(command -v podman || echo "")
fi
if [ -z "$IMAGE_BUILD_CMD" ]; then
    IMAGE_BUILD_CMD=$(command -v docker || echo "")
fi

if [ -z "$IMAGE_BUILD_CMD" ]; then
    echo -e '\033[1;31m' "podman or docker not found on system" '\033[0m'
    exit 1
fi

# Dockerfile cache mounts (RUN --mount=type=cache) require BuildKit on Docker.
# Podman supports the same syntax natively; no equivalent env var is needed.
if [[ "$(basename "${IMAGE_BUILD_CMD}")" == docker ]]; then
    export DOCKER_BUILDKIT=1
fi

IMAGE_RUN_CMD="${IMAGE_RUN_CMD:-${IMAGE_BUILD_CMD} run --rm -it}"
