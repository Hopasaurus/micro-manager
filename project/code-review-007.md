# Code review 007 — `implementations/golang/internal/cli`

    Date:   2026-08-04
    Scope:  The CLI wrapper package `implementations/golang/internal/cli`
            (11 source files, 9 test files, 5,309 lines): argument parsing,
            operation dispatch, directory resolution, the two machine output
            modes, rendering, the editor integration, and the error/exit-code
            mapping.
    Method: Full read of every source and test file; `go build`, `go vet`,
            `gofmt -l`, `go test ./...` (82 tests, all passing, none skipped);
            `./check.sh --all` green; plus live probes against the built
            binary in a scratch directory to confirm each suspected defect.
    Outcome: 11 findings — 3 CONFIRMED defects (reproduced against the real
            binary), 7 minor issues, 1 accepted-for-now. None of the defects
            corrupts data or breaks the suite; the worst silently loses output
            a script was promised.
    Tooling: Conducted in pi (Claude).

---

## 1. Scope under review

| Area | Files |
|---|---|
| Entry / exit | `cli.go` (Run, finish, exit codes, Env), `exit.go` (error→code mapping) |
| Parsing | `parse.go` (switch parser, ID grammar), `resolve.go` (directory resolution) |
| Dispatch | `run.go` (17 operations), `check.go` (`--check`, `--all`) |
| Output | `render.go` (human), `json.go` (§9.2 envelope), `porcelain.go` (§9.3), `usage.go` |
| Editor | `editor.go` |
| Tests | `cli_test.go`, `dryrun_test.go`, `editor_test.go`, `fix_test.go`, `grammar_test.go`, `json_test.go`, `parse_test.go`, `porcelain_test.go`, `status_test.go` |

`go build ./...`, `go vet ./...` and `gofmt -l` are clean across the package.
All 82 tests pass with zero skips (the `testdata` fixture corpus is present).
Findings below are behavioral.

---

## 2. Findings

### F1 — `--help` / `--version` under `--json` / `--porcelain` produce empty stdout with exit 0 (CONFIRMED)

`implementations/golang/internal/cli/cli.go:110-118`

`Run` replaces `env.Stdout` with `io.Discard` whenever a machine mode is
requested, then handles `--help` and `--version` with a direct `return ExitOK`
that bypasses `finish`. The text goes to `io.Discard` and no envelope is ever
emitted:

    $ mm --version --json    # stdout empty, exit 0
    $ mm --help --porcelain  # 0 bytes, exit 0
    $ mm --version           # "mm 0.1.0 (micro-manager format spec 1)"

