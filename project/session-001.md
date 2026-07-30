# Session 001 — notes

    Date:    2026-07-29
    Scope:   from an empty directory to 17 shipped tasks of the Go implementation
    Outcome: 4 specs (3,924 lines), 2 shell tools, a Go library at 8,201 lines
             with 164 passing tests, 7 micro-manager directories, all validating

---

## 1. What happened, in order

**Designing the format.** Started from "build a file structure for organizing
todos" and produced `structure.md`, `backlog.md`, `working.md`, `done.md`. The
first real decision was the item-line grammar — one line per item, checkbox plus
permanent ID plus ` | key:value` fields — chosen over HTML-comment metadata
because the files are hand-edited and invisible-when-rendered metadata is worse
to type.

**Adding the pieces, one request at a time.**

| Request | What it produced |
|---|---|
| bulk text for long descriptions | `details/T-NNNN.md`, `detail:` field, `_template.md` |
| `check.sh` | validator for the invariants, later the reference oracle |
| five copies in named directories | `find.sh`, the four conventional directory names |
| unify the tags | one `TAGLIST` form everywhere; the YAML flow sequence removed |
| add the validation + project name | `working.NN.md` frontmatter fully checked; `project:` required |
| variable WIP limit | `working.NN.md`; the file count *is* the limit |
| `spec-file-format.md` | 634 lines, ten invariants, tokens, known limitations |
| `spec-tools.md` | library/CLI split, operations, transactions, exit codes |
| `spec-gui.md` | DOM contract, routes, theming, discovery, local-only binding |
| `spec-tui.md` | terminal front end, sharing the colour tokens |
| SKILL.md + AGENTS.md | how to use the format; how to build the Go version |
| Go implementation | T-0001 … T-0017 |
| ISO 8601 | tightened dates to real calendar dates across spec and both impls |

**Building the Go implementation.** Seventeen tasks, tracked in
`implementations/golang/micro-manager/` using the very system being built —
which turned out to be the most useful decision of the session, because every
transition exercised the format under real use.

## 2. What worked well

**Specs before code.** By the time `ParseDate` was written, the spec already said
what a date was, why it had no timezone, and what the checker must reject. Every
Go file cites the section it implements. When behaviour was in question the
answer was in a document rather than in a discussion.

**`check.sh` as an oracle.** Writing the shell validator first, then implementing
the same invariants in Go and asserting the two agree, caught more than unit
tests would have. `TestAgreesWithCheckShOnRealDirectories` runs both over six
real directories *and* a deliberately broken one — the broken case was added
after noticing the clean-only version would pass for a validator that reported
nothing at all.

**Verifying against real data, not just fixtures.** Repeatedly the most valuable
check: 59 real item lines parsed and round-tripped; 21 real files byte-identical;
dry-run `Add`/`Update`/`Move`/`AttachDetail` against this project's own backlog.
Fixtures test what I imagined; the real files test what exists.

**Recording *why* in the code.** Comments explaining the reasoning — why `Date`
is not `time.Time`, why the temp file must be a sibling, why parsing is
permissive — repeatedly paid for themselves when a later task touched the same
code.

**Using the system to build the system.** Tracking the work in a micro-manager
directory surfaced the friction of hand-editing three files per transition, which
led to `mmx`, which then needed a permission rule, which is exactly the loop a
real user would hit.

**Detail files carrying the hard parts.** Writing `details/T-0012.md` (write
ordering) and `details/T-0018.md` (the never-create-a-slot rule) *before*
implementing them meant the subtle constraint was already written down when the
code got there.

## 3. Bugs I introduced and caught

Worth recording because the pattern is instructive: every one was caught by a
test that existed for a *different* reason.

1. **Pre-commit validation was silently a no-op** (`tx.go`). The baseline
   violation set was captured at commit time, after the model had already been
   mutated, so "before" and "after" were the same set and nothing ever looked
   introduced. The tool's central safety property did not work. Found by a test
   that deliberately added a colliding ID.
