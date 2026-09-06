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
  --add-many [FILE]         add many items, one per line, in one transaction
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
                            (legacy repairs, then the versioned chain)
  --stats                   throughput, cycle time, work in flight, tags
  --tick                    fire due someday schedules (tickler)
  --refresh-structure       explicitly refresh managed structure.md guidance
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

Shell quoting:
  Your shell expands command substitutions before mm sees an argument. On
  POSIX shells, single-quote literal text containing $() or backticks:

    mm --note T-0042 'Investigate $(hostname) and ` + "`date`" + ` literally'

  PowerShell and other shells have different quoting rules; use the literal
  quoting syntax for the shell you are running. This expansion is shell
  behavior, not mm parsing or validation.

Run 'mm --help --OPERATION' for one operation's switches.
For the on-disk layout and safe manual-editing rules, read structure.md in the
board directory.
`

// operationUsage is the per-operation help. The key is the operation switch.
var operationUsage = map[Op]string{
	OpInit: `mm --init --project NAME [--dir PATH] [--wip-limit N]
             [--prefix P] [--id-width N] [--description TEXT]

Creates board.md, done.md and details/_template.md at version 2. Without
--dir, creates ./micro-manager.

  --project NAME            required; the human name of the directory
  --wip-limit N             caps the working stage (absent means uncapped)
                            (not --wip: that is the operation that changes it)
  --prefix P                the ID prefix: one to four uppercase letters (T).
                            Choose it only while creating the board; it is
                            permanent because every item ID uses it
  --id-width N              digits in item IDs (4; 3-6 recommended)
  --description TEXT        seeds structure.md's first paragraph; writes
                            structure.md even if it would otherwise be skipped

Example: create a Garden board whose IDs begin G-0001, G-0002, and so on:

  mm --init --project "Garden" --prefix G
`,
	OpAdd: `mm --add TITLE [--prio P] [--tag T]... [--section S] [--top]
              [--blocked REASON] [--created DATE] [--tickler SCHEDULE]
              [--stage SLUG] [--reason TEXT] [--tickler-dest SLUG]
              [--detail | --detail-text TEXT | --detail-file PATH]

Appends to the BOTTOM of the section (version 1) or stage's run (version 2)
by default: a new item is not automatically more important than everything
already queued.

