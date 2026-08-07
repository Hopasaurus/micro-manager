# AGENTS.md

Orientation for an agent working in this repository. Read this first, then the
spec you need.

## What this is

**micro-manager**: a todo system stored as plain markdown, plus the tooling to
manipulate it. Every data file is readable by a human in a text editor and
parseable by a machine with a handful of regexes. Nothing needs a tool; a tool is
just faster than editing by hand.

The repository holds the specifications, a reference validator in shell, and
implementations in progress.

## Layout

```
check.sh                      reference validator for the ten invariants
find.sh                       discovery: finds micro-manager directories
skills/
  micro-manager-cli/            portable skill package: use `mm` in YOUR projects
    SKILL.md                    assumes `mm` is already on PATH; see its README
    README.md                   how to install it into pi, Claude Code, etc.
                                (named -cli, not micro-manager, so this repo's
                                 own name-based discovery doesn't reject it)
project/
  spec-file-format.md         NORMATIVE: on-disk formats, tokens, invariants I1-I10
  spec-tools.md               library/CLI contract, transactions, errors, exit codes
  spec-gui.md                 web UI: DOM contract, routes, theming, binding
  spec-tui.md                 terminal UI
  spec-pi-mm-plugin.md        pi extension: tools, /mm command, per-turn context
  SKILL.md                    how to use the format day to day
  session-00N.md              what happened in each session, oldest first
  code-review-00N.md          review passes, and what they changed
  report-lsp-results.md       what the language server caught, and its limits
implementations/
  golang/                     the Go implementation — furthest along
    AGENTS.md                 working rules for this implementation
    mm/                       THE LIBRARY (~12,700 lines, 471 tests)
    cmd/mm/                   CLI entry point
    cmd/mm-ui/                UI service entry point
    internal/cli/             wrapper: flags, rendering, exit codes
    internal/web/             the UI service of spec-gui.md: routes, SSE, theming
    micro-manager/            this implementation's own todo directory
    project/                  architecture.md, and the plan-*.md that phase the work
  python/mmx                  partial Python implementation; the tracking stopgap
  typescript/                 TypeScript implementation — scaffold only
  erlang/                     empty placeholder
plugins/
  pi-mm/                      the pi extension of spec-pi-mm-plugin.md. NOT named
                              micro-manager: discovery prunes at a name match,
                              so a source dir of that name hides what is under
                              it. Only the INSTALLED copy carries the name
sample-data/                  example micro-manager directories, used as fixtures
  sample1/ sample2/           conventional names
  hidden/ symbol/ symbol-hidden/
                              dotted and micro-sign variants
```

## Spec precedence

`spec-file-format.md` wins on anything about what a file may contain. Then
`spec-tools.md` on operation semantics. Then the UI specs. A tool that must
violate the format spec to do its job is wrong; the data is not.

`structure.md` inside each data directory is the human-facing explanation and
loses to the format spec on any detail.

## The rules that matter most

Read `spec-file-format.md` §7 for all ten. These are the ones that get broken:

- **I1 — one home per ID.** An item is in exactly one of `backlog.md`, one
  working file, or `done.md`. Moving is cut-and-paste; never copy.
- **I2 — `next_id` only goes up.** IDs are never reused or renumbered.
- **I9 — a detail file's `id` and `title` must match its item.** Changing a title
  MUST update `details/<ID>.md` in the same transaction. This is the sharpest
  coupling in the system and the easiest to forget, because writing the item line
  succeeds on its own.
- **I10 — working files are `working.NN.md`, uniform digit width, numbered 1..N
  with no gaps.** The file count IS the WIP limit; there is no setting.

Also non-obvious:

- **Dates are ISO 8601 AND real calendar dates.** `2026-02-31` is rejected
  (§3.3.1). Extended format only — `20260729` is not accepted.
- **`- [ ]` lines in a working file are subtasks**, with no IDs, and must never be
  parsed as items.
- **Unregistered `key:value` fields are legal** and MUST survive every move. They
  are the format's extension point.
- **`tags` is one form everywhere**: `tags:infra,ci`, no spaces, never a YAML list.

## How to work here

```bash
./check.sh --all                     # validate every directory
./find.sh                            # list micro-manager directories
implementations/python/mmx golang status
implementations/python/mmx golang start  T-0202
implementations/python/mmx golang note   T-0202 "some finding"
implementations/python/mmx golang finish T-0202
go -C implementations/golang test ./...
```

`mmx` was the stopgap until `mm --start`/`--finish` existed. They exist now, and
`mm` is the better tool — but `mmx` remains the default for tracking here
because it needs no build and is permitted without prompting: its target is
confined to `implementations/<name>/micro-manager`, see
`.claude/settings.local.json`. Invoke it by **absolute path**: a `cd` in an
earlier command changes the working directory for later ones and breaks the
relative form.

The real CLI is one build away and does everything `mmx` cannot — `--add`,
`--add-many`, `--check`, `--report`:

```bash
go -C implementations/golang build -o bin/mm ./cmd/mm
implementations/golang/bin/mm --status --dir implementations/golang/micro-manager
```

Track your own work in `implementations/golang/micro-manager/`. Start an item
before working on it and finish it when done — `mmx` re-runs `check.sh` after
every transition, so a bad edit surfaces immediately.

## Current state

Read the board rather than this list where the two disagree —
`implementations/golang/micro-manager/` is the record, and this is a summary of
it that will drift again.

- **Go library**: parsers, writers, validator, atomic writes, the transaction
  envelope, and every operation of `spec-tools.md` §6.2 **except**
  `setDescription`. Plus `addMany` and `parseAddLine` (§5.2.1), `archive`,
  `migrate`, `stats`, `tick`, `fix` and discovery.
