# Plan — git interoperability and overlapping-ID merges

    Status: proposed
    Date:   2026-08-01
    Scope:  the format's relationship to git, both validators, the Go library
    Items:  T-0101 (this spike, plan only); proposes T-0121 (spec carve-out),
            T-0122 (validators reject markers), T-0123 (library repair)

How a micro-manager directory behaves inside a git repository, what a merge
that overlaps task IDs actually does to the data, and where the repair belongs.

The format's core promise is that the files are plain text: human-editable in
any editor, parseable with a handful of regexes, and that no tool is required.
Git is therefore **transport, not part of the format contract** — this plan
never adds an artifact git must know about, and nothing here makes a directory
behave differently inside or outside a repository. The question is only what
happens when two people edit the same board concurrently and merge.

Specs are normative. Where this document appears to contradict one, the spec
wins; T-0121 lands the one spec change this plan proposes.

---

## 1. The question

Two people, one repo, one board. Both add an item. Both run `mm add` against
the same committed `next_id`, so both get the same ID. Both commit. The merge
— demonstrated below — is **silent**: git auto-merges, no conflict marker, and
the directory now contains two items with the same ID. The ten invariants are
violated and the only thing that notices is the validator.

The spike asks two things: how micro-manager interoperates with git at all, and
how merges that overlap task IDs are solved. The second is the hard half. This
plan measures what git actually does to the format, then places the fix.

## 2. What git sees

The format is already close to git's comfort zone:

- **One item per line, unique by ID.** Item lines start with `- [ ] [T-0043]`
  and the ID is unique in the directory (I1). Git's line-based three-way merge
  handles two people appending *different* items cleanly — the common case.
- **Small shared files.** `backlog.md` is the only file both people routinely
  edit. `done.md` grows but appends; `working.NN.md` holds one item each;
  `details/<ID>.md` is one file per item, so concurrent edits to *different*
  items never touch the same file.
- **Moves are delete-plus-insert.** I1 makes closing an item a cut from one
  file and a paste into another. Git sees two separate changes in two files and
  cannot know they are one operation; `diff.renames` helps *reading* history,
  not *merging* it.
- **`next_id` is one line in `backlog.md` frontmatter.** It is a high-water
  mark (I2), which is both the collision source and the repair handle.

## 3. The conflict taxonomy (measured 2026-08-01)

### 3.1 Silent duplicate allocation — the headline

Real merge, real repo, two branches from one base (`next_id: T-0043`):

```
branch alice:  adds [T-0043] Alpha, next_id → T-0044
branch bob:    adds [T-0043] Beta,  next_id → T-0044
merge:         Auto-merging mm/backlog.md — NO conflict markers
```

Resulting `## Ready` section: `T-0043 Beta` and `T-0043 Alpha` both present.
The `next_id` line merged cleanly because both sides made the *same* change
(T-0043 → T-0044); the two item lines landed at different positions, so git
kept both. **The merge succeeds and produces an invalid directory.** check.sh
reports `T-0043 is already defined at backlog.md:…` — the validator is the only
detector. This is the scenario the whole plan orbits.

### 3.2 Detail-file conflict

The same collision also makes both branches *create* `details/T-0043.md`. When
the contents differ, the merge conflicts on that file — loud, and correctly so.
Resolving it is the same rename decision as 3.1, just surfaced by git instead
of by check.sh.

### 3.3 Move versus edit (I1)

Alice finishes `T-0042` (deletes its line from `backlog.md`, inserts it under a
month in `done.md`). Bob edits `T-0042`'s title in place. The merge sees Alice
deleting a line Bob modified — a real conflict in `backlog.md`, while Bob's
edited line is what should land in `done.md`. Git cannot connect the two halves
of the move. A human resolves it; the tool's job is to keep the conflict small
and validate the resolution.

### 3.4 Same item finished twice

