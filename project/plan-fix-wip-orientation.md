# Plan — single working column on the board

    Status: draft
    Date:   2026-07-31
    Scope:  project/spec-gui.md, project/spec-tui.md,
            implementations/golang/ (the web UI),
            implementations/typescript/ (the plan only — no UI code exists)
    Items:  T-0072 (in implementations/golang/micro-manager/)
    Revised: 2026-07-31 after a review against the specs and the Go code;
             what changed and why is in the Appendix.

Show every working item in **one board column** instead of one column per
working file. Today `spec-gui.md` §5.5 fixes the board as ready, blocked,
someday, **one `board-column-slot-NN` per working file**, done. A project with
a WIP limit of 3 renders eight columns, three of them skinny and usually
half-empty, for a distinction the format itself calls meaningless: "slots are
interchangeable" (`spec-gui.md` §7.2). Working is one state; the slot is an
on-disk enforcement detail, not a lane.

Specs are normative, and the precedence of the repository AGENTS.md applies:
`spec-file-format.md`, then `spec-tools.md`, then the UI specs. Where this
document appears to contradict one, the spec wins and this plan has a bug.

---

## 1. What does not change

This is a **presentation change only**. Nothing below moves, and every one of
these is load-bearing:

- **The on-disk format.** `working.NN.md` files, one item each, the file count
  IS the WIP limit (I10). `check.sh` and every validator are untouched; there
  is no data migration and no user file is rewritten.
- **The library and CLI contract.** `spec-tools.md` §5.1.8 keeps
  `--start ID [--slot NN]`; without `--slot` the lowest-numbered idle slot is
  used; a full house still fails `WipLimitReached`. `--pause`, `--finish`,
  `--wip` unchanged.
- **The HTTP API.** `spec-gui.md` §4.2 maps `POST …/items/:itemId/start` to
  `--start` and enumerates no request bodies at all beyond the blanket
  `dryRun` rule, so nothing in it changes. A slot stays nameable wherever the
  operation is: scripting clients may still pass one; the board never does.
- **Item identity and state.** Working items still carry `data-slot` on their
  cards (§4, D5), and `data-wip-used` / `data-wip-limit` stay on the board.
- **The WIP dialog.** `dialog-wip-limit` still lists occupants as
  `dialog-wip-limit-slot-NN` — the dialog explains the on-disk constraint,
  which is still slot-shaped (§4, D6).

## 2. The target board model

The state the specs will describe after the change.

**Columns, in DOM order:**

1. `board-column-ready`
2. `board-column-blocked`
3. `board-column-someday`
4. `board-column-working` — **one column**, all working items
5. `board-column-done`

**The working column.** Testid `board-column-working`, class
`mm-column mm-column--working`, title and aria-label `Working`, always
rendered — when no item is in progress it renders with `data-count="0"` and
remains a live drop target, because a start has to land somewhere. It has no
`--add` button: `--add` creates backlog items, so `board-column-working-add`
does not exist (§4, D10).

**Cards.** Identical in every column as today. Within the working column,
cards are ordered by slot number, and `data-position` is the 1-based index in
that order (§4, D2). Working cards keep `data-slot="<NN>"` naming their home
file; `data-item-state="working"` is unchanged. `data-occupied` disappears
from the DOM entirely — WIP fullness already lives on the board element as
`data-wip-used` / `data-wip-limit` (§4, D4).

**Legal transitions (§7.2 rewritten):**

| From | To | Operation | Notes |
|---|---|---|---|
| backlog column | same column | `--move --position N` | reorder |
| ready/someday | blocked | `--block` | MUST prompt for a reason; cancelling aborts |
| blocked | ready/someday | `--unblock` | drops `blocked:` |
| ready/blocked/someday | ready/blocked/someday | `--move --section` | |
| backlog column | working column | `--start` | server picks the lowest idle slot; fails `WipLimitReached` when full (§4, D11) |
| working column | ready/someday | `--pause` | position from drop index |
| working column | blocked | `--pause --section blocked` | MUST prompt for a reason; cancelling aborts (§4, D12) |
| working column | working column | — | **illegal**; there is no working order to rearrange (§4, D3) |
| backlog or working | done | `--finish` | MUST prompt for outcome, default `shipped` |
| done | anywhere | — | **illegal** in v1; reopening is not a specified operation |

