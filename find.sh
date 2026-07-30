#!/usr/bin/env bash
# find.sh -- list every micro-manager todo directory in a tree.
#
# usage: find.sh [--exclude NAME]... [--no-default-excludes] [ROOT ...]
#
# Searches ROOT (default: the current directory) recursively and prints one
# matching directory path per line, sorted. A directory qualifies on its name
# alone -- micro-manager, .micro-manager, umanager or .umanager, where "u" is
# the micro sign. Contents are not inspected; check.sh does that.
#
# Both spellings of the micro sign are matched: U+00B5 MICRO SIGN and U+03BC
# GREEK SMALL LETTER MU render identically in most fonts and are trivially
# confused when typing, so a directory named with either one is found.
#
# The walk prunes well-known directories that never contain projects, and does
# not descend into a directory it has matched. --exclude adds a name to the
# prune list; --no-default-excludes starts from an empty list.
#
# Exits 0 when at least one directory is found, 1 when none are, 2 on a usage
# error -- so `find.sh >/dev/null && echo yes` is a valid emptiness test.
set -u

EXCLUDES=(.git .hg .svn .claude node_modules vendor target dist build .venv venv)

while [ "$#" -gt 0 ]; do
  case "$1" in
    -h|--help)
      sed -n '2,21p' "$0" | sed 's/^#[ ]\{0,1\}//'
      exit 0
      ;;
    --exclude)
      [ "$#" -ge 2 ] || { echo "find.sh: --exclude needs a name" >&2; exit 2; }
      EXCLUDES+=("$2"); shift 2
      ;;
    --exclude=*)
      EXCLUDES+=("${1#--exclude=}"); shift
      ;;
    --no-default-excludes)
      EXCLUDES=(); shift
      ;;
    --) shift; break ;;
    -*) echo "find.sh: unknown flag: $1" >&2; exit 2 ;;
    *)  break ;;
  esac
done

[ "$#" -gt 0 ] || set -- .

for root; do
  if [ ! -d "$root" ]; then
    echo "find.sh: not a directory: $root" >&2
    exit 2
  fi
done

MU=$(printf '\xc2\xb5')        # U+00B5 MICRO SIGN
MMU=$(printf '\xce\xbc')       # U+03BC GREEK SMALL LETTER MU

# -name tests for the directories we are looking for
match=( -name 'micro-manager'  -o -name '.micro-manager'
        -o -name "${MU}manager"  -o -name ".${MU}manager"
        -o -name "${MMU}manager" -o -name ".${MMU}manager" )

# -name tests for the directories we refuse to walk into
prune=()
for e in ${EXCLUDES[@]+"${EXCLUDES[@]}"}; do
  [ "${#prune[@]}" -eq 0 ] && prune+=( -name "$e" ) || prune+=( -o -name "$e" )
done

if [ "${#prune[@]}" -gt 0 ]; then
  found=$(find "$@" -type d \( "${prune[@]}" \) -prune \
                 -o -type d \( "${match[@]}" \) -print -prune \
          2>/dev/null | sort)
else
  found=$(find "$@" -type d \( "${match[@]}" \) -print -prune 2>/dev/null | sort)
fi

[ -n "$found" ] || exit 1
printf '%s\n' "$found"