This is the worst of both worlds: the human text is lost AND the §9.2 envelope
("the envelope MUST be present in both cases — a caller should never have to
distinguish 'JSON error object' from 'crash text'") is absent. A script parsing
stdout gets nothing at all — indistinguishable from a crash, except that the
exit code is 0. No test combines `--help`/`--version` with a machine mode.

Two defensible fixes: route help/version through `finish` so the envelope
carries the version (or the help text) in `result`, or — simpler — treat them
as meta-switches that always print human text to the real stdout regardless of
the machine mode, since §3.4 lists them as global switches rather than
operations.

---

### F2 — `--init --slots 0` / `--slot-width 0` are silently accepted as defaults (CONFIRMED)

`implementations/golang/internal/cli/run.go:128-141`

`runInit` explicitly guards `--id-width 0` with an explanatory error ("Zero is
the library's 'not given' value, so it must never reach Init as an explicit
request"), but applies no equivalent guard to `--slots` or `--slot-width`.
`mm/init.go:67-68` treats `wip == 0` as "use 1" and `width == 0` as "use 2",
so:

    $ mm --init --project Zero --slots 0
    created /tmp/.../micro-manager for Zero
      ... working.01.md        # one slot, claimed as success

A user typing `--slots 0` gets a one-slot directory and a success message. The
same reasoning as the id-width guard applies; both switches should reject 0
(negatives already are, by the library). `--wip 0` is separately handled by the
library with a clear message, so this is confined to `--init`.

---

### F3 — JSON error envelope records an `id` from any operation's subject, not just ID-taking ones (CONFIRMED)

`implementations/golang/internal/cli/run.go:53`

```go
if id, err := parseIDIn(in.Subject, g); err == nil {
    env.json.subject = string(id)
}
```

The comment says the id is recorded "where applicable", but applicability is
never checked. Any subject that happens to parse as an ID is recorded — the
number in `--wip`, the query in `--search`, a numeric title in `--add`. Because
it only shows in `errors[].id` on failure, the mislabeling is easy to miss:

    $ mm --wip 100 --json    # fails: 100 slots need 3 digits, width is 2
    "id": "T-0100"           # --wip is about slot files, not item T-0100

Same shape for `--add 7` or `--search 42` on failure. Restrict the recording
to the operations whose subject IS an item id: `show edit remove move start
pause finish block unblock note`.

---

### F4 — dead field `porcelainOut.op` (MINOR)

`implementations/golang/internal/cli/porcelain.go:56`

`op Op` is written at `cli.go:107` (`env.porcelain.op = in.Op`) and never read
anywhere. `porcelain.emit` does not need it (no header, no per-op decoration).
Dead field; the write at `cli.go:107` is dead too.

---

### F5 — the porcelain column-freeze test does not pin `--fix` (MINOR)

`implementations/golang/internal/cli/porcelain_test.go` — `TestPorcelainFieldOrderIsFrozen`

The frozen map covers 20 operations but not `OpFix`, whose columns
(`oldId newId file detail`) are documented in `porcelainFields` and emitted by
`runFix`. The test's own comment says it exists because "the compiler cannot
catch a swapped column" — `--fix` currently sits outside that protection.

---

### F6 — extra positionals are silently dropped for `--add` / `--search` (MINOR)

`implementations/golang/internal/cli/run.go`

`--add` and `--search` consume `Rest` only when `Subject` is empty (the `--`
fallback). A second positional is silently ignored:

    $ mm --search alpha extra-word-here    # searches only "alpha"
    $ mm --add "Title" more words          # title is "Title"; rest dropped

`--note` and `--block` deliberately consume `Rest` alongside a non-empty
subject, so the surface is inconsistent. An unquoted multi-word query — a
common typo — silently narrows the search rather than erroring.

---

### F7 — `requestedSwitch` ignores last-wins for boolean values (MINOR)

`implementations/golang/internal/cli/cli.go:93-104`

The raw-argument scan enables a machine mode on the first `--json` match,
without applying §3.3 rule 5 (last wins). `Parse` resolves
`--json --json=false` to JSON off, but the envelope is still emitted and
stdout is still discarded. Narrow edge case; the scanner exists to survive a
parse error, and the parse-error case works, but the disabled-by-last-wins case
does not.

---

### F8 — `takeValue` consumes `--` as a value (MINOR)

`implementations/golang/internal/cli/parse.go:330-343`

The operation-value path guards `args[i+1] != "--"`, but `takeValue` does not
(`isSwitch("--")` returns false, since a bare `--` is not a switch). So
`--prio --` sets `prio = "--"` and the resulting error names the prio instead
of "needs a value". Cosmetic — the operation still fails — but the message
misleads.

---

### F9 — the editor is launched from the `details/` directory (MINOR)

`implementations/golang/internal/cli/editor.go:77`

`openEditor` sets `cmd.Dir = filepath.Dir(path)`, so the editor's working
directory is the item's `details/` directory rather than the project root.
Harmless today (the path handed to the editor is absolute) but surprising for
editors that expose or buffer against their cwd.

---

### F10 — `searchUpward` falls back to `.` when `cwd` is empty (MINOR)

`implementations/golang/internal/cli/resolve.go:110-115`

`filepath.Dir("")` is `"."`, so if `os.Getwd` ever failed in `cmd/mm` (cwd is
passed in as `""`), the upward walk would start by reading the process's actual
working directory — the one global this whole design avoids. Every other
resolution step guards `cwd == ""`; this one does not. Extremely rare
(getwd failure only), but a one-line fix.

---

### F11 — `--include-archives` is documented but errors at runtime (ACCEPTED)

`implementations/golang/internal/cli/run.go`, `usage.go`

`--include-archives` is in `boolModifiers` and the `--report` help page, but
`runReport` returns `usagef("--include-archives is not implemented yet
(T-0043)")`. This is a deliberate forward declaration of the archive switch
ahead of the feature; it is honest, names the blocking task, and fails loudly.
**No task created** — it resolves itself when T-0043 lands. Recorded here so a
later pass does not re-report it as a regression.

---

## 3. Verified clean

Checked and found correct, recorded so a later pass need not redo them:

- **Dry-run contract.** `TestDryRunMatchesTheRealRun` runs every mutation twice
  against two identical directories — once dry, once real — and requires the
  exit code AND the report to agree, which pins §3.4's "return the code the
  real run would have returned" honestly rather than by wording.
- **Envelope on parse failure.** `requestedSwitch` scans the raw arguments
  before `Parse` can reject them, so `--nonsense --json` still yields a JSON
  envelope. `TestJSONEnvelopeSurvivesAParseError` pins it.
- **`--check` verdicts.** `checkFailed` wraps `mm.ErrInvariantViolation`, so a
  failing `--check` exits 1 with the findings on stdout and no trailing
  "command failed" line; under `--json` the envelope reports `ok:true` with
  per-directory verdicts in `result`. Both behaviors are tested.
- **Machine modes never degrade into prose.** `Run` discards stdout for the
  human renderers, so operations need no second code path; `finish` is the one
  place a result or failure reaches the user. `TestMachineModesDoNotDegradeIntoProse`
  pins it.
- **Editor safety.** `$EDITOR` is split on whitespace and passed to
  `exec.Command` directly — never `sh -c` — so `EDITOR="myeditor --wait;
  rm -rf /"` cannot execute a second command. `TestEditorIsNotRunThroughAShell`
  pins the exact argument list. A failed launch is reported but never fails the
  operation (the item and detail file are already written and validated).
- **Directory resolution.** Four steps in §4 order, each tested against the one
  below it; ambiguity is never guessed, MM_DIR errors name the variable, and
  `--dir` is used verbatim (validated by content, not name).
- **ID grammar.** Grammar is resolved once per invocation from the opened
  directory before any ID argument is interpreted; loosen here, validate in the
  library. `parseIDIn` accepts the full form and bare numbers, rejects foreign
  grammars. Fully covered in `grammar_test.go`.
- **Porcelain discipline.** Fields are escaped (tab/newline → space) so a value
  cannot invent columns; on failure the stream is empty (exit code says so);
  the freeze test pins 20 operations' column order.
- **Warnings vs violations.** Warnings are a separate stream from violations
  everywhere — stderr in human mode, a `warnings` array in JSON — and never
  affect the exit code.
- **Exit-code mapping** is centralised in `exit.go`, with `ErrWipLimitReached`
  deliberately checked before `ErrConflict` so the one failure with a routine
  remedy keeps its own code. `TestExitCodes` pins the whole table.
- **Library purity** per AGENTS.md: no `os.Exit`/`fmt.Print`/`os.Getenv` in
  `mm/`; `cmd/mm` is the only process-state touchpoint and resolves every
  environment variable into `Env` fields before `Run`.

---

## 4. Recommended order

1. **F1** — silently lost output is the worst of the three defects, and the
   fix also restores the envelope contract.
2. **F2** — silent acceptance of an invalid request; inconsistent with the
   guard the same function already has.
3. **F3** — misleading machine-readable error data on a routine failure path.
4. **F5 + F4** — porcelain hygiene; F5 is the one that guards future
   regressions.
5. **F6, F7, F8** — parser surface; each is a few lines with a test.
6. **F9, F10** — polish.

F11 needs no action (accepted, gated on T-0043).

---

## 5. Observations

The package is unusually disciplined about *where decisions live*: one place
reads the environment (`cmd/mm`), one place maps errors to exit codes
(`exit.go`), one place decides how output reaches the user (`finish`), one
place applies `--all` (`checkTargets`). That structure is what made the review
tractable — each finding names the single function it belongs to, and each fix
is a few lines in one file.

The one structural gap the findings share is the **early-return before
`finish`**. F1 is the only place `Run` returns without going through the single
output path, and it is the only operation that breaks the envelope contract.
The envelope-before-parse machinery (`requestedSwitch`) is thorough; the
early-return path simply predates it. Routing `--help`/`--version` through
`finish` closes that last gap.

F3 is a reminder that "where applicable" in a comment is a contract with no
enforcement: the parse of a subject into an ID should be gated on the
operation, exactly as `subjectID` already gates on `op` for the operations that
take one. Two functions do the same job with different amounts of care.
