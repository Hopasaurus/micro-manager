#!/usr/bin/env bash
# docker-run.sh -- run the micro-manager development container.
#
# usage: docker-run.sh [command ...]
#
#   (no arguments)              open an interactive bash shell in the container
#   ./docker-run.sh ./check.sh --all
#                               run a single command instead
#
# This repository is mounted at /workspace, so edits inside the container land
# on the host checkout. The container is deleted on exit (--rm).
#
# The GUI port 7717 (spec-gui.md section 9.6) is published on the host by
# default, so a host browser can reach mm-ui / mmts-ui. The service binds
# loopback per the spec, so for host access start it inside the container
# with --bind 0.0.0.0 --allow-remote, then open http://127.0.0.1:7717.
#
#   MM_UI_PORT=8080 ./docker-run.sh   publish as host port 8080 instead
#   MM_UI_PORT=none  ./docker-run.sh  do not publish any port
#
# Builds the image first when it does not exist. Exits non-zero when docker
# is missing.
set -euo pipefail

cd "$(dirname "$0")"

IMAGE=micro-manager-dev
TAG=latest

if ! command -v docker >/dev/null 2>&1; then
    echo "docker-run.sh: docker is not installed or not on PATH" >&2
    exit 1
fi

if ! docker image inspect "$IMAGE:$TAG" >/dev/null 2>&1; then
    echo "docker-run.sh: $IMAGE:$TAG not found; building it first" >&2
    ./docker-build.sh
fi

port=${MM_UI_PORT-7717}
publish=()
case "$port" in
    ""|none) ;;
    *) publish=(--publish "$port:7717") ;;
esac

# Allocate a tty only when attached to one, so the script also works piped.
tty=()
if [ -t 0 ] && [ -t 1 ]; then
    tty=(--interactive --tty)
fi

# ${arr[@]+...} keeps an empty array legal under set -u on bash 3.2.
exec docker run \
    --rm \
    ${tty[@]+"${tty[@]}"} \
    --hostname micro-manager-dev \
    --volume "$PWD:/workspace" \
    ${publish[@]+"${publish[@]}"} \
    "$IMAGE:$TAG" "$@"
