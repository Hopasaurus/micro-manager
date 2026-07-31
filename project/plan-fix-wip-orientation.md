# Plan — single working column on the board

    Status: draft
    Date:   2026-07-31
    Scope:  project/spec-gui.md, project/spec-tui.md,
            implementations/golang/ (the web UI),
            implementations/typescript/ (the plan only — no UI code exists)
    Items:  none yet — §8 proposes the tracking entries

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
- **The HTTP API.** `POST …/items/:itemId/start` keeps accepting a slot in its
  body (`spec-gui.md` §4.2). Scripting clients may still name a slot; the
  board simply never does.
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
| backlog column | working column | `--start` | server picks the lowest idle slot; fails `WipLimitReached` when full |
| working column | backlog column | `--pause` | position from drop index |
| working column | working column | — | **illegal**; there is no working order to rearrange (§4, D3) |
| backlog or working | done | `--finish` | MUST prompt for outcome, default `shipped` |
| done | anywhere | — | **illegal** in v1; reopening is not a specified operation |

The drop→operation payload changes in exactly one place: a drop on the working
column no longer carries a slot number, only the intent to start.

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
  A version-bump policy becomes necessary when v1 freezes, not before.
- **D9 — the TUI spec changes in lockstep.** spec-tui.md §5.1 fixes the same
  per-slot column list "matching the GUI", so it is edited in the same
  session; the two UI specs must never describe different boards. The TUI is
  unimplemented everywhere, so this costs a paragraph, not code.
- **D10 — the working column has no add button.** `--add` creates backlog
  items; an add affordance on the working column would either lie or invent a
  start-and-add hybrid. The appendix's generic `board-column-<key>-add`
  wording gets scoped to backlog columns explicitly. Noticed along the way
  and out of scope: the Go templates render an add link on the *done* column
  too — worth its own item, not folded into this one.

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
   working is `--start` (no slot named), working→working illegal, the
   slot↔slot row gone.
5. **Appendix A testid index** — `board-column-slot-<NN>` →
   `board-column-working`; the `board-column-<key>-*` suffix line notes that
   backlog columns carry `-add` and the working and done columns do not (D10).
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
   is `pause`. The drop payload loses the `values.slot` block. This file
   holds the one permitted client-side copy of the transition table
   (§2.1); the tests must keep it agreeing with the server.
5. **`item.go`** — the start handler keeps reading an optional `slot` form
   value (D7: the API keeps the parameter, and the menu POST is the same
   code path); only the drag caller stops sending it. No logic change.
6. **`templates/partials/dialogs.html`, `dialogs.go`** — untouched (D6).
7. **Tests** — the web suite's slot-column expectations rewrite:
   `board_test.go` (column order list becomes
   `…someday, board-column-working, done`; the `data-slot`/`data-occupied`
   column assertions become card-level `data-slot` and `data-count`
   assertions; add an empty-working-column case), `dragdrop_test.go`
   (slot-to-slot rejection becomes working-to-working; the start drop no
   longer posts `slot`; add WIP-full rejection coverage against the working
   column), plus whatever `item_test.go`, `render_test.go`, `shell_test.go`
   and `errors_test.go` pin about slot columns — an audit of every
   `board-column-slot` and `data-occupied` mention in the package is part
   of the work, not a follow-up.
8. **Docs** — `project/architecture-echo-v5.md` and `project/plan-gui.md`
   describe the board; update any per-slot-column wording they carry.

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
   memory of it. **T-0059** (the appendix audit) inherits the new Appendix A
   automatically — its whole point is parsing the spec, so no text change.
3. Nothing in `packages/`, `testdata/`, or `AGENTS.md` references the board
   layout; confirmed by grep, not by memory.

## 8. Sequencing and tracking

Order matters: spec first, implementation second, plans annotated last —
the data never changes, so there is no migration window to manage.

1. Land §5's spec edits (one commit, both UI specs).
2. Create a Go tracking item — suggested title "Render working items in a
   single board column", tags `ui,view` — and do §6 under it. It slots into
   the Go GUI's Phase 2/3 boundary without disturbing the phase plan.
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
| A future implementation reads a stale plan | §7 exists precisely for that; the TS plan points here instead of restating the board. |

## 10. Definition of done

1. `spec-gui.md` and `spec-tui.md` describe the §2 board and nothing else;
   `grep -n "slot" project/spec-gui.md` shows only card-level `data-slot`,
   the WIP dialog, and the API body.
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
