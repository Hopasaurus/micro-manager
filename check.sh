#!/usr/bin/env bash
# check.sh -- validate todo directories against the invariants in structure.md.
#
# usage: check.sh [-a|--all] [DIR ...]
#
#   (no arguments)   find todo directories under check.sh's own directory and
#                    offer them as a menu
#   DIR ...          check only the directories named
#   -a, --all        check every directory find.sh turns up, no prompting
#
# Exits 0 when every checked directory is clean, 1 when any violates an
# invariant, 2 on a usage or setup problem.
set -u

here=$(cd "$(dirname "$0")" && pwd)

usage() { sed -n '2,12p' "$0" | sed 's/^#[ ]\{0,1\}//'; }

# --- arguments -------------------------------------------------------------
all=0
explicit=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    -a|--all)  all=1 ;;
    -h|--help) usage; exit 0 ;;
    --)        shift; while [ "$#" -gt 0 ]; do explicit+=("$1"); shift; done; break ;;
    -*)        echo "check.sh: unknown flag: $1" >&2; usage >&2; exit 2 ;;
    *)         explicit+=("$1") ;;
  esac
  shift
done

if [ "${#explicit[@]}" -gt 0 ] && [ "$all" -eq 1 ]; then
  echo "check.sh: name directories or pass --all, not both" >&2
  exit 2
fi

[ -x "$here/find.sh" ] || { echo "check.sh: missing or non-executable $here/find.sh" >&2; exit 2; }

# --- working files ---------------------------------------------------------
tmp="${TMPDIR:-/tmp}/todocheck.$$"
trap 'rm -f "$tmp".*' EXIT
: > "$tmp.err"

problem() { echo "$1" >> "$tmp.derr"; }

# Read one frontmatter key out of a markdown file.
fm_get() {
  awk -v want="$2" '
    NR == 1 { if ($0 != "---") exit; next }
    $0 == "---" { exit }
    {
      p = index($0, ":")
      if (p == 0) next
      k = substr($0, 1, p - 1)
      gsub(/^[ \t]+|[ \t]+$/, "", k)
      if (k != want) next
      v = substr($0, p + 1)
      gsub(/^[ \t]+|[ \t]+$/, "", v)
      if (v ~ /^".*"$/) v = substr(v, 2, length(v) - 2)
      print v
      exit
    }' "$1"
}

