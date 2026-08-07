# micro-manager — tooling specification

    Spec version: 1
    Date:         2026-07-29
    Status:       draft
    Depends on:   spec-file-format.md (spec version 1)

This document specifies the command line tooling that reads and writes
micro-manager directories. It is language-agnostic: it constrains behavior,
argument surface, error semantics, and the split between library and wrapper,
not the implementation language or its idioms.

`spec-file-format.md` defines the on-disk formats and the invariants I1–I10.
This document defines what a tool may do to them. Where the two disagree about
what a file may contain, the format spec wins; a tool that would have to violate
it is wrong, not the data.

---

## 1. Scope

In scope: the operation set, its argument surface, the library API contract,
transactional behavior, output formats, and exit codes.

Out of scope: implementation language, storage beyond the markdown files,
networking, synchronization between machines, and the UI's own design. The UI is
referenced only as a second consumer of the library (§2.1).

## 2. Architecture

### 2.1 One library, compiled into each front end

All domain logic MUST live in a **library**. Each front end is a separate
binary that **compiles the library in** — statically or dynamically linked, as
an in-process dependency. The library is not a service, not a daemon, and not
something a front end shells out to.

Two front ends are planned:

- the **command line wrapper**, this document's `mm`;
- the **UI service**, specified in `spec-gui.md`.

```
    +---------------------+     +---------------------+
    |   mm  (CLI binary)  |     |     UI service      |
    |  +---------------+  |     |  +---------------+  |
    |  |    library    |  |     |  |    library    |  |   same library,
    |  +-------+-------+  |     |  +-------+-------+  |   linked into both
    +----------|----------+     +----------|----------+
               |                           |
               +-------------+-------------+
                             |
                    +--------v---------+
                    |  markdown files  |
                    +------------------+
```

**The UI service MUST NOT invoke the CLI binary, spawn it as a subprocess, or
parse its output to perform operations.** It calls the same library functions
the CLI calls. `--json` (§9.2) exists for third-party automation, not as an
integration path for the UI: routing the UI through a process boundary would
make the CLI's argument surface a second, undocumented API, and every operation
would acquire the CLI's exit-code and text-formatting semantics as accidental
contract.

The two binaries share the files, not a process. Both may run at once, against
the same directory, and §7 is what keeps that safe.

### 2.2 The library is a guest in a process it does not own

Every constraint below follows from one fact: the library is compiled into a
host binary whose lifecycle, output streams, and threading model belong to
somebody else. A UI service cannot tolerate a dependency that prints to stdout
or calls `exit()`.

1. It MUST implement every operation in §5 with no dependency on the CLI.
2. It MUST NOT write to stdout or stderr. Diagnostics are returned as values.
   If it logs, it MUST log through a sink the host supplies, and MUST be usable
   with no sink at all.
3. It MUST NOT terminate the host process — no `exit`, no `abort`, no
   uncatchable failure path. Failures are typed errors (§6.3).
4. It MUST NOT read `argv`, environment variables, the working directory, or a
   terminal. Everything an operation needs arrives as a parameter. Resolving
   `MM_DIR`, expanding `~`, handling `$EDITOR`, prompting, and confirming are
   the host's job (§2.6).
5. It MUST NOT install signal handlers, alter locale, or mutate any other
   process-global state.
6. It MUST hold no global mutable state. Two `Store` instances over two
   directories MUST be independently usable in one process.
7. Every operation MUST be callable without a prior operation, other than
   opening a store.
8. It MUST expose validation (§8) as a first-class operation, so a UI can show
   problems inline without running a checker.

### 2.3 Concurrency

The CLI is short-lived and single-threaded. The UI service is neither, and the
library has to satisfy the stricter of the two.

1. Distinct `Store` instances MUST be safe to use concurrently from different
   threads, including instances over the *same* directory.
2. A single `Store` instance MUST be safe to use concurrently, or the
   implementation MUST document it as requiring external synchronization.
   Internal synchronization is RECOMMENDED: operations are short, file-bound,
   and not worth pushing onto every caller.
3. No operation may block indefinitely. Any lock (§7 rule 6) MUST have a bounded
   wait and surface `Concurrent` rather than hanging a UI thread.
4. Operations MUST NOT assume they are the only writer. The CLI, the UI, and a
   text editor may all touch a directory in the same second; §7 rule 5 is the
   mechanism that makes that detectable rather than silently destructive.

### 2.4 Change detection without a watcher

A UI displays state that a hand edit or a CLI run can invalidate at any moment.

1. The library MUST expose a cheap **fingerprint** of a directory — derived from
   the size and modification time of the files it owns — that changes when the
   directory changes. A UI polls it to decide whether to re-read. This MUST NOT
   require reading or parsing file bodies.
2. The library MUST be fully functional with no filesystem watcher. Watchers are
   unavailable or unreliable often enough — network mounts, containers, some
   sandboxes, platform differences — that requiring one would make correctness
   environment-dependent.
3. The library MAY additionally offer watcher-backed notification as an opt-in
   convenience. It MUST NOT be the only path to noticing a change, and the
   library MUST behave identically when it is switched off.
4. Fingerprint equality means "no need to re-read". It is not a correctness
   mechanism: staleness is caught at write time by §7 rule 5, not by polling.

### 2.5 Requirements on the CLI wrapper

The CLI MUST be thin: argument parsing, terminal interaction, rendering, exit
codes. It MUST NOT contain domain logic. The practical test — **any behavior
reachable from the CLI MUST be reachable from the library with the same
semantics, and no CLI-only behavior may exist.** If the CLI needs a rule the
library does not have, the rule belongs in the library, where the UI gets it
too.

### 2.6 What belongs where

"Front end" below means either host binary. Rows marked CLI-only have no UI
analogue; the rest apply to both.

| Concern | Library | Front end |
|---|---|---|
| Parsing and writing files | ✓ | |
| Invariant validation | ✓ | |
| Deciding a move is illegal | ✓ | |
| WIP-limit enforcement | ✓ | |
| Choosing the next free slot | ✓ | |
| Report period arithmetic | ✓ | |
| Parsing `--since 2026-07-01` into a date | | ✓ (CLI-only) |
| Opening `$EDITOR` on a detail file | | ✓ (CLI-only) |
| Asking "are you sure?" | | ✓ |
| Colour, tables, column widths | | ✓ (CLI-only) |
| Mapping an error to an exit code | | ✓ (CLI-only) |

## 3. Invocation model

### 3.1 The command

One executable. This document calls it `mm`; an implementation MAY choose
another name and MUST use it consistently.

### 3.2 Switches, not subcommands

The operation MUST be selected by a **switch**, not a positional subcommand:

```
mm --add "Fix the deploy script" --prio high
```

not `mm add "Fix the deploy script"`.

Exactly one operation switch MUST be present per invocation. Zero is a usage
error; two or more is a usage error naming both. This keeps the surface flat and
uniformly parseable, at the cost of a longer switch namespace — operation
switches and modifier switches share one space, so no modifier may reuse an
operation's name.

Each operation switch takes its primary subject as its value where one exists
(`--show T-0042`), and everything else arrives as modifiers.

### 3.3 Argument conventions

1. Long form (`--add`) MUST be supported for every switch. Short aliases are
   OPTIONAL; where present they MUST be listed in `--help`.
2. `--switch VALUE` and `--switch=VALUE` MUST both be accepted.
3. `--` ends switch parsing; everything after it is a positional value.
4. An ID argument MUST accept the full form of an ID in the directory's
   declared grammar — the default is `T-0042` (spec-file-format.md §3.3.2). An
   implementation SHOULD also accept the bare number (`42`, `0042`) and
   resolve it, since typing the prefix is friction the format imposes for
   machine reasons.
