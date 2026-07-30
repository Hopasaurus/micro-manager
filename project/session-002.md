# Session 002 — retrospective

    Date:    2026-07-30
    Scope:   T-0018 through T-0039 — the rest of the library, then the whole CLI
    Outcome: 22 items shipped, `## Ready` empty for the first time.
             13,578 lines of library across 26 files, 4,276 lines of CLI,
             322 passing tests, 19 fixture directories, 3 commits.
             `mm` is a working command line tool.
    Then:    the two findings this retrospective raised were acted on in the
             same session — the skill rewritten (§4) and the permission rules
             applied (§3). Both sections record what was done.

---

## 1. What happened

Four batches, each one a list of item IDs and nothing else:

| Request | Items | What it produced |
|---|---|---|
| "list the next few tasks" | — | orientation |
| T-0018 → T-0022 | 5 | start, pause, finish, remove, init — the lifecycle |
| T-0023 → T-0028 | 6 | discovery, reporting, WIP limits, the fixture corpus, round-trip and oracle tests |
| T-0029 → T-0034 | 6 | the CLI: parser, resolution, exit codes, rendering, `--check` |
| T-0035 → T-0039 | 5 | dry-run proof, JSON, block/unblock/note, porcelain, `$EDITOR` |

Interleaved: a `.gitignore`, moving five sample directories under `sample-data/`,
three commits, and a report on the language server.

The working rhythm never changed: start the item in `working.01.md`, implement,
test, run `check.sh`, write the decisions into `details/T-NNNN.md`, close it.
The system tracked its own construction the whole way, which is the strongest
thing that can be said about a todo format.

## 2. What the session actually found

The code is the smaller half of the output. The larger half is a list of things
that were wrong and are now written down.

**Two spec self-contradictions**, both found by implementing §3.2 literally
rather than by reading:

- `--wip` was an operation *and* a modifier of `--init`. The first CLI smoke test
  refused `mm --init --project P --wip 2` as two operations. Renamed to
  `--slots`.
- `--note` was an operation *and* a modifier of `--finish` — predicted in the
  correction note the first fix left behind, and it landed exactly there: two
  existing tests started failing the moment `--note` became an operation.
  Renamed to `--closing-note`.
- **Still open:** `--detail ID` against `--add --detail`.

**A factual error in a detail file.** T-0024 claimed 2026-12-28 falls in ISO week
1 of 2027. It does not — 2026 is a 53-week year. A test written from that
example would have asserted the wrong answer, so the expectations were generated
with an independent implementation instead.

**Four library defects**, none of which a type checker could see:

- `doneFile.InsertItem` rendered the item line *before* setting its state, so
  every closed item came out with an open box;
- `insertPos` consumed the file's trailing newline, because the last element of
  a split file *is* that newline;
- violations about an *absent* frontmatter key pointed at line 1, the `---`
  delimiter;
- every mutating operation returned an **unexported** `txResult`, so no front
  end could write a function taking one. Now `mm.TxResult`.

**A documented command that silently corrupts the source tree.**
`go build -o mm ./cmd/mm` does not fail as assumed at the first commit: Go sees
`mm` is an existing directory and writes a 3.6 MB binary *into* it, where
nothing ignored it.

**Three tests that were skipping, not passing** — all from one directory move,
each found by a different accident. `t.Skipf` on a missing fixture path makes a
test survive being wrong.

The pattern across all of these: **running the thing found what reading it did
not.** The cross-check against `check.sh` earned its cost on its first execution.

---

## 3. Fewer permission checks

### What the allowlist looks like now

`.claude/settings.local.json` has 28 entries. They fall into two groups:

**Good, general rules** that will keep matching: `go build *`, `go test *`,
`go vet *`, `go run *`, `go -C *`, `gofmt *`, `./check.sh *`, `./find.sh *`,
`python3 -`, `git add *`.

**Brittle one-offs** that will never match again, because they are entire
compound command lines frozen as literal strings:

```
Bash(cd implementations/golang && go test ./... 2>&1 | tail -3 && echo "--- test count ---" && …)
Bash(git -C /Users/dlh/para/Projects/Manager check-ignore -v implementations/golang/mm/mm)
Bash(echo "exit=$?")
Bash(grep -Ei "\.\(exe|so|test\)$|/bin/|settings\.local")
```

**The cause is mine, not the tool's.** I wrote long compound one-liners —
`cd X && go build && go vet && go test | tail`. Each is a unique string, so each
needs its own approval and each approval is worthless afterwards. Had I run
`go -C implementations/golang test ./...`, the existing `go -C *` rule would have
covered it silently. Roughly a third of the prompts in this session were for
commands that an existing rule *would* have matched if I had written them
atomically.

