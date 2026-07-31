#!/usr/bin/env bash
# docker-build.sh -- build the micro-manager development image.
#
# usage: docker-build.sh [docker build args ...]
#
#   (no arguments)               build Dockerfile as micro-manager-dev:latest
#   --no-cache                   rebuild every layer from scratch
#   --build-arg GO_VERSION=x.y.z build with a different Go
#   --build-arg NODE_VERSION=vX  build with a different Node.js
#
# Any argument is forwarded to docker build verbatim. Exits non-zero when
# docker is missing or the build fails.
set -euo pipefail

cd "$(dirname "$0")"

IMAGE=micro-manager-dev
TAG=latest

if ! command -v docker >/dev/null 2>&1; then
    echo "docker-build.sh: docker is not installed or not on PATH" >&2
    exit 1
fi

docker build --tag "$IMAGE:$TAG" "$@" .
echo "docker-build.sh: built $IMAGE:$TAG -- run it with ./docker-run.sh"
