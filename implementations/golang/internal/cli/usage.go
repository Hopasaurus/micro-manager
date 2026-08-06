package cli

import (
	"fmt"
	"io"
)

// Usage text (spec-tools.md §3.4: --help, and --help with an operation).
//
// Every switch listed here is one the parser accepts, and every switch the
// parser accepts is listed here. A help text that has drifted from the parser
// is worse than none, so a test walks both and fails on a mismatch.

const generalUsage = `mm — micro-manager, a plain-markdown todo system

usage: mm --OPERATION [SUBJECT] [modifiers]

The operation is a switch, never a positional word: mm --add "…", not mm add "…".
Exactly one operation per invocation.

Operations:
  --init                    create a directory (needs --project NAME)
  --add TITLE               add a backlog item
  --list                    list items
  --show ID                 print one item
  --edit ID                 change an item's fields
  --remove ID               delete an item (needs --force)
  --move ID                 reposition a backlog item
  --start ID                backlog -> a working slot
  --pause ID                working slot -> backlog
  --finish ID               -> done.md
  --report                  what closed in a period
  --check                   validate against the ten invariants
  --wip N                   set the WIP limit by adding or removing slot files
  --block ID --reason T     move to Blocked with a reason
  --unblock ID              move back to Ready and drop the reason
  --note ID TEXT            append a dated note
  --status                  one screen: slots, WIP, counts, next, oldest
  --next                    print the top of ## Ready; exits non-zero if empty
  --search QUERY            substring or regex match over titles, tags, details
  --find                    list discovered micro-manager directories
  --fix                     repair duplicate IDs after a merge
  --archive                 roll old month groups out of done.md
  --migrate                 bring an older directory up to the current format
  --stats                   throughput, cycle time, work in flight, tags
  --tick                    fire due someday schedules (tickler)
  --help, --version

Global modifiers:
  --dir PATH                act on this directory
  --json                    one JSON object on stdout, on success and failure
  --porcelain               tab-separated records, one per line, no header
  --dry-run                 compute and report the change, write nothing
  --force                   proceed with a guarded destructive action
  --quiet                   suppress non-essential output; errors still print
  --verbose                 add detail
  --all                     every discovered directory (read-only operations)

Directory resolution, first hit wins:
  1. --dir PATH
  2. MM_DIR
  3. the nearest directory above the current one that holds a micro-manager dir
  4. a downward search from the current directory

Environment:
  MM_DIR                    default directory
  MM_REPORT_PERIOD          default --report period
  NO_COLOR                  suppress colour
  VISUAL, EDITOR            editor for detail files; VISUAL wins. Not opened
                            under --quiet, --json, --porcelain, --dry-run,
                            --no-edit, or when there is no terminal

Run 'mm --help --OPERATION' for one operation's switches.
`