### Applied

`.claude/settings.local.json` now carries 35 rules. Eleven were added from this
finding — read-only git and shell commands that ran repeatedly and grant nothing
the session did not already do:

```
Bash(git status:*)   Bash(git diff:*)   Bash(git log:*)    Bash(git show:*)
Bash(git check-ignore:*)   Bash(git ls-files:*)
Bash(sed:*)   Bash(grep:*)   Bash(wc:*)   Bash(ls:*)   Bash(file:*)
```

`git commit` is deliberately **not** among them: it was asked for explicitly
each time, three times, and confirming a commit is worth one prompt.

Several of the brittle one-offs were removed at the same time, including the
frozen compound command line and `Bash(echo "exit=$?")`.

> **The first attempt broke the file, and the cause was in this document.** The
> rules above were originally written here as a fenced ```jsonc block with `//`
> comments explaining each line. Pasted into `settings.local.json` — which is
> strict JSON, with no comment syntax — the file stopped parsing at the first
> `//`. A settings file that does not parse grants *nothing*, so the change
> aimed at fewer prompts would have produced more of them, and the failure is
> silent at the point of editing.
>
> Fixed by removing the comments; the file now parses and carries all 35 rules.
> The lesson is for whoever writes the next suggestion: **give configuration in
> the exact syntax of the file it goes into.** Annotate it in prose beside the
> block, never inside it. Verify with `python3 -c "import json; json.load(open(PATH))"`
> after any hand edit.

Two dead entries survive and are harmless: an exact `git -C … status --short`
line and an `echo "check-ignore exit=$?"`. They match nothing that will recur,
and removing them is not worth a second edit to a working file.

### What I should do differently

1. **One command per call.** `go -C DIR` and `git -C DIR` instead of
   `cd DIR && …`. Every compound command is a new permission string.
2. **Prefer the file tools.** `Read`, `Edit` and `Grep` need no shell approval at
   all. I reached for `sed -n`, `cat` and `grep` out of habit; a dozen prompts
   came from that alone.
3. **Stop echoing exit codes.** `echo "exit=$?"` became three separate allowlist
   entries and told me nothing the tool result did not already carry.

Expected effect: most of this session's prompts disappear, and the ones that
remain are the ones worth reading — commits, and the one destructive `rm`.

---

## 4. Does the micro-manager skill need an update?

**Yes, and one part of it is now actively wrong.** The skill is
`project/SKILL.md`, symlinked from `.claude/skills/micro-manager/SKILL.md`, so
edits propagate automatically. `implementations/golang/SKILL.md` is a copy and
needs `cp` after any change.

### Wrong, and misleading in a way that changes behaviour

Its **first section**, headed "Status — read this first", says:

> The **`mm` CLI does not exist yet.** … Until it is, perform operations by
> editing the files directly using the recipes in *Operations by hand* below …
> Do not invent `mm` invocations or claim to have run them.

Every clause of that is now false. The CLI exists, is tested, and is the fastest
correct way to do any of it. An agent reading this skill today will hand-edit
markdown and hand-maintain `next_id` when it could run one command — and, worse,
will believe it *must not* use the tool. This is the highest-value fix in the
whole retrospective, because it silently degrades every future session.

### Stale, in decreasing order of harm

| Location | Problem |
|---|---|
| "## The CLI, once it exists" | The heading is a lie now. The examples predate `--slots`, `--closing-note`, `--block`, `--unblock`, `--note`, `--wip`, `--find`, `--init`. |
| Same section | "`--json` emits a machine envelope" is now true and untested by the reader; `--porcelain` is missing entirely. |
| "Find the directories" | Says "**Four** conventional names" while the format spec's Appendix B defines **six** — both mu codepoints are separate directory names. The prose says both are matched, then the code block shows only U+00B5. |
| "Operations by hand" | Still correct and still worth keeping — the format's whole claim is that it needs no tool — but it should be framed as the fallback, not the method. |

### Applied

All of it, in the same session:

1. **The status section now says what is true** — format, `check.sh`/`find.sh`
   and the CLI all work; the UI service, the TUI and the other three
   implementations do not. It says to prefer the CLI, and why: `next_id`
   allocation, atomic multi-file writes and pre-commit validation come free.
2. **A "Using the CLI" section, placed before "Operations by hand"** rather than
   orphaned at the bottom under a heading reading "once it exists". It carries
   real examples, the resolution order, the exit-code table, and a two-row table
   for the switches that do not read the way you would guess — `--slots` not
   `--wip`, `--closing-note` not `--note` — with the reason. Those traps are this
   session's own doing, so a reader meets them before hitting them.
