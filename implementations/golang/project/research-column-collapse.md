# Research — generalizing column collapsing to every stage

    Status: draft
    Date:   2026-08-21
    Scope:  implementations/golang/internal/web, mm/config.go, spec-gui.md §5.5/§9.3
    Items:  T-0239 (this spike, report only)

`spec-gui.md` already describes column collapsing as a generic, per-stage
affordance (§5.5: "Every column MAY be collapsed, not only Someday"; §9.3:
`ui.board.collapsedStages`, a list). The Go implementation still hard-codes
the whole mechanism to the literal stage `someday`, left that way
deliberately during T-0230/T-0238 (`internal/web/board.go`'s
`buildBoardV2` carries a comment: "generalizing which stages get it... is
a client-preference-persistence design of its own... not a rendering
question"). This report finds exactly what is and is not generic today,
and recommends a shape for closing the gap.

`TestAuditTestids` does not currently require this: its `patternInstances`
table pins `board-column-<slug>-toggle` to exactly one required instance
(`board-column-someday-toggle`), with the comment "only someday carries
one" — matching spec-gui.md §5.5's own wording that offering the toggle on
every column or only some is an implementation's choice. So there is no
spec violation today, only an unfinished generalization.

## 1. What is already generic — no change needed

- **CSS** (`internal/web/static/mm.css:280-303`): every rule is keyed off
  `.mm-column[data-collapsed="true"]`, not a someday-specific class or ID.
  A collapsed column of any stage renders correctly the moment the
  attribute is present.
- **Drag-and-drop onto a collapsed column** (`mm.js`'s `isCollapsed`,
  `bodyAt`, `hoverTarget`, shipped for T-0152): all read
  `.closest('.mm-column[data-collapsed="true"]')` generically. A drop
  landing on any collapsed column already resolves to that column's body
  and lands at the bottom, with no stage name anywhere in the logic.
- **The view-model field**: `columnData.Collapsed bool`
  (`internal/web/board.go:47`) is already a plain per-column field, not a
  someday-only one. Nothing about its type or the template's use of it is
  special-cased beyond the population and rendering gates described below.

## 2. What is hard-coded to `someday` specifically

- **`board.go`'s population gate** — both `buildBoard` (v1, line ~237) and
  `buildBoardV2` (line ~313) only set `col.Collapsed` when
  `key/stage == "someday"`; every other column's `Collapsed` stays its
  zero value regardless of client state.
- **`somedayCollapsed(c)`** (board.go:363) reads one fixed header,
  `X-Someday-Collapsed`, as a single boolean — there is no per-stage
  concept in its signature at all.
- **`board.html`'s toggle button** (line 48) and **`data-collapsed`
  attribute** (line 42) both gate on `eq $c.Key "someday"` literally; no
  other column ever gets either.
- **`mm.js`** (`somedayCollapsed()`, the click handler, `restoreSomedayState`,
  lines 751-790): one localStorage key,
  `mm:someday-collapsed:<project>`, storing a single `'true'`/`'false'`
  string; one hard-coded toggle selector,
  `[data-testid="board-column-someday-toggle"]`; one hard-coded column
  selector, `[data-testid="board-column-someday"]`; one header name.
  None of this can address a second column without being rewritten to
  hold a *set* of stages rather than one flag.
- **`mm/config.go`**: `BoardConfig.ShowSomeday bool`
  (line 162) and its reader, `boolean("ui.board.showSomeday", ...)`
  (line 539), predate the spec's rename. `spec-gui.md` §9.3 documents only
  `ui.board.collapsedStages` (a list) today and says explicitly it is
  "renamed and generalized from version 1's boolean `showSomeday`" — the
  boolean is not a variant to keep supporting, it is what this list
  replaced. No shipped config in this repository's own `sample-data/` or
  `micro-manager/` sets `ui.board.showSomeday` (`grep` over both trees
  found no hits), so there is no real back-compat data to migrate.
- **`board_test.go`** (lines 61-104) asserts the current someday-only
  shape directly, including a check that the `ready` column carries *no*
  `data-collapsed` attribute at all (line 94) — that assertion is
  correct today and would need to flip once every column carries the
  attribute.

## 3. The one real design question — how the client encodes "which stages"

Today's mechanism is a boolean threaded two ways: an HTTP header
(`X-Someday-Collapsed: true|false`) so a server render is born in the
right state (avoiding the T-0150 flicker), and a localStorage entry so the
preference survives a reload. Generalizing to N stages means both carry a
*set* of stage slugs instead of one flag. Three shapes considered:

- **A. Comma-separated list**, e.g. `X-Collapsed-Stages: someday,review`
  and `localStorage['mm:collapsed-stages:<project>'] = 'someday,review'`.
  Matches the codebase's own existing comma-list convention for
  `tags:`/`TAGLIST` (`spec-file-format.md`, and the same word appears in
  this project's own skill doc) — no new encoding idiom for a reader to
  learn, trivial to parse/serialize on both sides
  (`split(',').filter(Boolean)` / `.join(',')`), and a header value stays
  a single readable line for anyone inspecting requests in the browser's
  dev tools.
- **B. JSON array** in both the header and localStorage. No real benefit
  over A here — stage slugs are already comma-list-safe (they cannot
  contain a comma; they are validated identifiers per
  `spec-file-format.md`'s `stages:` grammar) — and it adds
  `JSON.parse`/`JSON.stringify` plus error handling on every request for
  no expressive gain.
- **C. One header per stage** (`X-Someday-Collapsed`,
  `X-Review-Collapsed`, ...). Rejected: stages are a *directory choice*
  (`stages:` in `board.md`'s frontmatter), so the set of possible headers
  is unbounded and per-project — the client would need the directory's
  own stage list just to know which headers to send, coupling a generic
  mechanism to per-project schema in exactly the way §5.5's "no column is
  structurally more collapse-worthy than another" is trying to avoid.

**Recommendation: A.** One header, `X-Collapsed-Stages`, carrying a
comma-separated list of stage keys (empty string or header absent means
none collapsed); one localStorage key per project,
`mm:collapsed-stages:<project>`, holding the same encoding. `somedayCollapsed(c)`
becomes something like `collapsedStages(c) map[string]bool` (or a
`func(c *echo.Context) func(key string) bool`), read once per render and
consulted per column in place of the current single comparison.

## 4. `ui.board.collapsedStages` (config) vs. the live header — not in tension

§9.3's wording is "lists which columns render collapsed by default" —
this is the same relationship version 1's `showSomeday` boolean already
had to the header/localStorage mechanism: a **server-side default** for a
session with no client preference recorded yet, not a competing source of
truth. T-0150 already established the precedent that the *live* value is
always client-supplied so a refresh cannot flicker; nothing here reopens
that decision. The one new piece of wiring is that a fresh page load with
an empty localStorage set should seed the initial per-project value from
`ui.board.collapsedStages` (today it can only ever seed "someday, via
`ShowSomeday`'s default of `true`") — `mm.js` already reads
`data-project-id` off the root element for its localStorage key; the
config's default list would need to reach the client the same way the
theme tokens already do (inlined into the served HTML,
`architecture.md` §4.10), e.g. a `data-default-collapsed-stages`
attribute on the board root, read once when localStorage has no entry for
the project yet.

## 5. Which columns should offer the toggle

Spec leaves it to the implementation ("MAY offer the toggle on every
column or only some"). Nothing found while tracing the drag/drop and CSS
layers gives a technical reason to exclude any column — `done` already has
its own, orthogonal `doneLimit`/`?done=all` truncation mechanism (§5.5),
and collapsing is a pure display toggle on top of whatever the column
already renders, not a truth `doneLimit` needs to agree with. `working`
has no v1 slot-count coupling to collapsing either. Recommendation: offer
the toggle on **every** column — it is the option that actually matches
this task's own title ("generic to all columns") and needs no per-stage
carve-out list to maintain, versus special-casing which stages "deserve"
one.

## 6. Shape of the implementation (if this is taken up)

Mechanical, and precedented by this session's own generalizations
(per-stage WIP rows in T-0238, per-stage tickler destinations in
T-0230) — no new architecture, just extending the existing per-column
loop to one more field:

1. `mm/config.go`: replace `BoardConfig.ShowSomeday bool` with
   `CollapsedStages []string`; reader becomes a comma-list parse under
   `ui.board.collapsedStages`, default `["someday"]` (preserves today's
   out-of-the-box behavior). `spec-gui.md` §9.3 already documents this
   exact key and shape — no spec change needed, only catching the Go side
   up.
2. `internal/web/board.go`: `somedayCollapsed(c)` →
   `collapsedStages(c) map[string]bool` (parses `X-Collapsed-Stages`);
   both `buildBoard` and `buildBoardV2`'s per-column loops set
   `col.Collapsed = collapsedStages(c)[key]` unconditionally instead of
   gating on `key == "someday"`.
3. `internal/web/templates/partials/board.html`: drop the
   `{{if eq $c.Key "someday"}}` gates on both the toggle button and the
   `data-collapsed` attribute; button testid becomes
   `board-column-{{$c.Key}}-toggle` for every column, matching the
   pattern spec-gui.md's Appendix A already names generically.
4. `internal/web/static/mm.js`: generalize `somedayCollapsed()` to read
   the project's comma-list from `mm:collapsed-stages:<project>` and
   return a `Set`; generalize the click handler to match
   `[data-testid$="-toggle"]` under `.mm-column`, deriving the stage key
   from the column's `data-column`/`data-testid` rather than a literal
   string; generalize `restoreSomedayState` to loop every
   `.mm-column` and apply the set instead of touching one hard-coded
   element. Seed the initial (no-localStorage-yet) set from the board
   root's default-collapsed-stages data attribute (§4).
5. Tests: `board_test.go`'s someday-only assertions extend to cover a
   second stage collapsing independently (and the previous "ready never
   carries `data-collapsed`" assertion inverts to "every column carries
   it"); `audit_test.go`'s `patternInstances["board-column-<slug>-toggle"]`
   grows to require every rendered stage's toggle, not just someday's;
   `dragdrop_test.go`'s `TestCollapsedColumnsResolveADrop` needs no
   change — it already asserts against the generic selector.

Nothing here touches `mm/` beyond the config struct — this is entirely an
`internal/web` + config change, no library API, no new routes, no new
spec text.

## 7. Recommendation

Take up the shape in §6 as a follow-up implementation task. It is small,
mechanical, and fully precedented by generalizations already shipped this
session for other per-stage GUI surfaces; the one decision this report
was actually blocking on (§3's encoding, and §5's toggle scope) is
resolved above. Filed as a follow-up rather than folded into this report,
matching this repository's own precedent (T-0109 stayed report-only; its
recommended work shipped under separate task IDs).