The drop→operation payload changes in exactly one place: a drop on the working
column no longer carries a slot number, only the intent to start. One §7.3
attribute changes value without changing meaning: `data-drag-source` is "the
testid of the origin column", so dragging a working card now reports
`board-column-working` rather than `board-column-slot-01`. Slot identity is
carried by the card's `data-slot`, not by the drag.

**Everything else** — the §7.3 drag attribute set, the §7.4 keyboard move
mode (with one fewer target column), §7.5 failure handling including
`dialog-wip-limit` on `WipLimitReached`, §6.2's every-drag-has-a-menu-path
(`item-action-start` still starts to the lowest idle slot), the report, the
item panel with its subtasks, the status bar — is unchanged.

## 3. Why this is the right change

1. **The model says slots don't matter; the board says they do.** §7.2 calls
   slots interchangeable, then §5.5 gives each its own lane. The column layout
   is the one place the UI contradicts the domain model.
2. **Width is the scarcest board resource.** Columns scale with the WIP
   limit: limit 5 renders 8 columns, limit 10 renders 13 — most permanently
   near-empty, since WIP limits exist to stay small.
3. **Idle-slot columns exist only as drop targets.** An empty lane whose sole
   purpose is accepting a drop is what a single always-present working column
   does with a fifth of the DOM.
4. **Cheaper conformance.** One column with a derived order is one set of
   testid/attribute assertions instead of N; every future implementation
   (the TypeScript web client, the TUI) inherits the simpler contract.

## 4. Decisions, with the alternatives considered

- **D1 — one column, testid `board-column-working`.** Named for the state,
  not the mechanism, matching `data-item-state="working"` and
  `--mm-color-state-working` (both unchanged). Alternative: keep per-slot
  columns and restyle — rejected; the problem is the model, not the styling.
- **D2 — working cards sort by slot number.** Deterministic, stable across
  reloads, and free: the order is derived, never stored. Alternatives —
  `started` order (breaks when two items share a start date, and invites a
  "why did it move" bug), manual order (no operation exists to reorder
  working items; inventing one is scope creep).
- **D3 — drops inside the working column are illegal.** There is no working
  order to rearrange, so an intra-column drop means nothing; it is rejected
  with `data-drop-allowed="false"`, `data-drop-reason="Conflict"`, no request
  issued. This subsumes the old slot↔slot row. Alternative: allow drag to
  swap slots — rejected; swaps change no visible state and exist only to
  confuse.
- **D4 — `data-occupied` is removed.** It described per-slot columns, which
  no longer exist. Fullness is `data-wip-used` / `data-wip-limit` on the
  board; emptiness is `data-count="0"` on the column. Alternative: synthesize
  `data-occupied` on the working column — rejected; two sources of the same
  truth drift.
- **D5 — `data-slot` stays on working cards.** It names the item's home
  file, costs nothing, and gives tests a handle on slot identity now that
  columns no longer provide one. Removing it would be gratuitous breakage.
- **D6 — the WIP dialog is untouched.** `dialog-wip-limit` explains the
  on-disk constraint — the limit and its occupants — which is still
  slot-shaped, so `dialog-wip-limit-slot-NN` and the three remedies (finish,
  pause, raise-limit) stay. Alternative: rekey occupants by item — rejected;
  the testid names the slot the occupant holds, and churn without a model
  change is noise.
- **D7 — `--slot NN` survives everywhere except the board.** Library, CLI
  and API keep it; precision remains available to scripts. The board's start
  path — drag or `item-action-start` — always means "lowest idle slot", which
  is what the CLI does without `--slot`.
- **D8 — spec version stays 1.** All four specs are `Status: draft`, and no
  conformant implementation or external suite exists to break. The change
  lands as a draft revision: `Date` bumped, this plan recording the reason.
  A version-bump policy becomes necessary when v1 freezes, not before. Note
  what is being changed, though: §12 items 2 and 5 make the `data-testid` and
  `data-*` surface *the* conformance surface, so this is a conformance-surface
  change — cheap today because no suite exists, expensive the day one does.
