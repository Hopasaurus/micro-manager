#!/bin/sh
set -eu

repo=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
version_file="$repo/VERSION"

die() { printf '%s\n' "version: $*" >&2; exit 1; }

read_version() {
    [ -f "$version_file" ] || die "missing VERSION"
    version=$(sed -n '1p' "$version_file")
    [ "$(wc -l < "$version_file" | tr -d ' ')" = 1 ] || die "VERSION must contain one line"
    case "$version" in
        ''|*[!0-9A-Za-z.+-]*) die "invalid semantic version: $version" ;;
    esac
    printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-beta(\.(0|[1-9][0-9]*|[0-9a-f]*[a-f][0-9a-f]*))?)?$' ||
        die "expected X.Y.Z, optionally followed by -beta, -beta.N, or -beta.HASH"
}

expect_line() {
    file=$1 line=$2 count=$3
    got=$(grep -Fxc -- "$line" "$repo/$file" || true)
    [ "$got" = "$count" ] || die "$file: expected $count occurrence(s) of: $line (found $got)"
}

check() {
    read_version
    for file in project/spec-file-format.md project/spec-tools.md project/spec-gui.md project/spec-tui.md; do
        expect_line "$file" "    Product version: $version" 1
    done
    expect_line implementations/golang/internal/cli/cli.go 'var Version = "0.0.0-dev"' 1
    expect_line implementations/golang/internal/web/version.go 'var Version = "0.0.0-dev"' 1
    expect_line Makefile 'VERSION  := $(shell cat VERSION)' 1
    expect_line Makefile 'VERSION_LDFLAGS := -X github.com/Hopasaurus/micro-manager/internal/cli.Version=$(VERSION) \' 1
}

next_patch() {
    core=${version%%-*}
    if [ "$core" != "$version" ]; then
        printf '%s\n' "$core"
        return
    fi
    major=${core%%.*}; rest=${core#*.}; minor=${rest%%.*}; patch=${rest#*.}
    printf '%s.%s.%s\n' "$major" "$minor" "$((patch + 1))"
}

bump() {
    [ "${1-}" = patch ] || die "usage: ./version.sh bump patch [beta|beta.N|beta.HASH]"
    read_version
    check
    old=$version
    next=$(next_patch)
    if [ "$#" -eq 2 ]; then
        printf '%s\n' "$2" | grep -Eq '^beta(\.(0|[1-9][0-9]*|[0-9a-f]*[a-f][0-9a-f]*))?$' ||
            die "prerelease must be beta, beta.N, or beta.HASH"
        next="$next-$2"
    elif [ "$#" -gt 2 ]; then
        die "usage: ./version.sh bump patch [beta|beta.N|beta.HASH]"
    fi

    files="VERSION
project/spec-file-format.md
project/spec-tools.md
project/spec-gui.md
project/spec-tui.md"
    staged=""
    trap 'for f in $staged; do rm -f -- "$f"; done' EXIT HUP INT TERM
    old_re=$(printf '%s' "$version" | sed 's/[][\\.^$*+?{}|()\/]/\\&/g')
    for rel in $files; do
        src="$repo/$rel"
        tmp="$src.version-tmp.$$"
        case "$rel" in
            VERSION) printf '%s\n' "$next" > "$tmp" ;;
            project/spec-*.md) sed "s/^    Product version: $old_re$/    Product version: $next/" "$src" > "$tmp" ;;
            *) die "internal error: no updater for $rel" ;;
        esac
        chmod --reference="$src" "$tmp" 2>/dev/null || chmod 0644 "$tmp"
        staged="$staged $tmp"
    done
    for rel in $files; do
        src="$repo/$rel"
        mv -- "$src.version-tmp.$$" "$src"
    done
    staged=""
    check
    printf '%s -> %s\n' "$old" "$next"
}

case "${1-}" in
    current) [ "$#" -eq 1 ] || die "usage: ./version.sh current"; read_version; printf '%s\n' "$version" ;;
    check) [ "$#" -eq 1 ] || die "usage: ./version.sh check"; check; printf '%s\n' "$version" ;;
    bump) shift; bump "$@" ;;
    *) die "usage: ./version.sh {current|check|bump patch [beta|beta.N|beta.HASH]}" ;;
esac