3. **"Operations by hand" is reframed as the fallback**, with a line making the
   relationship explicit: the CLI writes identical files, and hand-editing is the
   format's central claim being true rather than a workaround.
4. **Four names → six**, both codepoints shown, U+00B5 marked as the one to
   write. The frontmatter `description:` had the same defect, which mattered
   more: it listed four names, so a directory called `μmanager` (U+03BC) would
   not have triggered the skill at all.
5. **The validate section shows both validators** and states they are
   cross-checked against each other, so either is trustworthy — the T-0028
   oracle is what makes that safe to print — while noting `check.sh` stays the
   reference because it needs only bash and awk.
6. `cp project/SKILL.md implementations/golang/SKILL.md`; all three copies are
   in sync.

**Verified rather than asserted.** Every one of the 22 command examples in the
updated skill was run against the real binary: all exit 0, the directory they
build passes `check.sh`, and both "not this" forms are genuinely refused with
the message the table promises. A skill whose examples do not run is the same
class of defect as the status section that prompted this.

---

## 5. Efficiency

### Where the session lost time — my side

**Shell mismatch, ~6 wasted calls.** The configured shell is `fish`; I wrote
POSIX. `set -e` is a variable assignment in fish, `$?` is `$status`, and
`for … done` is a parse error. Every multi-command script should have been
`bash -c '…'` from the first one, which is what I eventually did.

**Working directory resets between calls, ~5 wasted calls.** `cd implementations/golang`
in one call does not persist to the next. I hit "no such file or directory:
./check.sh", "lstat mm/", and a Python `FileNotFoundError` on the same cause.
The fix is the same one that reduces permission prompts: `go -C`, `git -C`, and
absolute paths.

**Python heredocs for edits.** Right for the bulk rename (`txResult` → `TxResult`
across 11 files in one pass) and wrong for single edits: they bypass the file
tools, produce stale language-server diagnostics, and are harder to review than
an `Edit` diff.

**Re-verification I did not need.** Reading a file back after writing it; running
the full suite when one package changed.

**A suggestion given in the wrong syntax.** The permission rules in §3 were
written as annotated JSONC and pasted into a strict-JSON file, which stopped it
parsing. Cost: one broken settings file and one round trip. Advice about a
config file is only as good as its syntax — see the note in §3.

### Where the session was efficient — worth repeating

- **Batched items.** Five or six IDs per request was the right size: enough to
  keep context warm across related work, small enough to review.
- **The oracle.** Comparing against `check.sh` on real directories caught more
  real defects than any amount of reasoning, at almost no cost.
- **Writing decisions into the detail files as I went.** They are now the
  explanation for every non-obvious choice, and they cost nothing extra because
  the finish step demanded them anyway.

### What you could do differently

Items 1 and 2 were **done in this session**, immediately after the retrospective
was written; they are recorded here as history rather than as advice.

1. ~~The permission rules in §3~~ — **applied.** 11 rules added, several dead
   one-offs removed, one broken paste corrected. See §3.
2. ~~Fix the skill's status section~~ — **applied**, along with everything else
   in §4. Every example in it was run against the binary.
3. **Keep batching by item ID.** "Do T-0023 – T-0028" carried more information
   than a paragraph of description would have, because the items already contain
   the requirements. This is the payoff for having written them down.
4. **Consider `--dry-run` as a review tool.** Now that dry runs are proven to
   match real runs exactly, `mm --start T-0042 --dry-run` is a way to see a
   change before authorising it — cheaper than reading a diff afterwards.
5. **Say when a spec is provisional.** Both namespace collisions were fixed by
   editing the spec. That was the right call under `AGENTS.md`'s rule, but it is
   a decision you might want to make yourself. A note saying "the spec is
   authoritative, ask before changing it" — or the opposite — would remove the
   judgement call.

---

## 6. State, and what is next

`## Ready` is empty. `## Blocked` is empty. Everything remaining is `## Someday`:

```
T-0042  status, next, search and find
T-0043  archive, for rolling months into done-YYYY.md
T-0044  migrate, for older directory layouts
T-0045  stats: throughput, cycle time, WIP over time
T-0040  pin the published module path (deferred)
```

Not in the backlog at all, and probably should be:

- **The UI service.** `spec-gui.md` is 600-plus lines of specification with no
  implementation and no items tracking one.
- **The other three implementations.** `python/`, `typescript/` and `erlang/`
  are empty. The conformance section of the tools spec exists precisely to make
  a second implementation checkable — and `check.sh` plus the fixture corpus
  would test one on day one.
- **The `--detail` namespace collision**, which is a real defect in a shipped
  spec with nothing tracking it.
- **`--status`, `--next`, `--search`** are the operations a person uses every
  day, and they sit in Someday while the whole of §5.1 is done.