5. Repeated modifiers: last wins, except for those documented as accumulating
   (`--tag`).
6. An unknown switch is a usage error. A tool MUST NOT silently ignore one.

### 3.4 Global switches

Valid with every operation:

| Switch | Meaning |
|---|---|
| `--dir PATH` | Act on this micro-manager directory (§4). |
| `--json` | Emit the machine envelope (§9.2) instead of human output. |
| `--porcelain` | Emit stable line-oriented output (§9.3). |
| `--dry-run` | Compute the change, report it, write nothing. |
| `--yes` | Assume yes for confirmations. Never implies `--force`. |
| `--force` | Proceed with a guarded destructive action (§5.1.6). |
| `--quiet` | Suppress non-essential human output. Errors still print. |
| `--verbose` | Add detail to human output. |
| `--help` | Print usage. With an operation switch, print that operation's usage. With `--json`, print the capability list instead (§3.4.1). |
| `--version` | Print tool version and the format spec version it implements. |

`--json` and `--porcelain` are mutually exclusive.

#### 3.4.1 `--help --json` — the capability list

`--help` prints prose for a person. `--help --json` MUST instead emit the §9.2
envelope, whose `result` states what this build accepts:

```json
{
  "ok": true,
  "operation": "help",
  "result": {
    "version": "0.1.0",
    "formatSpec": "1",
    "operations": ["add", "add-many", "archive", "..."],
    "modifiers": ["age", "all", "before", "..."]
  },
  "changes": [], "warnings": [], "errors": []
}
```

This exists because §5.2 and §5.3 make most of the surface OPTIONAL: a caller
cannot assume an operation is present, and until now the only ways to find out
were to read a help page written for a human, or to run the operation and
interpret exit 2. Both work — `mm --help --OPERATION` exits 0 when the build
has it and 2 when it does not, which is a fine shell test — but neither is
structured, and a help text that grows a section or re-indents a line silently
changes what a machine believes about the build.

Rules:

- The lists MUST be derived from what the parser accepts, not from the prose.
  Two sources of truth for one surface is the drift this is meant to end.
- Names appear **without leading dashes**, so a caller compares strings rather
  than stripping punctuation.
- With an operation named — `mm --help --add-many --json` — `result` MUST also
  carry `about` (the operation) and `usage` (its page). An operation this build
  does not have is an unknown switch and fails as one (exit 2, §10), with the
  envelope carrying the error like any other failure.
- Fields MAY be appended to `result` and MUST NOT be removed or repurposed,
  the same promise §9.3 makes for porcelain columns.
- `--version` keeps its one-line human form; the same two values appear here,
  so one call answers both "what can you do" and "what are you".

There is no `--porcelain` form. Porcelain is a stream of records — items,
violations, hits — and a capability list is one document about the tool rather
than a set of rows about data.

`--dry-run` MUST exercise the full path including validation, and MUST report
exactly what would change. An operation that cannot be dry-run does not exist:
every mutation MUST support it.

### 3.5 Environment

| Variable | Effect |
|---|---|
| `MM_DIR` | Default directory to act on (§4, step 2). |
| `MM_REPORT_PERIOD` | Default period for `--report` (§5.1.11). |
| `NO_COLOR` | Suppress colour, per §9.1. |
| `EDITOR`, `VISUAL` | Editor for detail files. `VISUAL` wins where both are set. |

**Environment variables are read by the wrapper, never by the library**
(§2.2 rule 4).
The wrapper resolves each one into an explicit parameter before calling in.
`Store.report()` receives a fully resolved `Period` — not a token, not a
variable name, and with no notion that a default was applied. This is what keeps
the UI service from silently inheriting the shell configuration of whoever
started it, and it is why the same operation is reproducible from either front
end.

A switch always beats a variable. An empty variable is treated as unset. An
invalid value in any of these MUST be reported as a usage error naming the
variable, never ignored.

## 4. Directory resolution

An operation acts on exactly one micro-manager directory unless documented
otherwise. The tool MUST resolve it in this order, stopping at the first hit:

1. `--dir PATH`, used verbatim. If it is not a micro-manager directory — it
   fails the emptiness test of `spec-file-format.md` Appendix B — that is an
   error, not the start of a search: the user named a place, and silently
   searching elsewhere would act on a directory they did not name.
2. The `MM_DIR` environment variable, treated the same way. Read by the
   wrapper and passed to the library as a parameter, never read by the library
   itself (§3.5).
3. Search upward from the current directory to the filesystem root for a
   directory containing a micro-manager directory, then use it if exactly one
   is found there.
4. Search downward from the current directory, using the discovery rules in
   `spec-file-format.md` Appendix B, and use the result if exactly one is found.

Steps 3 and 4 are DISCOVERY, so both apply Appendix B's emptiness test: a
name-matching directory holding neither `backlog.md` nor `done.md` is not a
candidate at all. It cannot be resolved to, and it cannot make a resolution
ambiguous — which is the practical half of the rule, since a source tree that
happens to contain a directory of that name would otherwise force every command
in it to be answered with `--dir`.

If more than one candidate is found at the resolving step, the tool MUST fail
with a not-found error listing the candidates and their `project` names, rather
than guessing. Ambiguity is never resolved silently. Two recognized names in
ONE parent — `micro-manager` beside `.micro-manager` — reach this rule as a
matter of course, and `--check` reports them as the mistake they are
(`spec-file-format.md` Appendix B).

`--all`, where an operation documents support for it, applies the operation
across every directory discovered by step 4 — and therefore never across one
the emptiness test excluded. A checker run over a whole machine reports the
boards that are broken, not every directory that shares the name.

Where a front end has configured scan roots (`spec-gui.md` §9.5), `--all` MUST
use them instead of step 4, so that every front end reports the same set of
projects. The CLI reads them from the shared config file; the library still
receives them as explicit `DiscoveryOptions` (§2.2 rule 4).

Discovery MUST NOT descend into a directory it has matched. A micro-manager
directory nested inside another is undefined (`spec-file-format.md` Appendix B),
and pruning at the match keeps a large tree cheap to walk.

## 5. Operations

Each entry gives the switch, its modifiers, behavior, and the errors it can
raise. Error names refer to §6.3.

### 5.1 Required operations

A conforming implementation MUST provide all of these.

---

#### 5.1.1 `--init` — create a directory

```
mm --init [--dir PATH] --project NAME [--slots N] [--slot-width W]
             [--prefix P] [--description TEXT]
```

Creates a micro-manager directory: `backlog.md` with the three sections and
`next_id: T-0001`, `done.md` with no month groups, `N` working files
(default 1), and `details/` containing `_template.md`. `structure.md` SHOULD be
written too.

`--slot-width` sets the digit width for working files (default 2, per
format spec §5.2.1). `--slots` sets how many are created.

`--prefix P` claims the board's ID grammar at creation (format spec §3.3.2):
the fresh `backlog.md` declares `id_prefix: P` and its first `next_id` is
`P-0001` instead of `T-0001`. P MUST be one to four uppercase ASCII letters
(`A-Z`); anything else — including empty — is `InvalidArgument`. The prefix is
the one grammar decision a board cannot re-make cleanly later: every existing
ID carries it, and I2 forbids renumbering, which is why it belongs at `--init`
and nowhere else.

