# Report — what gopls found

    Date:    2026-07-30
    Scope:   the Go implementation, after T-0018 through T-0028 were shipped
    Outcome: 3 defects fixed, 1 dead helper removed, 1 test consolidated;
             the largest find was a test that had been skipping, not passing

---

## 1. Summary

The language server was enabled partway through a session that had already
written eleven items' worth of code. It was used two ways, and the split between
them is the main finding of this report:

| Mode | How it arrives | What it caught here |
|---|---|---|
| **Passive diagnostics** | pushed automatically after each edit | 5 of 6 findings |
| **Explicit queries** | `workspaceSymbol`, `findReferences`, … | 1 of 6 findings |

The passive channel was worth more, and it was worth more *because it was free*.
Every finding below in that column was reported within a second of the edit that
caused it, before any build or test run.

## 2. What it found

### 2.1 Type errors, before the build (passive)

Three at once, in `roundtrip_test.go`:

```
cannot use PrioHigh (constant "high" of type Prio) as *Prio value
cannot use PrioLow  (constant "low"  of type Prio) as *Prio value
cannot use "Renamed while working" (untyped string) as *string value
```

`UpdateRequest` uses pointer fields so that "leave alone" is distinguishable
from "set to empty" — a deliberate design in the library that a caller writing
tests from memory gets wrong. The fix was a `ptr[T any](v T) *T` helper. Cost of
finding it this way: zero. Cost without: one `go test` cycle.

### 2.2 Two name collisions in one package (passive)

`describe redeclared in this block` — `discover.go` had an unexported
`describe(path) Directory`, and a new `describe([]finding) []string` was added
in `oracle_test.go`. Tests and source share a package here, so the collision is
real. Renamed to `describeFindings`.

The same thing had happened earlier in the session with `idleSlot`: a helper
function collided with a fixture constant of the same name in `working_test.go`.
That one was found by a failed `go test` run, because the language server was not
enabled yet. Same class of error, one caught in a second and one in a minute.

### 2.3 A simplification worth taking (passive)

```
discover.go:34 — Loop can be simplified using slices.Contains (slicescontains)
```

A hand-written six-line linear scan over the six recognised directory names,
replaced by `slices.Contains`. Minor, but it is exactly the kind of thing that
is never worth a dedicated review pass and always worth fixing when it is
pointed at you.

### 2.4 A hand-rolled stdlib function (explicit query)

`workspaceSymbol` for `itoa` returned this:

```
implementations/golang/mm/roundtrip_test.go:  itoa (Function) - Line 70
/usr/local/go/src/strconv/number.go:          Itoa (Function) - Line 214
```

Seeing them adjacent is the whole value. A hand-rolled integer formatter, and a
hand-rolled `quote`, both written for a diff-printing helper, both replaced by
`fmt.Sprintf("%d" / "%q")`. `AGENTS.md` already says "standard library first";
nothing enforced it.

### 2.5 The real find: a test that was skipping, not passing

The same `workspaceSymbol` result listed a symbol that should not have existed:

```
implementations/golang/mm/validate_test.go:
  TestAgreesWithCheckShOnRealDirectories (Function) - Line 225
```

T-0028 had just built a check.sh cross-check from scratch. This was a second
one, written much earlier, described in its own comment as "a cheap preview of
T-0028". Reading it showed:

- it pointed at `../../../sample1/micro-manager` and four siblings — **paths that
  moved under `sample-data/` earlier in the same session**;
- its `t.Skipf` was **inside the loop**, so the first missing directory skipped
  the whole test;
- the skipped remainder included a case nothing else covered: a directory broken
  four ways at once, requiring both validators to reject it.

So it had been silently skipping since the move rather than failing. It reported
`ok`. It was the third such test found in this session, all from the same
directory move, each by a different accident:

| Test | How it was found |
|---|---|
| `realdata_test.go` | grep for the moved paths, at move time |
| `write_test.go` `TestRoundTripRealRepositoryFiles` | noticed in `-v` output while writing T-0027 |
| `validate_test.go` `TestAgreesWithCheckShOnRealDirectories` | this `workspaceSymbol` query |

The unique half was folded into `oracle_test.go` as a subtest running through
the stronger set comparison; the rest was deleted as superseded, and its `isDir`
helper became dead and went with it.

### 2.6 Confirming a deletion was safe (explicit query)

`findReferences` on `IsDirectoryName` returned four hits, all inside
`discover.go`. Exported and used only internally is expected here — the CLI that
will call it does not exist yet (T-0030 onward) — but the query is the cheap way
to be sure a symbol is not load-bearing somewhere unexpected before touching it.

## 3. What it did not and cannot find

Worth stating plainly, because the section above could be read as an argument
that a language server replaces judgement:

- **It has no opinion about string literals.** All three stale tests were broken
  by paths inside string constants. gopls saw valid Go throughout. It surfaced
  the symbol; a human read the file.
- **It cannot tell a skip from a pass.** `t.Skipf` on a missing fixture path is
  a test that survives being wrong, and no static tool will say so. The defence
  is in the test design: log the count of files actually covered, so a silent
  skip is visible in `-v` output.
- **It does not know the invariants.** Every genuine defect in the library this
  session came from running the code against `check.sh`, not from any static
  analysis. §5.1 lists them.