# ===========================================================================
# check_dir DIR -- run every invariant against one todo directory.
# Prints its own findings; returns 0 when clean, 1 otherwise.
# ===========================================================================
check_dir() {
  dir="${1%/}"
  pfx="$dir/"
  : > "$tmp.derr"

  # --- the working file set: one file per WIP slot -------------------------
  # The count of working.NN.md files IS the WIP limit; each holds at most one
  # item, so the limit needs no separate enforcement.
  wfiles=(); widths=""; nums=""
  if [ -d "$dir" ]; then
    for f in "$dir"/working.*.md; do
      [ -e "$f" ] || continue
      b=${f##*/}
      n=${b#working.}; n=${n%.md}
      case "$n" in
        ''|*[!0-9]*) problem "$pfx$b: not a working file (want working.NN.md)"; continue ;;
      esac
      wfiles+=("$f")
      widths="$widths ${#n}"
      nums="$nums $((10#$n))"
    done
  fi

  if [ ! -d "$dir" ]; then
    problem "$dir: not a directory"
  elif [ ! -f "$dir/backlog.md" ] && [ ! -f "$dir/done.md" ]; then
    # The emptiness test (spec-file-format.md Appendix B). Neither file means
    # this was never a board — a source tree that shares the name, an empty
    # directory someone made by hand — so DISCOVERY skips it and find.sh never
    # offers it here. Reaching this line means the user named it explicitly,
    # and that has to be an error: silence would report success for a command
    # that checked nothing.
    #
    # One problem, not three. Listing a missing backlog.md, a missing done.md
    # and a missing working file describes a wrecked board; this is not one.
    problem "$dir: not a micro-manager directory (neither backlog.md nor done.md)"
  else
    # Exactly one of the two is a board that has LOST a file, which is a real
    # failure and the most alarming kind. It is reported, never skipped.
    for f in backlog.md done.md; do
      [ -f "$dir/$f" ] || problem "$dir: missing $f"
    done
    [ "${#wfiles[@]}" -gt 0 ] ||
      problem "$dir: no working.NN.md file — at least one is required"
  fi
  if [ -s "$tmp.derr" ]; then
    sort -t: -k1,1 -k2,2n "$tmp.derr" >&2
    cat "$tmp.derr" >> "$tmp.err"
    echo "  $dir: not a todo directory" >&2
    return 1
  fi

  # invariant 10: uniform digit width, numbered 1..N with no gaps
  w0=""
  for w in $widths; do
    if [ -z "$w0" ]; then w0=$w
    elif [ "$w" != "$w0" ]; then
      problem "$dir: working files mix digit widths — all must use the same number of digits"
      break
    fi
  done
  slots=${#wfiles[@]}
  missing=""; i=1
  while [ "$i" -le "$slots" ]; do
    case " $nums " in *" $i "*) ;; *) missing="$missing $i" ;; esac
    i=$((i + 1))
  done
  [ -z "$missing" ] ||
    problem "$dir: working files are not numbered 1..$slots (missing:$missing)"
  [ ! -f "$dir/working.md" ] ||
    problem "${pfx}working.md: legacy name — rename it to working.01.md"

  # --- invariants 1-7: the three item files --------------------------------
  # Emits ERR lines (problems) and REF lines (detail-file references).
  awk -v pfx="$pfx" '
function trim(s)  { gsub(/^[ \t]+|[ \t]+$/, "", s); return s }
function unq(s)   { if (s ~ /^".*"$/) s = substr(s, 2, length(s) - 2); return s }
function err(m)   { print "ERR\t" pfx base ":" FNR ": " m }
function ferr(m)  { print "ERR\t" pfx m }
function istags(s) { return (s ~ /^[A-Za-z0-9._-]+(,[A-Za-z0-9._-]+)*$/) }
# isslug/isrefs cover the board slug and the refs LINKLIST (spec-file-format.md
# §3.3): slug is lowercase [a-z][a-z0-9-]{0,15}, each link is SLUG:ID with ID
# in the generic form every declared grammar shares. Shape only -- resolution
# is never a checker job (§9); the GUI owns resolution, where a tree view exists.
function isslug(s) { return (s ~ /^[a-z][a-z0-9-]{0,15}$/) }
function isrefs(s) { return (s ~ /^[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15}(,[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15})*$/) }
# isschedule validates a tickler SCHEDULE expression (spec-file-format.md §3.3):
# a calendar date, a weekday with an optional ordinal, or a month day (01-31 or
# "last"), each with an optional @HH:MM wall time. Shape only -- the checker
# never evaluates a schedule (§10.1). The DATE alternative must be a real
# calendar date, so it goes through isdate.
function isschedule(s,   at, tm) {
  at = index(s, "@")
  if (at > 0) {
    tm = substr(s, at + 1)
    if (tm !~ /^[0-9][0-9]:[0-9][0-9]$/) return 0
    if (substr(tm, 1, 2) > "23") return 0
    if (substr(tm, 4, 2) > "59") return 0
    s = substr(s, 1, at - 1)
  }
  if (s ~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/) return isdate(s)
  if (s ~ /^(first|second|third|fourth|last)-(mon|tue|wed|thu|fri|sat|sun)$/) return 1
  if (s ~ /^(mon|tue|wed|thu|fri|sat|sun)$/) return 1
  if (s == "last") return 1
  if (s ~ /^(0[1-9]|[12][0-9]|3[01])$/) return 1
  return 0
}
function isnull(s) { return (s == "" || s == "null") }
# working file frontmatter: value of, and error located at, key k of file f
function wv(f, k)      { return ((f, k) in wf) ? wf[f, k] : "" }
function werr(f, k, m) { print "ERR\t" pfx f ":" (((f, k) in wl) ? wl[f, k] : 1) ": " m }
# isdate validates an ISO 8601 calendar date in extended format
# (spec-file-format.md §3.3.1): the lexical form AND a real day of a real month.
# A regex cannot express the month-length rule, so it is checked here.
function isdate(d,   yr, mo, dy, len) {
  if (d !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]$/) return 0
  yr = substr(d, 1, 4) + 0; mo = substr(d, 6, 2) + 0; dy = substr(d, 9, 2) + 0
  if (mo < 1 || mo > 12 || dy < 1) return 0
  len = 31
  if (mo == 4 || mo == 6 || mo == 9 || mo == 11) len = 30
  else if (mo == 2) len = isleap(yr) ? 29 : 28
  return (dy <= len)
}