`--description TEXT` seeds the board description (§5.3.2): TEXT becomes the
first paragraph of the fresh `structure.md`, replacing the template's default
prose — a board can be born with its purpose stated. Supplying it makes the
SHOULD a MUST: the description lives in `structure.md`, so the file has to be
written.

> **Corrected.** This modifier was `--wip` in an earlier revision, which
> contradicted §3.2: `--wip N` is an operation (§5.2), and §3.2 requires
> operation and modifier switches to share one namespace so that no modifier may
> reuse an operation's name. A parser implementing §3.2 literally reads
> `mm --init --project P --wip 2` as two operations and refuses it. Renamed to
> `--slots` rather than making the meaning of `--wip` depend on which operation
> preceded it, which is exactly the context-sensitivity §3.2 exists to prevent.
>
> **The same collision remains in two places** and must be resolved the same way
> when those operations are implemented. `--note ID TEXT` (§5.2) collided with
> `--finish --note TEXT` (§5.1.10) and was resolved at §5.1.10 by renaming the
> modifier to `--closing-note`. **Still open:** `--detail ID` (§5.2) collides
> with `--add --detail` (§5.1.2).

Errors: `AlreadyExists` if the target already holds any of these files;
`InvalidArgument` if `--project` is empty or `--prefix` is not one to four
uppercase ASCII letters.

---

#### 5.1.2 `--add` — create a backlog item

```
mm --add TITLE [--top] [--section ready|blocked|someday]
             [--prio high|med|low] [--tag T]... [--blocked REASON]
             [--detail] [--detail-text TEXT] [--detail-file PATH]
             [--created DATE] [--tickler SCHEDULE]
```

Allocates the ID from `next_id`, writes the item line, increments `next_id`.

- **Position: appended to the bottom of the section by default.** `--top`
  inserts at the top instead. The two are mutually exclusive with each other and
  with nothing else.
- `--section` defaults to `ready`. `--section blocked` REQUIRES `--blocked`;
  supplying `--blocked` implies `--section blocked` if no section was named.
- `--created` defaults to today. It exists for backfilling.
- `--tickler SCHEDULE` schedules the item (a SCHEDULE expression,
  spec-file-format §3.3). It REQUIRES `--section someday` — I7 allows the field
  nowhere else — and the item's `created:` anchors a never-fired recurring
  schedule's first fire. The GUI composes this value from its Wake-up controls
  (spec-gui §5.6); the CLI accepts the same grammar, which is the one the
  library parses.
- `--tag` accumulates: `--tag infra --tag ci` produces `tags:infra,ci`.

Detail file, at most one of:

- `--detail` — create `details/<ID>.md` from `_template.md` with `id` and
  `title` filled in, and set the `detail:` field. The **CLI** then opens it in
  `$EDITOR` unless `--quiet` or `--no-edit`; the library only creates it.
- `--detail-text TEXT` — same, with TEXT as the body.
- `--detail-file PATH` — same, with the contents of PATH as the body. PATH is
  read, not linked or moved.

Returns the created item, including its assigned ID. The ID MUST be reported in
every output mode — a caller that just created an item needs its handle.