2. **`SetFM` change detection defeated** (`working.go`). `renderWorkingItem`
   wrote into the parsed frontmatter before `SetFM` compared against it, so every
   value looked unchanged and updating an item in a working slot wrote nothing.
3. **`fm.End` shifted twice** (`write.go`), once via `refs` and once directly,
   plus a `ptrIn` helper that returned a pointer to a local copy and therefore
   did nothing at all.
4. **`Dirty()` tracked operations, not content**, so a `--move` that removed and
   re-inserted a line rewrote the file despite identical bytes.
5. **An unread file's zero stamp read as "exists, size 0"** rather than
   "missing", so creating any new file aborted as a phantom conflict.

## 4. Friction, and what would reduce it

**Late-breaking requirements that ripple.** The ISO 8601 change arrived after
`check.sh` and the Go `ParseDate` both had date validation, and after
`spec-file-format.md` §10 documented the *opposite* behaviour as a known
limitation. Resolving it meant editing four specs, two implementations, a test
that asserted the old behaviour, and three error messages.

*Suggestion:* when a cross-cutting constraint is already in mind — encoding, date
format, ID width, timezone policy — naming it during the format-spec phase costs
one sentence instead of a propagation pass.

**Two unresolved decisions still blocking work.** `T-0040` (module path) has been
blocked all session because the repository is not under version control and has
no remote. The Go module is `micromanager`, a placeholder.

*Suggestion:* `git init` and a decision on the published path would unblock it and
make every "verify nothing changed" check cheaper, since `git diff` beats
snapshotting files in tests.

**Ambiguous file paths.** Three requests named a path that did not exist:
`/symbol-hidden/.µmanager/` (leading slash — the filesystem root),
`implementation/golang/...` (singular), and `projects/` (plural). Each time I
chose the sensible reading and said so. That worked, but a moment's ambiguity per
request adds up.

**Working-directory persistence.** A `cd` in one tool call silently changed the
directory for later calls, which broke a relative-path invocation of `mmx` and
cost a round trip. Fixed by allowlisting the absolute path too.

**Batch sizing.** "do 5 more tasks" and "go on to 16" were both good units of
work — enough to finish something coherent, small enough to report honestly.
"keep going" and "go" were fine too but left the stopping point to me; naming a
count or a task made the hand-off cleaner.

*Suggestion:* keep using "do N tasks" or "do T-00NN". It produces a better report
than an open-ended continue.

**Permission prompts.** Hand-editing the tracking files needed repeated approval
until `mmx` existed with a hard-coded target directory and a narrow allowlist
rule. Worth doing earlier in a session with this shape: the third time a
repetitive action needs approval is the signal to build the narrow tool.

## 5. Things deliberately left undone

- **T-0018 onward** — 28 tasks remain, starting with `--start` and WIP
  enforcement. The detail files for the subtle ones are already written.
- **No CLI yet.** `cmd/mm` returns exit 2; T-0030–T-0034 build the real wrapper.
  Nothing is runnable from a shell except `check.sh`, `find.sh` and `mmx`.
- **No GUI or TUI code.** Both are fully specified; neither has a line written.
- **`mmx` is a stopgap** and says so. It gets deleted once `mm --start` and
  `mm --finish` exist.
- **Echo v5 was verified**, but nothing has been built against it, and
  `architecture-echo-v5.md` §7 lists the API surface that was *not* checked.

## 6. If I were starting this again

1. `git init` first. Every subsequent verification gets cheaper.
2. Settle the cross-cutting lexical rules — encoding, dates, ID width — in one
   pass while writing the format spec.
3. Build the reference validator early, exactly as happened; it paid for itself
   many times.
4. Write the narrow permitted helper tool at the third repetitive approval, not
   the tenth.
5. Keep the "verify against real data" habit. It found things fixtures did not.
