#!/usr/bin/env bash
# run.sh -- build and run the micro-manager CLI.
#
# usage: run.sh [ARGS...]
#
# Builds every command into bin/ and then runs bin/mm, passing ARGS through.
# The build fails loudly rather than silently running a stale binary.
set -eu

here=$(cd "$(dirname "$0")" && pwd)

mkdir -p "$here/bin"
go -C "$here" build -o bin/mm ./cmd/mm
go -C "$here" build -o bin/mm-ui ./cmd/mm-ui

exec "$here/bin/mm-ui" "$@"