# isleap applies the proleptic Gregorian rule ISO 8601 specifies.
function isleap(y) {
  return (y % 4 == 0 && y % 100 != 0) || (y % 400 == 0)
}

# ismonth validates an ISO 8601 year-month (a reduced-precision calendar date).
function ismonth(m,   mo) {
  if (m !~ /^[0-9][0-9][0-9][0-9]-[0-9][0-9]$/) return 0
  mo = substr(m, 6, 2) + 0
  return (mo >= 1 && mo <= 12)
}

# ---------------------------------------------------------------------------
# The declared ID grammar (spec-file-format.md §3.3.2).
#
# id_prefix (one to four uppercase ASCII letters, default T) and id_width (one
# or more digits, default 4) are declared in backlog.md frontmatter. Every ID
# in the directory -- item lines, working-file ids, next_id -- is matched
# against this grammar, never against a hardcoded T-NNNN shape. Resolved lazily
# so an invalid declaration is reported exactly once, wherever the grammar is
# first needed; the default stands in for an invalid part so the rest of the
# file still parses, which is what the Go validator does too.
# ---------------------------------------------------------------------------
function resolve_grammar(   i) {
  if (g_ready) return
  g_ready = 1
  idp = "T"; idw = 4
  if (idp_raw != "") {
    if (idp_raw ~ /^[A-Z]{1,4}$/) idp = idp_raw
    else gvs[++ngvs] = "backlog.md:" idp_line ": id_prefix must be one to four uppercase letters (A-Z): " idp_raw
  }
  if (idw_raw != "") {
    if (idw_raw ~ /^[0-9]+$/ && (idw_raw + 0) >= 1 && (idw_raw + 0) <= 15) {
      idw = idw_raw + 0
      if (idw < 3 || idw > 6)
        gws[++ngws] = "backlog.md:" idw_line ": id_width " idw " is outside the RECOMMENDED 3-6 range (spec-file-format.md §3.3.2 rule 3)"
    }
    else if (idw_raw ~ /^[0-9]+$/ && (idw_raw + 0) > 15)
      gvs[++ngvs] = "backlog.md:" idw_line ": id_width " idw_raw " is above the 15-digit cap (spec-file-format.md §3.3.2 rule 3)"
    else gvs[++ngvs] = "backlog.md:" idw_line ": id_width must be one to fifteen digits, at least 1: " idw_raw
  }
  gshape = idp "-"
  for (i = 0; i < idw; i++) gshape = gshape "#"
}

# validid reports whether s is an ID in the resolved grammar: the exact
# prefix, a hyphen, and exactly idw digits.
function validid(s,   i, c) {
  if (length(s) != length(idp) + 1 + idw) return 0
  if (substr(s, 1, length(idp)) != idp) return 0
  if (substr(s, length(idp) + 1, 1) != "-") return 0
  for (i = length(idp) + 2; i <= length(s); i++) {
    c = substr(s, i, 1)
    if (c < "0" || c > "9") return 0
  }
  return 1
}

# numid is the numeric part of an ID in the resolved grammar (0 for "0000").
function numid(s) { return substr(s, length(idp) + 2) + 0 }

# matchitem matches $0 against `- [BOX] [ID] TITLE` where ID is in the
# declared grammar (§4.2 rule 2) -- by TOKEN, never by fixed byte offsets,
# because the prefix and width are now variable. Sets it_box, it_id and
# it_rest; returns 1 on a match, 0 otherwise.
function matchitem(   i, idlen) {
  if (substr($0, 1, 7) != "- [ ] [" && substr($0, 1, 7) != "- [x] [") return 0
  idlen = length(idp) + 1 + idw
  if (substr($0, 8 + idlen, 2) != "] ") return 0
  it_id = substr($0, 8, idlen)
  if (!validid(it_id)) return 0
  it_box = substr($0, 4, 1)
  it_rest = substr($0, 8 + idlen + 2)
  if (it_rest == "") return 0        # a bare `]` after the ID is not a title
  return 1
}

