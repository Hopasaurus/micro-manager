# Research — reducing htmx refresh flicker

    Status: draft
    Date:   2026-08-02
    Scope:  implementations/golang/internal/web, project/architecture.md §4.4/§4.5
    Items:  T-0109 (this spike, report only)

The board sometimes visibly re-renders on refresh. This document finds where
the flicker comes from, explores updating in a more granular way, considers the
alternatives, and recommends a phased order. It is a research report, not a
plan: nothing here is binding until a decision is taken, and the one dependency
addition it suggests needs the same ask-before-adding the htmx vendoring got
(session-003 §4).

Specs are normative. Nothing below changes `spec-gui.md`: its DOM contract,
`data-*` attributes, and testids are untouched by every option here, because
the server keeps rendering the same markup — only what the browser does with it
changes.

---

## 1. How a refresh works today

Every region refetches itself over its normal route and **replaces itself with
`outerHTML`**, on two triggers that always coexist:

| Region | Element | Route | Swap |
|---|---|---|---|
| Board | `[data-testid="board"]` | `GET /p/:id/board?fragment=1` | `outerHTML` |
| Status | `[data-testid="app-status"]` | `GET /p/:id/status` | `outerHTML` |
| Check | `[data-testid="check"]` | `GET /p/:id/check?fragment=1` | `outerHTML` |
| Shell (theme) | `[data-testid="app"]` | `GET /p/:id/shell` | `outerHTML` |

Trigger strings (asserted in `internal/web/sse_test.go:285`):

```
hx-trigger="sse:board from:body, every 30s"
hx-trigger="sse:status from:body, every 30s"
hx-trigger="sse:check from:body, every 30s"
```

Every mutation — menu actions, drag commits (`mm.js` `htmx.ajax(..., target:
"[data-testid='board']", swap: 'outerHTML')`), the panel form, the dialogs —
also swaps the whole board, plus out-of-band swaps for the status bar, the
toast, and overlay dismissal (`templates/board.html` `board-swap`).

The broker (`internal/web/broker.go`) polls the directory fingerprint
(`mm/fingerprint.go`) every 5 s and, on **any** change, publishes three events
at once — `board`, `status`, `check` — because all three regions are computed
from the same lines. `theme` fires separately on a theme-file stamp change.