- **D9 — the TUI spec changes in lockstep.** spec-tui.md §5.1 fixes the same
  per-slot column list "matching the GUI", so it is edited in the same
  session; the two UI specs must never describe different boards. The TUI is
  unimplemented everywhere, so this costs a paragraph, not code.
- **D10 — the working column has no add button; the done column is left
  alone.** `--add` creates backlog items, so an add affordance on the working
  column would either lie or invent a start-and-add hybrid:
  `board-column-working-add` does not exist. The testid-index note of §5
  item 5 is scoped to the working column **only**, and deliberately says
  nothing about done:
  `templates/partials/board.html:34` renders `board-column-<key>-add`
  unconditionally on every column, so appendix wording that also denied done
  an add button would make the Go template non-conformant the day it landed
  and fail the T-0070 audit for a change this plan is not making. The done
  column's add link is a real oddity and gets its own item.
- **D11 — hover stays optimistic when WIP is full; the server rejects.**
  Dropping a backlog card on a full working column is a *legal transition
  that fails at runtime*, not an illegal target — which is how the old table
  read it too ("fails if occupied or WIP full"). So the column hovers
  `data-drop-allowed="true"`, the drop issues its request, the server answers
  `WipLimitReached`, and §7.5 opens `dialog-wip-limit` with the three
  remedies. This preserves today's behaviour exactly: `static/mm.js` has no
  WIP awareness and never read the `data-occupied` it is now losing.
  Alternative considered — compare `data-wip-used` with `data-wip-limit`
  client-side and hover `data-drop-allowed="false"` with
  `data-drop-reason="WipLimitReached"`, which is §7.3's own example.
  Rejected here because §7.2 then requires the drop to issue no request, and
  the remedy dialog §7.5 promises would never open unless the client learned
  to open it itself. Whoever wants honest hover MUST do both halves and add a
  clause to §7.5 naming a client-side refusal; that is a separate change.
- **D12 — a drag from working into Blocked prompts for a reason.** I5 ties
  `blocked:` to the section and the library enforces it: `PauseRequest`
  documents the requirement and `mm/op_pause_test.go:210` asserts
  `InvalidArgument` when the reason is missing. Today the browser posts a
  reason-less pause — `legality()` maps working→blocked to `pause` while only
  `block` and `finish` prompt (`static/mm.js:421`) — so that drag fails with
  an error toast. §7.2 is being rewritten anyway, so the row is split and the
  requirement stated. The server half already works: the pause handler reads
  a `reason` form value (`internal/web/item.go`). A bug fix carried by the
  rewrite, not new scope.

## 5. Spec edits

Made first, in one commit; both UI specs in the same session (D9).

**`spec-gui.md`** (bump `Date`, keep `Spec version: 1` — D8):

1. **§5.1 attribute table** — `data-slot` row: "working columns and cards" →
   "working item cards". Delete the `data-occupied` row (D4).
2. **§5.5 Board** — column list item 4 becomes "one `board-column-working`
   column for all working items, ordered by slot number"; the example HTML
   replaces the `board-column-slot-01` section with a `board-column-working`
   section (`class="mm-column mm-column--working"`, `data-count="2"`, no
   `data-slot`/`data-occupied` on the section) and gains a sentence fixing
   the card order and the always-rendered-empty rule of §2.
3. **§6.1 coverage table** — the `--start` and `--pause` rows name "the
   working column" instead of "a slot column".
4. **§7.2 transition table** — replaced with the table of §2 above: backlog→
   working is `--start` (no slot named), working→ready/someday is `--pause`,
   working→blocked is `--pause --section blocked` and MUST prompt for a
   reason (D12), working→working illegal, the slot↔slot row gone.
5. **Appendix A of the spec, the testid index** — `board-column-slot-<NN>` →
   `board-column-working`; the `board-column-<key>-*` suffix line notes that
   the working column carries no `-add` (D10). It says nothing about the done
   column — denying done an add button in the index would make the existing
   Go template non-conformant on landing, and that is a separate item.
6. **§5.10 dialogs** — no text change, but confirm the `dialog-wip-limit`
   wording still reads correctly against the new board (it does — D6).

**`spec-tui.md`** (bump `Date`):