// operationUsage is the per-operation help. The key is the operation switch.
var operationUsage = map[Op]string{
	OpInit: `mm --init --project NAME [--dir PATH] [--slots N] [--slot-width W]
             [--prefix P] [--id-width N]

Creates backlog.md, done.md, N working files, details/_template.md and
structure.md. Without --dir, creates ./micro-manager.

  --project NAME            required; the human name of the directory
  --slots N                 how many working files, and so the WIP limit (1)
                            (not --wip: that is the operation that changes it)
  --slot-width W            digits in working.NN.md (2)
  --prefix P                the ID prefix: one to four uppercase letters (T).
                            Declared once, read by every other operation
  --id-width N              digits in item IDs (4; 3-6 recommended)
`,
	OpAdd: `mm --add TITLE [--prio P] [--tag T]... [--section S] [--top]
              [--blocked REASON] [--created DATE] [--tickler SCHEDULE]
              [--detail | --detail-text TEXT | --detail-file PATH]

Appends to the BOTTOM of the section by default: a new item is not automatically
more important than everything already queued.

TITLE is exactly one argument — quote it if it has spaces (mm --add "Fix the
deploy script"). A surplus positional is a usage error, not a silent second
title; mm --add -- TITLE is the spelling for a title that starts with a dash.

  --top                     insert at the top instead
  --section ready|blocked|someday
  --blocked REASON          implies --section blocked
  --tickler SCHEDULE        schedule the item: a date (2026-09-01), a weekday
                            (mon@08:00, first-mon@08:00), or a month day
                            (15@08:00, last@08:00), each with an optional
                            @HH:MM time. REQUIRES --section someday
  --tag T                   accumulates: --tag infra --tag ci
  --detail                  create details/<ID>.md from the template, and open
                            it in $VISUAL or $EDITOR
  --no-edit                 create it but do not open an editor
`,
	OpList: `mm --list [--state S] [--section S] [--prio P] [--tag T]
               [--blocked-only] [--limit N]

Order is always the on-disk order. ## Ready order is your own prioritisation and
this will not re-sort it.

  --state backlog|working|done|all      (backlog)
  --blocked-only            only items carrying a blocked: field
`,
	OpShow: `mm --show ID [--detail]

Prints every field, where the item lives, and with --detail the detail file body.
`,
	OpEdit: `mm --edit ID [--title T] [--prio P] [--tag T]... [--untag T]...
               [--set KEY=VALUE]... [--unset KEY]... [--blocked REASON]
               [--created DATE] [--started DATE]

Changes fields in place. Position is --move; state is --start/--pause/--finish.
--set and --unset reach any key, including ones this tool does not know.
`,
	OpRemove: `mm --remove ID --force [--with-detail]

Deletes the item outright. The ID is retired, never reused.

--force is required and --yes will not satisfy it. To abandon work while keeping
the record, use --finish ID --outcome cancelled instead.

  --with-detail             delete the detail file too, rather than orphaning it
`,
	OpMove: `mm --move ID (--position N | --top | --end | --before ID | --after ID)
               [--section S] [--blocked REASON]

Exactly one destination. A position past the end is an error, not a clamp.
Moving into blocked needs a reason; moving out drops it.
`,
	OpStart: `mm --start ID [--slot NN]

Moves the item into a working slot. Without --slot, the lowest-numbered idle one.
If every slot is occupied this fails: finish one, pause one, or raise the limit
deliberately with --wip.
`,
	OpPause: `mm --pause ID [--section S] [--end] [--discard-notes] [--blocked REASON]

Returns the item to the TOP of ## Ready, keeping started:. The slot's ## Notes
are appended to the detail file, which is created if it does not exist — those
notes exist nowhere else.

  --end                     append instead of inserting at the top
  --keep-notes              preserve them; this is the default
  --discard-notes           throw the notes away instead
`,
	OpFinish: `mm --finish ID [--outcome shipped|cancelled|obsolete] [--done DATE]
                 [--closing-note TEXT] [--discard-notes]

Works from a working slot and from the backlog directly. --outcome cancelled is
how work is abandoned without deleting it.

  --closing-note TEXT       append a closing note to the detail file
                            (not --note: that is the operation that adds one)
  --keep-notes              preserve the slot's notes; this is the default
  --discard-notes           throw them away instead
`,
	OpReport: `mm --report [--period TOKEN | --last-week | --this-week | --week YYYY-Www
                 | --since DATE [--until DATE]]
                [--group-by outcome|tag|day|none]
                [--include-wip] [--include-backlog] [--include-archives]

Periods: last-week (default), this-week, YYYY-Www, last-N-days, YYYY-MM,
last-month, this-month, today, yesterday, all.

The default is last-week rather than this-week because a report over a closed
period is reproducible and one over an open period is not.
`,
	OpCheck: `mm --check [--all]

Runs the ten invariants and prints file:line: message for each finding. Exits 1
if any directory has one. This is the same validation every mutation runs before
it commits.

Violations are results, not errors: under --json the envelope reports ok:true
because the check ran, each directory carries its own ok, and the EXIT CODE is
what a script gates on.
`,
	OpWip: `mm --wip N

Sets the WIP limit by creating or deleting working files — the limit is the file
count, not a setting. Lowering it refuses if a slot that would go is occupied,
and never renumbers an occupied slot.
`,
	OpBlock: `mm --block ID --reason TEXT

Moves the item to ## Blocked and records why. Sugar over
--move ID --section blocked --blocked TEXT.

I5 requires every item under Blocked to carry a reason, so --reason is required.
`,
	OpUnblock: `mm --unblock ID [--end]

Moves the item back to ## Ready and drops the blocked: reason. It goes to the
top by default: something that has just become possible is usually the next
thing to pick up. --end appends instead.
`,
	OpNote: `mm --note ID TEXT

Appends a dated entry to the item's notes. Where it lands depends on where the
item is: a working item's notes go in its slot, next to the work, where --pause
and --finish already know to preserve them. Anything else goes to the item's
detail file, which is created if it does not exist.
`,
	OpFind: `mm --find [--dir PATH]

Lists micro-manager directories below the current directory, or below PATH.
`,
	OpStatus: `mm --status

One screen (spec-tools.md §5.2): what is in each working slot, WIP n/N, counts
by section, the top of ## Ready, and the oldest Ready item that has never been
started — the one quietly aging at the bottom of the list.
`,
	OpNext: `mm --next

Prints the top of ## Ready, the thing to start next. Exits non-zero (code 3)
when ## Ready is empty, so a script can stop rather than start something
arbitrary.
`,
	OpFix: `mm --fix [--dry-run]

Repairs what a git merge manufactures: an ID in two homes (I1) and the
next_id ceiling it leaves (I2). The duplicate is kept where it has advanced
furthest (done > working > backlog, then earliest created); the others are
renumbered to fresh IDs from next_id, and each one's detail file follows it
(I9). next_id only ever rises. A tie — same home, same created — refuses with
both items named. Refuses while anything else is wrong (conflict markers, a
dangling detail); safe to run twice, the second run is a no-op.

Porcelain columns: oldId newId file detail
`,
	OpArchive: `mm --archive [--before YYYY-MM | --age DAYS] [--dry-run]

Moves whole month groups out of done.md into done-YYYY.md, and the detail files
of the items in them into details-YYYY/, rewriting each archived detail: field
to match (spec-file-format.md §5.6). Whole groups only: a month is the finest
grain done.md records.

This is the one operation that takes data OUT of the validated set. Archived
items leave the ID pool — I1 and I2 stop seeing them — and a report over an
archived period finds nothing without --include-archives. It says so on every
run, including under --quiet.

Nothing is left behind and nothing is deleted, so the directory still validates
afterwards. Restoring is the same move backwards: paste the lines back into
done.md, move details-YYYY/<ID>.md back to details/, and rewrite the field.
Doing half of it is caught — the restored line is validated again, and I8
reports a detail: that is not details/<ID>.md, or one that is and names a file
that is not there.

  --before YYYY-MM          archive every group older than this month; the month
                            itself stays. A full date is accepted and its day
                            ignored. Default: the current month, so a bare
                            --archive rolls up everything before the month the
                            board is living in
  --age DAYS                the same cutoff as a policy: archive a group once
                            DAYS days have passed since its last day. --age 0
                            takes every complete month; --age 30 keeps each
                            month a further thirty days. This is the form a
                            scheduled run uses: it needs no editing each month

Porcelain columns: id file detail
`,
	OpMigrate: `mm --migrate [--project NAME] [--dry-run]

Brings a directory written by an earlier revision of the format up to the
current one, and reports every change (spec-tools.md §5.3). Three legacy shapes,
and nothing else — a migration that rewrites what it was not asked to rewrite is
indistinguishable from corruption in the diff:

  working.md                renamed to the lowest free working.NN.md, at the
                            digit width the directory already uses
  no project                backlog.md gains one: --project NAME, or the parent
                            directory's name, reported either way
  tags: [infra, ci]         a YAML flow sequence becomes the TAGLIST the format
                            uses everywhere, infra,ci — on item lines and in
                            working-file frontmatter alike

Safe to run twice: the second run finds nothing to do. A tags value it cannot
convert — a space inside a tag — is left exactly as written and named on stderr,
and the rest of the migration still runs.

Unlike --fix, it does not refuse over a violation it does not own: it is the
first thing to run on an old directory, and --fix blocks on the very thing this
clears. It does refuse two writes that would leave a directory --check rejects:
a working.md whose content is not a working file, and a conversion that would
reveal a duplicate ID.

Porcelain columns: kind file line after
`,
	OpStats: `mm --stats [--period TOKEN | --since DATE [--until DATE] | --week YYYY-Www]
                [--bucket day|week|month] [--include-archives]

Four measures over a period (spec-tools.md §5.3), from the three dates the
format records — created, started, done:

  throughput                items closed per bucket, and by outcome
  cycle time                done minus started, in whole days: mean, median,
                            p90 and range. An item with no started date is
                            counted as unmeasurable, never assumed
  in flight                 items between started and done, per day: peak, the
                            day it peaked, and the mean
  tags                      the closed items distributed over their tags, with
                            the untagged counted separately

The period defaults to ALL of history, not to last week: throughput over seven
days is a sample, and this is usually a question about the trend. MM_REPORT_PERIOD
does not apply — it is the default --report period, and stats is not a report.

"In flight" is not "slots occupied". The format records dates, not times
(spec-file-format.md §10.1), and a pause leaves no trace at all (§5.2), so an
item counts from its started date to its done date inclusive: a board that
starts and finishes ten things in one day reads as ten in flight that day, and
one paused for a month reads as in flight for that month. The WIP LIMIT is not
recorded historically either (§10.7), so nothing here measures pressure against
it.

  --bucket day|week|month   resolution of the series (week)
  --include-archives        read done-YYYY.md too; without it an archived
                            period reads as one in which nothing closed, and
                            says so

Porcelain columns: bucket since until closed wipPeak wipMean
`,
	OpTick: `mm --tick [--dry-run]

Runs the tickler once (spec-tools.md §5.3.3): every ## Someday item carrying
` + "`tickler:`" + ` is evaluated against today, and the due ones fire. Two kinds
of fire, decided by the schedule value:

  one-shot (2026-09-01)     the item moves to ## Ready, the schedule is
                            consumed (tickler: dropped), tickled:<today> stamped
  recurring (mon@08:00,     the item is a PROTOTYPE: it stays in ## Someday,
  first-mon@08:00,          tickled:<today> stamped, and a fresh Ready item is
  15@08:00, last@08:00)     spawned with the title, prio and tags only — no
                            detail, no refs — under a new ID from next_id

The default is manual, like every operation; a scheduled run is a cron entry
(` + "`mm --tick --dir ...`" + ` from a timer). Fires live entirely in backlog.md, so
the run is safe to overlap with the UI service's own ticker (spec-gui.md §2.4):
the tickled stamp and pre-commit validation keep two runners from firing one
item twice.

A run always reports what fired and what errored. A failing item never aborts
the run: it is reported per-item and the rest proceed. --dry-run prints exactly
what a real run would, prefixed "would:".

Porcelain columns: id outcome spawned tickled message
(outcome is move, spawn or error)
`,
	OpSearch: `mm --search QUERY [--regex] [--field F]... [--state S] [--limit N]

Substring or regex match over titles, tags and detail bodies, reporting state
and location per hit. A plain query is a case-insensitive substring; a regex is
matched exactly as written, so ask for case-insensitivity with (?i).

QUERY is exactly one argument — quote it if it has spaces (mm --search "fix the
deploy"). A surplus positional is a usage error: an unquoted multi-word query
would otherwise silently search only its first word.

  --regex                   treat QUERY as a regular expression
  --field title|tags|detail narrow where to look; repeats (default: all three)
  --state backlog|working|done
  --limit N                 cap the number of hits
`,
}

func writeUsage(w io.Writer, op Op) {
	if text, ok := operationUsage[op]; ok {
		fmt.Fprint(w, text)
		return
	}
	fmt.Fprint(w, generalUsage)
}
