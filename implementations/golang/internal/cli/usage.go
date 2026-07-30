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
  --find                    list discovered micro-manager directories
  --help, --version

Global modifiers:
  --dir PATH                act on this directory
  --json                    machine envelope (not implemented yet)
  --porcelain               stable tab-separated output (not implemented yet)
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

Run 'mm --help --OPERATION' for one operation's switches.
`

// operationUsage is the per-operation help. The key is the operation switch.
var operationUsage = map[Op]string{
	OpInit: `mm --init --project NAME [--dir PATH] [--slots N] [--slot-width W]

Creates backlog.md, done.md, N working files, details/_template.md and
structure.md. Without --dir, creates ./micro-manager.

  --project NAME            required; the human name of the directory
  --slots N                 how many working files, and so the WIP limit (1)
                            (not --wip: that is the operation that changes it)
  --slot-width W            digits in working.NN.md (2)
`,
	OpAdd: `mm --add TITLE [--prio P] [--tag T]... [--section S] [--top]
              [--blocked REASON] [--created DATE]
              [--detail | --detail-text TEXT | --detail-file PATH]

Appends to the BOTTOM of the section by default: a new item is not automatically
more important than everything already queued.

  --top                     insert at the top instead
  --section ready|blocked|someday
  --blocked REASON          implies --section blocked
  --tag T                   accumulates: --tag infra --tag ci
  --detail                  create details/<ID>.md from the template
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
                 [--note TEXT] [--discard-notes]

Works from a working slot and from the backlog directly. --outcome cancelled is
how work is abandoned without deleting it.

  --note TEXT               append a closing note to the detail file
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
`,
	OpWip: `mm --wip N

Sets the WIP limit by creating or deleting working files — the limit is the file
count, not a setting. Lowering it refuses if a slot that would go is occupied,
and never renumbers an occupied slot.
`,
	OpFind: `mm --find [--dir PATH]

Lists micro-manager directories below the current directory, or below PATH.
`,
}

func writeUsage(w io.Writer, op Op) {
	if text, ok := operationUsage[op]; ok {
		fmt.Fprint(w, text)
		return
	}
	fmt.Fprint(w, generalUsage)
}