7. **§5.1 Board** — the column list item 4 becomes "one Working column for
   all working items, ordered by slot number", and the "Working-slot columns
   additionally show…" sentence moves to the item-row description: working
   *rows* still show the slot number and `started` (that detail survives —
   it was always per-item, not per-column).

`spec-file-format.md` and `spec-tools.md` are untouched (§1).

## 6. Go implementation changes

The board UI in `implementations/golang/internal/web/` is built and tested;
this is a real change, not a paper one. Files, in the order they depend on
each other:

1. **`board.go`** — `buildBoard` drops the per-slot loop and builds one
   working column: collect every item with `State == working` across
   `dir.Slots`, sort by slot number, `data-position` = index in that order,
   testid `board-column-working`, title `Working`. `columnData` loses the
   `IsSlot`/`Occupied`/`Slot` uses for *columns* (the per-item `Slot` in
   `itemData` stays — D5); the `mm-column--slot` class becomes
   `mm-column--working`.
2. **`templates/partials/board.html`** — the column template's slot branch
   (`data-slot`, `data-occupied`, `mm-column--slot`) goes; the working
   column renders through the same generic path as the backlog columns,
   minus the add link (D10). The comment block naming the fixed DOM order
   is updated with the new order.
3. **`static/mm.css`** — `.mm-column--slot` rules rename to
   `.mm-column--working` (the `border-top` in
   `--mm-color-state-working` carries over untouched); the
   `[data-occupied="false"]` dashed/dimmed treatment re-keys to
   `[data-count="0"]` on the working column.
4. **`static/mm.js`** — `legality()`: the `slot-` key prefix becomes the
   single `working` key; the slot→slot row is deleted (working→working is
   caught by the new `from === 'working' && to === 'working'` rejection,
   NOT by the backlog `from === to` move rule — the one place the naive
   table would get it wrong); backlog→working is `start`, working→backlog
   is `pause`. The drop payload loses the `values.slot` block. Two further
   changes fall out of the decisions: working→blocked joins the prompt set at
   line 421, currently `op === 'block' || op === 'finish'`, and posts its
   reason with the pause (D12); no WIP comparison is added, because hover
   stays optimistic and the server rejects (D11). This file holds the one
   permitted client-side copy of the transition table (§2.1); the tests must
   keep it agreeing with the server.
5. **`item.go`** — the start handler keeps reading an optional `slot` form
   value (D7: the API keeps the parameter, and the menu POST is the same
   code path); only the drag caller stops sending it. The pause handler
   already reads a `reason` form value, so D12 needs nothing here. No logic
   change in either.
6. **`templates/partials/dialogs.html`, `dialogs.go`** — untouched (D6).
7. **Tests** — the web suite's slot-column expectations rewrite:
   `board_test.go` (column order list becomes
   `…someday, board-column-working, done`; the `data-slot`/`data-occupied`
   column assertions become card-level `data-slot` and `data-count`
   assertions; add an empty-working-column case), `dragdrop_test.go`
   (slot-to-slot rejection becomes working-to-working; the start drop no
   longer posts `slot`; a full-WIP start drop still issues its request and
   is rejected by the server, per D11; a working→blocked drag prompts and
   posts a reason, per D12 — a regression test, since it fails today), plus
   whatever `item_test.go`, `render_test.go`, `shell_test.go`
   and `errors_test.go` pin about slot columns — an audit of every
   `board-column-slot` and `data-occupied` mention in the package is part
   of the work, not a follow-up.
8. **Docs** — verified, not assumed: grepping
   `implementations/golang/project/` for `slot column`, `per working slot`
   and `board-column-slot` returns nothing, so `architecture.md`,
   `architecture-echo-v5.md` and `plan-gui.md` need no edit. Recorded here so
   the next reader does not go looking.

Verification: `go test ./...` green; the server run against the
repository's real `micro-manager/` directories and eyeballed at limits 1
and 3+; `./check.sh --all` untouched and still green; the T-0070 audit
(templates vs. the spec appendices) re-run after §5 lands — appendix and
templates change together or it correctly fails.

## 7. TypeScript changes — the plan, not code

No TypeScript UI code exists; the change flows through the spec. What gets
edited so the plan never describes the old board:

1. **`project/plan-typescript-implementation.md` §2** — a line recording
   that the board model changed to a single working column on 2026-07-31,
   pointing here. §3.7's drag bullet and the §7 risk table quote the §7.2
   table only generically and need no rewording; verify, don't assume.
2. **Tracking annotations** — when next edited, the detail files for
   **T-0047** (board) and **T-0050** (drag and drop) should note the single
   working column so nobody implements the pre-change spec from a stale
   memory of it. **T-0059** (the appendix audit) inherits the spec's revised
   Appendix A automatically — its whole point is parsing the spec, so no text
   change.
3. Nothing in `packages/`, `testdata/` or `AGENTS.md` references the board
   layout; confirmed by grep, not by memory. `AGENTS.md` does cite "the
   spec-gui.md §7.2 drag-transition table" as the one permitted duplication
   in the client — that reference stays correct, because the table's content
   changes and its identity does not. No edit; do not "fix" it.

## 8. Sequencing and tracking

Order matters: spec first, implementation second, plans annotated last —
the data never changes, so there is no migration window to manage.

1. Land §5's spec edits (one commit, both UI specs).
2. Create a Go tracking item — suggested title "Render working items in a
   single board column", tags `ui,view` — and do §6 under it. It slots into
   the Go GUI's Phase 2/3 boundary without disturbing the phase plan. Put its
   ID in this document's `Items:` header when it exists; every sibling plan
   in `project/` carries real IDs and this one still says `none yet`.
3. Annotate the TypeScript plan per §7 (no item needed; if one is wanted,
   a note on T-0047 at scheduling time suffices).
4. Record the change in the next session file under `project/`.

Nothing is created or renamed in this step by the plan itself — the tracking
entries are proposals, made when the work is scheduled.

## 9. Risks

| Risk | Containment |
|---|---|
| An external conformance suite written against per-slot columns breaks | Everything is `Status: draft`; no such suite exists yet (spec-gui.md §12 is unbuilt). D8 records the reasoning. |
| The Go client-side legality table drifts from the server's | The drag tests assert both ends of every row of §7.2; the working→working row is called out in §6.4 because the naive `from === to` rule gets it wrong. |
| The two UI specs diverge during the edit | One commit, both files (D9); the TUI has no code to desync. |
| Test churn masks a real regression | The Go rewrite edits expectations test-by-test against §2's table; no blanket find-and-replace of testids. |
| The D12 bug fix is lost in the noise of the rewrite | It gets its own regression test named for the transition, not folded into a drag-table sweep — it is the one behaviour here that is broken *today*. |
| A future implementation reads a stale plan | §7 exists precisely for that; the TS plan points here instead of restating the board. |

## 10. Definition of done

1. `spec-gui.md` and `spec-tui.md` describe the §2 board and nothing else.
   `grep -n "slot" project/spec-gui.md` returns exactly four survivors and no
   others: the `data-slot` row in §5.1, "an item in a working slot" in §5.6,
   `dialog-wip-limit-slot-NN` in §5.10, and "the same precedence slot" in
   §9.2 — the last an unrelated sense of the word, which is why this is
   asserted by reading the four, not by grepping for zero.
2. `go -C implementations/golang test ./...` passes with the rewritten
   expectations, and the working column is verified live against the
   repository's own directories at WIP limits 1 and 3+.
3. `./check.sh --all` is green — it never saw the change, which is the
   point: no data moved.
4. `implementations/typescript/project/plan-typescript-implementation.md`
   points here, and T-0047/T-0050 carry the annotation the next time their
   files are touched.
5. This plan's §2 table and `spec-gui.md` §7.2 are identical — compare, do
   not trust.
6. A drag from the working column into Blocked prompts for a reason and
   succeeds (D12), and a full-WIP start drop still reaches the server and
   opens `dialog-wip-limit` (D11).

## Appendix — revision notes, 2026-07-31