TITLE is exactly one argument — quote it if it has spaces (mm --add "Fix the
deploy script"). A surplus positional is a usage error, not a silent second
title; mm --add -- TITLE is the spelling for a title that starts with a dash.

  --top                     insert at the top instead
  --section ready|blocked|someday     version 1
  --blocked REASON          version 1; implies --section blocked
  --stage SLUG               version 2; defaults to ready. A reason with no
                            --stage implies the directory's needs_reason stage
  --reason TEXT              version 2; required entering a needs_reason stage
  --tickler-dest SLUG         version 2; overrides the stage's own
                            tickler_stages destination for this item only
  --tickler SCHEDULE        schedule the item: a date (2026-09-01), a weekday
                            (mon@08:00, first-mon@08:00), or a month day
                            (15@08:00, last@08:00), each with an optional
                            @HH:MM time. Requires --section someday (version
                            1) or a --stage that is a tickler_stages source
                            (version 2)
  --tag T                   accumulates: --tag infra --tag ci
  --detail                  create details/<ID>.md from the template, and open
                            it in $VISUAL or $EDITOR
  --no-edit                 create it but do not open an editor
`,
	OpAddMany: `mm --add-many [FILE] [--top] [--section S] [--prio P] [--tag T]...
                   [--blocked REASON] [--created DATE] [--tickler SCHEDULE]

Adds many items in ONE transaction, one per input line (spec-tools.md §5.2.1).
Reads FILE, or stdin when FILE is absent or is "-".

A bad line means NOTHING is written — that is the whole difference from a shell
loop over --add, which would leave you with eleven items added and no record of
where it stopped.

The line grammar is the item line with the box and the id removed, because the
id is allocated here and never supplied:

  Fix the deploy script
  Rotate the leaked token | prio:high | tags:infra,ci
  - Pasted out of a checklist | prio:low

  blank lines           skipped
  a leading "- ", "* "  stripped, "- [ ] " too: a pasted checklist just works
  fields                exactly what --add can set: prio, tags, refs, created,
                        blocked, tickler, and unregistered keys, kept verbatim
  [T-0042]              refused: ids are allocated, and reusing one breaks I2

The modifiers are DEFAULTS; a line that names the same field wins. A line's
blocked: reason sends that line to Blocked, so one run may write two sections.
--top puts the batch at the top, still in the order it was given.

  --add-many does not take --detail, --detail-text or --detail-file: one body
  cannot belong to several items.

Porcelain columns: the same as --list, one row per created item.
`,
	OpList: `mm --list [--state S] [--section S] [--stage S] [--prio P] [--tag T]
               [--blocked-only] [--limit N]

Order is always the on-disk order. ## Ready order is your own prioritisation and
this will not re-sort it.

  --state backlog|working|done|all      (backlog)
  --section SECTION         version 1 only
  --stage SLUG              version 2 only
  --blocked-only            only items carrying a blocked: field (v1) or a
                            reason: field (v2)
`,
	OpShow: `mm --show ID [--detail]

Prints every field, where the item lives, and with --detail the detail file body.
`,
	OpEdit: `mm --edit ID [--title T] [--prio P] [--tag T]... [--untag T]...
               [--set KEY=VALUE]... [--unset KEY]... [--blocked REASON]
               [--created DATE] [--started DATE]

Changes fields in place. Position is --move; state is --start/--pause/--finish.
--set and --unset reach any key, including ones this tool does not know.
--stage is not valid with --edit. Move an item to a custom stage explicitly:

  mm --move T-0042 --stage review
`,
	OpRemove: `mm --remove ID --force [--with-detail]

Deletes the item outright. The ID is retired, never reused.

--force is required and --yes will not satisfy it. To abandon work while keeping
the record, use --finish ID --outcome cancelled instead.

  --with-detail             delete the detail file too, rather than orphaning it
`,
	OpMove: `mm --move ID (--position N | --top | --end | --before ID | --after ID)
               [--section S] [--blocked REASON] [--stage SLUG] [--reason TEXT]

Exactly one destination. A position past the end is an error, not a clamp.

Version 1: --section moves between ## Ready/Blocked/Someday; moving into
blocked needs a reason (--blocked), moving out drops it.

Version 2: --stage moves onto any declared stage; --reason supplies one when
the destination needs_reason and the item does not already carry one. Unlike
version 1, a reason already on the item is NOT dropped on leaving a
needs_reason stage.
`,
	OpStart: `mm --start ID [--slot NN]

Moves the item into a working slot. Without --slot, the lowest-numbered idle one.
If every slot is occupied this fails: finish one, pause one, or raise the limit
deliberately with --wip.
`,
	OpPause: `mm --pause ID [--section S] [--end] [--discard-notes] [--blocked REASON]
                 [--stage SLUG]

Returns the item to the TOP of ## Ready (version 1) or the ready stage
(version 2) by default, keeping started:. Notes accumulated while it was
working are appended to the detail file, which is created if it does not
exist — those notes exist nowhere else.

  --end                     append instead of inserting at the top
  --keep-notes              preserve them; this is the default
  --discard-notes           throw the notes away instead
  --section ready|blocked|someday     version 1; --blocked implies blocked
  --stage SLUG               version 2; defaults to ready. Pausing onto a
                            needs_reason stage requires the item to already
                            carry a reason:
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
                [--group-by outcome|tag|day|none] [--include-stage SLUG]...
                [--include-wip] [--include-backlog] [--include-archives]

Periods: last-week (default), this-week, YYYY-Www, last-N-days, YYYY-MM,
last-month, this-month, today, yesterday, all.

The default is last-week rather than this-week because a report over a closed
period is reproducible and one over an open period is not.

  --include-stage SLUG      append what currently sits on SLUG, marked with
                            it; repeatable, one stage per occurrence
                            (version 2 only)
  --include-wip             a shorthand for --include-stage working on a
                            version-2 directory; version 1's working slots
                            otherwise
`,
	OpCheck: `mm --check [--all]

Runs the ten invariants and prints file:line: message for each finding. Exits 1
if any directory has one. This is the same validation every mutation runs before
it commits.

Violations are results, not errors: under --json the envelope reports ok:true
because the check ran, each directory carries its own ok, and the EXIT CODE is
what a script gates on.
`,
	OpWip: `mm --wip N [--stage SLUG]

Version 1: sets the WIP limit by creating or deleting working files — the
limit is the file count, not a setting. Lowering it refuses if a slot that
would go is occupied, and never renumbers an occupied slot.

Version 2, with --stage: sets that one stage's own cap (spec-file-format.md
§5.1.3) instead — a real setting this time, wip.<slug> in board.md's
frontmatter. --wip 0 --stage SLUG clears it (uncapped, which is also what
never setting one at all means). Lowering it below the stage's current count
refuses rather than leaving a directory already over its own new limit.
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
	OpMigrate: `mm --migrate [--project NAME] [--to VERSION] [--dry-run]

Runs two independent things, in order, and reports every change from both
(spec-tools.md §5.3, §5.3.4):

  1. The legacy repair. Three specific pre-version-1 shapes, and nothing
     else — a migration that rewrites what it was not asked to rewrite is
     indistinguishable from corruption in the diff:

       working.md              renamed to the lowest free working.NN.md, at
                                the digit width the directory already uses
       no project               backlog.md gains one: --project NAME, or the
                                parent directory's name, reported either way
       tags: [infra, ci]        a YAML flow sequence becomes the TAGLIST the
                                format uses everywhere, infra,ci — on item
                                lines and in working-file frontmatter alike

  2. The versioned chain. Brings the directory to --to, or the latest
     version this build implements when --to is absent — which is how a
     version-1 directory (backlog.md + working.NN.md) becomes version 2
     (board.md): folded stage by stage, blocked: renamed reason:, working-file
     notes moved into details/<ID>.md. --to VERSION stops partway instead,
     useful once this build supports more than one version bump.

Both phases run even though the second reads what the first may have just
written: a directory the legacy repair just made valid version 1 is exactly
the directory that can now migrate. Safe to run twice — a second run finds
nothing left to do at either phase, exit 0 either way. A directory with git
uncommitted changes is named on stderr first, a recommendation, not a guard.

A tags value the legacy repair cannot convert — a space inside a tag — is
left exactly as written and named on stderr; the rest of that phase still
runs. If it leaves the directory still invalid, the version chain refuses to
proceed past it — reported as "version not migrated", not a command failure,
since the legacy repair's own progress already stands. Resolve what --check
still reports and run --migrate again.

Unlike --fix, the legacy repair does not refuse over a violation it does not
own: it is the first thing to run on an old directory, and --fix blocks on
the very thing this clears. It does refuse two writes that would leave a
directory --check rejects: a working.md whose content is not a working file,
and a conversion that would reveal a duplicate ID. The version chain refuses
similarly over any line it cannot parse at all, rather than silently leaving
the item out of the migrated board.

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

The default is manual, like every operation; a scheduled run invokes
mm --tick --dir PATH from a timer. Fires live entirely in backlog.md, so
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
	OpRefreshStructure: `mm --refresh-structure [--dry-run]

Explicitly regenerates the managed parts of structure.md for a version-2 board.
A missing file is created. An existing file is updated only when it contains
exactly one checked refresh marker and one valid user-notes boundary pair.

The board description and every byte inside the user-notes area are preserved.
Uncheck or delete the refresh marker to protect an existing file. To keep a
protected file and create a fresh managed guide, rename it first and run this
command again. --migrate never performs this refresh implicitly.

Porcelain columns: file action
`,
}

func writeUsage(w io.Writer, op Op) {
	if text, ok := operationUsage[op]; ok {
		fmt.Fprint(w, text)
		return
	}
	fmt.Fprint(w, generalUsage)
}