FNR == 1 {
  base = FILENAME; sub(/^.*\//, "", base)
  section = ""; month = ""
  isw = (base ~ /^working\.[0-9]+\.md$/)
  if (isw) { nw++; wname[nw] = base }
  infm = ($0 == "---")
  if (!infm) err("file must open with YAML frontmatter (---)")
  next
}

# Git conflict markers are invalid in every data file (spec-file-format.md
# §5.1): a line whose first non-blank characters are exactly <<<<<<<,
# =======, or >>>>>>> is refused loudly, never ignored - a half-resolved
# merge must never be indistinguishable from valid prose.
{
  t = $0
  sub(/^[ \t]+/, "", t)
  if (t ~ /^<<<<<<</ || t ~ /^=======/ || t ~ /^>>>>>>>/) {
    err("git conflict-marker line: " $0)
    next
  }
}

infm {
  if ($0 == "---") { infm = 0; next }
  p = index($0, ":")
  if (p == 0) next
  k = trim(substr($0, 1, p - 1))
  v = substr($0, p + 1)
  if (k != "title") sub(/[ \t]+#.*$/, "", v)     # strip trailing YAML comment
  v = unq(trim(v))
  if (base == "backlog.md") {
    if (k == "next_id") next_id = v
    if (k == "project") project = v
    if (k == "id_prefix") { idp_raw = v; idp_line = FNR }
    if (k == "id_width")  { idw_raw = v; idw_line = FNR }
    if (k == "board")     { board_raw = v; board_line = FNR }
  }
  if (isw) { wf[base, k] = v; wl[base, k] = FNR }
  next
}

/^## / {
  section = trim(substr($0, 4))
  if (base == "backlog.md") {
    if      (section == "Ready")   h_ready   = 1
    else if (section == "Blocked") h_blocked = 1
    else if (section == "Someday") h_someday = 1
    else err("unknown section heading: " section)
  }
  if (base == "done.md") {
    if (ismonth(section)) month = section
    else { month = ""; err("heading must be an ISO 8601 YYYY-MM, got: " section) }
  }
  next
}

# Item lines. Only backlog.md and done.md hold them; a "- [ ]" in a working file
# is a subtask, which has no ID by design.
(base == "backlog.md" || base == "done.md") && /^- \[/ {
  resolve_grammar()
  if (!matchitem()) {
    err("malformed item line: " $0); next
  }
  box  = it_box
  id   = it_id
  rest = it_rest

  n = split(rest, part, / \| /)
  title = trim(part[1])
  if (title == "") err(id " has an empty title")

  split("", fld)
  for (i = 2; i <= n; i++) {
    p = index(part[i], ":")
    if (p == 0) { err(id " field is not key:value: " part[i]); continue }
    k = trim(substr(part[i], 1, p - 1))
    v = trim(substr(part[i], p + 1))
    if (index(v, "|") > 0) err(id " field " k " contains a pipe")
    if (k in fld) err(id " repeats field " k)
    fld[k] = v
  }

  # invariant 1: one home per ID
  # idloc values are relative to the directory: they are quoted inside messages
  # that already carry a location, and passed to ferr(), which adds the prefix.
  if (id in idloc) err(id " is already defined at " idloc[id])
  else idloc[id] = base ":" FNR

  # invariant 3: box state matches the file
  if (box == "x" && base == "backlog.md") err(id " is closed but sits in backlog.md")
  if (box == " " && base == "done.md")    err(id " is open but sits in done.md")

  # invariant 7 + field vocabulary
  if (("prio" in fld) && fld["prio"] !~ /^(high|med|low)$/)
    err(id " has prio:" fld["prio"] " (want high, med or low)")
  if (("outcome" in fld) && fld["outcome"] !~ /^(shipped|cancelled|obsolete)$/)
    err(id " has outcome:" fld["outcome"] " (want shipped, cancelled or obsolete)")
  if (("tags" in fld) && !istags(fld["tags"]))
    err(id " has malformed tags: " fld["tags"])
  if (("refs" in fld) && !isrefs(fld["refs"]))
    err(id " has malformed refs: " fld["refs"])
  if (("tickler" in fld) && !isschedule(fld["tickler"]))
    err(id " has tickler:" fld["tickler"] " (want a SCHEDULE: a date, a weekday, or a month day, each with optional @HH:MM)")
  split("created started done tickled", datekey, " ")
  for (i in datekey)
    if ((datekey[i] in fld) && !isdate(fld[datekey[i]]))
      err(id " has " datekey[i] ":" fld[datekey[i]] " (want an ISO 8601 date, YYYY-MM-DD)")

  # invariant 7: tickler belongs in ## Someday and needs created, the anchor
  # a never-fired recurring schedule needs (spec-file-format.md §5.1). Shape
  # only: the checker never evaluates a schedule (§10.1).
  if ("tickler" in fld) {
    if (base != "backlog.md" || section != "Someday")
      err(id " carries tickler: outside ## Someday (the only valid home)")
    if (!("created" in fld))
      err(id " carries tickler: but has no created: field")
  }

  if (base == "backlog.md") {
    # invariant 5
    if (section != "Ready" && section != "Blocked" && section != "Someday")
      err(id " is not under Ready, Blocked or Someday")
    else if (section == "Blocked" && !("blocked" in fld))
      err(id " is under Blocked with no blocked: field")
    else if (section != "Blocked" && ("blocked" in fld))
      err(id " has a blocked: field but is under " section)
  }

  if (base == "done.md") {
    # invariant 6
    if (!("done" in fld))    err(id " has no done: field")
    if (!("outcome" in fld)) err(id " has no outcome: field")
    if (month == "") err(id " is not under a YYYY-MM heading")
    else if (("done" in fld) && substr(fld["done"], 1, 7) != month)
      err(id " has done:" fld["done"] " under heading " month)
  }

  if ("detail" in fld) print "REF\t" id "\t" title "\t" fld["detail"] "\t" pfx base ":" FNR
  next
}

END {
  resolve_grammar()

  # invariant 4: every working file is coherent; each occupied one joins the
  # ID pool. The WIP limit is the file count, so nothing else enforces it.
  for (j = 1; j <= nw; j++) {
    f = wname[j]
    st = wv(f, "status")

    if (st == "working") {
      nwip++
      if (isnull(wv(f, "id")))
        werr(f, "id", "status is working but id is null")
      else if (!validid(wv(f, "id")))
        werr(f, "id", "id is not a " gshape " id: " wv(f, "id"))
      else if (wv(f, "id") in idloc)
        werr(f, "id", wv(f, "id") " also appears at " idloc[wv(f, "id")])
      else {
        idloc[wv(f, "id")] = f
        if (!isnull(wv(f, "detail")))
          print "REF\t" wv(f, "id") "\t" wv(f, "title") "\t" wv(f, "detail") "\t" pfx f ":" wl[f, "detail"]
      }

      # invariant 7: these fields carry the same forms as on an item line
      if (isnull(wv(f, "title")))
        werr(f, "title", "status is working but title is null")
      if (!isnull(wv(f, "prio")) && wv(f, "prio") !~ /^(high|med|low)$/)
        werr(f, "prio", "prio:" wv(f, "prio") " (want high, med or low)")
      if (!isnull(wv(f, "tags")) && !istags(wv(f, "tags")))
        werr(f, "tags", "malformed tags: " wv(f, "tags") " (want name,name or null)")
      if (!isnull(wv(f, "refs")) && !isrefs(wv(f, "refs")))
        werr(f, "refs", "malformed refs: " wv(f, "refs") " (want slug:id,slug:id or null)")
      if (!isnull(wv(f, "created")) && !isdate(wv(f, "created")))
        werr(f, "created", "created:" wv(f, "created") " (want an ISO 8601 date, YYYY-MM-DD)")
      if (!isdate(wv(f, "started")))
        werr(f, "started", "started:" wv(f, "started") " (want an ISO 8601 date, YYYY-MM-DD; set when the item starts)")
      if (!isnull(wv(f, "tickler")) && !isschedule(wv(f, "tickler")))
        werr(f, "tickler", "tickler:" wv(f, "tickler") " (want a SCHEDULE: a date, a weekday, or a month day, each with optional @HH:MM)")
      if (!isnull(wv(f, "tickled")) && !isdate(wv(f, "tickled")))
        werr(f, "tickled", "tickled:" wv(f, "tickled") " (want an ISO 8601 date, YYYY-MM-DD)")
      # invariant 7: a working slot is not ## Someday, so tickler: there is
      # residue of a half-finished transition (reported at the id line, where
      # the Go checker reports its placement finding).
      if (!isnull(wv(f, "tickler")))
        werr(f, "id", wv(f, "id") " carries tickler: in a working slot (## Someday is the only valid home)")

    } else if (st == "idle") {
      nk = split("id title prio tags refs detail created started", wk, " ")
      for (m = 1; m <= nk; m++)
        if (!isnull(wv(f, wk[m])))
          werr(f, wk[m], "status is idle but " wk[m] " is " wv(f, wk[m]))
    } else {
      werr(f, "status", "status must be working or idle, got: " st)
    }
  }
  wipn = nwip + 0; lim = nw + 0
  print "WIP\t" wipn "\t" lim

  # backlog.md frontmatter carries the directory identity
  if (isnull(project))
    ferr("backlog.md: frontmatter has no project name")
  if (board_raw != "" && !isslug(board_raw))
    ferr("backlog.md:" board_line ": board must be a slug [a-z][a-z0-9-]{0,15}: " board_raw)

  # invariant 2: every ID is below next_id
  if (next_id == "")
    ferr("backlog.md: frontmatter has no next_id")
  else if (!validid(next_id))
    ferr("backlog.md: next_id is not a " gshape " id: " next_id)
  else
    for (id in idloc)
      if (numid(id) >= numid(next_id))
        ferr(idloc[id] ": " id " is at or above next_id (" next_id ")")

  if (!h_ready)   ferr("backlog.md: no ## Ready heading")
  if (!h_blocked) ferr("backlog.md: no ## Blocked heading")
  if (!h_someday) ferr("backlog.md: no ## Someday heading")

  # problems with the declared grammar itself (§3.3.2): an invalid declaration
  # is a violation; a width outside 3-6 is a warning, never a failure (rule 3).
  for (i = 1; i <= ngvs; i++) print "ERR\t" pfx gvs[i]
  for (i = 1; i <= ngws; i++) print "WARN\t" pfx gws[i]
}
' "$dir/backlog.md" "${wfiles[@]}" "$dir/done.md" > "$tmp.raw"

  grep '^ERR' "$tmp.raw" | cut -f2- >> "$tmp.derr"
  grep '^REF' "$tmp.raw" > "$tmp.ref"
  # §3.3.2 rule 3: an id_width outside 3-6 is a warning, never a failure.
  # The two leading spaces keep it out of the finding stream, which is what
  # the exit code and the cross-validator agreement both run on.
  grep '^WARN' "$tmp.raw" | cut -f2- | sed 's/^/  /' >&2

  # --- invariant 8: detail: paths resolve to well-named, existing files -----
  while IFS=$'\t' read -r _tag id title detail loc; do
    [ -n "${detail:-}" ] || continue
    if [ "$detail" != "details/$id.md" ]; then
      problem "$loc: $id points at $detail (expected details/$id.md)"
      continue
    fi
    if [ ! -f "$dir/$detail" ]; then
      problem "$loc: detail file does not exist: $pfx$detail"
      continue
    fi
    fid=$(fm_get "$dir/$detail" id)
    ftitle=$(fm_get "$dir/$detail" title)
    [ "$fid" = "$id" ] ||
      problem "$pfx$detail: frontmatter id is '${fid:-<missing>}', expected '$id'"
    [ "$ftitle" = "$title" ] ||
      problem "$pfx$detail: frontmatter title is '${ftitle:-<missing>}', expected '$title'"
  done < "$tmp.ref"

  # --- invariant 9: no orphaned or double-claimed detail files -------------
  for f in "$dir"/details/*.md; do
    [ -e "$f" ] || continue
    case "${f##*/}" in _*) continue ;; esac
    rel="details/${f##*/}"
    refs=$(awk -F'\t' -v p="$rel" '$4 == p { n++ } END { print n + 0 }' "$tmp.ref")
    if [ "$refs" -eq 0 ]; then
      problem "$pfx$rel: orphan -- no item references it"
    elif [ "$refs" -gt 1 ]; then
      problem "$pfx$rel: referenced by $refs items (expected exactly 1)"
    fi
  done

  # --- report --------------------------------------------------------------
  if [ -s "$tmp.derr" ]; then
    sort -t: -k1,1 -k2,2n "$tmp.derr" >&2
    cat "$tmp.derr" >> "$tmp.err"
    echo "  $dir: $(wc -l < "$tmp.derr" | tr -d ' ') problem(s)" >&2
    return 1
  fi

  # The summary counts item lines under the directory's declared prefix; the
  # default T applies when the declaration is absent or invalid.
  gidp=$(fm_get "$dir/backlog.md" id_prefix)
  case "$gidp" in
    ''|[A-Z]|[A-Z][A-Z]|[A-Z][A-Z][A-Z]|[A-Z][A-Z][A-Z][A-Z]) gidp=${gidp:-T} ;;
    *) gidp=T ;;
  esac
  ready=$(grep -c "^- \[ \] \[$gidp-" "$dir/backlog.md")
  closed=$(grep -c "^- \[x\] \[$gidp-" "$dir/done.md")
  wip=$(awk -F'\t' '$1 == "WIP" { print $2 "/" $3 }' "$tmp.raw")
  project=$(fm_get "$dir/backlog.md" project)
  echo "  $dir: ok -- $project: $ready in backlog, wip $wip, $closed done"
  return 0
}

