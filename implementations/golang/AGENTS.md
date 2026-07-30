# AGENTS.md — Go implementation

Instructions for an agent working in `implementations/golang/`.

This directory holds the Go implementation of micro-manager: the library, the
`mm` CLI, and later the UI service. **Nothing is built yet.** Treat what follows
as the contract for what you create, not a description of what exists.

## Read these first

The specifications are normative. Do not infer behavior from this file when a
spec covers it, and do not change behavior a spec fixes without changing the
spec in the same session.

| Spec | What it binds |
|---|---|
| `../../project/spec-file-format.md` | On-disk formats, tokens, invariants I1–I10 |
| `../../project/spec-tools.md` | Library API, CLI switches, transactions, errors, exit codes |
| `../../project/spec-gui.md` | UI service, config, theming, discovery, binding |
| `../../project/spec-tui.md` | Terminal front end |
| `../../project/SKILL.md` | How the format is used day to day |

Precedence: format spec, then tools spec, then the UI specs. A tool that must
violate the format spec to do its job is wrong; the data is not.

## Non-negotiables

From `spec-tools.md` §2.2. The library is compiled into the CLI *and* into the
UI service, so it is a guest in a process it does not own.

The library packages MUST NOT:

- write to stdout or stderr — return diagnostics, never print them;
- call `os.Exit`, `log.Fatal`, or `panic` on an expected failure;
- read `os.Args`, `os.Getenv`, or the working directory;
- install signal handlers or mutate process-global state;
- hold global mutable state.

`$EDITOR`, `MM_DIR`, prompting, colour, and exit-code mapping belong to
`cmd/mm`. If the CLI needs a rule the library lacks, the rule goes in the
library — the UI service needs it too.

Enforce this mechanically. A test that greps the library packages for `os.Exit`,
`fmt.Print*`, and `os.Getenv` is cheap and catches the drift that review misses.

## Layout

```
implementations/golang/
  go.mod                     module path — set it before writing code
  mm/                        THE LIBRARY. Importable; must not be internal/.
    store.go                 Open, Store, per-directory operations
    item.go                  Item, Slot, Directory, field types
    parse.go                 item lines, frontmatter, sections
    write.go                 atomic writes, ordering, round-trip fidelity
    validate.go              I1–I10, one implementation only
    discover.go              DiscoveryOptions/DiscoveryResult walker
    report.go                period resolution and report building
    theme.go                 theme + config load/merge (used by UI, not CLI)
    errors.go                the error taxonomy
  cmd/mm/main.go             CLI wrapper: flags, rendering, exit codes
  internal/cli/              wrapper guts — parsing, output modes, prompts
  testdata/                  fixture micro-manager directories
  project/                   implementation planning notes (already present)
```

The library is `mm/`, not `internal/mm/`. The UI service is a separate binary
that imports it; `internal/` would make that impossible.

## Build, test, run

Go 1.26 is installed on this machine. Pin a version in `go.mod`.

```bash
go build ./...
go test ./...
go vet ./...
go run ./cmd/mm --check --all
```

Before claiming anything works, run `go test ./...` and report the actual
result. If tests fail, say so with the output.

**gopls is installed and enabled — use it.** Act on the diagnostics that arrive
after each edit rather than waiting for a build, and query `findReferences`
before deleting a symbol or `workspaceSymbol` before writing a helper that may
already exist. The general guidance, and what it did and did not catch in this
codebase, is in `../../AGENTS.md` and `../../project/report-lsp-results.md`.

## Mapping the spec to Go

**Types** (`spec-tools.md` §6.1). Field names are normative; Go idiom governs
casing and representation.

```go
type Item struct {
    ID       ID
    Title    string
    State    State              // Backlog | Working | Done
    Section  Section            // Ready | Blocked | Someday, backlog only
    Slot     int                // 0 = not in a slot
    Position int                // 1-based within its column
    Prio     Prio
    Tags     []string
    Detail   string
    Created  Date
    Started  Date
    Done     Date
    Outcome  Outcome
    Blocked  string
    Extra    map[string]string  // unregistered fields — MUST round-trip
    Source   Location
}
```

`Extra` is load-bearing. Unregistered fields are the format's extension point
(`spec-file-format.md` §9); dropping them on a move silently destroys data.
Every operation that rewrites an item must carry it through, and a test must
prove it.