Both people move the same item to `done.md` with different `done:` dates or
notes. Two branches, same base line, both modify it → conflict on that line.
Human picks one (or the later date).

### 3.5 Concurrent starts collide on a working file

The working file set is fixed by the WIP limit (I10); `mm start` fills the
lowest idle slot. Two people starting at once both take the same slot, both
write the same `working.NN.md` with different items → conflict on the whole
file. Loud, and the resolution is mechanical (keep both? no — one slot, one
item; the loser's start must be redone after the merge, or the slot content
merged by hand).

### 3.6 Conflict markers are silently absorbed — a latent hole

Probe: put a git conflict block inside `backlog.md`:

```
<<<<<<< HEAD
- [ ] [X-003] Local edit | created:2026-08-01
=======
- [ ] [X-003] Remote edit | created:2026-08-01
>>>>>>> theirs
```

**Both validators accept the file.** Spec §5.1 says non-item prose "MAY appear
anywhere and MUST be ignored by readers", so the three marker lines are legal
prose, and the two item lines inside the block parse as real items (the
duplicate ID is what eventually trips I1). When a conflict's two sides carry
*different* IDs — the common case for a moved line colliding with an edited
neighbor — the markers pass with **zero findings** and the file renders garbage
until a human happens to notice. A half-resolved merge is indistinguishable
from valid prose by construction.

## 4. Decisions

1. **Git is transport, not part of the format.** No `.gitattributes`, no
   per-repo artifacts, no behavior that only makes sense inside a repository.
   The format's contract is with text files; `check.sh` stays the oracle. The
   interop story is: a board is a directory, a repo may hold one board or many
   (`find.sh` already treats "several boards in one tree" as a first-class
   shape, and prunes `.git`), and `git push`/`pull` is the zero-server sync
   story the "micro" promise implies.

2. **Collisions are solved by detection plus mechanical repair, not by
   prevention.** Preventing two people from allocating the same ID needs a
   lock — a server, a central counter, or an exclusive checkout — and all of
   those violate the format's no-server promise. The demonstrated cost of
   *not* preventing is low: the collision is always caught by the validator,
   and the repair below is deterministic. Per-branch prefixes or ID ranges are
   rejected outright: they violate I2 (one monotonic counter per directory)
   and §3.3.2 rule 1 (one grammar per directory, T-0118).

3. **Conflict-marker lines become a format violation.** Three exact line
   shapes — a line starting `<<<<<<<`, `=======`, or `>>>>>>>` — are reserved
   in every data file. This is a deliberate, narrow carve-out from §5.1's
   prose rule: those three shapes are how a half-merged file announces itself,
   and a reader that swallows them is doing the opposite of the format's job.
   Both validators reject the file and name the line. This closes 3.6 and makes
   every *real* conflict loud instead of latent.

4. **`mm fix` is a directory-local, deterministic, idempotent repair.** It
   fixes exactly the two findings a merge manufactures — the I1 duplicate and
   the I2 ceiling — nothing else:
   - `next_id` becomes `max(current next_id, max ID in use + 1)` — never
     lowered, so I2's monotonicity holds by construction.
   - For each ID that appears more than once (in one file or across files,
     which is I1), keep the item in the most advanced home — `done.md` beats a
     working file, which beats `backlog.md` — and among equal homes the
     earliest `created`; renumber the losers to fresh IDs drawn from
     `next_id`, bumping it as it goes.
   - A renumber updates the item line *and* renames `details/<old>.md` to
     `details/<new>.md` **in the same transaction** — the I9 coupling, which
     is the sharpest rule in the format and the easiest to break.
   - A tie (same home, same `created`) refuses with both items named rather
     than guessing — the repair must never invent an answer a human should give.
   - It refuses to run while conflict markers remain (now a violation, so this
     is just "the directory must be loadable"). Dry-run prints the exact
     splices; the real run goes through the transaction envelope — read,
     modify, validate, write atomically. Running twice is a no-op.
   - It is not git-aware and does not need to be: the same command repairs a
     hand-edited directory that broke I1/I2, which is the same value as a
     post-merge one.