# ===========================================================================
# Decide what to check.
# ===========================================================================
targets=()

if [ "${#explicit[@]}" -gt 0 ]; then
  targets=("${explicit[@]}")
else
  # find.sh runs relative to here/ so the menu stays short; keep the absolute
  # path alongside so the check works from any cwd.
  rel=(); abs=(); disp=()
  while IFS= read -r line; do
    line=${line#./}
    rel+=("$line")
    abs+=("$here/$line")
    name=$(fm_get "$here/$line/backlog.md" project 2>/dev/null)
    if [ -n "$name" ] && [ "$name" != "null" ]; then
      disp+=("$line  ($name)")
    else
      disp+=("$line")
    fi
  done < <(cd "$here" && "$here/find.sh" .)

  if [ "${#rel[@]}" -eq 0 ]; then
    echo "check.sh: no todo directories found under $here" >&2
    exit 2
  fi

  if [ "$all" -eq 1 ] || [ "${#rel[@]}" -eq 1 ]; then
    targets=("${abs[@]}")
  elif [ ! -t 0 ]; then
    echo "check.sh: ${#rel[@]} todo directories found; name one or pass --all:" >&2
    printf '  %s\n' "${rel[@]}" >&2
    exit 2
  else
    n=${#rel[@]}
    echo "${n} todo directories under $here:"
    PS3=$'\n''Check which? '
    select _ in "${disp[@]}" "[all of them]" "[quit]"; do
      case "${REPLY:-}" in
        ''|*[!0-9]*) echo "check.sh: enter a number from 1 to $((n + 2))" >&2; continue ;;
      esac
      if   [ "$REPLY" -ge 1 ] && [ "$REPLY" -le "$n" ]; then
        targets=("${abs[$((REPLY - 1))]}"); break
      elif [ "$REPLY" -eq $((n + 1)) ]; then
        targets=("${abs[@]}"); break
      elif [ "$REPLY" -eq $((n + 2)) ]; then
        exit 0
      else
        echo "check.sh: enter a number from 1 to $((n + 2))" >&2
      fi
    done
    # select falls through on EOF (Ctrl-D) with nothing chosen
    [ "${#targets[@]}" -gt 0 ] || { echo "check.sh: nothing selected" >&2; exit 2; }
  fi
fi

# ===========================================================================
# Run.
# ===========================================================================
bad=0
for d in "${targets[@]}"; do
  check_dir "$d" || bad=$((bad + 1))
done

echo ""
if [ "$bad" -gt 0 ]; then
  echo "check.sh: $(wc -l < "$tmp.err" | tr -d ' ') problem(s) in $bad of ${#targets[@]} director$([ "${#targets[@]}" -eq 1 ] && echo y || echo ies)" >&2
  exit 1
fi
echo "check.sh: ok -- ${#targets[@]} director$([ "${#targets[@]}" -eq 1 ] && echo y || echo ies) clean"