The fingerprint is one SHA-256 over the file listing plus per-file
`name\0size\0mtime`, including every file under `details/`. It is deliberately
coarse (`fingerprint.go` comment: "not a lock and not a correctness
mechanism").

## 2. Where the flicker comes from

Six distinct sources, in rough order of how often a user meets them.

1. **The unconditional `every 30s` backstop.** An idle tab fetches and swaps
   the board, status, and check views every 30 seconds even when nothing
   changed. 120 board fetches per hour of pure idleness, each one a teardown
   and rebuild of every column and card. This is the guaranteed, periodic
   flicker — the one that happens while the user is *not* doing anything.
2. **Whole-board `outerHTML` replacement.** Every swap removes and recreates
   every card. `:hover` dies mid-hover, focus is dropped to `<body>`, the board
   reflows completely, and CSS effects restart from zero. The cost is paid in
   full even when the refresh changed a single byte.
3. **The mutation double-swap.** A mutation swaps the board from its POST
   response; within 5 s the broker's next poll sees the new fingerprint and
   publishes `board`; the client fetches and swaps the *identical* board a
   second time. Two teardowns around every mutation, and a `data-busy` window
   that stays true through both.
4. **Detail-body edits refetch the board.** The fingerprint covers `details/`
   content. Writing a note or editing a detail body changes nothing a card
   renders (`data-has-detail` is a boolean; it changes only when a detail file
   appears or disappears), yet the whole board is refetched, re-rendered, and
   swapped. This is the most common write in the app, hitting the largest
   fragment.
5. **Event over-broadcasting.** `board`, `status`, and `check` always fire
   together, and there is no way to express "only the ready column changed".
   A change to one item re-renders every region, including a full validation
   pass on the check view (which the 30 s backstop also runs unconditionally).
6. **A refresh during a drag.** The drag state lives in attributes on the
   dragged card (`data-dragging`, `data-move-mode`, `data-drop-*`). Any refresh
   that lands mid-gesture replaces the card and kills the move. Rare — the
   window is a few seconds — but it turns an in-flight drag into a no-op.

The theme shell swap (`sse:theme`) is the one deliberate whole-app replacement,
and it is mostly invisible: the resolved tokens are inlined in the served HTML
(`architecture.md` §4.10), so there is no flash of the previous theme. Focus and
scroll are still lost on a theme edit, but that is a minor case.

## 3. Options

### A. Morphing (idiomorph) — make refreshes invisible

htmx 2.x swaps whole nodes by default but supports **morphing** via extensions:
diff the old and new DOM and apply only the differences, preserving focus,
hover, scroll, and event state on everything that did not change.
`hx-swap="outerHTML"` becomes `hx-swap="morph"`.

Verified against primary sources: the htmx docs describe morph-style swaps as
provided "via extensions", and htmx 2.0.7 has **no morph support in core** (no
morph symbols in the vendored `htmx.min.js`, which is byte-identical to upstream
2.0.7). The idiomorph npm package ships the extension as one self-contained
file — `dist/idiomorph-ext.min.js` bundles the Idiomorph library and registers
the htmx `morph` extension (`defineExtension("morph")`, `Idiomorph.morph`) — so
the vendoring is a single file loaded after htmx core exactly like
`htmx-ext-sse.js`. Caveat: without the extension active on the swapping element
(`hx-ext="morph"`), `hx-swap="morph"` falls back to the **default swap style
(`innerHTML`)**, not the plain `outerHTML` swap — so the swap-verb change
(T-0129) and the `hx-ext` wiring must land together.

What it buys, specifically against §2:

- Source 1 dies: a no-op refresh diffs to nothing and changes nothing on
  screen. The 30 s backstop can stay as the dead-stream guarantee and stop
  flickering.
- Source 3 dies: the second (identical) board swap becomes a zero-cost no-op,
  so mutation echo needs no suppression logic.
- Source 2 dies: unchanged cards keep their DOM node, so hover and focus
  survive; only the changed card re-renders.
- Source 6 mostly dies: unchanged cards keep their node — but note the caveat
  below.

Caveats:

- **Client-only attributes.** idiomorph removes attributes present in the old
  DOM but absent in the new. The server never emits `data-dragging` /
  `data-move-mode`, so a morph *will* strip them — a refresh mid-drag still
  ends the drag. Cheap fix: `mm.js` already knows when a move is in flight
  (its `move` variable); suppress region refreshes while it is non-null. This
  is a two-line change and pairs naturally with E below.
- `data-collapsed` on the someday column is set client-side; the server
  renders it absent. `restoreSomedayState` already re-applies it in
  `htmx:afterSettle`, which fires after a morph too, so behaviour is unchanged.
- Morph cost is a DOM diff per swap. On a large board (many done items) that
  may exceed a plain replace; measure on the largest fixture before and after.
  The diff is server-free — it happens in the browser — so it cannot corrupt
  server truth.
- `sse_test.go` and the fragment-parity tests are unaffected: the server emits
  the same bytes; only the swap verb changes.

This is the highest leverage / lowest risk option. One vendored file, a swap
verb change on four templates, one suppression pair in `mm.js`.

### B. True granular fragments — per-column and per-card routes

The literal reading of "break up the htmx updates": instead of one whole-board
fragment, serve the smallest region that changed.

- New internal fragment routes (same family as `/status` and `/shell`, which
  already exist and are not in the spec's route table):
  `GET /p/:id/column/:key?fragment=1` rendering the existing `column` partial,
  and `GET /p/:id/item/:id/card` rendering the existing `item-card` partial.
- Each column subscribes to its own event: `sse:column-ready`,
  `sse:column-working`, `sse:column-done`, … and each card optionally to
  `sse:item-T-0042`.
- The `column` and `item-card` partials are already the single source of the
  markup (§4.3), so fragment parity — byte-identical standalone vs in-page —
  holds by the same machinery that already proves it for the board.

B is only useful *with* event granularity (C): keeping one `board` event and
splitting only the routes would still refetch everything. B and C are one
option really, presented separately because they can be staged independently.

Costs: new routes, new event names, per-column trigger attributes on the
board, and a more complex broker. The DOM contract does not change — columns
and cards still render with the same testids and attributes, just in smaller
fragments.

### C. Event granularity — per-file stamping in the broker

Today the broker cannot say *what* changed; the fingerprint is one hash
(`fingerprint.go` deliberately does not decompose it). Give the broker a
per-file view:

- `backlog.md` → `column:ready`, `column:blocked`, `column:someday`, `status`
- `working.NN.md` → `column:working`, `status`
- `done.md` / `done-YYYY.md` → `column:done`, `status`
- `details/` **listing** (add/remove) → the card's column(s) — `data-has-detail`
  changes
- `details/<ID>.md` **content** (size/mtime) → **nothing on the board**

That last row is the prize: the most common write — editing a detail body —
currently refetches the entire board for no visible change. It also kills
source 5 (over-broadcasting) and makes the check view revalidate only when a
data file actually changed.

Mechanics: the library's `Fingerprint` stays exactly as the spec's §2.4
defines it (one value, served over the API). The broker needs the per-file
stamps the hash is built from, which is new **library** surface — a small
`mm` helper (e.g. `FileStamps()`) rather than a re-stat in `internal/web`,
because the owned-file set is a library rule. That is a proper new item, not a
web-package hack. The event set grows from the closed `board, status, check,
theme` of `architecture.md` §4.5 to include `column:*` (and `item:*` if B goes
per-card) — an `architecture.md` update, not a spec one.

### D. Kill the unconditional backstop when SSE is healthy

The `every 30s` clause exists so the view converges if the stream dies. The
SSE extension already reconnects with exponential backoff, but the backstop is
the guarantee, and `spec-gui.md` §2.3 requires correctness with polling alone
— so polling must exist, it just need not run *unconditionally*.

- D1 — keep `every 30s` and rely on morph (A) to make it a no-op. Cheapest.
- D2 — conditional polling in `mm.js`: on `htmx:sseClose` / `htmx:sseError`,
  start a 5 s `setInterval` that issues the same fragment fetch through
  `htmx.ajax()`; on `htmx:sseOpen`, stop it. SSE-disabled config still renders
  plain poll triggers (as `architecture.md` §4.5 already specifies), so the
  spec's polling-alone requirement is untouched. D2 removes 120 fetches/hour
  of idle traffic even when the swaps are invisible, and stops the check view
  revalidating every 30 s for nothing.

D2 changes the trigger strings that `sse_test.go` asserts; the test moves from
"string contains `every 30s`" to "polling fallback exists and is wired".

### E. Mutation-echo suppression

The double-swap (source 3) exists because the service's own mutation is
indistinguishable from an external write at the broker. The client *can*
distinguish: record the time the last mutation response settled, and skip an
`sse:board`/`sse:status` event arriving within ~1–2 s of an own mutation — the
POST response already carried the fresh region, so the fetch would return
identical bytes. Per-tab state, so tab A's mutation still refreshes tab B
through the same SSE event. With morph (A) this becomes optional (the second
swap is already invisible), but it still saves a round trip and shrinks the
`data-busy=true` window. Add to the same change: suppress region refreshes
while a drag is in flight (source 6).

### F. Cosmetic — transitions and settle

htmx exposes swap-phase classes (`.htmx-added`, `.htmx-swapping`) and
`hx-settle-delay` for CSS transitions. A short fade-in on `.htmx-added` makes
even a coarse swap feel intentional, and `prefers-reduced-motion` (§11.6) and
test mode (§4.4) already disable it. Presentation only — it does not fix focus
or hover loss, and it can *add* perceived motion if overdone. Optional polish,
cheap, entirely in `mm.css`.

### G. Shell swap on theme edit

Keep the whole-app replacement for `sse:theme` — it is correct and nearly
invisible already. Optionally morph it (A) to preserve focus and scroll on a
theme edit; the shell is the one place morph cost could bite (it carries the
style block and the panel root), so measure rather than assume.

## 4. Rejected and non-options

- **`sse-swap` content push** — the SSE extension can swap event payloads
  directly, but `architecture.md` §4.5 already rejected it: it creates a
  second rendering path (violating §4.3's byte-parity rule) and requires
  `data:`-prefixing every line of every template forever. Granular events over
  the normal routes keep one rendering path. Nothing in this report revisits
  that; it is the correct trade.
- **Client-side state / SPA re-render** — `spec-gui.md` §2.1 forbids trusting
  the client with domain rules and §4.3 requires server-computed state.
  Rejected by construction.
- **Full-page reloads** — strictly worse than the current swaps.
- **Virtualised / lazy columns** — the DOM contract fixes column structure,
  `data-count`, and positional reads (§5.5); virtualisation breaks it and
  breaks drag. No. (`ui.board.doneLimit` is a display-limit choice, not a
  flicker fix.)
- **Throttling the broker poll** — the poll already coalesces (one publish per
  tick); slowing it only widens the window between an external edit and the
  refresh. Not the problem.

## 5. Trade-offs

| Option | Flicker sources killed | New code | Risk | Deps |
|---|---|---|---|---|
| A. Morph | 1, 2, 3 (mostly), 6 (with E) | swap verbs, 1 vendored file | diff cost on big boards | idiomorph.js (ask first) |
| B. Per-column/card routes | part of 2, 5 | new routes, triggers | fragment-parity surface grows | none |
| C. Per-file event stamps | 4, 5 | library helper + broker diff | new library API | none |
| D1. Keep backstop | — | none | none | none |
| D2. Conditional polling | 1 (traffic) | mm.js interval | trigger-string tests change | none |
| E. Echo/drag suppression | 3, 6 | mm.js flags | per-tab logic, small | none |
| F. Transitions | perceived | mm.css | over-animation | none |
| G. Shell morph | focus/scroll on theme edit | one swap verb | diff cost on shell | idiomorph (shared with A) |

A + E together remove sources 1, 2, 3, and 6 — everything except the
over-broadcasting and detail-edit cases (4 and 5), which are exactly what C
fixes.

## 6. Recommendation — phased, and gated on measurement

**Phase 1 — invisible refreshes (A + E, optionally D1).** Vendor `idiomorph.js`
(pinned, licence recorded, ask first per session-003), change the swap verb on
the board, status, and check templates, and add the two suppression flags to
`mm.js` (own-mutation echo, in-flight drag). Keep the 30 s backstop: under
morph it stops flickering. This is small, reversible, touches no routes and no
library, and should remove nearly all *perceived* flicker within a session.

**Measure before Phase 2** — this is `architecture.md` §4.5 open question 3
("If board re-fetches turn out to dominate, a per-column event may be worth
it. Measure before splitting."). Instrument `board?fragment=1` for a session:
bucket fetches by cause (backstop vs `sse:board`), count how many return
byte-identical boards, and time the swap on a fixture with a large done
column. It is a legitimate outcome of this spike that Phase 1 alone is enough
and the coarse events are fine — in which case this report's Phase 2 does not
happen.

**Phase 2 — fewer refreshes (C, then B if the numbers justify it).** Add the
per-file stamps helper to `mm/` (a new item, with the usual round-trip and
no-environment discipline), diff per-file in the broker, publish `column:*`
events, add the per-column fragment routes, and let detail-body edits stop
touching the board at all. This is the "granular updates" the item asks for,
applied to the measured dominant case rather than to every case.

**Phase 3 — polish (F, optionally G).** Transitions in `mm.css` if Phase 1
still feels abrupt; shell morph if a theme edit measurably loses state.

## 7. Touch points and consequences

- **`internal/web/sse_test.go:285`** asserts the `every 30s` trigger strings.
  Phase 1 keeps them; Phase 2 (per-column triggers) and D2 (conditional
  polling) change them deliberately, with the test rewritten to assert the new
  wiring, not just the old string.
- **Dependency policy** (`architecture.md` §3): `idiomorph.js` is a vendored
  JS addition like htmx itself — 0BSD, same authors, but pin the version and
  record the licence in the table. The session-003 rule stands: ask before
  adding it to the repository.
- **`architecture.md` §4.4/§4.5** need updating for whatever lands: swap verbs
  in the htmx-patterns table, the event set if it grows past
  `board, status, check, theme`, and a note in §4.5 that the backstop is a
  no-op under morph rather than a refresh guarantee.
- **`spec-gui.md` needs no change.** Rendering strategy and swap mechanics are
  non-normative there (§1), the DOM contract and `data-*` attributes are
  produced server-side exactly as today, and §2.3's polling-alone requirement
  survives D2 by construction.
- **Fragment parity** (T-0054's guarantee) is unaffected: every option renders
  the same partials through the same handlers; only the client's swap verb or
  the trigger wiring changes.

## 8. Measurement after Phase 1 (T-0131)

Phase 1 shipped in b2d4bb3 (T-0128 idiomorph, T-0129 morph swaps, T-0130 echo
and drag suppression). This section is the gate §6 asked for, and the answer to
`architecture.md` §4.5 open question 3. Measured 2026-08-02 on darwin/arm64,
Chrome, against generated fixtures and a 300-item board.

Reproduce the server-side half with:

```
go test ./internal/web/ -run 'TestRefreshFetch|TestIdleFetchBudget' -v
go test ./internal/web/ -bench 'BenchmarkRefreshFragment|BenchmarkBrokerFingerprintPoll' -benchmem -run '^$'
```

### 8.1 Fetches by cause — the backstop is all of it

| Window | Board fetches | Status fetches | SSE events |
|---|---|---|---|
| idle project, 33 min | — | 65 (one per 30 s) | **0** |
| idle project, 130 s, fresh tab | 4 (t=18,49,79,109) | 4 (t=18,48,78,108) | 0 |

On a project nobody is writing to, **100% of refresh traffic is the
unconditional `every 30s` backstop and 0% is event-driven**. The budget is
2 regions x 120 fetches/hour = **240 fetches/hour per idle board tab**, plus
120/hour for an open check tab.

### 8.2 How much of it changes anything — none of it

`TestRefreshFetchIsIdenticalWhenNothingChanged`: 20/20 refetches of board,
status and check were **byte-identical** on a quiescent directory. This is a
proof rather than a sample — the fragment is a pure function of the files — and
the paired assertion confirms a real write does change the bytes, so the
backstop is not measuring nothing.

### 8.3 Payload — `DoneLimit` already caps it

| `done.md` | board fragment (default `doneLimit:20`) | cards | uncapped (`doneLimit:0`) |
|---|---|---|---|
| 20 | 134,322 B raw / 5,966 B gzip | 32 | same |
| 200 | 134,322 B raw / 5,965 B gzip | 32 | 876,126 B raw / 29,742 B gzip (212 cards) |
| 1000 | 134,322 B raw / 5,966 B gzip | 32 | 4,173,730 B raw / 133,767 B gzip (1012 cards) |

The board fragment **does not grow with the project** at the default config.
"Board re-fetches dominate as the board ages" is already answered by
`ui.board.doneLimit`. Status is ~676 B raw / ~323 B gzip.

The uncapped column is the one real payload risk: a user who sets
`doneLimit:0` is asking for a 4 MB re-render every 30 seconds.

### 8.4 Server cost — capped output, uncapped work

| | `done.md`=20 | =200 | =1000 |
|---|---|---|---|
| `board?fragment=1` | 3.49 ms, 2.1 MB alloc | 4.21 ms, 3.1 MB | 8.66 ms, 7.9 MB |
| `status` | 0.42 ms | 0.96 ms | 3.39 ms |
| broker `Fingerprint()` | 27.4 us | — | 27.4 us |

`DoneLimit` caps the bytes but **not the parse**: the store reads all of
`done.md` per request, so cost grows with the file even though the response is
constant. Status is a 676 B response that costs 3.4 ms at 1000 done items.

The broker poll is **negligible and flat** — 27 us, 720/hour, ~20 ms of CPU per
hour per open project. Per-file stamps (T-0132) would save nothing here.

### 8.5 Swap cost — morph is faster at the default, ~7% slower uncapped

Swap only, response already in hand, median of 11–15 runs, identical content
(the backstop's worst case for a differ: everything to compare, nothing to
change).

| Board | `outerHTML` | `morph` |
|---|---|---|
| 32 cards / 134 KB (default cap) | 11.9 ms (6.7–34.6) | **6.7 ms (6.1–9.7)** |
| 312 cards / 1.29 MB (uncapped) | 69.5 ms (66.8–89.8) | 74.4 ms (67.7–111.4) |

At the default cap morph is **faster and far more consistent** — it skips
destroying and rebuilding 32 subtrees that did not change. The feared diff cost
only appears on a pathological uncapped board, and even there it is ~7%.

### 8.6 A regression found while measuring

Morph leaks an htmx polling chain on every swap that targets the board *from
another element* — i.e. every mutation. Idle traffic becomes
`(1 + mutations) x 120 fetches/hour` and grows for the life of the tab. Filed
as **T-0138** with the reproduction and cause; it is on `main`.

This matters to the numbers above: §8.1 was measured on tabs with no mutations.
A working session is worse than the 240 fetches/hour quoted.

### 8.7 Go / no-go for Phase 2 — **NO-GO**

Phase 2 is C (per-file stamps, fewer and more precise events) then B
(per-column fragment routes, smaller payloads). Against the numbers:

- **C targets events. There are no events.** Zero SSE events on an idle project
  (§8.1); the broker poll it would optimise costs 27 us (§8.4). C's savings on
  the measured dominant case are nil.
- **B targets payload. Payload is already capped** at 6 KB gzip by `doneLimit`
  (§8.3), and B would *increase* request count by splitting one fetch into five.
- **Morph already paid off the swap cost** — faster than the code it replaced at
  the default cap (§8.5). The teardown Phase 2 was partly meant to avoid is gone.

The traffic that actually exists is 240 fetches/hour of provably identical
bytes, caused by the unconditional backstop. Nothing in Phase 2 removes a single
one of them. **D2 removes all of them**, in `mm.js`, with no library change, no
new routes and no growth of the event set.

Decision:

- **T-0132, T-0133, T-0134 — no-go.** Left blocked, to be cancelled unless a
  future measurement on a *write-heavy, multi-writer* project shows event
  traffic that matters. Their premise is not wrong, it is unmet.
- **T-0139 (D2, conditional polling) — the Phase 2 that should happen**, filed
  from §3 D.
- **T-0138 first.** It is a live regression and it inflates exactly the number
  D2 is meant to reduce.

Answering `architecture.md` §4.5 open question 3 directly: **board re-fetches do
dominate, but not for the reason the question assumed.** They dominate because
they are unconditional, not because the event is too coarse. Splitting the event
is the wrong fix; not polling when the stream is healthy is the right one.