(This document has one appendix; "Appendix A", "B" and "C" elsewhere in it
always mean `spec-gui.md`'s.)

The first draft was reviewed against the specs and the Go code before any
work was scheduled against it. Nine changes came out of that review; the
three marked **contradiction** would have produced a wrong spec edit or a
failing audit, and are the reason this appendix exists rather than a quiet
rewrite.

| # | Change | Why |
|---|---|---|
| 1 | **contradiction** — D10 rewritten; §5 item 5 now scopes the testid-index note to the working column only | The draft said the index should record that "the working and done columns" carry no `-add`, while D10 declared the done column out of scope. `templates/partials/board.html:34` renders `board-column-<key>-add` on *every* column, so the index wording would have made the Go done column non-conformant the moment it landed — failing the T-0070 audit for a change this plan explicitly is not making. |
| 2 | **contradiction** — DoD 1's grep assertion replaced with the four named survivors | The draft claimed a post-change `grep -n "slot" project/spec-gui.md` would show only `data-slot`, the WIP dialog and "the API body". It would also show §5.6's "an item in a working slot" and §9.2's "the same precedence slot" — an unrelated sense of the word. As written the check fails on a literal run and teaches the reader to ignore it. |
| 3 | **contradiction** — §1's HTTP API bullet reworded | It cited `spec-gui.md` §4.2 as keeping a slot "in its body". §4.2 is an endpoint table plus one blanket `dryRun` rule; it enumerates no request bodies at all. The guarantee was real (the operation maps to `--start`, whose `--slot` is untouched) but the citation was not. |
| 4 | **new D11** — hover stays optimistic when WIP is full | The draft asked for "WIP-full rejection coverage" in the tests without saying who decides fullness, which makes the test unwritable. Removing `data-occupied` (D4) leaves the client only the board-level counts. Verified that `static/mm.js` has no WIP awareness today, so keeping the server authoritative preserves behaviour exactly; the honest-hover alternative is recorded with the §7.5 wrinkle that makes it a separate change. |
| 5 | **new D12, §2 table row split** — working→blocked prompts for a reason | The draft copied the old row "slot column → backlog column = `--pause`" verbatim. Blocked *is* a backlog column, and I5 requires a reason: `mm/op_pause_test.go:210` asserts `InvalidArgument` without one, while `static/mm.js:421` prompts only for `block` and `finish`. That drag is broken today, and copying the row would have enshrined it. Rewriting §7.2 is the cheapest moment to fix it. |
| 6 | §2 gains a `data-drag-source` sentence | §7.3 defines it as the origin column's testid, so a working card's drag source silently changes from `board-column-slot-01` to `board-column-working`. It is part of the drag contract and a test surface; leaving it unsaid invites an implementer to preserve slot identity there. |
| 7 | §6 item 8 replaced with a verified negative | It sent the implementer to update per-slot wording in `plan-gui.md` and `architecture-echo-v5.md`. There is none. A file list that does not pay out teaches distrust of the lists that do, so the grep result is recorded instead. |
| 8 | §7 item 3 notes the TypeScript `AGENTS.md` §7.2 reference | The claim that nothing in `implementations/typescript/` describes the board is true, but `AGENTS.md` does name the §7.2 table as the client's one permitted duplication. The reference stays valid — content changes, identity does not — and saying so stops a future reader "fixing" a citation that is not broken. |
| 9 | D8 cites §12; §8 step 2 records the item ID; DoD gains item 6; §9 gains a row | Small consistency repairs. §12 items 2 and 5 make the testid and `data-*` surface the conformance surface, which is the strongest support D8 has and it was missing. The header still reads `Items: none yet` with nothing telling anyone to update it. D11 and D12 needed a done-condition and a risk row, or they would live only in the decision list. |

What the review did **not** change, having checked it: every section number
cited here (§5.1's table, §5.5, §5.6, §5.10, §6.1, §7.2–§7.5, Appendix A, and
spec-tui.md §5.1); the `--mm-color-state-working` token that D1 relies on,
which already exists in Appendix B and `mm.css:293`; the `from === to` trap
called out in §6.4, which is real — `static/mm.js:293` returns an allowed
`move` before any slot test, so a naive port would permit working→working;
and the Go file inventory in §6, which matches the code.

Line numbers in this document are pointers as of 2026-07-31, not contracts:
`internal/web/` is under active edit for the report view, and `mm.js` in
particular has already shifted once. Grep for the quoted code, not the line.
