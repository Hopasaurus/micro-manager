#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/mm-version-test.XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM

files="VERSION
version.sh
Makefile
project/spec-file-format.md
project/spec-tools.md
project/spec-gui.md
project/spec-tui.md
implementations/golang/internal/cli/cli.go
implementations/golang/internal/web/version.go"

for rel in $files; do
    mkdir -p -- "$tmp/$(dirname -- "$rel")"
    cp -- "$repo/$rel" "$tmp/$rel"
done
chmod +x "$tmp/version.sh"

current=$($tmp/version.sh current)
[ "$($tmp/version.sh check)" = "$current" ] || exit 1

next_patch=$(printf '%s\n' "$current" | awk -F. '{ print $1 "." $2 "." ($3 + 1) }')
[ "$($tmp/version.sh bump patch)" = "$current -> $next_patch" ] || exit 1
[ "$($tmp/version.sh current)" = "$next_patch" ] || exit 1

beta_core=$(printf '%s\n' "$next_patch" | awk -F. '{ print $1 "." $2 "." ($3 + 1) }')
$tmp/version.sh bump patch beta.1 >/dev/null
[ "$($tmp/version.sh current)" = "$beta_core-beta.1" ] || exit 1
$tmp/version.sh bump patch >/dev/null
[ "$($tmp/version.sh current)" = "$beta_core" ] || exit 1

sed 's/Product version:/Product version: 9.9.9 #/' \
    "$tmp/project/spec-tools.md" > "$tmp/project/spec-tools.md.bad"
mv -- "$tmp/project/spec-tools.md.bad" "$tmp/project/spec-tools.md"
if "$tmp/version.sh" check >/dev/null 2>&1; then
    printf '%s\n' "version_test: drift was not detected" >&2
    exit 1
fi
[ "$($tmp/version.sh current)" = "$beta_core" ] || {
    printf '%s\n' "version_test: failed check changed VERSION" >&2
    exit 1
}

printf '%s\n' "version tests: ok"