- **The `mm` CLI is built and is what this repository tracks its own work
  with.** Every required operation of §5.1, plus `--block`/`--unblock`,
  `--note`, `--search`, `--find`, `--wip`, `--add-many`, `--fix`, `--archive`,
  `--migrate`, `--stats` and `--tick`. `--dry-run`, `--json` and `--porcelain`
  work on all of them. **Not built**: `--describe` (§5.3.2), `--detail ID` and
  `--subtask` as operations (§5.2 — `--detail` exists only as a modifier of
  `--add`/`--show`), `--export` and `--top-up` (§5.3).
- **The UI service is built** (`internal/web`, `cmd/mm-ui`): the board, the item
  panel, the report and the check view of `spec-gui.md`, plus the tickler on
  `tickler.interval`. Build both with `make build-all`.
- **The pi plugin is built** (`plugins/pi-mm`, spec `spec-pi-mm-plugin.md`):
  the whole required tool surface, the recommended tools gated on the installed
  `mm`, the `/mm` command and per-turn context.
- **No TUI code.** `spec-tui.md` is fully specified and nothing is written.
- **20 items in the backlog**, 168 done. The top of `## Ready` is genuinely
  next; `## Blocked` is mostly the GUI flicker-fix phases of
  `implementations/golang/project/research-app-fllicker.md`.
- **Python, TypeScript, Erlang are stubs.** `python/mmx` tracks work here and
  implements start/finish/note/status and nothing else; `typescript/` is a
  four-package scaffold; `erlang/` is a placeholder.

## Working rules for the Go implementation

Full detail in `implementations/golang/AGENTS.md`. The short version:

- The library `mm/` has **zero third-party dependencies** and is compiled into
  every front end, so it must not print, call `os.Exit`, read the environment, or
  hold global state. This **is** enforced, by `mm/hygiene_test.go`, which parses
  the sources rather than grepping them — so it is about calls rather than about
  the characters in a comment. The grep is still the quick check:

  ```bash
  grep -rnE 'os\.Exit|fmt\.Print|os\.Getenv|log\.Fatal' \
      implementations/golang/mm/*.go | grep -v _test.go
  ```
- Writes are **line splices over the retained original**, never a re-render.
  Parse-then-write must be byte-identical, and a no-op must write nothing.
- Every mutation goes through the transaction envelope: read, modify in memory,
  **validate**, then write atomically. Validation before commit is what makes it
  impossible to write a directory the checker rejects.
- Do not use `time.Time` for dates. The format is date-granular; a timezone
  shifts a `done:` date across a month boundary into the wrong group.

## Traps discovered the hard way

- **`fileEdit.Dirty()` is content-based, not operation-based.** A move that
  removes and re-inserts a line must count as no change.
- **Do not mutate a parsed frontmatter before calling `SetFM`** — `SetFM` compares
  against the file to decide whether to rewrite the line, so pre-writing the
  value makes it a no-op. Build values with `workingFields()`, then apply.
- **The transaction baseline must be captured at `begin()`**, not at commit. At
  commit the model has already changed, so nothing looks introduced and
  pre-commit validation silently does nothing.
- **A file the load never saw is `missing`, not a zero-length file that exists**,
  or every creation aborts as a phantom conflict.
- **`check.sh` is the oracle.** If the Go validator and `check.sh` disagree about
  a directory, one of them is wrong. There is a test that asserts they agree over
  the real directories and a deliberately broken one.

## Use the language server

An LSP is available and should be used whenever one is configured for the
language you are editing — gopls is installed and enabled for Go. Two ways, and
the cheap one is the one that pays:

- **Read the diagnostics that arrive after every edit.** Type errors,
  redeclarations and simplification hints appear within a second, before any
  build. Acting on them there costs nothing; ignoring them means finding the
  same thing a `go test` cycle later. Tests and source share a package here, so
  a helper in a `_test.go` file really can collide with one in the library.
- **Query before changing or deleting a symbol.** `findReferences` before
  removing something, `workspaceSymbol` when you suspect a helper already
  exists. Writing a second `itoa` next to `strconv.Itoa`, or a second
  check.sh cross-check next to the existing one, is the failure this prevents.

What it will not do: it has no opinion about paths inside string literals, it
cannot tell a skipping test from a passing one, and it does not know the ten
invariants. Those still need `check.sh` and a careful read. A clean diagnostic
stream is the floor, not the ceiling.

**"No references" is not "safe to delete."** The CLI and the UI service now call
most of `mm/`'s exported surface, so a zero-reference symbol is a sharper signal
than it was — but not a verdict: what is left unclaimed is largely what the TUI
of `spec-tui.md` will need, and that is unwritten. Reference counts answer "who
calls this now", never "should this exist". See `project/report-lsp-results.md`
for what it caught here and the seven ways it can mislead.

Prefer an absolute `filePath` — relative paths are rejected. If a diagnostic
contradicts a clean `go build`, believe the build and re-check.

## Verification habits that keep paying off

- Run the real thing, not just fixtures. Dry-run operations against this
  project's own `implementations/golang/micro-manager/` — real data catches what
  invented fixtures do not.
- `./check.sh --all` after anything that touches a data file.
- Before claiming something works: `go -C implementations/golang test ./...` and
  report the actual result.
- **A test that skips is not a test that passes.** Three tests in this
  repository were silently skipping for a whole session because `t.Skipf` on a
  missing fixture path makes a test survive being wrong. Log the count of files
  a test actually covered, and read `-v` output after moving anything.