5. **Custom merge drivers are out.** A `merge=mm` driver in `.gitattributes`
   would make `backlog.md`'s three-way merge reimplemented in a second
   implementation, exercised only during merges — rare, high-stakes, and a
   silent-corruption bug there is far worse than a duplicate ID that check.sh
   catches. Git's own merge stays in charge; the repair runs after it, and the
   repair is the *inverse of check.sh's findings*, which is a clean contract:
   check.sh says what is wrong, `mm fix` fixes exactly that.

6. **The tool detects; hooks and humans decide.** `mm` never auto-commits
   (session-002: `git commit` stays a deliberate, user-owned act). A
   documented pre-commit hook runs `check.sh` over every board directory; a
   documented post-merge ritual is `check.sh`, then `mm fix --dry-run`, resolve
   whatever git flags by hand, re-run `check.sh`. Nothing runs `mm fix` for
   real without a human reading the dry run, because 3.3/3.4 are judgment calls
   the repair cannot make.

## 5. What lands as a result

- **T-0121 (spec, proposed)**: §5.1's prose rule gains the reserved-shape
  carve-out: a line whose first non-blank characters are `<<<<<<<`, `=======`,
  or `>>>>>>>` is invalid in every data file, and a reader MUST refuse it
  loudly rather than ignore it.
- **T-0122 (validators, proposed)**: check.sh and the Go validator reject the
  three marker shapes, naming the file and line; a broken-marker fixture joins
  the corpus and the oracle test asserts lockstep.
- **T-0123 (library, proposed)**: `mm fix` per decision 4 — duplicate and
  ceiling repair with dry-run, transaction-envelope writes, and the I9
  detail-rename coupling; tests run it over a genuinely merged board (built
  from the 3.1 reproduction) and over a hand-broken I1/I2 directory, asserting
  check.sh-green before and after.
- **SKILL.md**: a "working with git" section — one board per directory,
  commit after ops, pull-then-merge, the post-merge ritual, and what `git log
  --follow details/<ID>.md` does and does not show (item *line* moves across
  files are not followed; the detail file is the stable per-item history).
- **This plan flips to decided** when T-0121–T-0123 land, exactly as
  plan-id-widths.md flipped when its items shipped.

## 6. Why not the alternatives

- **Lock the counter (server / file lock / exclusive checkout)**: a server
  contradicts "micro"; a file lock only works for people sharing one working
  tree, which is not how git is used; exclusive checkout kills concurrent work.
  The cost of the collision is already just one duplicate ID that a machine
  fixes deterministically.
- **Renumber by branch (per-branch prefixes or ranges)**: violates I2's single
  counter and §3.3.2 rule 1 — the T-0118 decision. The format has exactly one
  ID space per directory, on purpose.
- **Custom merge drivers**: reimplementing three-way merge in another
  implementation, exercised only at merge time, with silent corruption as the
  failure mode. Git's tested merge plus a deterministic post-merge repair is
  strictly more robust.
- **Auto-resolve every conflict (union-merge item lines)**: 3.3/3.4 need
  judgment — which title, which `done` date. A tool that guesses is a tool
  that corrupts; the repair keeps guessing out by construction and refuses ties.

## 7. Definition of done

- Both validators reject a conflict-marker line in any data file, and the
  oracle test agrees over a marker fixture.
- `mm fix` repairs a merged board reproduced from 3.1 — two `T-0043` items,
  one detail file, `next_id` bumped once, losers renumbered with their detail
  files renamed in the same transaction — and check.sh is green before and
  after, with the dry run matching the real run byte for byte on the board.
- A second test repairs a hand-broken directory (same ID in two homes) and
  asserts the tie case refuses instead of guessing.
- SKILL.md documents the post-merge ritual; `git status` after the fix shows
  exactly the intended splices, nothing else.