Errors: `InvalidArgument` (empty title, title containing `|`, malformed tag,
bad date, `--section blocked` without a reason), `Conflict` (`next_id` exhausted
at the declared width's cap — `T-9999` for the default grammar).

---

#### 5.1.3 `--list` — read items

```
mm --list [--section S] [--state backlog|working|done|all]
          [--prio P] [--tag T] [--blocked] [--limit N] [--all]
```

Lists items. Default state is `backlog`; `--state all` spans backlog, working
slots, and done. Filters combine with AND. Order MUST be the on-disk order —
`## Ready` order is meaningful (format spec §5.1) and a listing that re-sorts it
by default hides the user's own prioritization. `--sort` MAY be offered but MUST
default to on-disk order.

`--all` lists across every discovered directory, grouping by directory and
showing each `project` name.

Errors: none beyond resolution failures.

---

#### 5.1.4 `--show` — read one item

```
mm --show ID [--detail]
```

Prints one item: every field, its current state (which file it lives in, and
which slot if working), and — with `--detail` — the full detail file body.

`--show` MUST work regardless of where the item lives.

Errors: `NotFound`.

---

#### 5.1.5 `--edit` — update an item

```
mm --edit ID [--title TITLE] [--prio P] [--tag T]... [--untag T]...
             [--set KEY=VALUE]... [--unset KEY]...
             [--blocked REASON] [--detail|--detail-text|--detail-file]
```

Modifies fields in place, wherever the item lives. Position is not changed —
that is `--move`. State is not changed — that is `--start`/`--pause`/`--finish`.

- `--tag`/`--untag` add and remove individual tags rather than replacing the
  list; `--set tags=a,b` replaces it wholesale.
- `--set`/`--unset` reach any field, including unregistered ones (format spec
  §9). A tool MUST allow setting a key it does not know, and MUST validate the
  keys it does know.
- Changing `--title` on an item with a detail file MUST update that file's
  frontmatter `title` in the same transaction, or I9 breaks. This is the single
  most important coupling in the operation set.

Errors: `NotFound`, `InvalidArgument`, `Conflict` (e.g. `--unset` on a field
required in the item's current state, such as `done` on a closed item).

---

#### 5.1.6 `--remove` — delete an item

```
mm --remove ID --force
```

Deletes the item line outright. The ID is retired, not recycled: `next_id` is
NOT decremented (format spec I2).

**This operation MUST be guarded.** It requires `--force`, or an interactive
confirmation from the CLI, and MUST NOT be satisfied by `--yes` alone. The
reason is in the data model: the format keeps cancelled work in `done.md` with
`outcome:cancelled` precisely so that abandoning something leaves a record.
`--remove` is for a typo, a duplicate, an item added to the wrong directory —
not for work you decided against. Implementations SHOULD say so when refusing,
and point at `--finish ID --outcome cancelled`.

By default a detail file belonging to the removed item is left in place and
becomes an I9 orphan, which the checker will report. Implementations MUST
either delete it in the same transaction or report the orphan; silently leaving
an invalid directory is not conforming. RECOMMENDED: `--with-detail` deletes it,
and the default reports what was left behind.

Errors: `NotFound`, `PreconditionFailed` (no `--force`), `Conflict` (item is in
a working slot — pause or finish it first).

---

#### 5.1.7 `--move` — reposition a backlog item

```
mm --move ID (--position N | --top | --end | --before ID2 | --after ID2)
            [--section S]
```

Moves an item within `backlog.md`. Exactly one destination selector is required.

- `--position N` — 1-based index within the target section. `N` greater than the
  section length is an error, not a clamp; silently doing something adjacent to
  what was asked is worse than refusing.
- `--top`, `--end` — first or last in the section.
- `--before`/`--after ID2` — relative to another item, which MUST be in the same
  section after any `--section` is applied.
- `--section S` moves between sections. Combined with a position selector, the
  position is interpreted in the destination section. Moving into `blocked`
  REQUIRES a `blocked:` field to exist or be supplied via `--blocked`; moving
  out of `blocked` MUST drop it (I5). Moving out of `someday` MUST drop a
  `tickler:` field — the schedule is consumed, and the checker would otherwise
  reject the field outside `## Someday` (format spec I7); `tickled:` is kept,
  it is historical (§5.3.3).

Only backlog items can be moved: ordering is meaningless in `done.md` beyond its
month grouping, and working slots are interchangeable (format spec §5.2.1).
Moving an item between working slots is `--start --slot`, not `--move`.

Errors: `NotFound`, `Conflict` (item not in backlog), `InvalidArgument`
(position out of range, `--before` naming an item in another section).

---

#### 5.1.8 `--start` — backlog → working

```
mm --start ID [--slot NN]
```

Removes the item line from `backlog.md` and writes its fields into a working
file's frontmatter, setting `status: working` and `started` to today. Fields
transfer verbatim, including unregistered ones and `detail:` (format spec §5.2
requires identical lexical forms, so this is a copy, never a conversion). The
body sections are left empty except `## Task`, which SHOULD be seeded with the
title and a link to the detail file when one exists.

Slot selection: `--slot NN` names one explicitly and fails if it is occupied.
Without it, the **lowest-numbered idle slot** is used.

**If every working file is occupied, this MUST fail with `WipLimitReached`.**
The tool MUST NOT create a new working file to make room — that would silently
raise the WIP limit, which is the one thing the limit exists to prevent. The
error message MUST state the limit, what currently occupies the slots, and the
three ways forward: finish something, pause something, or raise the limit
deliberately with `--wip`.

Errors: `NotFound`, `Conflict` (item not in backlog), `WipLimitReached`,
`PreconditionFailed` (`--slot` names an occupied or nonexistent slot).

---

#### 5.1.9 `--pause` — working → backlog

```
mm --pause ID [--top] [--section S] [--keep-notes|--discard-notes]
```

The inverse of `--start`. Writes the item back as a backlog line preserving all
fields including `started`, and resets the slot to `status: idle` with every
item field `null`.

Subtasks in `## Plan` are discarded — they are scratch by design. `## Notes`
content is the risk: it exists nowhere else. The tool MUST NOT discard it
silently. Default behavior MUST be to preserve it by appending to the item's
detail file, creating that file if necessary; `--discard-notes` opts out
explicitly.

Position defaults to the top of `## Ready` — a paused item is usually the next
thing you will pick up, not the last. `--top`/`--section` override.

Errors: `NotFound`, `Conflict` (item is not in a working slot).

---

#### 5.1.10 `--finish` — → done

```
mm --finish ID [--outcome shipped|cancelled|obsolete] [--done DATE]
              [--closing-note TEXT]
```

Moves an item to `done.md`: box becomes `x`, `done` is set (default today),
`outcome` is set (default `shipped`), all other fields are preserved, and the
line is inserted at the **top** of the month group matching the `done` date,
creating that group if absent and placing it in newest-first order.

One field does not survive: a scheduled item carries `tickler:` nowhere but
`## Someday` (format spec I7), and `done.md` is not it, so finishing a
scheduled item drops the schedule — the user's way to retire a prototype.
`tickled:` is kept, it is historical (§5.3.3).

MUST work from a working slot (resetting it to idle) and from `backlog.md`
directly — closing something without ever starting it is normal, and
`--outcome cancelled` from the backlog is the supported way to abandon work.

`## Notes` from a working slot is handled as in `--pause`: preserved into the
detail file by default, since this is the last moment it exists.

`--closing-note TEXT` appends a closing note to the detail file, creating it if
needed.

> **Corrected.** This modifier was `--note` in an earlier revision, colliding
> with the `--note ID TEXT` operation of §5.2 in the same way `--wip` collided
> with `--init` (see §5.1.1). Resolved the same way and for the same reason: the
> operation keeps the short name — §5.2 calls it the highest-frequency write in
> daily use — and the modifier is renamed.

Errors: `NotFound`, `InvalidArgument` (bad outcome or date), `Conflict` (item
already in `done.md`).

---

#### 5.1.11 `--report` — weekly report

```
mm --report [--period TOKEN | --last-week | --this-week | --week YYYY-Www
             | --since DATE [--until DATE]]
            [--group-by outcome|tag|day|none] [--include-wip]
            [--include-backlog] [--include-archives] [--all]
```

Gathers completed items into a list. This is the operation people run at the end
of a week or the start of the next one, so the human output MUST be paste-ready
— a markdown list, no box drawing, no colour when redirected.

**Period tokens.** One vocabulary, accepted by `--period` and by the
`MM_REPORT_PERIOD` environment variable (§3.5):

| Token | Period | Support |
|---|---|---|
| `last-week` | the most recent **complete** ISO 8601 week, Monday–Sunday | MUST |
| `this-week` | current ISO 8601 week, Monday through today | MUST |
| `YYYY-Www` | a specific ISO 8601 week date (`WEEK`) | MUST |
| `last-N-days` | today and the N−1 days before it, e.g. `last-7-days` | MUST |
| `all` | every item in scope, no date filter | MUST |
| `last-month` | the most recent complete calendar month | SHOULD |
| `this-month` | current month through today | SHOULD |
| `YYYY-MM` | a specific calendar month | SHOULD |
| `today`, `yesterday` | one day | MAY |

`--last-week` and `--this-week` are aliases for the corresponding tokens, kept
because they read better in a shell.

**Precedence**, first match wins:

1. `--since`/`--until` — an explicit inclusive date range. `--until` defaults to
   today.
2. `--week YYYY-Www`.
3. `--period TOKEN`, `--last-week`, or `--this-week`.
4. `MM_REPORT_PERIOD`, if set and non-empty.
5. **Default: `last-week`** — the most recent complete ISO week.

The built-in default is `last-week` rather than the current week because the
report is most often written *about* a week that has finished, and a period that
is still accumulating produces a different answer every time it is run. A report
over a closed period is reproducible; one over an open period is not.

An unparseable `MM_REPORT_PERIOD` MUST be a usage error naming the variable and
its value. The tool MUST NOT silently fall back to the default — a typo in a
shell profile would otherwise quietly change every report the user ever runs.

Selection reads `done.md` and matches each item's `done` field against the
period. Every date, time and timestamp this tool reads or writes is ISO 8601 in
the extended format (format spec §3.3.1), including in `--json` output and in the
files of `spec-gui.md` §9–§10. Because `done` is a date with no time, the period
is date-granular; an implementation MUST NOT invent finer resolution.

`--since` and `--until` accept a `DATE` and nothing else: no locale-dependent
form, no relative expression. `2026-07-01` is a period bound; `01/07/2026` is a
usage error, because it means two different days depending on who typed it.

Every output mode MUST state the resolved period and where it came from
(switch, environment, or default). A report whose period is invisible is a
report you cannot check.

Content:

- Completed items in the period, newest first. All outcomes are included and
  each is labelled — a week's cancellations are part of the week.
- `--group-by` defaults to `none` (a flat list). `outcome` groups shipped /
  cancelled / obsolete; `tag` repeats an item under each of its tags and lists
  untagged items last; `day` groups by `done` date.
- `--include-wip` appends what is currently in the working slots, marked as
  in progress. RECOMMENDED for a standup-style report.
- `--include-backlog` appends the top few `## Ready` items as "next".
- `--include-archives` also reads `done-YYYY.md` files. Without it, a report
  covering an archived period silently returns nothing, so the tool MUST warn
  when the requested period predates the oldest month group present in
  `done.md`. An archived item's detail file is found through its own `detail`
  field, which names `details-YYYY/` (format spec §5.6); a reader MUST follow
  the field rather than assume `details/`.
- `--all` reports across every discovered directory, grouped by `project`.

An item's detail file is not inlined; the report SHOULD reference it.

Errors: `InvalidArgument` (unparseable period from any source, `--since` after
`--until`, `last-N-days` with N below 1).

---

#### 5.1.12 `--check` — validate

```
mm --check [--all]
```

Runs every invariant I1–I10 and reports violations as `file:line: message`,
sorted by path then numeric line. Exits 1 if any directory has a violation.

`--all` checks what discovery found, which excludes a name-matching directory
holding neither `backlog.md` nor `done.md` (`spec-file-format.md` Appendix B).
A directory named explicitly is checked whatever it holds: failing the same
test there is an error, since the user asked about that directory.

`--check` MUST also report a **sibling collision** — two recognized names in
one parent directory, `spec-file-format.md` Appendix B — against each colliding
directory, so that checking either one reports it. It is not one of I1–I10 and
does not come from the library's validator: a collision is a property of the
parent, and per-directory validation must not depend on where a board sits.
The finding counts toward the exit code like any other.

This MUST be the same validation code the mutating operations run before
committing (§8). Two implementations of the invariants will diverge.

Errors: none; violations are results, not errors.

### 5.2 Recommended operations

SHOULD be provided. Each is derivable from the required set but is common enough
that its absence pushes users back to editing by hand.

| Switch | Behavior |
|---|---|
| `--block ID --reason TEXT` | Move to `## Blocked` and set `blocked:`. Sugar over `--move --section blocked`. |
| `--unblock ID [--top]` | Move to `## Ready` and drop `blocked:`. |
| `--note ID TEXT` | Append a dated entry to the working file's `## Notes`, or to the detail file if the item is not in a slot. The highest-frequency write in daily use. |
| `--wip N` | Set the WIP limit by creating or deleting working files. Deleting MUST refuse unless the highest-numbered files are idle (I10), and MUST NOT renumber occupied slots. |
| `--status` | One screen: what is in each slot, WIP `n/N`, counts by section, oldest untouched Ready item, and — when set — the board description (§5.3.2). The JSON envelope carries it in `directory.description` (§9.2); human output MAY show it. |
| `--next` | Print the top of `## Ready` — the thing to start next. Exits non-zero if empty. |
| `--search QUERY` | Substring or regex match over titles, tags, and detail bodies; reports state and location per hit. |
| `--find` | List discovered micro-manager directories with their `project` names. The discovery step, exposed — so a name-matching directory that fails Appendix B's emptiness test does not appear. |
| `--detail ID` | Open, create, or print the detail file for an item. |
| `--subtask ID TEXT` / `--subtask-done ID N` | Append to and tick off `## Plan` entries in a working slot. |
| `--add-many [FILE]` | Add many items in ONE transaction, one per input line (§5.2.1). Sugar over repeated `--add`, except for the part that is not sugar: the whole batch commits or none of it does. |

#### 5.2.1 `--add-many` in detail

```
mm --add-many [FILE] [--top] [--section ready|blocked|someday]
              [--prio high|med|low] [--tag T]... [--blocked REASON]
              [--created DATE] [--tickler SCHEDULE]
```

Reads a list of items and adds them all, in the order given, as one
transaction. Input comes from `FILE`, or from **stdin** when `FILE` is absent
or is `-`. Capture is what this exists for: a meeting, a paste from a chat, a
`grep` over a codebase's `TODO`s — a list that already exists, going onto the
board without one invocation per line.

Reading that input is the **host's** job, not the library's: §2.2 rule 4 keeps
files, stdin and terminals out of the library, so the CLI hands it a list of
requests. The line grammar below is the library's, exposed as a parse function
(§6.2) — a GUI with a paste box needs the same grammar, and a second
implementation of it would drift.

**One transaction, not N.** This is the whole reason the operation exists
rather than being left to a shell loop. All the items are built, all their IDs
are allocated, the result is validated once, and then `backlog.md` is written
once (§7). A malformed line anywhere means **nothing** is written, and the
error names the line. A loop over `--add` gives the opposite: eleven items
added and the twelfth rejected, with no record of where the run stopped.

**The line grammar** is the item line of the format spec (§4.2) with the box
and the ID removed — the ID is allocated, never supplied:

```
TITLE
TITLE | prio:high | tags:infra,ci
- TITLE | prio:low
```

- A **blank line** is skipped. Trailing whitespace is trimmed.
- A leading markdown bullet — `- `, `* `, or `- [ ] ` — is stripped, so a
  checklist pasted out of a document is valid input as it stands.
- The fields are the format's `key:value` pairs, separated by ` | `
  (space pipe space), and MUST be exactly the set `--add` can set:
  `prio`, `tags`, `refs`, `created`, `blocked`, `tickler`, and unregistered
  keys, which are preserved verbatim as everywhere else. A registered field
  `--add` cannot set — `detail`, `started`, `done`, `outcome`, `tickled` — is a
  usage error naming the line, not a silently dropped value.
- `detail:` is refused for a reason worth stating: the path must match the
  item's own ID (I8), and the ID does not exist until this operation allocates
  it. A caller who wants detail files adds the items and then writes them.
- A line that carries a **bracketed ID** (`- [ ] [T-0042] …`) is a usage error.
  It is what pasting existing items looks like, and honouring the ID would
  reuse a retired number (I2); the fix is to delete the ID, and saying so is
  more useful than silently renumbering. What counts as an ID here is the
  SHAPE — `[LETTERS-DIGITS]` in any directory's grammar, not just this one's —
  so `[WIP] Ship the thing` stays an ordinary title.

**Modifiers are defaults; a line's own fields win.** `--prio high` sets the
priority of every line that does not name one. `--section` chooses the section
for the batch, and a line's `blocked:` reason moves that line to `## Blocked`
exactly as it would under `--add` — so one run may write into two sections.

A line's fields mean **exactly** what the same fields mean to `--add`, which
settles the one case where the two could plausibly differ: `tickler:` does NOT
imply `--section someday` the way `blocked:` implies `## Blocked`, because
`--add` requires the section to be named (§5.1.2) and a bulk line is not a
place to invent a second rule. A schedule on a line in any other section is the
same error `--add` raises.

**Order is the input's order.** Appended to the bottom of the section by
default; `--top` inserts the batch at the top, still in the input's order. A
`--top` that reversed the batch would be a surprise nobody wants: the list
someone pasted is a list they had already put in an order.

The detail-file switches of §5.1.2 (`--detail`, `--detail-text`,
`--detail-file`) are NOT accepted: one body cannot belong to N items, and
opening N editors is not a thing to do to someone.

Output MUST report every assigned ID, in every output mode (§9.1) — a caller
that just created twelve items needs twelve handles. `--json`'s `result` is the
array of created items; `--porcelain` emits one item per line.

Errors: `InvalidArgument` (any of the line rules above, an empty input, a
detail switch), `Conflict` (`next_id` exhausted part-way through the batch —
reported against the line that would have exceeded the cap, with nothing
written), plus everything `--add` can raise.

### 5.3 Optional operations

MAY be provided.

| Switch | Behavior |
|---|---|
| `--archive [--before YYYY-MM \| --age DAYS]` | Move old month groups from `done.md` to `done-YYYY.md`, and their detail files to `details-YYYY/` (format spec §5.6). MUST warn that archived items leave the ID pool and stop being covered by I1/I2 (format spec §10.5). |
| `--migrate` | Bring a directory to the current format version: rename `working.md` → `working.01.md`, add a missing `project`, normalize `tags` from a YAML flow sequence to a `TAGLIST`. MUST be dry-runnable and MUST report every change. |
| `--stats [--since DATE]` | Throughput, cycle time from `started` to `done`, WIP over time, tag distribution. |
| `--export [--format json\|csv]` | Whole-directory dump for external tooling. |
| `--top-up` | Interactive triage over `## Someday`, promoting items to `## Ready`. |
| `--describe TEXT` | Write or replace the first paragraph of `structure.md` — the board description (§5.3.2). |
| `--tick [--dry-run]` | Run the tickler once: evaluate every `## Someday` item's `tickler:` schedule against today and fire the due ones — one-shot items move to `## Ready` with the schedule consumed; recurring items are prototypes that spawn a new Ready item on each fire (§5.3.3). MUST report what fired and what errored. |

#### 5.3.1 `--archive` in detail

The only optional operation that moves data out of the validated set, so what
it does is normative even though providing it is not.

**The cutoff is month-granular**, because a month group is the finest grain
`done.md` records (format spec §5.3). Two ways to express it, mutually
exclusive:

- `--before YYYY-MM` archives every group older than that month; the named
  month stays. A day component, if one is given, is ignored rather than
  refused. Absent both switches, the cutoff is the current month, so a bare
  `--archive` rolls up everything before the month the board is living in.
- `--age DAYS` states the same cutoff as a policy: a month group is archived
  once `DAYS` days have passed since its last day. `--age 0` archives every
  group whose month is complete; `--age 30` keeps each month for a further
  thirty days. This is the form a scheduled run uses, because it does not have
  to be edited every month.

**Detail files move with their items** and the archived `detail` fields are
rewritten to `details-YYYY/<ID>.md` (format spec §5.6 rule 3). An
implementation that moves one without the other is not conforming: leaving the
file in `details/` strands it as an I9 orphan in a directory that was clean,
and rewriting the field without moving the file writes an archive that points
at nothing. The write ordering of §7 rule 4 covers both halves — the archive
file and `details-YYYY/` are written before `done.md` and `details/` give
anything up, so an interruption leaves a duplicate to re-run over rather than a
hole.

**`next_id` is not touched** (format spec §5.6 rule 5), and neither is any
archive that already exists beyond the group being merged into it.

**Nothing archives on its own initiative.** A tool MAY run this on a schedule,
but only from configuration that names the policy — the `DAYS` of `--age` —
and MUST report what it moved. The default everywhere is manual: an operation
whose effect is that the checker stops seeing part of the record is not one to
perform quietly.

Errors: `InvalidArgument` (a month component outside `01`–`12`, `DAYS` below
zero, or both switches given at once).

#### 5.3.2 `--describe` in detail

The board description is the first paragraph of `structure.md` — the format's
human-documentation file (spec-file-format.md §5.5: free prose, optional, never
validated). Because the file is deliberately unstructured, "first paragraph" is
a convention this operation defines and `--status` reads:

> The first paragraph is the first run of consecutive non-blank lines after
> any leading title heading (a line beginning with `#`), skipping a leading
> frontmatter block (format spec §4.1) and blank lines. An absent
> `structure.md`, or a file with no such run, is "no description" — a valid
> state, never an error.

`--describe TEXT` makes TEXT that paragraph:

- **`structure.md` present** — the first paragraph is replaced with TEXT; every
  other line of the file is preserved verbatim. The write is a line splice over
  the retained original (§7), so TEXT equal to the current paragraph writes
  nothing.
- **`structure.md` absent** — a minimal one is created: the standard
  frontmatter (`doc: structure`, `version: 1`, `updated:` today), a single
  leading title heading, and TEXT as its first paragraph.
- **Empty TEXT** clears the description: the paragraph is removed, the rest of
  the file untouched, and reading the result is "no description" again.

The operation is a mutation like any other — it runs the §7 transaction
machinery, so it accepts `--dry-run`, detects concurrent modification, and
writes atomically. Since `structure.md` is outside I1–I10, pre-commit
validation (§8) cannot reject it; that is also why the operation exists: a
front end that must never touch board files itself (spec-pi-mm-plugin.md §8.1)
routes every description write here, its one sanctioned path to this file.

Errors: none specific to the value — free prose cannot be invalid.
`Concurrent` and `Io` apply as for any write (§7).

#### 5.3.3 `--tick` in detail

Runs the tickler once: every `## Someday` item carrying `tickler:` (format
spec §6) is evaluated against today, and the due ones fire. Firing is a
mutation like any other — it accepts `--dry-run` and runs the §7 transaction
machinery. The default is manual, like every operation; a scheduled run is
the cron/systemd surface (`mm --tick --dir …` from a timer, with
`Persistent=true` so a missed run fires on boot).

**Two kinds of fire**, a property of the schedule value, not a separate flag
(format spec §3.3 `SCHEDULE`):

- **One-shot** (`tickler:2026-09-01`): the item moves to `## Ready`, its
  `tickler:` field is dropped, and `tickled:<today>` is stamped. The schedule
  is consumed; the item is an ordinary ready item from here on.
- **Recurring** (`mon@08:00`, `first-mon@08:00`, `15@08:00`, `last@08:00`):
  the item is a **prototype**. A new item is added to `## Ready` — a fresh ID
  (`next_id` bumps, format spec I2), the prototype's title, `prio`, and
  `tags`, and `created:<today>` — while the prototype stays in `## Someday`
  with `tickler` intact and `tickled:<today>` stamped, ready for its next
  fire.

**A spawn copies title, prio, and tags — nothing else.** In particular it
carries no `detail:`: format spec I9 claims every detail file exactly once, so
a spawned copy pointing at the prototype's file would abort the transaction on
validation. The prototype keeps its own detail; the spawned copy starts bare.

**Both fires live entirely in `backlog.md`** — Someday and Ready are the same
file, so a fire is a single-file transaction, the simplest shape the envelope
has. Nothing in a fire touches working files, `done.md`, or `details/`.

**The move out of Someday clears `tickler`.** A fire's one-shot path is
exactly the §5.1.7 rule — moving an item out of `## Someday` drops `tickler:`
and keeps `tickled:` — plus `tickled:<today>`. Without the rule, dragging a
scheduled someday item to Ready would leave a field the checker rejects
(format spec I7: `tickler` is Someday-only) and the move would die with
`InvariantViolation` for no visible reason.

**Due test.** Let `last` be `tickled` (absent for a never-fired item):

- one-shot: due iff `fire-date > last`; an absent `last` means the date has
  arrived. A backdated one-shot — written down after its date — fires on the
  next run: "overdue, fire now" is the least surprising reading.
- recurring: due iff `next(last ?? created) <= today`, where `next(after)` is
  the smallest fire instant **strictly after** `after`. A `mon@08:00`
  prototype written on a Tuesday fires the following Monday, never the Monday
  that already passed — `created` anchors the first fire (format spec §5.1).
  A runner that missed a week still catches up: the strictly-after rule keeps
  `next(last) <= today` true across the gap.

**`tick` is date-granular like every operation.** The caller passes today — a
daily cron and a minute-clock UI both work. A schedule's `@HH:MM` is
evaluated inside the library's calendar math (format spec §10.1: wall-clock in
the evaluating process's zone), but it never leaves the expression, and
nothing in the data records a time.

**Idempotent across overlapping runners.** A UI service goroutine and a cron
entry can tick one board at once. The `tickled` stamp is the primary guard:
after a fire, `next(tickled)` is in the future, and every v1 period is at
least a day, so a schedule cannot fire twice in one day. A runner whose read
went stale before the winner wrote fails pre-commit validation with
`Concurrent` (§7 rule 5) — reported as a per-item error, and a failing item
never aborts the run. A consumed one-shot has no `tickler` left to
re-evaluate; the only failure mode left is two processes moving the same
line, which rule 5 resolves.

**Cadence is free.** Any interval works and a missed run catches up; the
effective firing granularity is the cadence of the least frequent runner, not
a property of the data.

Errors: a malformed `tickler:` value (hand-edited garbage) is reported as a
per-item error and the run continues — never fatal. `Concurrent` and `Io`
apply as for any write (§7).

## 6. Library API

Notation is pseudo-code: `name(params) -> Result<T, Error>`. Implementations map
this to their own idioms (exceptions, result types, error returns) provided the
semantics survive. Field names are normative; parameter passing style is not.

### 6.1 Types

```
Item {
  id            ID              # "T-0042"
  title         string
  state         Backlog | Working | Done
  section       Ready | Blocked | Someday | null    # backlog only
  slot          int | null                          # working only
  position      int | null                          # 1-based, backlog only
  prio          "high" | "med" | "low" | null
  tags          [string]
  detail        path | null
  created       date | null
  started       date | null
  done          date | null
  outcome       "shipped" | "cancelled" | "obsolete" | null
  blocked       string | null
  tickler       string | null   # SCHEDULE, Someday only (format spec §6)
  tickled       date | null     # last tickler fire (format spec §6)
  extra         map<string,string>   # unregistered fields, preserved verbatim
  source        Location             # file + line, for diagnostics
}

Slot        { number int, width int, occupied bool, item Item|null }
Directory   { path, project string, wipLimit int, wipUsed int, nextId ID,
              description string | null   # first paragraph of structure.md (§5.3.2); null when none }
Violation   { file path, line int|null, invariant string, message string }

DiscoveryOptions {
  roots           [path]    # one or more base directories to scan down from
  maxDepth        int       # levels below each root; default 6, 0 = unlimited
  followSymlinks  bool      # default false
  includeHidden   bool      # default true — .micro-manager must be findable
  excludes        [string]  # directory-name globs pruned during the walk
  maxResults      int       # default 500
  timeoutMs       int       # default 5000
}
DiscoveryResult {
  directories  [Directory]
  partial      bool         # a limit or timeout was hit
  skipped      int          # unreadable directories
  scannedAt    TIMESTAMP    # ISO 8601, format spec §3.3.1
}
Change      { kind Created|Updated|Moved|Deleted, id ID, before, after, file }

Schedule    — immutable value type (format spec §3.3 SCHEDULE)
  parse(string)          -> Schedule    # InvalidArgument on a malformed value
  String()               -> string      # canonical form
  isOneShot()            -> bool        # a bare DATE; the other shapes recur
  fireDate()             -> DATE|null   # a one-shot's date; null when recurring
  next(after DATE)       -> DATE|null   # smallest fire instant strictly after;
                                        # null when it will never fire again
Tickler     { id ID, schedule string, last DATE|null, next DATE|null }
                                        # read-only; last = tickled; next =
                                        # Schedule.next(last ?? created) against
                                        # the caller's "now" (§5.3.3)
TickResult  { fired [FiredTickler], errors [TickError] }
FiredTickler { id ID, kind move|spawn, spawned ID|null, tickled DATE }
TickError   { id ID, error string }
```

`extra` is load-bearing: format spec §9 makes unregistered fields the extension
point, and a library that drops them on a move silently destroys data it does
not understand.

### 6.2 Operations

```
open(path)                      -> Store
Store.directory()               -> Directory
Store.fingerprint()             -> Fingerprint     # §2.4, cheap staleness poll
Store.list(Filter)              -> [Item]
Store.get(ID)                   -> Item
Store.add(AddRequest)           -> Item
Store.addMany([AddRequest])     -> [Item]          # --add-many (§5.2.1), ONE transaction
Store.parseAddLine(string)      -> AddRequest      # the §5.2.1 line grammar
Store.update(ID, UpdateRequest) -> Item
Store.remove(ID, RemoveOptions) -> Change
Store.move(ID, Destination)     -> Item
Store.start(ID, slot?)          -> Item
Store.pause(ID, PauseOptions)   -> Item
Store.finish(ID, FinishOptions) -> Item
Store.report(Period, ReportOptions) -> Report
Store.validate()                -> [Violation]
Store.setWipLimit(int)          -> Directory
Store.setDescription(text)      -> Directory   # --describe (§5.3.2)
Store.ticklers(now DATE)        -> [Tickler]   # read-only (§5.3.3)
Store.tick(now DATE, dryRun bool) -> TickResult  # --tick (§5.3.3)
discover(DiscoveryOptions)      -> DiscoveryResult
```

Every mutating operation MUST accept a dry-run flag and return the `Change` set
it would produce without writing. Every mutating operation MUST return the
resulting state, not void — a UI needs to re-render without re-reading.

### 6.3 Errors

A typed taxonomy. Implementations MUST distinguish these; the CLI maps them to
exit codes (§10) and a UI maps them to messages.

| Error | Raised when |
|---|---|
| `NotFound` | ID, slot, or directory does not exist. |
| `Ambiguous` | Directory resolution matched more than one candidate. |
| `InvalidArgument` | A value fails the format spec: bad date, unknown prio, pipe in a title, malformed tag. |
| `Conflict` | The operation contradicts the item's state: starting a done item, moving a working item, finishing twice. |
| `WipLimitReached` | `--start` with every slot occupied. Distinct from `Conflict` because it is the one error with a routine, expected remedy. |
| `PreconditionFailed` | A guard was not satisfied: `--remove` without `--force`, `--slot` on an occupied slot. |
| `InvariantViolation` | The requested change would produce a directory that fails I1–I10. Carries the `Violation` list. |
| `Concurrent` | The directory changed underneath the operation (§7). |
| `Io` | Filesystem or permission failure. |

Every error MUST carry a message naming the item or file involved. `Conflict`
and `WipLimitReached` SHOULD name the remedy.

## 7. Transactions, atomicity, concurrency

An operation is a transaction over one directory. It MUST be all-or-nothing:
`--start` touches `backlog.md` and a working file, `--edit --title` touches an
item's file and possibly a detail file, and a partial application of either
leaves the directory invalid.

Required behavior:

1. **Read, modify in memory, validate, then write.** Never mutate a file
   incrementally.
2. **Validate before committing.** If the resulting model violates any
   invariant, abort with `InvariantViolation` and write nothing (§8).
3. **Write atomically.** Each file is written to a temporary file in the same
   directory and renamed over the target. A crash MUST leave either the old file
   or the new one, never a truncated one.
4. **Order writes so a crash between them is recoverable.** Where a multi-file
   transaction cannot be made atomic, order the writes so the surviving state
   fails validation loudly rather than losing an item. For `--start`: write the
   working file first, then remove the line from `backlog.md`. A crash between
   them duplicates the item — which I1 catches — instead of destroying it.
5. **Detect concurrent modification.** Record each file's size and modification
   time when read; if they differ at write time, abort with `Concurrent`. This
   is cheap and catches the realistic case: the user editing by hand in another
   window while a UI has the directory open.
6. **Advisory locking is RECOMMENDED.** An implementation MAY take an exclusive
   lock for the duration of a mutation. It MUST NOT require the lock to read,
   and MUST NOT leave a stale lock that blocks a later run — the files must stay
   editable by hand with no tool present, which is the point of the format.

Never-do list:

- MUST NOT reorder `## Ready` as a side effect of an unrelated operation.
- MUST NOT renumber IDs or decrement `next_id`.
- MUST NOT drop unregistered fields or unknown frontmatter keys.
- MUST NOT rewrite files it did not need to change. A no-op operation writes
  nothing, so a directory under version control produces no diff.
- MUST NOT reformat untouched lines. Whitespace churn across a file hides the
  one line that actually changed.

Writers MAY normalize field order to the canonical order (format spec §6.1) on
lines they are already rewriting.

`updated:` in a file's frontmatter, where present, SHOULD be set to today by any
operation that writes that file.

## 8. Validation

Two levels:

- **Pre-commit validation** — every mutation validates the resulting model
  before writing. This is not optional: it is what makes it impossible for the
  tool to produce a directory its own checker rejects.
- **On-demand validation** — `--check` and `Store.validate()`.

Both MUST use the same implementation of I1–I10.

Reading is deliberately more permissive than writing. A tool MUST be able to
open, list, and report on a directory that already has violations — refusing to
read a broken directory removes the tool exactly when it is needed. Mutations on
a directory with pre-existing violations MUST either fix or preserve them, and
MUST NOT be blocked by a violation unrelated to the item being touched.

## 9. Output

### 9.1 Human output

The default. Optimized for reading in a terminal and pasting into a message.
Colour MUST be suppressed when stdout is not a terminal, and when `NO_COLOR` is
set. Nothing essential may be conveyed by colour alone.

Every mutating operation MUST report what changed, including the assigned ID for
`--add`. Silence on success is wrong here: the ID is the handle for every
subsequent command.

### 9.2 `--json`

One JSON object on stdout, nothing else — no progress text, no warnings mixed in
(those go to stderr).

```json
{
  "ok": true,
  "operation": "add",
  "directory": { "path": "...", "project": "Sample One", "description": "A small-file todo system." },
  "result": { },
  "changes": [ { "kind": "created", "id": "T-0004", "file": "backlog.md" } ],
  "warnings": [],
  "errors": []
}
```

`directory.description` is the board description — the first paragraph of
`structure.md` (§5.3.2). It is omitted when the board has no description; that
absence is a valid state, never an error.

On failure, `ok` is false, `errors` is non-empty, and each error carries `code`
(a §6.3 name), `message`, and where applicable `id` and `file`.

The envelope MUST be present in both cases — a caller should never have to
distinguish "JSON error object" from "crash text".

`--help --json` uses the same envelope, with the capability list as its
`result` (§3.4.1). It is the one meta-answer that does: `--version` stays a
line of text, because a version string is already machine-readable and the
same two values ride along in the capability list for anyone who wants them
structured.

### 9.3 `--porcelain`

Line-oriented, tab-separated, stable across versions, no header. Intended for
shell pipelines. Fields per operation MUST be documented and MUST only ever be
appended to.

## 10. Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Invariant violation: `--check` found problems, or a mutation was rejected by pre-commit validation. |
| 2 | Usage error: unknown switch, no operation, two operations, missing required value. |
| 3 | Not found: unknown ID, unresolvable or ambiguous directory. |
| 4 | Precondition failed: WIP limit reached, wrong state, guard not satisfied. |
| 5 | Concurrent modification. |
| 6 | I/O or environment failure. |

`--dry-run` returns the code the real run would have returned, so it can gate a
script.

## 11. Conformance

**Minimally conforming**: §2 architecture, §3 invocation, §4 resolution, all of
§5.1, §6 (as adapted to the language), §7, §8, §10, and human output. May omit
`--json`, `--porcelain`, and §5.2/§5.3.

**Fully conforming**: additionally all of §5.2 and both machine output modes.

A conforming implementation MUST NOT extend the operation switch namespace with
anything that changes the meaning of a switch defined here, and MUST NOT add an
operation that can produce a directory failing I1–I10.

## 12. Worked examples

```bash
# capture something, with the long description in one go
mm --add "Fix the deploy script" --prio high --tag infra --tag ci \
   --detail-file ~/notes/deploy-incident.md
# -> T-0042 added to Ready (bottom)

# something urgent: top of the list
mm --add "Rotate the leaked token" --prio high --top

# reorder after triage
mm --move T-0042 --position 3
mm --move T-0031 --section someday

# start work; fails cleanly at the limit
mm --start T-0042
# -> error: WIP limit reached (3/3)
#      slot 01  T-0018  Migrate the build cache
#      slot 02  T-0027  Fix flaky auth test
#      slot 03  T-0031  Rewrite the deploy docs
#    finish one, pause one, or raise the limit with --wip 4

mm --pause T-0031          # notes preserved into details/T-0031.md
mm --start T-0042          # takes slot 03

# during the work
mm --note T-0042 "cache key was stale; see build log 4471"

# close it out
mm --finish T-0042 --outcome shipped

# Monday morning: last week, the default
mm --report --group-by outcome

# Friday afternoon, about the week that is ending
mm --report --this-week --include-wip

# or change the default for this shell
export MM_REPORT_PERIOD=this-week

# validate before committing
mm --check --all

# a fresh board with its own ID grammar (format spec §3.3.2)
mm --init --project Garden --prefix G --description "Gardening board"

# a board's one-line purpose; --status and the plugin read it back
mm --describe "Personal board: everything in flight, in one place."

# Monday morning: see what the tickler would fire, then fire it
mm --tick --dry-run
# -> would fire T-0043 (move to Ready), T-0044 (spawn T-0045, recurring)
mm --tick
```

---

## Appendix A: operations, files touched, invariants at risk

| Operation | Writes | Can break |
|---|---|---|
| `--init` | all | I10 (slot width/contiguity) |
| `--add` | `backlog.md`, `details/` | I2 (`next_id`), I5 (blocked), I7, I8 |
| `--edit` | item's file, `details/` | I7, **I9 (title drift)** |
| `--remove` | `backlog.md` | **I9 (orphaned detail)**, I2 if `next_id` touched |
| `--move` | `backlog.md` | I5 (blocked field on section change) |
| `--start` | `backlog.md`, working file | **I1 (duplicate)**, I4, I10 |
| `--pause` | working file, `backlog.md`, `details/` | **I1**, I4, I5 |
| `--finish` | working file, `done.md`, `details/` | **I1**, I3, I4, I6 |
| `--wip` | working files | **I10** |
| `--describe` | `structure.md` | — |
| `--tick` | `backlog.md` | I2 (a spawn bumps `next_id`) |
| `--archive` | `done.md`, `done-YYYY.md`, `details/`, `details-YYYY/` | I1, I2 (items leave the pool); **I9 (a detail file left behind in `details/`)** |
| `--report`, `--list`, `--show`, `--check` | nothing | — |

Bold entries are the ones where a partial write loses data rather than producing
a detectable inconsistency. They warrant the write ordering in §7 rule 4.

## Appendix B: switch index

Operation switches, one per invocation:

```
required     --init --add --list --show --edit --remove --move
             --start --pause --finish --report --check
recommended  --block --unblock --note --wip --status --next --search
             --find --detail --subtask --subtask-done
optional     --archive --describe --migrate --stats --export --top-up --tick
```

Global modifiers, valid everywhere:

```
--dir --json --porcelain --dry-run --yes --force --quiet --verbose
--help --version
```

Operation-specific modifiers:

```
--top --end --position --before --after --section
--prio --tag --untag --set --unset --title --blocked --reason
--created --started --done --outcome --reason --closing-note
--detail --detail-text --detail-file --no-edit --with-detail
--slot --project --slots --slot-width --prefix --description
--period --week --last-week --this-week --since --until --group-by
--include-wip --include-backlog --include-archives
--state --limit --sort --all --keep-notes --discard-notes
```
