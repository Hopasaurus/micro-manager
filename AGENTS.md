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
project/
  spec-file-format.md         NORMATIVE: on-disk formats, tokens, invariants I1-I10
  spec-tools.md               library/CLI contract, transactions, errors, exit codes
  spec-gui.md                 web UI: DOM contract, routes, theming, binding
  spec-tui.md                 terminal UI
  SKILL.md                    how to use the format day to day
  session-001.md              what happened in the first session
implementations/
  golang/                     the Go implementation — furthest along
    AGENTS.md                 working rules for this implementation
    mm/                       THE LIBRARY (8,201 lines, 164 tests)
    cmd/mm/                   CLI entry point — a stub so far
    internal/cli/             wrapper: flags, rendering, exit codes
    micro-manager/            this implementation's own todo directory
    project/                  architecture.md, architecture-echo-v5.md
  python/mmx                  partial Python implementation; the tracking stopgap
  erlang/, typescript/        empty placeholders
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
implementations/python/mmx golang start  T-0018
implementations/python/mmx golang note   T-0018 "some finding"
implementations/python/mmx golang finish T-0018
go -C implementations/golang test ./...
```

`mmx` is the tracking stopgap until `mm --start`/`--finish` exist. It is
permitted without prompting because its target is confined to
`implementations/<name>/micro-manager` — see `.claude/settings.local.json`. Invoke
it by **absolute path**: a `cd` in an earlier command changes the working
directory for later ones and breaks the relative form.

Track your own work in `implementations/golang/micro-manager/`. Start an item
before working on it and finish it when done — `mmx` re-runs `check.sh` after
every transition, so a bad edit surfaces immediately.

## Current state

- **Go library**: T-0001 … T-0017 shipped. Parsers, writers, validator, atomic
  writes, transaction envelope, and `Add`/`Update`/`Move`/detail operations.
- **28 tasks remain**, next is **T-0018** (`--start` with WIP enforcement).
  Detail files already exist for the subtle ones: T-0018, T-0024, T-0028, T-0030.
- **No working CLI.** `cmd/mm` returns exit 2. T-0030–T-0034 build the wrapper.
- **No GUI or TUI code.** Both fully specified, nothing written.
- **T-0040 is blocked**: the module path is the placeholder `micromanager`
  because there is no VCS remote. `git init` would unblock it.

## Working rules for the Go implementation

Full detail in `implementations/golang/AGENTS.md`. The short version:

- The library `mm/` has **zero third-party dependencies** and is compiled into
  every front end, so it must not print, call `os.Exit`, read the environment, or
  hold global state. **This is not yet enforced by a test** — T-0029 adds one.
  Until then, check it by hand:

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

**"No references" is not "safe to delete."** Most of `mm/`'s exported surface has
zero callers today because the CLI and the UI service that the specs require are
not written yet. Reference counts answer "who calls this now", never "should this
exist". See `project/report-lsp-results.md` for what it caught here and the
seven ways it can mislead.

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