## 4. Operational notes

- **Absolute paths.** A relative `filePath` was rejected; the same call with an
  absolute path worked.
- **Stale diagnostics are possible.** After adding an import via a script, an
  `undefined: slices` diagnostic persisted for one turn while `go build` was
  already clean. Trust the build over a diagnostic that contradicts it, then
  re-check.
- **`workspaceSymbol` is fuzzy and verbose.** A query for `itoa` returned 100
  symbols including `IPPROTO_AH` and `IDS_Trinary_Operator`. It is a net for
  scanning, not a lookup.

## 5. Downsides to watch for

Observed here unless marked otherwise. None of these argues for turning it off;
they argue against treating its output as authority.

### 5.1 A clean diagnostic stream is not a correct program

The most likely harm is the confidence, not the tool. "No diagnostics" means the
code compiles and satisfies some lint rules. Every real defect this session sat
comfortably inside code gopls was happy with:

- `doneFile.InsertItem` rendered the item line *before* setting the state, so
  every closed item came out with an open box;
- `insertPos` stepped past the final empty element that **is** a file's trailing
  newline, so every `done.md` written into a fresh month group lost its last
  byte;
- two violations pointed at line 1 — the `---` delimiter — for a key that was
  absent.

All three were found by running the code against `check.sh`. A clean editor is
the *floor*, and it is easy to mistake it for the ceiling when it is the loudest
signal in the loop.

### 5.2 "No references" does not mean "safe to delete"

`findReferences` on `IsDirectoryName` returned four hits, all inside
`discover.go`. In most codebases that reads as dead code. Here it is the
intended public API of a library whose consumers — `cmd/mm`, and later the UI
service — **have not been written yet** (T-0030 onward).

This repository is unusually exposed to that mistake: `mm/` is deliberately not
`internal/`, the CLI is a stub, and most of the exported surface has exactly
zero callers by design. An enthusiastic "remove unused exports" pass driven by
reference counts would gut the API the specs require. Reference counts answer
"who calls this today", never "should this exist".

Related blind spots for the same query: build-tagged files outside the active
configuration, symbols reached by reflection or by name from another language,
and the other implementations in this repo, which are separate modules.

### 5.3 It only sees the current build configuration

*Anticipated, and nearly hit.* The first draft of `discover.go` detected symlink
cycles with `syscall.Stat_t` device and inode numbers. That does not compile on
Windows — and gopls, analysing for the host GOOS, would have reported nothing.
It was caught by knowing the rule, not by the tool. `os.SameFile` replaced it.

Anything gated behind `//go:build`, any other GOOS/GOARCH, and anything under
`testdata/` (which the Go toolchain ignores by design) is outside what the
server is checking.

### 5.4 Stale state after edits it did not see

Observed once: after an import was added by a `python` heredoc rather than by an
editor operation, an `undefined: slices` diagnostic persisted for a turn while
`go build` was already clean. The hazard is acting on it — "fixing" an import
that is already correct, or reverting a good edit to satisfy a phantom.

This matters more in an agentic loop than in an editor, because scripted and
bulk edits are normal here and the server is not always told about them
promptly. Rule: when a diagnostic contradicts a clean build, the build wins.

### 5.5 Noise, and the cost of it

`workspaceSymbol` for `itoa` returned 100 symbols, including `IPPROTO_AH` from
`syscall` and `IDS_Trinary_Operator` from `unicode/tables.go`. Two risks: the
signal is easy to skim past — the stale-test find in §2.5 was one line in that
wall — and in an agentic loop every such result is context spent. Query
deliberately; it is a net for scanning, not a lookup.

### 5.6 Simplification hints are suggestions, not review

The `slices.Contains` hint was correct and worth taking. The failure mode is
applying that class of hint reflexively: some change nil/empty semantics, some
pull in a dependency the project has a stated policy about, and some make code
shorter and less clear. `AGENTS.md` says a dependency needs a stated reason —
a linter does not know that.

### 5.7 It covers one language in a polyglot repository

Go is roughly a third of what matters here. The reference validator is awk, the
discovery tool is bash, the format itself is markdown files, and the specs are
prose that binds the implementation. gopls has nothing to say about any of it —
including the case where the Go implementation and `check.sh` disagree, which is
the single most valuable class of bug this project has.

## 6. Recommendation

Keep it on for Go. The passive channel alone justifies it: three of the six
findings were type or declaration errors caught before a build, and the
remainder cost one query. Treat it as a fast floor under the work, not as
evidence the work is right — §5.1 and §5.2 are the two that would actually cost
something here.

**Python is not worth a server yet.** The entire Python surface is
`implementations/python/mmx`, a single script `AGENTS.md` calls "the tracking
stopgap", plus an empty `project/` directory. A language server buys cross-file
navigation and type intelligence; there is no second file to navigate to. The
trigger to revisit is `implementations/python/` growing a real package — at which
point `pyright` is the one to want, because the API design here leans on typed
request structs and a sentinel-error taxonomy, and that is precisely where a
Python type checker earns its keep. Scope it to that package when it exists:
`mmx` has no annotations and no `pyproject.toml`, and strict mode over the whole
tree would produce a wall of noise about a file nobody is developing.
