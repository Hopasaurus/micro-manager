---
name: micro-manager
description: Manage a plain-markdown todo project through the `mm` command-line tool — add, list, start, pause, block, note, finish, move, edit, remove, report on and validate tasks kept in backlog.md/working.NN.md/done.md/details/ files, with WIP-limited working slots and ten machine-checked invariants. Use whenever a micro-manager, .micro-manager, µmanager or μmanager directory is present nearby, or when asked to track, plan, prioritize or work through a todo list. Assumes the `mm` binary is already installed and on PATH.
compatibility: Requires the `mm` binary on PATH (any OS). No other runtime dependency — `mm` reads and writes plain markdown directly.
metadata:
  portable: true
  requires-cli: mm
---

# micro-manager

A todo system stored as plain markdown — `backlog.md`, one `working.NN.md` per
work-in-progress slot, `done.md`, and optional `details/<ID>.md` files for
longer notes. Every file is readable by a human in a text editor; the `mm` CLI
is what makes editing it fast and impossible to corrupt.

> This skill assumes `mm` is already on PATH. Confirm with `mm --version`
> before relying on it. If it is missing, it is not this skill's job to build
> it — say so and stop rather than hand-editing the files (see *If `mm` is not
> available* at the end).

## Find the project

A micro-manager directory is recognized by name alone, in six equivalent
spellings: `micro-manager`, `.micro-manager`, `µmanager`, `.µmanager`,
`μmanager` (Greek mu), `.μmanager` (Greek mu, hidden). They render almost
identically; don't assume which one is present.

You rarely need to locate it yourself — `mm` does, in this order: `--dir PATH`,
then `$MM_DIR`, then the nearest directory above the current one that contains
one, then a downward search from the current directory. **It refuses rather
than guessing** if more than one candidate matches, and lists them. Use
`--dir PATH` explicitly whenever you already know which project you mean, and
always when more than one might be nearby.

```bash
mm --find                 # list every micro-manager directory below here
mm --status                # confirm which one mm would use, and its state
```

## Quick reference

Every invocation is exactly one operation, given as a switch — never a
positional word: `mm --add "…"`, not `mm add "…"`.

| Operation | Effect |
|---|---|
| `mm --init --project NAME [--slots N]` | create a new project (default: `./micro-manager`, 1 slot) |
| `mm --add TITLE [--prio P] [--tag T]... [--top]` | append a backlog item (bottom of its section unless `--top`) |
| `mm --add-many [FILE]` | add many items, one per input line, in ONE transaction (stdin when no FILE) |
| `mm --list [--state S] [--tag T] [--prio P]` | list items in on-disk order (never re-sorted) |
| `mm --show ID [--detail]` | print one item, optionally with its detail file |
| `mm --status` | one screen: slots, WIP, counts, next item, oldest untouched item |
| `mm --next` | print the top of `## Ready`; exit code 3 if it's empty |
| `mm --search QUERY [--regex]` | substring/regex match over titles, tags, detail bodies |
| `mm --start ID [--slot NN]` | backlog → a working slot (lowest idle one by default) |
| `mm --note ID TEXT` | append a dated note (to the working slot, or the detail file if not started) |
| `mm --pause ID` | working slot → back to top of backlog, notes preserved to the detail file |
| `mm --finish ID [--outcome shipped|cancelled|obsolete]` | → `done.md` (works directly from the backlog too) |
| `mm --block ID --reason TEXT` | move to `## Blocked` with a reason (required) |
| `mm --unblock ID` | move back to `## Ready`, drop the reason |
| `mm --move ID (--position N \| --top \| --end \| --before ID \| --after ID)` | reposition within its section |
| `mm --edit ID [--title T] [--set KEY=VALUE] [--unset KEY]` | change fields in place, including ones `mm` doesn't know about |
| `mm --remove ID --force [--with-detail]` | delete outright; the ID is retired, never reused |
| `mm --report [--last-week \| --this-week \| --since DATE] [--group-by outcome\|tag\|day]` | what closed in a period (default: last complete week) |
| `mm --wip N` | set the WIP limit by adding/removing slot files — the file count *is* the limit |
| `mm --check [--all]` | validate against the ten invariants |
| `mm --fix` | repair duplicate IDs left by a git merge |
| `mm --archive [--before YYYY-MM \| --age DAYS]` | roll old month groups out of `done.md` into `done-YYYY.md` |
| `mm --migrate [--project NAME]` | bring a directory written by an older revision up to the current format |
| `mm --stats [--bucket day\|week\|month]` | throughput, cycle time, work in flight and tag distribution |
| `mm --tick [--dry-run]` | fire the `## Someday` items whose `tickler:` schedule is due |

Run `mm --help` for the full list and `mm --help --OPERATION` for one
operation's exact switches — trust that over anything paraphrased here.

## A typical session

```bash
mm --status                                   # orient: what's queued, what's running

mm --add "Rotate the leaked staging token" --prio high --tag security --top
mm --add "Write the onboarding doc" --prio low --tag docs

mm --add-many < notes-from-the-meeting.txt    # a whole list, in one transaction

mm --start T-0031                             # into the lowest idle slot
mm --note T-0031 "root cause: old key still in the CI cache"
mm --finish T-0031 --outcome shipped --closing-note "rotated, cache cleared"

mm --block T-0032 --reason "waiting on design review"
mm --unblock T-0032                           # back to the top of Ready

mm --report --group-by outcome                # last week's closed work
mm --check                                    # confirm the directory is still clean
```

Notes worth internalizing:

- **`--add` appends to the bottom** of its section by default — a new item is
  not automatically more urgent than what's already queued. Use `--top` when it
  is.
- **`--add-many` is how a list gets captured**, and it is one transaction: a bad
  line means *nothing* is written, so there is never a half-added batch to
  reconcile. Each line is an item line with the box and the ID removed —
  `Fix the deploy script | prio:high | tags:infra,ci` — blank lines are skipped,
  a leading `- `, `* ` or `- [ ] ` is stripped so a pasted checklist just works,
  and the modifiers (`--prio`, `--tag`, `--section`…) are defaults that a
  line's own fields override. It refuses a line carrying a `[T-0042]` rather
  than renumbering it: IDs are allocated, and reusing one breaks I2. It takes no
  `--detail` switches — one body cannot belong to several items.
- **`--start` fails, not queues,** when every working slot is occupied (a WIP
  limit reached, exit code 4). The fix is to finish or pause something, or to
  raise the limit deliberately with `mm --wip N` — never by working around it.
- **`--remove` requires `--force`** and deletes permanently; to abandon work
  while keeping the record, use `--finish ID --outcome cancelled` instead.
- **`--pause` and `--finish` both preserve a working slot's notes** into the
  item's detail file before the slot resets — that file is the only place they
  survive.
- **`--edit --set KEY=VALUE` and `--unset KEY`** reach arbitrary fields,
  including ones `mm` itself doesn't parse. Unregistered fields are the
  format's extension point and must never be dropped — `mm` already handles
  this; don't hand-edit around it.
- **`--archive` moves data out of the checked set.** Archived months live in
  `done-YYYY.md` and their detail files in `details-YYYY/`, neither of which is
  validated: those IDs leave the pool, and a `--report` over an archived period
  needs `--include-archives` to find anything. It warns on every run, even
  under `--quiet`. Nothing is stranded and nothing is deleted, so the directory
  still passes `--check`; restoring means moving the line AND its detail file
  back, and doing half of it is reported. Dry-run it first, and don't reach for
  it until `done.md` is genuinely unwieldy.
- **`--migrate` is for an old directory, and runs before `--fix`.** It renames
  a pre-slot `working.md`, adds a missing `project`, and converts a
  `tags: [infra, ci]` flow sequence to `tags:infra,ci` — those three shapes and
  nothing else. It is safe to run twice, and it will not refuse over a
  violation it does not own, which `--fix` will.
- **`--tick` is the tickler, and it never runs itself.** A someday item can
  carry `tickler:` — a date (`2026-09-01`), a weekday (`mon@08:00`,
  `first-mon@08:00`) or a month day (`15@08:00`, `last@08:00`), set with
  `mm --add --section someday --tickler ...`. A bare date is one-shot: the item
  moves to Ready and the schedule is consumed. A recurring one keeps the item
  in Someday as a prototype and spawns a fresh Ready item each time it fires.
  Both stamp `tickled:`. Fire them with `mm --tick` from a cron entry, and
  dry-run it first — it reports what fired and what errored either way.
- **`--stats` measures flight, not slots.** Cycle time is `done:` minus
  `started:` in whole days, and the in-flight series counts an item from its
  started date to its done date whatever happened in between — a pause leaves
  no trace in the files. It defaults to all of history, and an item with no
  `started:` is counted as unmeasurable rather than guessed at.

## Scripting against it

Every operation accepts `--dry-run` (compute and report, write nothing — the
exit code and report match what a real run would produce, so it's a safe way
to check a command first), `--json` (one JSON object on stdout, success or
failure) and `--porcelain` (tab-separated, no header, stable field order —
prefer this over `--json` for a quick grep/cut, and over human output for
anything you intend to parse).

```bash
mm --list --porcelain --state backlog
# T-0001<TAB>backlog<TAB>Ready<TAB>high<TAB>writing<TAB>Write the quarterly report

mm --dry-run --start T-0031 --json
```

Exit codes:

| Code | Meaning |
|---|---|
| 0 | success |
| 1 | invariant violation (`--check` found problems, or a mutation was rejected before it was written) |
| 2 | usage error |
| 3 | not found (unknown ID, unresolvable or ambiguous directory, or `--next` with an empty `## Ready`) |
| 4 | precondition failed (WIP limit reached, wrong state, guard not satisfied) |
| 5 | concurrent modification (another writer changed the file since it was read) |
| 6 | I/O or environment failure |

## How the data is organized (just enough to reason about it)

- An item is in exactly one place: `backlog.md` (sections `## Ready`,
  `## Blocked`, `## Someday`), one `working.NN.md` file, or `done.md`. Moving
  it is what every mutating operation above does.
- **The number of `working.NN.md` files *is* the WIP limit** — there is no
  separate setting. `mm --wip N` is the only way to change it.
- Every item has a permanent ID (`T-0042` by default) that is never reused or
  renumbered, even after `--remove`.
- A `detail:` field points at a `details/<ID>.md` file for longer notes; it
  follows the item through every move.
- Unregistered `key:value` fields on an item line are legal and must survive
  every move untouched — they're the format's extension point.

This is deliberately incomplete. `mm` enforces the real rules and validates
before every write, so treat `mm --check` and `mm --help` as authoritative over
this summary, not the other way around.

## If `mm` is not available

Don't hand-edit the files to work around a missing CLI — a hand edit that
gets one of the ten invariants wrong (an ID reused, a WIP file miscounted, a
detail file's title left stale) produces a directory `mm --check` itself would
reject. Tell the user `mm` isn't on PATH and stop, unless they explicitly ask
for a manual edit and you can run `mm --check` afterward to confirm it's still
valid.
