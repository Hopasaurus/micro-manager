# Research: arbitrary column names + user-selected column ordering

    Status:  research, not a spec change
    Scope:   spec-file-format.md, spec-tools.md, spec-gui.md, spec-tui.md,
             implementations/golang
    Trigger: initial use case is a "Review" column between Working and Done

This is exploratory. Nothing here is normative; it exists to lay out the design
space and recommend a minimal-diff path before anyone writes an actual spec
patch.

## 1. Why this is not a small change

The board's columns are not a display-layer concept sitting on top of a
flexible data model. They **are** the data model. Today:

- **State is which file an item lives in**, not a field on the item.
  `implementations/golang/mm/item.go:233-239` says it outright: `State is
  which of the three files an item currently lives in` — `backlog`, `working`,
  or `done`, a closed three-value enum used in every operation (`op_start.go`,
  `op_pause.go`, `op_finish.go`, `op_move.go`, `validate.go`, `write.go`, …).
- **The three files have different schemas.** `backlog.md` items are lines
  under one of exactly three fixed sections (`spec-file-format.md` §5.1: "Exactly
  three sections MUST be present, with these names, in this order"). Working
  items live in frontmatter, one item per file, and the file *is* the WIP slot
  (§5.2). `done.md` items are lines requiring `done` and `outcome` fields,
  grouped under month headings (§5.3).
- **I1 ("one home per ID")** is defined as "exactly one of `backlog.md`, one
  working file (frontmatter `id`), or `done.md`" (§7). This invariant is the
  spine everything else (I3, I4, I6, the transaction rules in `spec-tools.md`
  §7) is built on.
- **The GUI and TUI hardcode five columns** in a fixed DOM order — Someday,
  Ready, Blocked, Working, Done (`spec-gui.md` §5.5, mirrored in
  `implementations/golang/internal/web/board.go:15-16,202-262` and
  `spec-tui.md` §5.1). Column count and order are not configuration; they are
  contract (`data-testid`, routes, drag-and-drop legality in §7.2).

So "arbitrary column names with user-selected ordering" is really two
different asks bundled together, and they have very different costs:

1. **A genuinely open-ended, fully reorderable pipeline** (any number of
   columns, any name, any position, defined per board) — this touches I1,
   I3, I4, I6, every operation's state machine, both DOM contracts, and the
   drag-and-drop legality table. It is a spec-version-bump-sized change.
2. **One additional, named, orderable-among-themselves stage wedged into a
   fixed anchor point of the pipeline** (the stated use case: "between
   Working and Done") — this can be done additively, with zero effect on a
   board that doesn't opt in.

The rest of this note designs (2) and explains why (1) is not worth doing yet.

## 2. What "Review" actually needs to be

Before designing storage, it's worth pinning down the semantics, because they
rule out some of the cheap-looking options:

- An item enters Review after work is judged done-for-now but not yet
  accepted. The natural entry point is "finish working, but not to `done.md`
  yet" — i.e., it should **free the WIP slot**, the same way `--pause` does.
  A "review" that keeps the item parked in its working file would not free
  the slot, which defeats a large part of the point (WIP is scarce; review
  turnaround is often slower than doing the work).
- It is not "not yet started" — it fails `backlog.md`'s own definition:
  "Holds every item not yet started" (§5.1). Putting review items in a fourth
  backlog section would be lexically cheap but semantically wrong, and would
  make `started`/history handling ambiguous (does a reviewed item still carry
  `started`? Yes — losing it would be a regression).
- It is not "done" — `done.md` requires `outcome` (shipped/cancelled/obsolete)
  and its box is `x` (closed, I3). An item under review hasn't shipped. Faking
  it with `outcome:pending` would make every existing done.md consumer (report,
  checker, `--stats`) treat an unfinished item as finished.
- It needs an **order** (it's a queue/list a person triages, like Ready), not
  frontmatter-per-file like a working slot.
- It should be resumable in both directions: sent back to Working (more work
  needed) or forward to Done (accepted), and conceivably back to Backlog
  (rejected, needs rethinking). So its transitions look like a superset of
  what `--pause`/`--finish` already do from Working.

This shape — an **ordered list of open items, entered from Working (and
possibly Backlog), exited to Working, Backlog, or Done** — is structurally
closest to `backlog.md`'s `## Ready` section, not to `done.md` or to a working
slot. That similarity is the basis for the recommendation below.

## 3. Candidate designs

### A. New file per custom stage, declared in `backlog.md` frontmatter (recommended)

Add one optional frontmatter key to `backlog.md`:

```
stages: review
```

- Value is a comma-separated, order-significant list of `SLUG`s (reuse the
  existing `SLUG` token, §3.3, lowercase-start, hyphen-allowed — already used
  for `board`). Order in the list is left-to-right column order.
- **Absent key ⇒ zero custom stages ⇒ byte-identical behavior to today.** This
  is what makes it additive: every existing directory, reader, and writer is
  unaffected. Same trick the format already uses for `id_prefix`/`id_width`
  (§3.3.2 rule 6: "Additive, not a version bump").
- Each declared slug `S` requires a sibling file `S.md` (e.g. `review.md`),
  analogous to `backlog.md`/`done.md` being required once declared. Missing
  file ⇒ I-violation, same pattern as a missing `backlog.md`/`done.md` today.
- `S.md` schema — deliberately a **trimmed copy of `backlog.md`'s Ready
  section**, not a new grammar:

  ```
  ---
  doc: stage
  version: 1
  stage: review
  updated: 2026-08-16
  ---

  - [ ] [T-0042] Fix the deploy script | prio:high | started:2026-08-01
  ```

  One implicit section (no Ready/Blocked/Someday split — nothing here argues
  for sub-sections, and adding them can be a later, additive step if a real
  need shows up). Order is significant, exactly like `## Ready`. Box is
  always open (`" "` — an item in review hasn't closed). Item-line grammar,
  field registry, and parsing rules (§4.2, §6) are reused verbatim.
- **I1 extends by one clause**: "…or exactly one declared stage file." Every
  other invariant either doesn't apply (I3, I6 are `done.md`-specific) or
  extends the same way I already does for backlog (I7's field validation,
  I5-style "well-formed" checks).
- **Placement is fixed at "between Working and Done"** for v1 — multiple
  declared stages order *among themselves* (`stages: qa,review` puts QA left
  of Review), but the whole group sits at that one anchor. This is
  deliberately less general than a fully free pipeline (see §4) and is what
  keeps this additive rather than a rewrite.

Operations (`spec-tools.md`), sketched:

- New op, working name `--send ID --stage SLUG`: moves an item into a
  declared stage from Working or Backlog. Frees the working slot exactly as
  `--pause` does (same "preserve `## Notes` into the detail file" rule, §5.1.9).
- Leaving a stage reuses existing verbs rather than inventing new ones:
  - `--finish ID` also becomes legal from a stage file (today it's legal from
    Backlog or Working, §5.1.10) — stage → Done.
  - `--start ID` also becomes legal from a stage file — stage → Working
    (sent back for more work), taking the lowest idle slot exactly as it does
    from Backlog today.
  - `--pause ID` also becomes legal from a stage file — stage → Backlog
    (rejected, not just "needs more work").
- `--move` stays backlog-only, matching its current documented scope (§5.1.7)
  — a stage file has no sections to move between, only position, which
  `--send` covers to the extent it exists.
- `--list --state review` (or `--state SLUG`) needs `--state`'s vocabulary
  (currently a closed `backlog|working|done|all` enum, §5.1.3) opened up to
  accept declared stage slugs.

GUI/TUI:

- `spec-gui.md` §5.5's fixed column list becomes: Someday, Ready, Blocked,
  Working, **‹declared stages, in `stages:` order›**, Done. This is the one
  place the DOM contract itself changes — today's five `board-column-*`
  testids are enumerated by name; a conforming implementation would need a
  documented pattern for stage columns (`board-column-stage-review`,
  `data-section` analog) rather than a hardcoded list. That's a real spec
  edit, but it is purely additive to the DOM contract (new optional column
  shape), not a change to the five existing ones.
- Drag-and-drop legality table (§7.2) gains rows for stage ↔ Working/Backlog/
  Done, mirroring the operation sketch above.
- Item menu gains `item-<id>-action-send` (or reuses a generic "move to
  stage" affordance).

**Cost**: one new frontmatter key (opt-in), one new file schema (small, reuses
existing grammar), one I1 clause, 2-3 operation extensions (reusing existing
verbs where possible), a documented pattern for stage columns in the two UI
specs, and — in the Go implementation — a fourth `State` value that is no
longer a closed enum but "backlog | working | done | stage:‹slug›", touching
the same files the `grep` above found (`item.go`, `validate.go`, `write.go`,
`op_*.go`, `status.go`, `board.go`). That's real work but it is additive and
scoped; nothing about an *undeclared* board changes.

### B. Fourth backlog.md section

Add `## Review` as a fourth fixed section in `backlog.md`, always present like
Ready/Blocked/Someday.

- Cheapest possible diff to the file format (one more heading, §5.1).
- Rejected: conflicts with backlog.md's own definition ("every item not yet
  started" — a reviewed item has `started` set and came from Working, so it
  is definitionally not a backlog item). Also fails the actual requirement —
  it doesn't free a working slot on its own; whatever operation moves an item
  there still has to behave like `--pause`, so you gain nothing over Option A
  except putting semantically working-adjacent data in the wrong file. And it
  is *not* arbitrary/orderable — it would be one more fixed name in a fixed
  file, the opposite of what was asked.

### C. Flag on done.md items ("soft done")

Reuse `done.md` with a new `outcome` value like `pending-review`, or a new
boolean field `reviewed:false`.

- Rejected: `done.md` is defined as closed, box `x`, and every consumer
  (`--report`, `--stats`, month-archival in §5.6, I6) treats presence there as
  "this happened." Overloading it to also mean "maybe happened" breaks every
  existing reader's assumption for zero savings — you still need new fields,
  new validation, and new UI treatment; you've just put them in the file with
  the strongest "this is final" guarantee in the whole format.

### D. Third `status` value on working files (`status: review`)

Add `status: review` alongside `working`/`idle` in `working.NN.md` (§5.2.2).

- Rejected as the primary mechanism because it does **not** free the slot —
  the item stays pinned to a WIP slot while "in review," which contradicts
  §2's requirement that review capacity be independent of WIP capacity. It
  could be layered *on top of* Option A later for boards that want "review
  without leaving the slot" as a distinct, smaller feature, but it doesn't
  address the stated use case on its own.

### E. Fully general, user-ordered pipeline (arbitrary stage count and position)

Replace the fixed Backlog→Working→Done spine with a directory-declared,
fully ordered list of stages, each with its own file and its own semantics
(WIP-limited or not, terminal or not), e.g.:

```
pipeline: backlog,working,review,qa,done
```

- This is the maximally literal reading of "arbitrary column names with user
  selected column ordering" — no fixed anchor points at all.
- Rejected **for now**: it turns State from a closed 3-value enum into fully
  open data everywhere it appears — I1, I3, I4, I6, `--start`/`--pause`/
  `--finish`'s fixed semantics (which today are meaningful precisely *because*
  there are only three states — "pause" means "back to backlog," "finish"
  means "to done," and those meanings evaporate if the graph is arbitrary),
  the WIP-limit machinery (§5.2.1, which is keyed to "the working state"
  specifically), and both UI specs' column enumeration and drag-legality
  tables. It is a spec-version-bump change, not an additive one, and the
  stated initial use case (one Review column, fixed position) doesn't need
  it. Worth revisiting only if a second and third custom-stage use case shows
  up that actually needs free positioning (e.g., a stage *before* Working, or
  per-board reordering of Backlog/Working/Done themselves) — Option A's
  `stages:` key is deliberately named/shaped so it could grow toward this
  later (e.g. a future `stages_before: `, or generalizing to a `pipeline:`
  key) without breaking boards that only ever set `stages:`.

### F. Collapse Working into Backlog; generalize state to a `stage:` field (recommended, supersedes A)

Raised in discussion, and worth taking seriously as the primary design rather
than a variant: if `working.NN.md` is folded into `backlog.md` and the WIP
limit becomes an explicit config value instead of a file count, the entire
"arbitrary columns, user-ordered" ask is answered by **one field and one
config key**, not by adding a fourth file type on top of three existing ones.

**Shape, and a rename.** A directory becomes: `board.md` (everything not
done — Someday, Ready, Blocked, Working, Review, any custom stage), `done.md`
(unchanged), `done-YYYY.md` / `details-YYYY/` (unchanged), `details/<ID>.md`
(unchanged). `working.NN.md` disappears entirely. This is a bigger cut than
"add a column" — it removes an entire file type and the WIP-slot-as-file idea
that `spec-file-format.md` §5.2.1 and I10 are built around.

**Decided (T-0221): the merged file is renamed from `backlog.md` to
`board.md`, and its prose definition is rewritten.** §5.1's current opening —
"Holds every item not yet started" — becomes false the moment `stage:working`
(and `stage:review`, …) items live there, so it's replaced with something
like "Holds every item not yet done: Someday, Ready, Blocked, Working, and
any board-declared custom stage." Renaming now, while adoption is small
enough that the transition is genuinely cheap, avoids carrying a filename
that actively misdescribes its own contents. One small
naming wrinkle worth flagging rather than solving here: `backlog.md` already
has an unrelated `board:` frontmatter key (§5.1) — the slug used for
cross-board `refs` — so the file is now named `board.md` and separately
carries a field called `board:`. Different namespaces, no functional
collision, but worth a sentence in the eventual spec patch so a reader
doesn't assume the two are the same concept. The rest of this section uses
`board.md` throughout; where a comparison is made to *today's* file, it's
still named `backlog.md` since that's its real, current name.

**The `stage:` field.** Every item line gains a registered field, `stage`,
value a `SLUG`-shaped token (reusing the existing token, not inventing one).
It replaces `## Ready`/`## Blocked`/`## Someday` as the source of truth for
where an item sits. **Decided (T-0217): the headings are dropped, not kept as
non-normative commentary.** An unenforced heading a reader ignores is a
heading that drifts and misleads the next hand-editor — worse than no
heading at all — so `board.md`'s body becomes a flat, unsectioned list of
item lines; a writer MAY still group items physically by `stage:` for
readability, but nothing requires or checks it. Built-in stage values:
`someday`, `ready`, `blocked`, `working`. Everything else — `review`, `qa`,
whatever a board wants — is exactly as first-class as the built-ins. This is
the actual "arbitrary column names" answer: there is no longer a closed list
to extend.

**Stage order.** One new `board.md` frontmatter key:

```
stages: someday,ready,blocked,working,review
```

Comma-separated, order-significant, defines both column order and the set of
valid `stage:` values for the directory (I7 extends: a `stage:` value not in
this list is invalid). **Decided (T-0220): a board that never customizes it
gets a default of exactly `someday,ready,blocked,working`** — reproducing
today's four-column order exactly, so the zero-config *behavior* is unchanged
even though the on-disk *shape* is not (see Compatibility, below).

**Display labels, decoupled from the stored slug.** A direct payoff of moving
away from `## Ready`/`## Blocked`/`## Someday` headings: once nothing in the
file stores a human-readable column name — `stage:` values are machine slugs,
never rendered verbatim — renaming a column becomes a pure frontmatter edit,
touching zero item lines. That wasn't true before this redesign: today's
section names *are* the literal heading text embedded in `## Ready`, and
Option A's file-per-stage idea has the same problem one level up (the slug
doubles as the filename, so renaming it means renaming a file every reader
has to relocate). Add one more optional key:

```
stage_labels: working:Doing,review:Code Review
```

Comma-separated `slug:Label` pairs, split on the *first* colon (the same rule
§4.1 already uses for frontmatter key/value splitting, applied one level
down) — so a label MAY itself contain a colon, but not a comma (the pair
delimiter; a label needing a literal comma is the one thing this can't
express, worth a footnote rather than a blocker). Only slugs worth
overriding need an entry — it's sparse, not a full enumeration:

- **Deliberately a separate key from `stages:`**, not `stage:Label` pairs
  folded into the ordering list. Order and label are independent axes:
  reordering columns shouldn't force restating every label, and aliasing one
  label shouldn't force opting into an explicit order. `id_prefix`/`id_width`
  already set the precedent of keeping independently-optional concerns in
  separate keys rather than one compound one.
- **Applies uniformly to built-in and custom stages.** `working:Doing` is as
  valid as `review:Code Review` — the mechanism doesn't distinguish, which is
  the point: there's no reason "Working" should be harder to rename than
  "Review."
- **Missing entry ⇒ derive from the slug**: title-case, hyphens become
  spaces (`code-review` → "Code Review"). This is what makes the key
  optional and additive rather than something every board must populate —
  a v2 board that predates this key, or that never sets it, renders exactly
  the capitalized-slug labels it would have anyway.
- **Belongs in `board.md` frontmatter, not `config.json`.** `config.json`
  (`spec-gui.md` §9) is explicitly UI-scoped, per-installation, and "outside
  the file-format spec... MUST be ignored by format readers" (§9.1) — right
  for density or theme choice, wrong for something that should look the same
  from every front end pointed at the directory, the same way `project` and
  `board` already do. A label is board identity, not a UI preference.
- **Custom-stage color, resolved (T-0224).** `spec-gui.md` §8.3's theme token
  taxonomy has a *closed* set of five `state.*` color tokens —
  `state.ready state.blocked state.someday state.working state.done` — with
  no slot for a custom stage's color. **Decided: dynamic per-stage tokens are
  available, with a defined fallback to `accent.base`** — a theme MAY declare
  `state.review` (or any other declared `stage:` slug) and, when it doesn't,
  the stage renders in `accent.base` rather than being colorless or an error.
  This is the version of "dynamic `state.<slug>` tokens" that doesn't break
  §8.3's "every token is REQUIRED, a theme missing one inherits the built-in
  default" rule: that rule stays true, unchanged, for the five *fixed*
  tokens; per-stage tokens are a separate, fully optional overlay with their
  own fallback rather than an addition to the required set, so the required
  set never depends on a specific board's `stages:` list.

**WIP limit.** **Decided (T-0219): shaped as a map from the start**, not a
single `wip_limit: N` that would need migrating later — `wip.<slug>: N`,
one key per capped stage, e.g. `wip.working: 3`, and later `wip.review: 2` if
a board ever wants Review capped too. This needs no new lexical machinery:
frontmatter keys are unconstrained beyond "everything before the first `:`"
(§4.1), so a dotted key is already legal today, and the map is really just
"zero or more ordinary flat keys sharing a `wip.` prefix" — consistent with
frontmatter staying a flat string map (§4.1 rule 7), not a nested structure.
A stage with no `wip.<slug>` key is uncapped.

**Decided (§5, decision 10): no built-in exception — every stage,
`working` included, defaults to unlimited when its `wip.<slug>` key is
absent.** This drops the special-casing the open question proposed and keeps
the rule uniform: absence always means uncapped, full stop, for any stage a
board declares. The consequence flagged when this was still open is real but
lands somewhere other than the *default*: a **migration** of an existing
board must still emit an explicit `wip.working: N` (N = that board's current
working-file count) for every board it converts, because the migration has
concrete data to preserve and "silently uncapping every existing board" would
be a real behavior change happening *at migration time*, regardless of what
the format's own default is. Separately, `--init`'s own default (today: one
working file, §5.1.1) is a **tool** default, not a **format** default — the
same relationship the format already has with slot count, which nothing
requires to be any particular number either. `--init` on a fresh v2 board
SHOULD still write `wip.working: 1` explicitly, matching today's out-of-the-
box behavior, even though the underlying format now treats an absent key as
unlimited for every stage uniformly.

Checked against the count of items in the target stage on whichever operation
enters it (`--start` for `working`; whatever generalized "enter a stage"
operation exists for others, §3, Option A's operation sketch is the closest
analogue). This is the one real trade-off worth naming plainly regardless of
key shape or which stages get capped: today the limit is **structurally**
unbreakable for `working` specifically — "starting a fourth item with three
slots is impossible rather than invalid" (§10.7) — because there is no fourth
file to hold it. Under this design every capped stage's limit becomes a
**checked** invariant like every other rule in I1–I10, not a physical
impossibility. That is a small loss of a property the current spec is proud
of, but it is the same kind of check the format already relies on everywhere
else (a `blocked` item without a reason is invalid, not impossible), so it's
consistent with the rest of the design, not a new kind of risk.

**Tickler eligibility, and where a fire lands, both generalize (§5, decisions
11 and 15).** Today `tickler:` is restricted to `## Someday` specifically,
and a fire — one-shot move or recurring spawn — always lands in `## Ready`
(I7, §5.1, §5.3.3). Both are decided, and the landing stage isn't a single
board-wide setting — it's **per source stage**, encoded directly in
`tickler_stages`:

```
tickler_stages: someday->ready
```

Each entry is `SOURCE->DEST`, comma-separated for multiple entries (e.g.
`tickler_stages: someday->ready,triage->working`) — `SOURCE` is a
tickler-eligible stage, `DEST` is where a fire from it lands by default.
**Defaulting to `someday->ready`** — the single pair — when the key is
absent, reproducing today's behavior exactly. `SOURCE` and `DEST` are
independent: a board MAY route two different sources to the same
destination, but a `SOURCE` MUST NOT repeat (one destination per source,
same "repeated key is an error" shape §4.2 rule 6 already applies to a
single item line). Once the key is present, every entry MUST be a complete
`SOURCE->DEST` pair — a bare slug with no arrow is a format error, not an
implicit default; the only implicit default is the whole key being absent.

I7's placement rule generalizes from "MUST sit in `## Someday`" to "MUST sit
in a `SOURCE` named in `tickler_stages`," and `Store.tick`/`--tick`
(`spec-tools.md` §5.3.3) scans every named `SOURCE` instead of only Someday.
`created`'s companion requirement is unchanged — tied to `tickler`'s
presence, not to which stage carries it.

**Decided: the destination is also overridable per item**, with a new
registered field on the item line itself —

```
- [ ] [T-0042] Investigate the flaky test | tickler:mon@08:00 | tickler_dest:review | created:2026-08-01
```

`tickler_dest` is a `SLUG` naming a declared stage, valid only where
`tickler` is valid (same placement rule), and when present overrides that
item's `SOURCE` entry's configured `DEST` for both fire kinds — a one-shot
move and a recurring spawn alike, since it's the *item* being routed either
way, not a property of which kind of schedule it carries. Parsing it needs
the same directory context `stage:` itself already needs (decision 13) —
not a new pattern, the second field to use it.

One edge case worth a sentence, not a full open question: nothing stops a
board from routing a fire (via `tickler_stages` or `tickler_dest`) into a
stage that's also listed in `needs_reason` (below) — a fired item would then
immediately be invalid for lacking the reason the tickler mechanism has no
way to supply. Self-inflicted misconfiguration, not something the format
needs to prevent structurally; a checker warning is enough if this ever
comes up in practice.

`wip.<slug>` keys, `tickler_stages` entries (both halves), `tickler_dest`,
and `stage_labels` keys share one more rule worth stating once rather than
per-key: **any stage slug named outside of `stages:` itself that isn't also
a member of `stages:` is invalid**, the same shape I7 already applies to a
`stage:` value on an item line. A board can't cap, schedule, route, or label
a stage it hasn't declared.

**Where `## Task`/`## Plan`/`## Notes`/`## Blockers` go.** This was the real
design gap in the merge: `working.NN.md` today carries a per-item body
(§5.2.2) that a single item line cannot hold. **Decided (T-0218): that
content moves permanently into `details/<ID>.md` (unconstrained markdown
already, §5.4), and `## Plan` stays structurally parsed there** — not
demoted to unstructured prose. `--note` and `--subtask`/`--subtask-done`
target the detail file directly, creating it on first use, under
conventional headings (`## Notes`, `## Plan`). This removes complexity
rather than adding it: §5.1.9's whole "MUST NOT discard `## Notes` silently,
preserve into detail file by default" carve-out exists only because there
are two places the content could live today; once there's one place, that
rule has nothing left to guard against.

Two consequences worth carrying into the actual spec patch: (1) §5.4's "no
heading is required and readers MUST NOT depend on any" becomes not quite
true anymore — `## Plan`, specifically, when present in a detail file, is now
normative subtask-checkbox syntax (the same "`- [` lines are subtasks, never
items" parsing rule §5.2.2 states for working files today, relocated rather
than dropped). Every other heading in a detail file stays exactly as
unconstrained as before. (2) a detail file stops being purely optional the
moment an item is ever noted or subtasked — `--note`/`--subtask` must be able
to create `details/<ID>.md` on demand, the same way `--pause` already can
today (§5.1.9), just as the *only* path now rather than a fallback.

**Invariants.** A genuine simplification, not just a rename:

- **I1** — "every ID appears in exactly one of `board.md` or `done.md`."
  One clause instead of three.
- **I3** — unchanged (open box in backlog, closed box in done).
- **I4** ("working coherence," today about per-file frontmatter) —
  **disappears**. Its content folds into I7: `started` required when
  `stage:working`, exactly as `reason` is required when its stage is
  listed in `needs_reason` (I5, generalized — see below).
- **I10** (working file set: contiguous slots, uniform width) —
  **disappears entirely**. There is no file set to be well-formed about.

Net: fewer invariants than today, not the same count restructured.

**I5 generalizes too (§5, decisions 14, 17, 18) — the last of the three
built-in-specific rules, and the one that closes out the "does `Stage` need
an enum" question for good.** A new key,

```
needs_reason: blocked
```

comma-separated stage slugs (membership set, order not significant, same
shape as `tickler_stages`'s source list) — **defaulting to `blocked` alone
when absent**, reproducing today's I5 requirement exactly for a board that
never touches the key.

Two more decisions land on top of the key itself:

- **The field is renamed from `blocked:` to `reason:`** (decision 17) — once
  the requirement is board-configurable, keeping a field called `blocked`
  reads oddly the moment a board sets `needs_reason: qa` and the required
  field has nothing to do with being blocked. This also fixes a small
  existing mismatch: `spec-tools.md` §5.2's `--block ID --reason TEXT`
  already names its modifier `--reason`, while today it sets a field called
  `blocked:` — the rename makes the CLI modifier and the field it sets match
  for the first time.
- **`reason:` becomes valid on any stage, required only where listed**
  (decision 18) — I5's other half ("no item elsewhere... carries blocked")
  is dropped rather than carried forward symmetrically. `reason:` behaves
  like `prio` or `tags` now: legal everywhere, sometimes required. This is
  simpler than the placement-restricted design first drafted here, not just
  differently shaped — one validation rule ("required when stage ∈
  `needs_reason`") replaces two ("required when...; forbidden when not...").
  One consequence worth carrying forward explicitly: since the field is no
  longer forbidden outside `needs_reason` stages, there's no format-level
  reason to strip it automatically when an item leaves one — unlike
  `tickler:`, which the format actively drops on exit from `## Someday`
  today (§5.1.7) because it would otherwise be *invalid* to keep. `reason:`
  has the same shape as `tickled:` instead, which the format explicitly
  keeps as historical after `tickler:` is consumed (§5.3.3) — so leaving a
  `needs_reason` stage SHOULD leave `reason:` in place rather than drop it,
  by the same logic. Not separately asked, but it's the reading the
  "optional everywhere" decision leads to on its own, not a new judgment
  call layered on top.

With this, **`Stage` has zero built-in-specific behavior left anywhere in
the format** — WIP capping, tickler eligibility and landing, and the reason
requirement are all board-declared config now, not Go-level special cases.
`someday`, `ready`, `blocked`, and `working` are purely the *default values*
of `stages:`/`tickler_stages`/`needs_reason`, exactly as ordinary as any
custom stage a board might declare. This supersedes the earlier lean in the
Decisions table (row 12) toward keeping one named constant for `blocked` —
there's no longer a hardcoded rule for it to anchor.

**Transactions.** `--start`, `--pause`, and moving between any two
backlog-side stages become **single-file, in-place edits** of `board.md` —
flip the `stage:` field, nothing else moves. Only `--finish` (→ `done.md`)
and archiving stay two-file transactions. `spec-tools.md` §7 rule 4's
crash-ordering dance ("write the working file first, so a crash duplicates
rather than destroys") currently exists because `--start` touches two files;
under this design it no longer needs to, for that operation.

**Ordering within a stage, resolved (T-0222).** Working item cards are
currently "ordered by slot number" (§5.5, §5.1 TUI) — a stable, meaningful
order that disappears along with the slot concept. **Decided: plain file
order is sufficient**, for `stage:working` exactly as for every other stage —
no separate ordering rule (e.g., by `started` date) is needed. `--start`
appends to the end of the `stage:working` run, matching how items already
land at the bottom of `## Ready` by default today (§5.1.2).

**Report and stats scope, resolved (T-0223).** `--report --include-wip` and
`--stats` currently key off "the working state" as a fixed, singular concept
(§5.1.11, §5.3). **Decided: generalize.** They gain `--include-stage=SLUG`
(repeatable, one board-declared stage per occurrence), and `--include-wip`
survives as a named shorthand for `--include-stage=working` rather than being
retired — existing scripts and muscle memory keep working, and a board with a
Review stage can additionally ask for `--include-stage=review` without a
second, differently-shaped flag being invented for it.

**GUI/TUI.** Every column — Someday, Ready, Blocked, Working, Review, any
custom stage — becomes the *same* rendering rule: filter `board.md` by
`stage:`, render in `stages:` order. No more slot-number special-casing
(`data-slot`, "ordered by slot number," §5.5) for the Working column alone.
Only Done stays structurally different, because it genuinely is — a dated,
archivable, append-only record, which nothing here proposes changing. This is
a bigger, more pleasant unification of the two UI specs than Option A, which
left Working as a special case and wedged one more column type in beside it.

**Compatibility.** This is **not** additive the way Option A was, and it's
worth being direct about that rather than stretching the word: dropping
`working.NN.md`, renaming `backlog.md` to `board.md`, and repurposing its
section grammar is a format version bump (`version: 2`), not a new optional
key. A v1 reader pointed at a v2 directory does not degrade gracefully — it
has no `backlog.md`/`working.NN.md` to satisfy the Appendix B emptiness test
or I10, and, since headings are dropped, no `## Ready`/`## Blocked`/
`## Someday` sections to satisfy §5.1 either. That has to be an explicit,
loud version mismatch (§9's existing rule for a `version` a reader doesn't
implement), not a silent misread.

Given how little is in the wild against this format today, that cost is
real but small — and **revised (§7): migration is not a one-time throwaway
script.** It's built as the first entry in a general, versioned migration
mechanism `--migrate` grows into, reusable for whatever the *next* version
bump turns out to be. §8 designs the mechanism; the v1→v2 step itself is:
rename `backlog.md` to `board.md` and rewrite its opening description; for
each `working.NN.md` with `status: working`, emit a `board.md` item line
with `stage:working` and the same fields (dropping `slot` — there is no
slot concept to preserve, initial order falls out of slot order for
stability); move any `## Notes`/`## Plan`/`## Blockers` content into
`details/<ID>.md` under matching headings, creating the file if needed;
delete the working file. `status: idle` files just get deleted.

## 4. Recommendation

**Option F**, not Option A. Option A was designed under the constraint of
zero disruption to any existing directory; given how few directories exist
today and that a migration script doesn't need to be maintained, that
constraint is worth relaxing in exchange for a design that is simpler *and*
more general — one field and one config key instead of a fourth file type
bolted onto three existing ones, fewer invariants instead of the same count
restructured, and every non-Done column rendered by one rule instead of
Working staying a permanent special case.

Option A is left in §3 rather than deleted: it's the fallback if this project
ever needs to add a column to a board with real installed data that can't be
casually migrated, and its shape (declare an ordered list of stage names,
insert a small item-line-shaped file or field per stage) is the same idea
Option F reaches for anyway — it just pays for staying additive by keeping
Working, and the file-per-stage split, as permanent special cases.

Renaming a column (this session's follow-up ask) is a natural extension of F,
not a bolt-on: because `stage:` is a stable machine slug never rendered
verbatim, an optional `stage_labels: slug:Label,…` frontmatter key aliases the
display text with zero touches to any item line. See "Display labels,
decoupled from the stored slug" under §3F. Notably, this specific capability
is much *more* expensive under Option A, where the slug is also the filename
(`review.md`) every reader has to locate — renaming there means either
renaming a file or adding the same kind of alias indirection on top, whereas
under F it falls out of the redesign already adopted.

## 5. Decisions made

| # | Question | Decision | Task |
|---|---|---|---|
| 1 | Do `## ` section headings survive in `backlog.md`? | Dropped. `stage:` is the sole source of truth; no non-normative heading kept. | [T-0217](../implementations/golang/micro-manager/details/T-0217.md) |
| 2 | Where do `## Plan` subtasks live? | Move to `details/<ID>.md`; `## Plan` stays structurally parsed there, not demoted to prose. | [T-0218](../implementations/golang/micro-manager/details/T-0218.md) |
| 3 | WIP-cap key shape? | A map from the start — `wip.<slug>: N` per capped stage — not a bare `wip_limit` that would migrate later. | [T-0219](../implementations/golang/micro-manager/details/T-0219.md) |
| 4 | Default `stages:` value? | Exactly `someday,ready,blocked,working`, matching today's four columns and order. | [T-0220](../implementations/golang/micro-manager/details/T-0220.md) |
| 5 | `backlog.md`'s name and self-description? | Renamed to `board.md`; "Holds every item not yet started" rewritten to name every non-done stage. | [T-0221](../implementations/golang/micro-manager/details/T-0221.md) |
| 6 | Tie-break ordering within `stage:working`? | Plain file order — same rule as every other stage, no slot-derived ordering. | [T-0222](../implementations/golang/micro-manager/details/T-0222.md) |
| 7 | How do `--report --include-wip`/`--stats` reference stages? | Generalize to repeatable `--include-stage=SLUG`; `--include-wip` stays as a shorthand for `--include-stage=working`. | [T-0223](../implementations/golang/micro-manager/details/T-0223.md) |
| 8 | How does a custom stage get a theme color? | Dynamic per-stage `state.<slug>` tokens, optional, falling back to `accent.base` when a theme doesn't set one. | [T-0224](../implementations/golang/micro-manager/details/T-0224.md) |
| 9 | Go `State`/`Stage` type shape? | Confirmed: two separate concepts, not one field. `State` narrows to a closed enum naming *which file* (`board`/`done`) — no longer three values. `Stage` is a distinct, non-enum `string`-based type, meaningful only when `State` is board-side, validated at runtime against that directory's declared `stages:` rather than a fixed Go const set. Flagged as real, non-trivial implementation work, not a drop-in rename. | [T-0225](../implementations/golang/micro-manager/details/T-0225.md) |
| 10 | `wip.working` when absent? | Uncapped, same as every other stage — no built-in exception. Migration and `--init` still emit an explicit value where today's behavior needs preserving; that's a tool/migration default, not a format default. | — (chat, follow-up to T-0225) |
| 11 | Does tickler eligibility generalize beyond Someday? | Yes — a new `tickler_stages: <slugs>` key names which stages MAY carry `tickler:` and get scanned by `--tick`, defaulting to `someday` alone when absent. | — (chat, follow-up to T-0225) |
| 12 | Do built-in stages still need typed Go constants? | **Superseded by decision 14 below** — at the time, `blocked`'s reason requirement was still hardcoded, so one constant for it seemed likely to survive. Once that generalized too, the answer firmed up to **no per-stage enum, no exceptions**. | — (chat, follow-up to T-0225) |
| 13 | Does `Stage` parsing need directory context? | Confirmed yes, reusing the `id_prefix`/`id_width` pattern (§3.3.2) rather than inventing a second one. | — (chat, follow-up to T-0225) |
| 14 | Does `blocked`'s reason requirement generalize? | Yes — a new `needs_reason: <slugs>` key, defaulting to `blocked` alone. Closes out decision 12: `Stage` now has zero built-in-specific behavior anywhere in the format. | — (chat, follow-up to §6.1) |
| 15 | Does the tickler's landing stage generalize, and how? | Two mechanisms: `tickler_stages` becomes `SOURCE->DEST` pairs (per-source default, defaulting to `someday->ready`), plus a new per-item `tickler_dest` field that overrides the default for that one item. | — (chat, follow-up to §6.2) |
| 16 | Does `StateBacklog` rename to track T-0221? | Confirmed yes — renames alongside the `board.md` file rename, in the same pass. | — (chat, follow-up to T-0225) |
| 17 | Does the `blocked:` field itself get renamed? | Yes — to `reason:`. Also fixes an existing mismatch: `spec-tools.md`'s `--block --reason TEXT` modifier already named itself `--reason` while setting a field called `blocked:`; now they match. | — (chat, follow-up to decision 14) |
| 18 | Does `reason:` stay placement-restricted like `blocked:` was? | No — it becomes valid on any stage, required only where `needs_reason` lists it. Simpler than the placement-restricted design first drafted: one validation rule instead of two. | — (chat, follow-up to decision 14) |
| 19 | Is migration a one-time throwaway script? | **Revised: no.** It's the first entry in a general, versioned, chained `--migrate` mechanism (§7), meant to be reused by whatever the next format version bump turns out to be. | — (chat, revises §3F "Compatibility") |
| 20 | Implementation sequencing? | Specs updated first (all four spec-*.md files), then the Go implementation — with the Go implementation's own live board (`implementations/golang/micro-manager`) kept operational throughout via the phased rollout in §7, not by maintaining permanent dual-version support. | — (chat) |
| 21 | How long do v1 fixtures stick around? | Kept for as long as v1 support is kept — one paired decision, retired together, not by attrition. Not a fixed number or location; that's left to whoever writes the migration step's tests. | — (chat) |

Each decision's reasoning and its knock-on consequences are folded into the
relevant paragraph of §3F or §7 above, not just recorded here. Decisions
10-20 were answered directly rather than through a board task, so they carry
no `T-` link.

## 6. Open questions before writing an actual spec patch

None remain. The last two — whether `blocked:` gets renamed, and whether it
stays placement-restricted — are resolved as decisions 17-18 (§5). Every
question raised since T-0225 (§5, decisions 9-18) traced back to the same
"is `Stage` still special-cased anywhere" thread, and it closes clean: it
isn't. This section is kept, empty, as the place the next round of design
questions lands — the pattern established across §5-§6 (resolve here first,
fold the result into §3F's prose, only then close it out) held for three
full rounds and is worth keeping rather than collapsing away.

## 7. Migration: a general mechanism, not a one-off

**Revised (§5, decision 19).** The v1→v2 transition designed in §3F
("Compatibility") was originally scoped as a throwaway script, on the
reasoning that adoption is small enough not to need more. On reflection,
this is the format's *first* real structural version bump — `id_prefix`/
`id_width` was explicitly designed to be additive, never one (§3.3.2 rule
6) — so it's the right moment to design how migration should work in
general, not just get this one transition done. `--migrate` already exists
as an optional operation (`spec-tools.md` §5.3) doing a grab-bag of small
historical fixups; this generalizes it into a real mechanism the *next*
version bump reuses too, rather than reinventing.

### 7.1 The mechanism

**A directory's format version is `board.md`'s `version:` field** — the
same file that already carries `next_id`, `project`, `id_prefix`/
`id_width`, and (per §3F) `stages:`/`stage_labels:`/`wip.<slug>`/
`tickler_stages`/`needs_reason`. It's the one file every directory has had
since v1 and the natural single source of truth for "what shape should the
rest of this directory be in."

**Migration steps are a registered, ordered chain**, each one a `(from, to)`
version pair with its own transform — not a single hardcoded v1→v2 function.
`spec-tools.md` documents the chain as a running table (a migration history,
the same idea as a database migrations folder), so a reader sees every
version transition the format has ever had in one place. Entry one:

| From | To | Changes |
|---|---|---|
| 1 | 2 | `backlog.md`→`board.md`; `working.NN.md` folded in via `stage:`; `blocked:`→`reason:`; new config keys `stages`, `stage_labels`, `wip.<slug>`, `tickler_stages`, `needs_reason`; new item field `tickler_dest`; I4/I10 retired, I1/I5/I7 generalized. |

**`--migrate` runs the full chain**, current version to latest, in one
invocation — a directory two versions behind (nobody has run `mm` in a
while) upgrades through every intermediate step without a person needing to
know they existed. Each step validates before the next begins, so a chain
never runs step 2 against a directory step 1 left invalid.

**Every step is transactional**, reusing `spec-tools.md` §7's existing
discipline rather than inventing a parallel one: read every file, build the
new model in memory, validate it against the *target* version's invariants,
write atomically, report exactly what changed (`--dry-run` shows this
without writing, as every mutation already must, §3.4). A step touching
most of the directory's files at once is exactly the case §7's crash-
ordering rules exist for.

**`--migrate` SHOULD warn on uncommitted changes** in a git-tracked
directory, recommending a commit first. Cheap insurance that reuses git
rather than inventing a backup mechanism the format doesn't otherwise need —
consistent with how heavily this project already leans on git elsewhere.

**Longevity policy, so old-version support doesn't become a permanent
maintenance tax:** a conforming implementation MUST fully support the
*current* version's full operation set, and MUST be able to migrate any
older version forward — keeping every step function in the chain
indefinitely is cheap, it's a one-shot data transform, not a parallel
implementation. It is **not** required to support full day-to-day mutation
(`--add`/`--start`/`--pause`/`--finish`/…) against an unmigrated directory
forever — those SHOULD refuse with a clear "this directory is version N,
run `--migrate`" error naming the current version, rather than either
silently operating in old semantics forever or failing unhelpfully.
Read-only operations (`--list`, `--show`, `--check`) MAY continue working
against an old version at an implementation's discretion — reading is cheap
to keep, writing-with-all-its-invariants is what gets expensive to maintain
twice.

**Decided (§5, decision 21): v1 fixtures are kept for exactly as long as v1
support is kept, as one paired decision, not two.** "v1 support" here means
the `1→2` step staying in the maintained chain — keeping it costs little (a
one-shot transform, per the policy above), so in practice this reads as
"indefinitely," but it's stated as a condition rather than "forever" on
purpose: the day this project ever *does* decide v1 support isn't worth
carrying anymore (say, v1 boards are long gone and the chain has grown to
v1→v2→v3→v4), the fixture(s) that exist solely to regression-test the `1→2`
step retire in the same deliberate act, not by separate accident or neglect
later. Until that explicit decision is made, the fixture(s) stay — see §7.2
for what they are.

### 7.2 Rollout sequencing for this migration specifically

**Decided (§5, decision 20): specs first, then the Go implementation.**
`spec-file-format.md`, `spec-tools.md`, `spec-gui.md`, and `spec-tui.md` all
get patched to describe v2 and the migration mechanism before any Go code
changes, matching this project's own stated convention that the spec leads
implementation for anything touching invariants.

**The Go implementation's own board (`implementations/golang/micro-manager`)
is live and in daily use** — this whole design was worked through against
it, via the same `mm` binary being modified. Keeping it functioning
throughout is a sequencing discipline, not a permanent architecture:

1. Build v2 read/write/validate and the `1→2` migration step in the library,
   developed and tested against `sample-data/` and new v2 test fixtures —
   not against the live board. The binary in daily use for the live board
   during this phase is whatever already works today; in-progress v2 code
   isn't swapped in until it's proven.
2. Test the migration step itself against a *copy* of the live board (a
   branch or scratch checkout) before running it for real, since it's the
   one operation that touches nearly every file in the directory at once.
3. Once (1) and (2) are solid: commit any pending live-board work, then run
   `mm --migrate` against `implementations/golang/micro-manager` itself as
   one deliberate, reviewable checkpoint — not a side effect of an unrelated
   code change. Git makes this revertable if something's wrong.
4. From that point, day-to-day board management continues through the new
   v2-supporting binary normally, per the longevity policy above.

**Decided (§5, decision 21; mechanism at §7.1): keep one or more v1
fixtures, retired only alongside an explicit future decision to drop v1
support**, not left to attrition once every real board has moved on. A
copy of the pre-migration state of `implementations/golang/micro-manager`
(captured right before step 3 above runs) is the natural first one — it's
real data the `1→2` step was actually designed against, not a synthetic
minimal case. Whether to also keep one of `sample-data/`'s boards
un-migrated on purpose, and exactly where the fixture(s) live
(`testdata/`, alongside `sample-data/`, or their own directory), is left to
whoever writes the migration step's tests — the decision made here is that
fixture(s) exist and persist, not their exact number or location.

## 8. What this note does *not* decide

This is research, not a plan. Before implementing:

- Draft the actual spec patch text for all four `spec-*.md` files — this
  note has settled every substantive design question it raised, but no
  spec file has been touched yet. §7.1's migration-history table is written
  in the shape it should take inside `spec-tools.md`, not as a diff.
- Write the `1→2` migration step and its test fixtures alongside the spec
  patch, not after — per §7.1, it's the thing that proves the new shape
  actually round-trips real `working.NN.md` content (notes, subtasks,
  blockers) without loss, and per §7.2 it has to be solid before it ever
  touches the live board.
- Pick the fixture(s)' exact number and location (§7.2 leaves this open,
  only that they exist and persist) when writing the migration step's tests.