Do not use `time.Time` for the date fields. The format is date-granular with no
timezone (`spec-file-format.md` §3.3); a `time.Time` invites a timezone bug that
shifts a `done:` date across a month boundary and moves an item into the wrong
group.

**Errors** (`spec-tools.md` §6.3). Sentinel errors plus typed wrappers, matched
with `errors.Is` / `errors.As`. The CLI maps them to exit codes (§10); the UI
service maps them to HTTP status (`spec-gui.md` §4.3). Neither mapping belongs
in the library.

```go
var (
    ErrNotFound          = errors.New("not found")
    ErrAmbiguous         = errors.New("ambiguous")
    ErrInvalidArgument   = errors.New("invalid argument")
    ErrConflict          = errors.New("conflict")
    ErrWipLimitReached   = errors.New("wip limit reached")
    ErrPreconditionFailed= errors.New("precondition failed")
    ErrInvariantViolation= errors.New("invariant violation")
    ErrConcurrent        = errors.New("concurrent modification")
)
```

`ErrWipLimitReached` is separate from `ErrConflict` on purpose: it is the one
error with a routine remedy, and its message must name the limit, the current
occupants, and the three ways forward.

`ErrInvariantViolation` carries the `[]Violation` list.

**Every mutating operation** takes a dry-run flag and returns the resulting
state plus its `[]Change` — never `error` alone. A UI re-renders from the
return value instead of re-reading.

## Transactions

`spec-tools.md` §7, and the part most likely to be got wrong:

1. Read, modify in memory, **validate**, then write. Never mutate a file
   incrementally.
2. Validation runs before every commit. The tool must not be able to produce a
   directory its own checker rejects.
3. Write atomically: temp file in the same directory, `fsync`, then `os.Rename`.
4. Order multi-file writes so a crash is recoverable. For `--start`, write the
   working file first and remove the backlog line second — a crash between them
   duplicates the item, which I1 catches, rather than destroying it.
5. Record size and mtime at read; if they differ at write, return
   `ErrConcurrent`. The CLI, the UI service, and a text editor may all be
   writing.
6. Never rewrite a file you did not change, never reorder `## Ready` as a side
   effect, never reformat untouched lines. A no-op operation produces no diff.

Reading is deliberately more permissive than writing: a directory with existing
violations must still open, list, and report. Refusing to read a broken
directory removes the tool exactly when it is needed.

## Testing

**Fixtures.** Put micro-manager directories in `testdata/`. A fixture must
validate clean unless it exists to test violation reporting, in which case name
it for the invariant it breaks (`testdata/broken-i9-orphan-detail/`).

**Use `check.sh` as an oracle.** The repository root has a working reference
validator. A test that runs the Go implementation over a directory and then runs
`../../check.sh` against the result — asserting both agree — catches divergence
between two implementations of I1–I10 far more cheaply than reasoning about it.
Skip that test when `bash` is unavailable rather than failing.

**Round-trip.** For every fixture: parse, write back unchanged, assert the bytes
are identical. This is the cheapest possible guard against dropping `Extra`
fields, reordering sections, or reformatting.

**Table-driven** for the item-line and frontmatter parsers. Include the ugly
cases the format spec §10 documents: a title containing `|` followed by `:`,
`2026-02-31`, mixed working-file digit widths, a `- [ ]` subtask line inside a
working file.

Do not write tests that assert on the wording of a message. Assert on error
identity and on file content.

## Style

- Standard library first. A dependency needs a reason stated in the commit.
- `gofmt`; `go vet` clean.
- Wrap errors with `%w` and enough context to name the file and item.
- Exported identifiers in `mm/` get doc comments citing the spec section they
  implement — `// Start moves an item from the backlog into a working slot
  // (spec-tools.md §5.1.8).`
- Keep `cmd/mm` thin. If a function there does anything a UI would also need, it
  is in the wrong package.

## Definition of done for a unit of work

1. `go build ./...`, `go test ./...`, `go vet ./...` all pass, and you have run
   them and seen the output.
2. `go run ./cmd/mm --check --all` agrees with `../../check.sh --all` on every
   fixture and on the sample directories at the repository root.
3. Round-trip tests pass, including `Extra` preservation.
4. No new dependency without a stated reason.
5. Behavior matches the cited spec section, or the spec was updated in the same
   change with the reason recorded.
