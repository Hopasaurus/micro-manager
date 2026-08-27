# micro-manager — user interface specification

    Spec version: 2
    Date:         2026-08-20
    Status:       draft
    Depends on:   spec-file-format.md (v2), spec-tools.md (v2)
    Covers:       web GUI (normative); the TUI is specified in spec-tui.md

This document specifies the micro-manager user interface. It is language- and
framework-agnostic: several independent implementations are expected, and a
single external test suite must be able to drive all of them without
modification.

That last requirement is what makes this document unusually prescriptive about
markup. DOM structure, `data-testid` values, `data-*` state attributes, CSS
custom property names, and URI routes are **contract**, not implementation
detail. An implementation may choose any language, framework, and rendering
strategy; it may not choose different locators.

---

## 1. Scope

Normative: the view inventory, the DOM contract, routes, the HTTP API, drag and
drop semantics, theming, configuration, and the recent/favorites lists.

Non-normative: visual design beyond what tokens express, framework choice,
rendering strategy (server-rendered, client-rendered, or hybrid), and packaging.

Out of scope: the file formats (`spec-file-format.md`) and the operation
semantics (`spec-tools.md`). This document specifies how a user reaches those
operations, never what they mean. Where behavior appears to differ, the tools
spec wins.

## 2. Architecture

### 2.1 The UI service links the library

Per `spec-tools.md` §2.1, the UI is a **service binary with the library compiled
in**. It MUST NOT invoke the `mm` CLI, spawn it as a subprocess, or parse its
output. Every operation goes through the same library functions the CLI calls.

```
    +-------------------------------+
    |          UI service           |
    |   +-----------------------+   |        +-------------------+
    |   |       library         |   |<------>|  browser client   |
    |   +-----------+-----------+   |  HTTP  +-------------------+
    +---------------|---------------+
                    |
           +--------v---------+
           |  markdown files  |
           +------------------+
```

The browser client is not trusted with domain rules. Validation, WIP-limit
enforcement, and legality of a move are decided by the library on the server.
The client MAY replicate rules to disable controls early, but MUST treat the
server's answer as authoritative, and MUST recover correctly when an optimistic
update is rejected.

### 2.2 Two front ends over one core

The web GUI is specified here in full. The TUI, a lighter keyboard-driven front
end, is specified in `spec-tui.md` and summarized in §13. Everything shared —
the operation set, the semantic colour tokens, the keyboard move model,
configuration, recent and favorites — is specified once **here** and referenced
from there rather than duplicated.

### 2.3 Freshness

The service MUST expose the library's fingerprint (`spec-tools.md` §2.4) so a
client can detect changes made by the CLI or a text editor. Clients MUST poll
`GET /api/v1/projects/:projectId/fingerprint` at a bounded interval (default 5s,
configurable) or subscribe to `GET /api/v1/events`. Implementations MUST work
correctly with polling alone; an event stream is an optimization.

When the fingerprint changes, the client MUST re-read and re-render, and MUST
surface a non-blocking notice if the change affected an item the user is
currently editing.

### 2.4 The tickler service

The service MAY run `Store.tick` (`spec-tools.md` §5.3.3) on a configurable
interval so scheduled someday items fire without a CLI cron. It is opt-in
through the system config key `tickler.interval` (a duration like `1m`; absent
or `null` means off, the default). A process that mutates the board on its own
initiative must not start quietly — the same principle as `spec-tools.md`
§5.3.1's "nothing archives on its own initiative".

- The service runs tick per directory it holds, on its own clock. `tick` is
  date-granular and idempotent across overlapping runners (§5.3.3), so a UI
  service and a CLI cron may tick the same board at once.
- The client learns that something fired on the next fingerprint poll (§2.3);
  a dedicated SSE event is a MAY.
- The service MUST log what fired — item, kind, spawned ID — a mutating
  background process needs an audit trail.

## 3. Identity and addressing

### 3.1 `projectId`

A micro-manager directory is addressed in URIs by a stable derived identifier,
never by raw path.

```
projectId = lowercase(hex(sha256(canonicalPath)))[0:12]
```

`canonicalPath` is the absolute path with symlinks resolved, no trailing
separator, encoded UTF-8 in NFC. The algorithm is fixed so that a test fixture
computes the same id against every implementation.

Implementations MUST NOT accept a filesystem path in a URI position. A path
arrives only through configuration, the recent/favorites lists, or an explicit
open action.

### 3.2 `itemId`

The item ID verbatim from the data spec: an ID in the directory's declared
grammar — the default is `T-0042` (spec-file-format.md §3.3.2) —
case-sensitive, used unaltered in routes and testids.

### 3.3 `themeId`

`[a-z0-9][a-z0-9-]{0,63}`, unique within a theme library. A library file MAY
reuse the id of a built-in theme, which shadows it rather than duplicating it
(§8.7).

## 4. URI routes

Routes are contract. An implementation MUST serve exactly these paths, MUST NOT
require a different prefix, and MUST NOT add a route that shadows one below.

### 4.1 View routes

| Route | View | Notes |
|---|---|---|
| `/` | Home | Recent and favorites (§10). |
| `/projects` | Project list | Everything discovery finds, grouped by scan root (§5.3, §9.5). |
| `/p/:projectId` | — | MUST redirect (302) to `/p/:projectId/board`. |
| `/p/:projectId/board` | Board | Default working view (§5.5). |
| `/p/:projectId/item/:itemId` | Item panel | Over the board, not a page swap (§5.6). |
| `/p/:projectId/new` | Add item | Panel in the same position as the item panel. |
| `/p/:projectId/report` | Report | §5.7. |
| `/p/:projectId/check` | Validation | §5.8. |
| `/p/:projectId/settings` | Project settings | §5.9. |
| `/settings` | System settings | §5.9. |
| `/settings/theme` | Theme editor | §8. |
| `/settings/themes` | Theme library | §8.8. |
| `/settings/themes/:themeId` | One theme | §8.8. |
| `/about` | Version info | MUST show UI spec version, tools spec version, format spec version. |

Query parameters on `/p/:projectId/board`, all OPTIONAL and combinable:

| Parameter | Values |
|---|---|
| `stage` | any declared `STAGE` slug (`spec-file-format.md` §5.1.1), repeatable |
| `state` | `board` \| `done` \| `all` |
| `prio` | `high` \| `med` \| `low` |
| `tag` | tag name, repeatable |
| `q` | free-text search |
| `done` | `all` — render every done item, ignoring `ui.board.doneLimit`. Without it the done column is capped at the limit (§5.5). |

On `/p/:projectId/report`: `period`, `since`, `until`, `group-by`,
`include-wip`, `include-stage` (repeatable), `include-backlog`,
`include-archives` — same vocabulary and precedence as `spec-tools.md`
§5.1.11.

Rules:

1. Every view state a user can reach MUST be addressable. Opening an item,
   filtering a board, and choosing a report period all change the URI.
2. Loading a URI directly MUST produce the same state as navigating to it.
3. Back and forward MUST restore prior state, including open panels.
4. An unknown `projectId` MUST render the not-found view with HTTP 404, never a
   redirect to `/`.

### 4.2 API routes

JSON over HTTP under `/api/v1`. The version prefix is mandatory.

| Method | Path | Operation (`spec-tools.md`) |
|---|---|---|
| GET | `/api/v1/health` | liveness, no auth |
| GET | `/api/v1/projects` | discovery / `--find`; `?refresh=true` forces a rescan |
| POST | `/api/v1/scan` | re-run discovery, returns `DiscoveryResult` |
| GET | `/api/v1/scan` | last `DiscoveryResult` including `scannedAt` and `partial` |
| POST | `/api/v1/projects` | `--init` |
| GET | `/api/v1/projects/:projectId` | directory summary |
| GET | `/api/v1/projects/:projectId/fingerprint` | §2.3 |
| GET | `/api/v1/projects/:projectId/items` | `--list` |
| POST | `/api/v1/projects/:projectId/items` | `--add` |
| GET | `/api/v1/projects/:projectId/items/:itemId` | `--show` |
| PATCH | `/api/v1/projects/:projectId/items/:itemId` | `--edit` |
| DELETE | `/api/v1/projects/:projectId/items/:itemId` | `--remove`, requires `force=true` |
| POST | `/api/v1/projects/:projectId/items/:itemId/move` | `--move` |
| POST | `/api/v1/projects/:projectId/items/:itemId/start` | `--start` |
| POST | `/api/v1/projects/:projectId/items/:itemId/pause` | `--pause` |
| POST | `/api/v1/projects/:projectId/items/:itemId/finish` | `--finish` |
| POST | `/api/v1/projects/:projectId/items/:itemId/block` | `--block` |
| POST | `/api/v1/projects/:projectId/items/:itemId/unblock` | `--unblock` |
| POST | `/api/v1/projects/:projectId/items/:itemId/note` | `--note` |
| GET | `/api/v1/projects/:projectId/detail/:itemId` | detail file body |
| PUT | `/api/v1/projects/:projectId/detail/:itemId` | write detail body |
| GET | `/api/v1/projects/:projectId/report` | `--report` |
| GET | `/api/v1/projects/:projectId/check` | `--check` |
| GET·PUT | `/api/v1/projects/:projectId/wip` | `--wip` |
| GET·PUT | `/api/v1/projects/:projectId/config` | §9.3 |
| GET·PUT·DELETE | `/api/v1/projects/:projectId/theme` | §8 |
| GET·PUT | `/api/v1/config` | §9.2 |
| GET·PUT·DELETE | `/api/v1/theme` | system theme |
| GET | `/api/v1/themes` | theme library |
| POST | `/api/v1/themes/import` | §8.8 |
| GET | `/api/v1/themes/:themeId/export` | §8.8, `Content-Disposition: attachment` |
| GET·PUT | `/api/v1/recent` | §10 |
| GET·PUT | `/api/v1/favorites` | §10 |
| GET | `/api/v1/events` | SSE, OPTIONAL |

Every mutating endpoint MUST accept `dryRun: true` and return the change set
without writing, mirroring `--dry-run`.

The add (`POST /items`) and edit (`PATCH /items/:itemId`) endpoints accept the
Wake-up group's control fields — `tickler-kind`, `tickler-date`,
`tickler-weekday`, `tickler-ordinal`, `tickler-monthday`, `tickler-time`
(§5.6) — and the server composes the `tickler:` value from them, setting or
removing the field in the same transaction as the rest of the form. The
grammar lives in one place: the server composes, the library validates on
write, and a composed value the library rejects is `InvalidArgument` (400) —
the controls cannot produce one, a hand-crafted request can.

### 4.3 Error mapping

Library errors (`spec-tools.md` §6.3) map to HTTP status codes as follows. The
body MUST carry the error `code` verbatim so clients branch on the name, not the
status.

| Library error | HTTP | Notes |
|---|---|---|
| `NotFound` | 404 | |
| `Ambiguous` | 409 | Body lists candidates. |
| `InvalidArgument` | 400 | |
| `Conflict` | 409 | |
| `WipLimitReached` | 409 | Body carries `wipUsed`, `wipLimit`, and occupants. |
| `PreconditionFailed` | 412 | |
| `InvariantViolation` | 422 | Body carries the violation list. |
| `Concurrent` | 409 | Client MUST re-read before retrying. |
| `VersionMismatch` | 409 | Body names both versions; client SHOULD offer the migration action of §5.9. |
| `Io` | 500 | |

Error body:

```json
{ "ok": false,
  "errors": [ { "code": "WipLimitReached", "message": "...",
                "id": "T-0042", "file": "board.md" } ] }
```

### 4.4 Test mode

When the service starts with `MM_UI_TEST=1`, or a request carries `?mm-test=1`,
implementations MUST:

1. disable all transitions and animations;
2. disable auto-dismissing toasts (they persist until dismissed);
3. set `data-test-mode="true"` on the app root;
4. keep every other behavior identical.

Test mode MUST NOT change layout, locators, or which operations are permitted.

## 5. Views and the DOM contract

### 5.1 Conventions

**Locators.** Every element named in this document MUST carry the exact
`data-testid` given. Values are case-sensitive kebab-case, except where an item
ID or project ID is interpolated, which appears verbatim (`item-T-0042`).

An implementation:

- MUST NOT rename, omit, or conditionally drop a required `data-testid`;
- MUST NOT reuse a required testid for a different element;
- MAY add additional testids, prefixed `x-`, for its own tests.

**Classes.** Presentational classes MUST use the `mm-` prefix and BEM-ish shape:
`mm-block`, `mm-block__element`, `mm-block--modifier`. Class names are fixed so
that a theme's supplementary CSS is portable across implementations. Tests
SHOULD locate by `data-testid` and MAY assert on classes only for styling
conformance.

**State.** Dynamic state MUST be exposed as `data-*` attributes, never through
class names alone, so tests never depend on styling decisions.

| Attribute | Values | Applies to |
|---|---|---|
| `data-state` | `loading` \| `ready` \| `empty` \| `error` | any container that loads |
| `data-busy` | `true` \| `false` | app root; `false` means quiescent |
| `data-item-state` | `board` \| `done` | item cards |
| `data-stage` | any declared `STAGE`, absent for a done item | items and board columns |
| `data-prio` | `high` \| `med` \| `low` \| `none` | item cards |
| `data-tags` | comma-separated `TAGLIST`, empty if none | item cards |
| `data-collapsed` | `true` \| `false` | any collapsible `board-column-*` (§5.5) |
| `data-outcome` | `shipped` \| `cancelled` \| `obsolete` | done items |
| `data-has-reason` | `true` \| `false` | item cards |
| `data-needs-reason` | `true` \| `false` | board columns — whether this column's stage is in `needs_reason` |
| `data-tickler` | the item's `tickler:` `SCHEDULE`, absent otherwise | item cards on a tickler-eligible stage |
| `data-has-detail` | `true` \| `false` | item cards |
| `data-wip-used` / `data-wip-limit` | integers | board; and any column whose stage carries a `wip.<slug>` cap |
| `data-theme-name` / `data-theme-source` | name; `project` \| `system` \| `builtin` | app root |
| `data-scanned-at` | `TIMESTAMP` | discovery views |
| `data-project-id` / `data-project-name` | id; `project` frontmatter value | app root |

`data-busy="false"` is the quiescence signal an external suite waits on. It MUST
be `true` while any request is in flight and `false` only when the view fully
reflects server state.

### 5.2 App shell

Present on every route.

```html
<div data-testid="app" data-busy="false" data-theme-name="…"
     data-theme-source="project" data-project-id="…" class="mm-app">

  <header data-testid="app-header" class="mm-header">
    <a data-testid="brand" class="mm-brand" href="/">
      <img data-testid="brand-logo" class="mm-brand__logo" alt="">
      <span data-testid="brand-name" class="mm-brand__name">…</span>
    </a>

    <button data-testid="project-switcher" class="mm-project-switcher"
            aria-haspopup="listbox" aria-expanded="false">…</button>
    <div data-testid="project-switcher-menu" role="listbox" hidden>
      <div data-testid="project-switcher-favorites" role="group">…</div>
      <div data-testid="project-switcher-recent" role="group">…</div>
      <a data-testid="project-switcher-all" href="/projects">…</a>
    </div>

    <input data-testid="search-input" class="mm-search" type="search">

    <nav data-testid="app-nav" class="mm-nav">
      <a data-testid="nav-board"    href="/p/…/board">…</a>
      <a data-testid="nav-report"   href="/p/…/report">…</a>
      <a data-testid="nav-check"    href="/p/…/check">…</a>
      <a data-testid="nav-settings" href="/p/…/settings">…</a>
    </nav>
  </header>

  <main data-testid="app-main" class="mm-main" data-state="ready">…</main>

  <footer data-testid="app-status" class="mm-status">
    <span data-testid="status-wip">…</span>
    <span data-testid="status-counts">…</span>
    <span data-testid="status-check">…</span>
  </footer>

  <div data-testid="toast-region" role="status" aria-live="polite"></div>
  <div data-testid="dialog-root"></div>
</div>
```

The current nav item MUST carry `aria-current="page"`.

`status-check` MUST reflect the last validation result and MUST carry
`data-violations` with the count, so a suite can assert cleanliness without
opening the check view.

### 5.3 Projects list

`/projects` renders everything discovery finds (§9.5), grouped by scan root.

```html
<section data-testid="projects" data-state="ready" data-count="7"
         data-partial="false" data-scanned-at="2026-07-29T09:14:00Z">
  <section data-testid="projects-root-0" data-path="/home/u/code" data-count="4">
    <!-- project-card-<projectId>, as in §5.4 -->
  </section>
  <button data-testid="projects-rescan">…</button>
</section>
```

`data-partial="true"` MUST be set when the walk hit `maxResults` or `timeoutMs`,
and the view MUST say so visibly — a truncated scan is indistinguishable from a
missing project otherwise.

### 5.4 Home

`/` — recent and favorites (§10).

```html
<section data-testid="home" data-state="ready">
  <section data-testid="favorites-list" data-count="3">
    <article data-testid="project-card-<projectId>" data-project-id="…"
             data-favorite="true" class="mm-project-card">
      <h3 data-testid="project-card-name">…</h3>
      <span data-testid="project-card-path">…</span>
      <span data-testid="project-card-wip">…</span>
      <button data-testid="project-card-favorite-toggle" aria-pressed="true"></button>
    </article>
  </section>
  <section data-testid="recent-list" data-count="10">…</section>
  <button data-testid="project-open">…</button>
  <button data-testid="project-init">…</button>
</section>
```

Empty lists MUST render the container with `data-count="0"` and
`data-state="empty"` rather than omitting it.

### 5.5 Board

`/p/:projectId/board` — the primary view and the drag-and-drop surface.

**Columns are dynamic, one per entry in the directory's declared `stages`**
(`spec-file-format.md` §5.1.1), in that order, followed always by
`board-column-done` — the one column that stays structurally last, because
`done.md` is a genuinely different, terminal file (§5.1.6 there). A board
that never customizes `stages` renders the familiar four ahead of Done —
Someday, Ready, Blocked, Working — but nothing in this document hardcodes
that list any longer; a fifth declared stage (`review`, say) is a fifth
`board-column-*` section rendered in its declared position, with no
different markup shape than the built-in four.

**Testid pattern: `board-column-<slug>`**, uniformly — `board-column-someday`,
`board-column-review`, and so on. There is no separate naming scheme for a
custom stage; every column, built-in or not, is addressed the same way.
Column *label* text comes from `spec-file-format.md` §5.1.2 (`stage_labels`,
or the derived title-case default) — never the raw slug.

```html
<section data-testid="board" class="mm-board"
         data-wip-used="1" data-wip-limit="3" data-state="ready">

  <section data-testid="board-column-someday" class="mm-column"
           data-stage="someday" data-count="3" data-collapsed="false"
           role="list" aria-label="Someday">
    <header data-testid="board-column-someday-header" class="mm-column__header">
      <button data-testid="board-column-someday-toggle" class="mm-column__toggle"
              type="button" aria-label="Toggle Someday column">&gt;</button>
      <h2 data-testid="board-column-someday-title" class="mm-column__title">Someday</h2>
      <span data-testid="board-column-someday-count">3</span>
      <button data-testid="board-column-someday-add">…</button>
    </header>
    <div data-testid="board-column-someday-body" class="mm-column__body">
      <!-- item cards -->
    </div>
  </section>

  <section data-testid="board-column-ready" class="mm-column"
           data-stage="ready" data-count="7"
           role="list" aria-label="Ready">…</section>

  <section data-testid="board-column-working" class="mm-column"
           data-stage="working" data-count="1"
           data-wip-used="1" data-wip-limit="3"
           role="list" aria-label="Working">…</section>

  <!-- a declared custom stage renders with identical structure -->
  <section data-testid="board-column-review" class="mm-column"
           data-stage="review" data-count="2"
           role="list" aria-label="Code Review">…</section>

  <section data-testid="board-column-done" data-count="12" role="list">…</section>
</section>
```

`data-wip-used`/`data-wip-limit` on the board root refer to the `working`
stage specifically, kept for the common "how full is my plate" glance a
dashboard wants at the top level. **Any column whose stage carries a
`wip.<slug>` cap** (`spec-file-format.md` §5.1.3) — `working` or otherwise —
carries the same two attributes on the column itself; a column with no cap
carries neither.

A capped column also renders the pair **visibly**, as `board-column-<slug>-wip`
(`used/limit`) immediately left of the column's add control — not just as data
attributes for a script to read. Its color is the column's own `color.state.
<slug>` token (`accent.base` fallback, §8.3), the same one the column border
already uses, so the badge reads as *that column's* count; once `used` reaches
`limit` it switches to the required `feedback.danger` token instead, since the
column is now refusing new entries. **A column with no cap renders no badge.**

**Every declared stage's column carries an add control** (`board-column-<slug>
-add`), `working` included — `--add --stage working` is a legal operation
(`spec-tools.md` §5.1.2) like any other declared stage, so the column that
represents it is no exception. (Version 1's four fixed columns are a partial
exception: `board-column-working` there has no add control, because version 1
has no `--add --stage working` operation at all — an item reaches Working only
via `--start`. This is the one place the two versions' Working columns differ.)
When the column's stage is WIP-capped and already full, the add control MUST
still be present but disabled, carrying `data-reason="WipLimitReached"` —
the same present-but-disabled convention `spec-gui.md` already requires of an
item's own action menu, rather than hiding the control outright.

Item card, identical in every column:

```html
<article data-testid="item-T-0042" class="mm-item"
         role="listitem" tabindex="0" draggable="true"
         data-item-id="T-0042" data-item-state="board" data-stage="ready"
         data-prio="high" data-tags="infra,ci" data-has-reason="false"
         data-has-detail="true" data-position="3">
  <h3 data-testid="item-T-0042-title" class="mm-item__title">…</h3>
  <span data-testid="item-T-0042-id" class="mm-item__id">T-0042</span>
  <span data-testid="item-T-0042-prio" class="mm-item__prio">high</span>
  <ul data-testid="item-T-0042-tags" class="mm-item__tags">
    <li data-testid="item-T-0042-tag-infra">infra</li>
  </ul>
  <span data-testid="item-T-0042-detail-indicator" hidden></span>
  <button data-testid="item-T-0042-menu" aria-haspopup="menu">…</button>
</article>
```

`data-item-state` is `board` or `done` (`spec-tools.md` §6.1's two-value
`State`); `data-stage` carries the item's `stage:` and is absent for a done
item. `data-has-reason` reports whether the item carries `reason:`
(`spec-file-format.md` §5.1.5) — true on any stage now, not only one whose
`needs_reason` requires it; a client that wants to flag *missing* a required
reason compares `data-has-reason` against the column's own knowledge of
whether its stage is in `needs_reason`, carried as `data-needs-reason` on
the column element.

`data-position` is the 1-based index within its column and MUST be kept accurate
after every reorder — it is how a test asserts ordering without reading text.
Ordering within every column, `working` included, is plain item order
(`spec-file-format.md` §5.1.6) — there is no slot-derived order to fall back
to. Every declared column MUST be rendered even when empty (`data-count="0"`).

**A card on a tickler-eligible stage carrying `tickler:`** (a stage named as
a `SOURCE` in `tickler_stages`, `spec-file-format.md` §5.1.4) additionally
carries `data-tickler="<schedule>"` on the article and a next-fire badge —
this is no longer someday-specific:

```html
<span data-testid="item-T-0042-tickler" class="mm-item__tickler"
      data-next="2026-08-10">next Mon 08:00</span>
```

The badge text is computed server-side with `Schedule.next(today)`
(`spec-tools.md` §6.1) on every board render: a recurring schedule shows the
weekday name of the next fire plus its time — `next Mon 08:00`, or `next Mon`
when the schedule carries no time; a one-shot shows its date — `next
2026-09-01`, with the time appended when it carries one; a schedule whose
next fire is `null` — already due, the next tick fires it — reads `due`.
`data-next` carries the computed next-fire date and is absent when it is
`null`. The badge is the affordance — visible text, never a colour-only or
tooltip-only hint (§5.1).

**Every column MAY be collapsed, not only Someday** — `stages` is a directory
choice, so no column is structurally more collapse-worthy than another
anymore. Any `board-column-<slug>` whose header carries a
`board-column-<slug>-toggle` button supports it, with the same behavior
version 1 defined for Someday specifically: when expanded
(`data-collapsed="false"` or absent), the toggle displays `>` and the column
body displays its items; when collapsed (`data-collapsed="true"`), the
toggle displays `v`, the column body is hidden, the column title is rotated
90 degrees, and the column shrinks to show only the rotated header. An
implementation MAY offer the toggle on every column or only some; `Someday`
carrying it by default, matching version 1's behavior, is RECOMMENDED but no
longer required of any specific stage by name.

The done column reports what is really done, not what fits:

- Its `data-count` is the number of cards rendered — like every column — but it
  also carries `data-total`, the full count of done items under the current
  filters. The two differ only while `ui.board.doneLimit` is hiding some.
- `board-column-done-count` reads "shown of total" when the column is limited —
  `20 of 110` — and the bare count when it is not. The header count MUST NOT be
  the capped number on its own: a board showing 20 cards with 110 done must say
  so.
- When `data-total` exceeds `data-count`, the column body ends with
  `board-column-done-show-all`, a link to the addressable state
  `/p/:projectId/board?done=all` (§4.1). With `?done=all` the column renders
  every done item, `data-count` equals `data-total`, and the link is absent —
  there is nothing left to show. The link is how a user sees the other 90;
  expanding in place would be client state the server could not be held to.

The item menu (§6.2) MUST contain one entry per legal operation, each with
testid `item-T-0042-action-<operation>`, e.g. `item-T-0042-action-start`.
Illegal actions MUST be present and `disabled` with `data-reason` naming the
error code, rather than hidden — a test asserts *why* an action is unavailable.

### 5.6 Item panel

`/p/:projectId/item/:itemId`. Rendered over the board; the board MUST remain in
the DOM.

```html
<aside data-testid="item-panel" data-item-id="T-0042" data-state="ready"
       role="dialog" aria-modal="false" aria-labelledby="item-panel-title">
  <h2 data-testid="item-panel-title" id="item-panel-title">…</h2>
  <form data-testid="item-form">
    <input    data-testid="item-field-title"   name="title">
    <select   data-testid="item-field-stage"   name="stage">
    <select   data-testid="item-field-prio"    name="prio">
    <input    data-testid="item-field-tags"    name="tags">
    <input    data-testid="item-field-reason"  name="reason">
    <fieldset data-testid="item-tickler" data-present="true">
      <legend>Wake up</legend>
      <select data-testid="tickler-kind" name="tickler-kind">
        <option value="never">Never</option>
        <option value="one-time">One-time</option>
        <option value="weekly">Weekly</option>
        <option value="monthly">Monthly</option>
      </select>
      <input  data-testid="tickler-date" type="date" name="tickler-date">
      <select data-testid="tickler-weekday" name="tickler-weekday">…</select>
      <select data-testid="tickler-ordinal" name="tickler-ordinal">…</select>
      <input  data-testid="tickler-monthday" type="text"
              pattern="(0?[1-9]|[12][0-9]|3[01]|last)"
              name="tickler-monthday">
      <input  data-testid="tickler-time" type="time" name="tickler-time">
      <select data-testid="tickler-dest" name="tickler-dest">…</select>
    </fieldset>
    <textarea data-testid="item-field-detail"  name="detail"></textarea>
    <button   data-testid="item-save">Save</button>
    <button   data-testid="item-cancel">Cancel</button>
  </form>
  <section data-testid="item-actions">
    <button data-testid="item-action-start">…</button>
    <button data-testid="item-action-pause">…</button>
    <button data-testid="item-action-finish">…</button>
    <button data-testid="item-action-block">…</button>
    <button data-testid="item-action-remove" data-guarded="true">…</button>
  </section>
  <section data-testid="item-notes">…</section>
  <section data-testid="item-plan">…</section>
  <section data-testid="item-meta" data-created="…" data-started="…"></section>
</aside>
```

`item-field-reason` is renamed from version 1's `item-field-blocked`
(`spec-file-format.md` §5.1.5, §10) and is always rendered, not only for a
`stage:blocked` item — `reason` is valid on any stage now, required only
where the directory's `needs_reason` lists the item's current stage. A
client SHOULD mark it required (visually and via `aria-required`) exactly
when `item-field-stage`'s current value is in `needs_reason`, and MUST
re-evaluate that on every stage change, including one made in this same
form before saving.

`item-action-remove` MUST carry `data-guarded="true"` and MUST open
`dialog-confirm-remove` requiring explicit confirmation, per `spec-tools.md`
§5.1.6. It MUST NOT be satisfiable by a single click.

For an item whose stage is `working`, `item-plan` renders the item's detail
file's `## Plan` subtasks as `subtask-<n>` checkboxes and `item-notes`
renders its `## Notes` dated log (`spec-file-format.md` §5.4) — but neither
is exclusive to `working` any longer: any item with a detail file MAY show
both, since subtasks and notes always live in `details/<ID>.md` now, not in
a transient working file.

**The Wake-up group** — the tickler controls — is one partial served by both
panels, rendered inside `item-form`:

- New-item panel (`/p/:projectId/new`): rendered when its stage selector
  (`item-field-stage`, new panel only) is set to a stage named as a
  `SOURCE` in the directory's `tickler_stages` (`spec-file-format.md`
  §5.1.4; default `someday`). The selector defaults to `ready`, where the
  group is hidden unless `ready` itself is `tickler_stages`-eligible.
- Item panel: rendered for an item on a tickler-eligible stage.
  `data-present="true"` with the controls pre-filled from the item's parsed
  schedule when it carries `tickler`; `data-present="false"` (kind `never`,
  controls empty) when it does not — an unscheduled eligible item can gain a
  tickler here, and choosing kind `never` removes one.
- `tickler-dest`, new in this version: a select of declared stages,
  defaulting to the current stage's `tickler_stages` `DEST`. Composing it
  sets `tickler_dest` (`spec-file-format.md` §5.1.4, §6) only when it
  differs from that default — leaving it at the default keeps the field
  absent, so a board that never overrides a destination writes no extra
  data.

The kind select chooses the shape; the matching input is shown and the others
hidden. The server composes the `tickler:` value from the controls:

| kind | controls | composed `SCHEDULE` |
|---|---|---|
| `never` | — | no `tickler:` field (an existing one is removed) |
| `one-time` | `tickler-date` (required), `tickler-time` (optional) | `2026-09-01`, `2026-09-01@08:00` |
| `weekly` | `tickler-weekday` (required, `mon`…`sun`), `tickler-ordinal` (optional: `first` `second` `third` `fourth` `last`), `tickler-time` (optional) | `mon@08:00`, `first-mon@08:00` |
| `monthly` | `tickler-monthday` (required, `1`–`31` or `last`), `tickler-time` (optional) | `15@08:00`, `last@08:00` |

- The monthday is zero-padded to two digits in the composed value —
  `05@08:00`, never `5@08:00` (format spec §3.3 `SCHEDULE`). An absent
  `tickler-time` composes to no `@HH:MM` (the schedule's 00:00).
- `tickler-monthday` is a text input, not a number input. Its `pattern` is
  exactly the allowed token set — a day `1`–`31` (composed zero-padded) or
  the sentinel `last` — because a `type="number"` input cannot hold `last`,
  which the format (§3.3 `SCHEDULE`), the CLI and the server composer all
  accept.
- The grammar lives in one place: the server composes, the library validates
  on write (§4.2).
- Pre-fill parses the item's `tickler` back into the controls: a bare `DATE`
  is `one-time`; `DATE@HH:MM` adds the time; `[ordinal-]weekday[@HH:MM]` is
  `weekly`; `monthday|last[@HH:MM]` is `monthly`.

### 5.7 Report

```html
<section data-testid="report" data-state="ready"
         data-period="2026-W30" data-period-source="default"
         data-from="2026-07-20" data-to="2026-07-26" data-count="9">
  <form data-testid="report-controls">
    <select data-testid="report-period">…</select>
    <input  data-testid="report-since" type="date">
    <input  data-testid="report-until" type="date">
    <select data-testid="report-group-by">…</select>
    <input  data-testid="report-include-wip" type="checkbox">
    <select data-testid="report-include-stage" multiple>…</select>
  </form>
  <div data-testid="report-body">
    <section data-testid="report-group-shipped" data-count="7">
      <div data-testid="report-item-T-0042" data-outcome="shipped"
           data-done="2026-07-23">…</div>
    </section>
  </div>
  <button data-testid="report-copy">Copy as markdown</button>
</section>
```

`data-period-source` MUST be one of `switch`, `config`, `default`, mirroring the
precedence in `spec-tools.md` §5.1.11 — the resolved period must be visible, and
`report-copy` MUST place the same paste-ready markdown the CLI produces on the
clipboard.

### 5.8 Check

```html
<section data-testid="check" data-state="ready" data-violations="0">
  <button data-testid="check-run">…</button>
  <ul data-testid="check-results">
    <li data-testid="check-violation-0" data-invariant="I9"
        data-file="details/T-0001.md" data-line="4">…</li>
  </ul>
</section>
```

`data-invariant` MUST carry the `I1`–`I10` identifier from the format spec.

### 5.9 Settings

Both `/settings` and `/p/:projectId/settings` use the same skeleton, with
`data-scope="system"` or `data-scope="project"`.

```html
<section data-testid="settings" data-scope="project" data-state="ready">
  <section data-testid="settings-theme">
    <select data-testid="settings-theme-select"></select>
    <span   data-testid="settings-theme-source">project</span>
    <button data-testid="settings-theme-edit">…</button>
    <button data-testid="settings-theme-export">…</button>
    <input  data-testid="settings-theme-import" type="file">
    <button data-testid="settings-theme-clear">…</button>
  </section>
  <section data-testid="settings-lists">
    <input data-testid="settings-recent-count"    type="number" min="0" max="50">
    <input data-testid="settings-favorites-count" type="number" min="0" max="50">
  </section>
  <section data-testid="settings-scan" data-root-count="2" data-partial="false">
    <ul data-testid="settings-scan-roots">
      <li data-testid="settings-scan-root-0" data-path="/home/u/code"
          data-missing="false">
        <button data-testid="settings-scan-root-0-remove">…</button>
      </li>
    </ul>
    <input  data-testid="settings-scan-root-add" type="text">
    <input  data-testid="settings-scan-max-depth" type="number" min="0">
    <input  data-testid="settings-scan-follow-symlinks" type="checkbox">
    <input  data-testid="settings-scan-hidden" type="checkbox">
    <input  data-testid="settings-scan-excludes" type="text">
    <button data-testid="settings-scan-rescan">…</button>
    <span   data-testid="settings-scan-status" data-scanned-at="…">…</span>
  </section>
  <section data-testid="settings-wip" data-scope="project">
    <!-- one row per declared stage; a stage with no wip.<slug> key renders
         its input empty, meaning uncapped -->
    <div data-testid="settings-wip-row-working">
      <span>working</span>
      <input data-testid="settings-wip-limit-working" type="number" min="1">
    </div>
    <div data-testid="settings-wip-row-review">
      <span>review</span>
      <input data-testid="settings-wip-limit-review" type="number" min="1">
    </div>
  </section>
  <section data-testid="settings-migrate" data-version="1" hidden>
    <p>This board is on an older format version.</p>
    <button data-testid="settings-migrate-run">Migrate</button>
  </section>
  <!-- version 2 only; absent entirely for a version-1 project -->
  <section data-testid="settings-audit">
    <input data-testid="settings-audit-toggle" type="checkbox">
    <a data-testid="settings-audit-view" href="/p/x/audit">…</a>
  </section>
  <button data-testid="settings-save">Save</button>
</section>
```

`settings-wip` is per-stage and project-scoped, unlike version 1's single
directory-wide `settings-wip-limit`: one `settings-wip-limit-<slug>` input
per entry in the directory's `stages`, matching `wip.<slug>`
(`spec-file-format.md` §5.1.3). An empty input on save removes that stage's
cap (uncapped) rather than writing an invalid value.

`settings-migrate` is present, and its `hidden` attribute removed, only when
the open board reports `VersionMismatch`-eligible (§4.3) — i.e. it is a
version-1 directory. `settings-migrate-run` calls the migrate action
(`spec-tools.md` §5.3.4); on success the view MUST re-fetch the directory
summary and re-render, since every column, testid, and config key described
in this document changes shape the moment the migration lands.

`settings-lists` and `settings-scan` MUST be present only when
`data-scope="system"` — list counts and scan roots are system-scoped (§9.4, §9.5).
In project scope both sections MUST be absent, not disabled.

There is no settings control for `server.*`. Binding is configured only by file
or command line (§9.6), and a UI control for it MUST NOT exist.

A root whose path no longer resolves MUST render with `data-missing="true"` and
MUST NOT be removed automatically.

`settings-audit` is version 2 only — absent entirely for a version-1
project, the same rule `settings-wip`'s per-stage form already follows,
rather than rendering a control that would refuse to save. Checking
`settings-audit-toggle` and saving sets `board.md`'s `audit` key
(`spec-file-format.md` §5.1.8) to `true`; unchecking and saving sets it to
`false`. `settings-audit-view` links to the audit log (§5.11) and MUST be
present only while audit is on — there is nothing to view otherwise.

### 5.10 Dialogs, toasts, errors

- Dialogs render into `dialog-root` with testid `dialog-<name>`, `role="dialog"`,
  `aria-modal="true"`, and MUST trap focus. Required: `dialog-confirm-remove`,
  `dialog-wip-limit`, `dialog-conflict`, `dialog-import-theme`.
- Toasts render into `toast-region` with testid `toast-<n>` and
  `data-severity="info|success|warning|danger"`.
- `dialog-wip-limit` MUST list current occupants with testid
  `dialog-wip-limit-slot-NN` and offer finish, pause, and raise-limit actions —
  the same three remedies the CLI names.
- `dialog-conflict` appears on `Concurrent` and MUST offer reload; it MUST NOT
  offer a blind overwrite.

### 5.11 Audit log

`/p/:projectId/audit`: a read-only rendering of `audit.md`
(`spec-file-format.md` §5.7), reachable from `settings-audit-view` (§5.9).
Version 2 only, and only meaningful once `audit: true` is set — this route
still resolves for a version-1 project or one with audit off, rendering an
explanation rather than a 404, since a stale bookmark or a shared link is not
a broken one.

```html
<section data-testid="audit" data-available="true" data-enabled="true">
  <!-- exactly one of the three below, depending on data-available/data-enabled -->
  <p data-testid="audit-unavailable">…</p>   <!-- version 1 -->
  <p data-testid="audit-disabled">…</p>      <!-- version 2, audit: false -->
  <p data-testid="audit-empty">…</p>         <!-- version 2, audit: true, no entries yet -->

  <div data-testid="audit-entries">
    <div data-testid="audit-entry-0" data-item-id="T-0251" data-field="stage">
      <time>2026-08-27T14:32:10Z</time>
      <a href="/p/x/item/T-0251">T-0251</a>
      <span>stage</span>
      <span>working</span>
    </div>
  </div>
</section>
```

Tag names are illustrative, not fixed (§5.1: only `data-testid` and `data-*`
are). This build renders entries as styled rows rather than a `<table>`,
matching `report-item`/`check-violation` elsewhere in this document; a
conforming implementation MAY do either.

Entries render **newest first** — the opposite of `audit.md`'s own on-disk
append order (§5.7), matching a log viewer's usual convention: what just
happened is what a reader opens the page to see. `audit-entry-<n>` numbers
that rendered order, not any identifier from the file itself.

This view has no write path of its own: enabling or disabling the log is
`settings-audit-toggle` (§5.9), and every entry it shows was produced by an
operation that already exists elsewhere in this document. Nothing here
mutates the directory.

## 6. Operation coverage

### 6.1 Parity with the CLI

The GUI MUST provide every required operation in `spec-tools.md` §5.1 and SHOULD
provide every recommended one in §5.2.

| Operation | Reachable from |
|---|---|
| `--init` | `project-init` on Home |
| `--add` | `board-column-*-add`, `/p/:id/new` |
| `--list` | Board, with query-parameter filters |
| `--show` | Item panel |
| `--edit` | `item-form` |
| `--remove` | `item-action-remove` → `dialog-confirm-remove` |
| `--move` | Drag (§7), or `item-*-action-move` — any declared stage, not only Ready/Blocked/Someday |
| `--start` | Drag to the working column, or `item-action-start` |
| `--pause` | Drag from the working column to another column, or `item-action-pause` |
| `--finish` | Drag to the done column, or `item-action-finish` |
| `--report` | `/p/:id/report` |
| `--check` | `/p/:id/check`, plus `status-check` |
| `--block` / `--unblock` | Drag to/from the `blocked` column, or item actions — sugar over `--move --stage blocked`/`ready` |
| `--note` | `item-notes` |
| `--wip` | `settings-wip`, per stage (§5.9) |
| `--status` | `app-status` |
| `--next` | Top card of the board's default resting column (`ready`, unless `stages` orders differently) |
| `--search` | `search-input` |
| `--find` | `/projects` |
| `--tick` | The tickler service (§2.4), when `tickler.interval` is set — the GUI's equivalent of a scheduled `--tick`, not a manual button |

### 6.2 Every drag has a non-drag equivalent

Every operation reachable by dragging MUST also be reachable from the item menu
or the item panel. Drag and drop is an accelerator, never the only path. This is
required three times over: for keyboard and assistive-technology users, for the
planned TUI, and because a test suite that can only express intent through
synthesized pointer gestures is brittle across frameworks.

## 7. Drag and drop

### 7.1 Model

Item cards are draggable. Column bodies are drop targets. A drop carries an
intended destination column and an insertion index.

The client MUST NOT apply a drop optimistically without server confirmation
unless it can fully revert. On rejection it MUST restore the pre-drag DOM,
including `data-position` on every affected card, and surface the error.

A **collapsed** column (§5.5) is a drop target like any other, even though its
body is hidden: a pointer anywhere over the collapsed column MUST resolve to
that column, and the drop MUST land at the **bottom** of it. There is no
visible list to aim within, so no other index has a meaning the user could
have intended. The `data-drop-*` attributes of §7.3 are maintained as usual;
the placeholder is inside the hidden body and so is not visible, and the
column's own drop-target styling carries the feedback.

### 7.2 Legal transitions

| From | To | Operation | Notes |
|---|---|---|---|
| any non-done column | same column | `--move --position N` | reorder — `working` included: version 2's `working` is an ordinary declared stage with its own ordered run (`spec-file-format.md` §5.1.6), not version 1's separate, order-free slot files, so it has exactly as much order to rearrange as any other stage |
| any column | a `needs_reason` column (e.g. `blocked`) | `--move --stage` / `--block` | MUST prompt for a reason unless the item already carries one; cancelling aborts |
| a `needs_reason` column | another column | `--move --stage` / `--unblock` | `reason:` is kept, not dropped (`spec-file-format.md` §5.1.5) |
| any two non-`working`, non-done columns | — | `--move --stage` | general case; every declared stage is a legal destination for every other |
| any column | `working` | `--start` | fails `WipLimitReached` when that stage's `wip.working` cap is met |
| `working` | any other non-done column | `--pause` | position from drop index |
| `working` | `working` | `--move --position N` | reorder, same as the first row. Distinct from `--start` on an item already on `working`, which stays illegal (`spec-tools.md` §5.1.8's own `Conflict`) — a drag within `working` is a reorder, never a re-`--start` |
| any column | done | `--finish` | MUST prompt for outcome, default `shipped` |
| done | anywhere | — | **illegal**; reopening is not a specified operation |

A column whose stage is a tickler-eligible `SOURCE` moving an item *out* of
it MUST drop `tickler:`/`tickler_dest:` (`spec-file-format.md` §5.1.4) as
part of the same drop — the schedule is consumed, matching `--move`'s rule
(`spec-tools.md` §5.1.7).

An illegal target MUST be marked `data-drop-allowed="false"` on hover and MUST
reject the drop with no request issued.

### 7.3 DOM during a drag

| Attribute | On | Meaning |
|---|---|---|
| `data-dragging="true"` | the dragged card | set on drag start, removed on end |
| `data-drag-source` | app root | testid of the origin column |
| `data-drop-target="true"` | hovered column | at most one at a time |
| `data-drop-allowed` | hovered column | `true` \| `false` |
| `data-drop-index` | hovered column | 0-based insertion index |
| `data-drop-reason` | hovered column | error code when not allowed, e.g. `WipLimitReached` |

A placeholder element `data-testid="drop-placeholder"` MUST be present in the
hovered column at the insertion point. These attributes are the only supported
way for a test to assert drag state mid-gesture, so they MUST be accurate on
every pointer move, not only on drop.

### 7.4 Keyboard equivalent

Required and normative, not an accessibility afterthought:

| Key | Action |
|---|---|
| `Space` or `Enter` on a focused card | enter move mode; set `data-move-mode="true"` |
| `ArrowUp` / `ArrowDown` | change insertion index within the column |
| `ArrowLeft` / `ArrowRight` | change target column |
| `Enter` | commit |
| `Escape` | cancel, restore focus and position |

Move mode MUST maintain the same `data-drop-*` attributes as a pointer drag, so
one set of test assertions covers both input paths.

### 7.5 Failure

A drop that the server rejects MUST: revert the DOM, keep focus on the item,
raise a toast with `data-severity="danger"` carrying the error code, and — for
`WipLimitReached` — open `dialog-wip-limit`.

## 8. Theming

### 8.1 Goals

1. A theme lives with the project, so **the look of the screen tells the user
   which project they are in** before they read a word.
2. A system theme is the fallback when a project has none.
3. Switching projects re-themes the UI immediately, with no reload.
4. Themes are files: creatable, exportable, importable, shareable.
5. The colour half of a theme is shared with the TUI (§8.5).

### 8.2 Theme file

JSON, UTF-8. Chosen over the project's markdown-with-frontmatter idiom because
themes are nested structured data and `spec-file-format.md` §4.1 frontmatter is
a deliberately flat map.

Locations:

| Path | Role |
|---|---|
| `<micro-manager dir>/theme.json` | Project theme. Highest precedence. |
| `$XDG_CONFIG_HOME/micro-manager/theme.json` | System theme. Fallback. |
| `$XDG_CONFIG_HOME/micro-manager/themes/<themeId>.json` | Theme library. |

`XDG_CONFIG_HOME` defaults to `$HOME/.config` when unset or not absolute.

The library also contains the themes compiled into the implementation, which
have no path and are always present. They are named and specified in §8.7.

Every date, time and timestamp in a theme, a config file, a list file, a `data-*`
attribute, or an API payload MUST be ISO 8601 in the extended format
(`spec-file-format.md` §3.3.1). Timestamps carry an explicit UTC designator —
`2026-07-29T09:14:00Z` — because these files are synchronised between machines.
Dates are `DATE`; a point in time is `TIMESTAMP`; never a locale form, never
epoch seconds.

A theme file inside a micro-manager directory is outside the file-format spec
(`spec-file-format.md` §1) and MUST be ignored by format readers and checkers.
It never participates in invariants I1–I10.

```json
{
  "schemaVersion": 1,
  "id": "sample-one-dark",
  "name": "Sample One — Dark",
  "author": "…",
  "appearance": "dark",
  "brand": { },
  "color": { },
  "colorDark": { },
  "gui": { },
  "tui": { },
  "extra": { }
}
```

`appearance` is `light`, `dark`, or `auto`. `auto` REQUIRES both `color` (light)
and `colorDark`, and follows `prefers-color-scheme`.

Unknown top-level keys and unknown keys inside `extra` MUST be preserved on
read-modify-write. A theme editor that silently drops what it does not
understand cannot round-trip a theme from a newer implementation.

### 8.3 Token taxonomy

**`color` — shared with the TUI.** Every token is REQUIRED; a theme missing one
inherits it from the built-in default rather than being rejected.

```
bg.base        bg.raised      bg.sunken      bg.overlay
fg.default     fg.muted       fg.subtle      fg.inverted
border.default border.strong  border.focus
accent.base    accent.fg      accent.muted
state.ready    state.blocked  state.someday  state.working  state.done
prio.high      prio.med       prio.low
feedback.success  feedback.warning  feedback.danger  feedback.info
selection.bg   selection.fg
drag.valid     drag.invalid
```

Values are `#rrggbb` or `#rrggbbaa`. The set is intentionally small and
semantic: it is the largest palette a terminal can render faithfully, and every
token maps to something a TUI also needs.

**`state.<slug>` is open-ended, not closed to the five listed above** — a
theme MAY additionally declare `state.<slug>` for any stage a board
declares (`spec-file-format.md` §5.1.1), e.g. `state.review`. Unlike the
five REQUIRED tokens above, a `state.<slug>` token is entirely OPTIONAL:
when a declared stage has no matching token, a client MUST render it in
`accent.base` rather than treating the theme as incomplete. This keeps the
REQUIRED set exactly five, independent of any specific board's `stages`
list, while still letting a theme author give a custom stage its own color
when they care to.

**`gui` — ignored by the TUI.**

```
font.family.ui      font.family.mono
font.size.{xs,sm,md,lg,xl}      font.weight.{normal,medium,bold}
font.lineHeight.{tight,normal,loose}
space.{0,1,2,3,4,5,6,8}         radius.{none,sm,md,lg,full}
border.width.{thin,thick}       shadow.{none,sm,md,lg}
motion.duration.{fast,normal,slow}   motion.easing.{standard,enter,exit}
density.{compact,normal,comfortable}
```

**`tui` — ignored by the GUI.** Optional per-token overrides for terminals that
cannot do truecolor, plus attribute hints.

```json
"tui": {
  "ansi256": { "accent.base": 39, "state.blocked": 131 },
  "ansi16":  { "accent.base": "brightblue" },
  "attrs":   { "prio.high": ["bold"], "state.done": ["dim"] }
}
```

Where an override is absent, an implementation MUST derive the nearest ANSI
colour from the hex value. Derivation MUST be deterministic; implementations
SHOULD use nearest-neighbour in CIELAB.

### 8.4 CSS custom properties

Every `color` and `gui` token MUST be exposed as a CSS custom property on the
app root, named by lowercasing the token path and replacing `.` with `-`:

```
color.bg.base        →  --mm-color-bg-base
color.prio.high      →  --mm-color-prio-high
gui.space.3          →  --mm-space-3
gui.font.size.md     →  --mm-font-size-md
gui.radius.lg        →  --mm-radius-lg
gui.motion.duration.fast → --mm-motion-duration-fast
```

`color.*` maps to `--mm-color-*`; each `gui.<group>.*` maps to `--mm-<group>-*`.

All styling MUST resolve through these properties. An implementation MUST NOT
hard-code a colour that a token covers — this is what makes a theme portable
across implementations, and it is externally testable: a suite reads the
computed value of `--mm-color-accent-base` and asserts the element uses it.

### 8.5 TUI compatibility

The `color` group is the shared contract. A TUI implementation:

- MUST read the same theme files from the same locations with the same
  precedence;
- MUST use the `color` tokens and MAY use `tui`;
- MUST ignore `gui` without error;
- MUST NOT require any token the GUI does not require.

No token may be added to `color` that a terminal cannot express. Gradients,
shadows, and opacity-dependent effects belong in `gui`.

### 8.6 Branding

```json
"brand": {
  "name": "Sample One",
  "short": "S1",
  "tagline": "…",
  "logo":  "data:image/svg+xml;base64,…",
  "icon":  "data:image/png;base64,…",
  "ascii": "…",
  "accent": "#3b82f6"
}
```

- `name` defaults to the project's `project` frontmatter value when absent. The
  two are separate on purpose: `project` is data, branding is presentation.
- `logo` and `icon` MUST be data URIs in an exported theme (§8.8). Inside a
  project theme they MAY be paths relative to the theme file.
- `ascii` is the TUI's brand mark and is ignored by the GUI.
- `brand-logo` MUST have `alt=""` when `brand-name` is displayed alongside it.

### 8.7 Resolution and live switching

Resolution order, first hit wins:

1. `<micro-manager dir>/theme.json`
2. Theme named by project config `theme.id`, resolved from the theme library
3. `$XDG_CONFIG_HOME/micro-manager/theme.json`
4. Theme named by system config `theme.id`
5. Built-in default

Missing tokens at any level fall through to the built-in default per token, not
per file — a project theme that sets only `accent.base` and `brand` is valid and
common.

**Built-in themes.** Steps 2 and 4 resolve an id against the theme library, and
the library is the files under `$XDG_CONFIG_HOME/micro-manager/themes/` TOGETHER
WITH the themes compiled into the implementation. Two ids are fixed:

| `themeId` | | Role |
|---|---|---|
| `micro-manager` | MUST | The built-in default of step 5. It defines every token in §8.3 — that is what makes per-token fallback total, and it is the only theme of which it is required. |
| `micro-manager-lite` | SHOULD | A light companion of reduced visual weight: `appearance: light`, one palette, and no `gui` group, so typography, spacing and radii fall through to the default. |

The ids are normative; the palettes are not. A theme is chosen by id in
`config.json` (§9.2, §9.3), exported under its id (§8.8) and listed by id, so an
implementation that renamed its default would emit configs and exports no other
implementation could resolve — and a theme picker offering an id that resolves to
nothing silently serves the default instead. What the colours actually are is
presentation, and this document does not fix them.

Four rules follow from a built-in being a library entry with no file:

1. It appears in the theme library listing (`GET /api/v1/themes`,
   `/settings/themes`) alongside the files, and it is exportable (§8.8).
   Exporting a built-in is how a user forks one.
2. It resolves **at the precedence position its id was named at**, not at step 5.
   A project naming `micro-manager-lite` therefore still outranks a system
   `theme.json`, which it would not if built-ins were reachable only as the
   default.
3. A library **file** whose id matches a built-in shadows it: the file is read,
   the built-in is ignored, and the id is listed once rather than twice. This is
   the supported way to patch a shipped theme.
4. An implementation MAY compile in further themes, which behave exactly as
   these two do. One that does not ship a named built-in treats the id as a
   library id with no file: resolution continues at the next candidate.

The theme ids in this document's examples — `sample-one-dark` in §8.2 and §9.3,
`nord-dark` in §9.2 — are illustrations, not themes an implementation ships.
Offering one in a picker as though it existed is the failure mode rule 2 above
describes: the selection resolves to nothing and the default is served with no
indication that the choice was ignored.

**Switching projects MUST re-resolve and re-apply without a page reload.** The
implementation MUST update the custom properties on the app root and update
`data-theme-name` and `data-theme-source`. A test asserts: navigate from project
A to project B, then read `--mm-color-accent-base` and `data-theme-source` and
confirm both changed. Any flash of the previous theme MUST be avoided by
applying the new values before the first paint of the new view.

Editing a theme MUST apply live to the current view, so the editor is a preview.

### 8.8 Export and import

**Export** (`GET /api/v1/themes/:themeId/export`) emits one self-contained JSON
file with every asset inlined as a data URI and no external references. Filename
`<themeId>.mm-theme.json`, `Content-Type: application/json`,
`Content-Disposition: attachment`.

**Import** (`POST /api/v1/themes/import`) accepts that file. It MUST:

1. reject a `schemaVersion` it does not implement, naming the version;
2. validate every colour value, reporting each bad token by path;
3. never partially apply — a rejected theme changes nothing;
4. preserve unknown keys;
5. on an `id` collision, require an explicit overwrite or rename choice via
   `dialog-import-theme`, never silently replace. A built-in id collides like any
   other: overwriting writes a library file that shadows the built-in (§8.7
   rule 3), which is a shadow and not a replacement — the built-in is still
   there when the file is removed.

Import targets are the theme library, the system theme, or the current project's
theme; the destination is an explicit parameter with no default.

## 9. Configuration

### 9.1 Locations

| Path | Scope |
|---|---|
| `$XDG_CONFIG_HOME/micro-manager/config.json` | System |
| `<micro-manager dir>/config.json` | Project |

Both are JSON, both are optional, and a missing file is equivalent to an empty
object. Like `theme.json`, a project `config.json` is outside the file-format
spec and MUST be ignored by format readers.

### 9.2 System configuration

```json
{
  "schemaVersion": 1,
  "theme": { "id": "nord-dark" },
  "ui": {
    "recentCount": 10,
    "favoritesCount": 10,
    "recentMaxStored": 100,
    "density": "normal",
    "defaultView": "board",
    "pollIntervalMs": 5000,
    "confirmRemove": true
  },
  "report": { "period": "last-week", "groupBy": "none", "includeWip": false },
  "scan": {
    "roots": ["~/code", "~/para/Projects"],
    "maxDepth": 6,
    "followSymlinks": false,
    "includeHidden": true,
    "excludes": ["node_modules", ".git", "target", "vendor", ".venv", "dist"],
    "maxResults": 500,
    "timeoutMs": 5000,
    "rescanOnFocus": true
  },
  "tickler": { "interval": null },
  "server": {
    "bind": "127.0.0.1",
    "port": 7717,
    "allowRemote": false,
    "socket": null
  }
}
```

`ui.recentCount` and `ui.favoritesCount` are how many entries the UI *displays*
(0–50, default 10 each). `ui.recentMaxStored` is how many are retained on disk
and is independent — shrinking the display count MUST NOT discard stored
history.

`report.period` is the UI's equivalent of `MM_REPORT_PERIOD` and occupies the
same precedence slot (`spec-tools.md` §5.1.11 step 4). A UI service MUST NOT
read `MM_REPORT_PERIOD` from its own environment: the service outlives any one
user's shell, and inheriting the environment of whoever started it is exactly
the coupling `spec-tools.md` §3.5 exists to prevent.

`tickler.interval` turns on the tickler service (§2.4): a duration like
`"1m"`, absent or `null` meaning off (the default). It is a system-level
choice — the service is a process concern, not a board's, and a project
config MUST NOT set it.

### 9.3 Project configuration

```json
{
  "schemaVersion": 1,
  "theme": { "id": "sample-one-dark" },
  "ui": {
    "density": "compact",
    "defaultView": "board",
    "board": { "collapsedStages": ["someday"], "doneLimit": 20 }
  },
  "report": { "period": "this-week", "groupBy": "outcome" }
}
```

`ui.board.collapsedStages` lists which columns render collapsed by default
(§5.5) — renamed and generalized from version 1's boolean `showSomeday`,
since every column supports the same toggle now, not only Someday. A stage
named there that the directory no longer declares is simply never matched;
an implementation MUST NOT error on it.

`ui.board.doneLimit` caps how many done cards the board column renders (default
20; 0 means no cap). It is a display cap, not a truth: the column header still
reports the full done count, and `?done=all` overrides the cap entirely (§5.5).

The `tui` object is reserved for terminal-specific settings (`spec-tui.md` §9).
The GUI MUST ignore it and MUST preserve it when rewriting a config file.

A project config MUST NOT set `ui.recentCount`, `ui.favoritesCount`,
`ui.recentMaxStored`, or anything under `server`. Those are system-scoped;
present-but-ignored is not acceptable, and an implementation MUST report them as
a validation warning naming the key.

### 9.4 Merge

Built-in defaults, then system config, then project config; last writer wins per
leaf key, not per object — a project overriding `ui.density` MUST NOT discard
the system's `ui.defaultView`.

System-scoped keys are never overridable. Theme resolution is §8.7 and does not
follow this merge, because a theme is chosen whole, not merged field by field —
except for the per-token fallback to the built-in default described there.

Writing settings MUST target one scope explicitly. The settings view exposes
scope through `data-scope` and MUST make clear which file a change lands in.

**Every front end MUST preserve keys it does not understand when rewriting any
shared file** — config, theme, recent, or favorites. Two front ends over one set
of files will otherwise strip each other's settings on alternate runs, each
"correctly" writing back only what it knows.

### 9.5 Project discovery scope

Both front ends discover projects by scanning **down from configured base
directories** rather than from wherever they happen to have been started.

`scan.roots` is a list of base directories. Each is scanned recursively for
directories named per `spec-file-format.md` Appendix B. Rules:

1. Roots are system-scoped. A project config MUST NOT set them — a project
   cannot decide which projects exist.
2. `~` and environment variables in a root are expanded by the front end, never
   by the library (`spec-tools.md` §2.2 rule 4). The library receives absolute
   paths in `DiscoveryOptions`.
3. A root that does not exist or is unreadable MUST be reported in settings and
   skipped, not treated as fatal. External drives come and go.
4. Results are deduplicated by canonical path, so the same directory reachable
   from two roots appears once.
5. Discovery MUST NOT descend into a matched directory (`spec-tools.md` §4).
   A match that holds none of `board.md`, `backlog.md`, or `done.md` is not
   a project and MUST NOT be listed — the emptiness test of
   `spec-file-format.md` Appendix B — and it is still not descended into. A
   directory found by `backlog.md` alone is listed as a version-1 project,
   with a badge or marker so the migration action (§5.9) is discoverable
   from `/projects` too, not only from a board already open. A project list that
   offered a source repository sharing the name would be one the user has to
   learn to ignore.
6. `includeHidden` defaults to **true**, because `.micro-manager` and
   `.µmanager` are conventional names. An implementation that skips dotted
   directories by default is non-conforming.
7. `followSymlinks` defaults to false. When enabled, implementations MUST detect
   cycles by device and inode, and MUST NOT recurse indefinitely.
8. `maxResults` and `timeoutMs` bound the walk. When either is hit the result
   MUST be marked partial and the UI MUST say so — silently truncated discovery
   looks identical to a missing project.
9. Ordering is by `project` name, then by path.

**When `scan.roots` is empty**, the front end MUST fall back to the directory it
was started in as a single root, and MUST surface a prompt to configure roots.
It MUST NOT default to scanning `$HOME` or `/`.

**Caching.** Discovery results MAY be cached, and SHOULD be, since a deep tree
is expensive. The fingerprint poll of §2.3 detects changes *within* a known
project and does not detect a new project appearing. Therefore a rescan MUST be
available on demand (`POST /api/v1/scan`, `scan-rescan`), and `scan.rescanOnFocus`
re-runs discovery when the window regains focus. Cached results MUST carry
`scannedAt` and the UI MUST show it.

### 9.6 Service binding and local access

The service has unauthenticated read and write access to the user's files. This
specification defines no authentication, and it therefore requires the service
to be unreachable from the network.

1. `server.bind` defaults to `127.0.0.1` and `server.port` to `7717`.
2. The service MUST verify the resolved bind address is a loopback address —
   `127.0.0.0/8` or `::1` — and MUST refuse to start otherwise, naming the
   address, unless `server.allowRemote` is explicitly `true`.
3. `0.0.0.0` and `::` MUST be refused under the same rule. Binding to every
   interface is never the accidental outcome of a default.
4. `server.allowRemote` MUST NOT be settable from the web UI. It is changed only
   by editing the config file or by an explicit command line flag. A setting
   that removes a protection must not be reachable from the surface that
   protection defends.
5. When `allowRemote` is true the service MUST emit a prominent startup warning
   naming the address and port, and SHOULD display a persistent banner in the
   UI.
6. `server.socket`, when set, binds a Unix domain socket instead of TCP. This is
   the most restrictive option and SHOULD be offered.
7. If the port is in use the service MUST fail with a clear message. It MUST NOT
   silently pick another port — a user who bookmarks `:7717` should not find a
   different project there tomorrow.

**Browser-side protections.** A loopback bind is not sufficient on its own,
because any page the user visits can issue requests to `localhost` and DNS
rebinding can defeat naive origin checks. The service MUST therefore:

- reject any request whose `Host` header is not `localhost`, `127.0.0.1`,
  `[::1]`, or an explicitly configured hostname, with 421;
- reject any state-changing request (anything other than `GET` and `HEAD`) whose
  `Origin` is present and not the service's own origin, with 403;
- never send `Access-Control-Allow-Origin: *`, and never reflect an arbitrary
  origin;
- set `Content-Security-Policy` restricting `connect-src` to `'self'`.

The command line flags `--bind`, `--port`, and `--socket` override config and
follow the same rules. `--allow-remote` is the only way to set `allowRemote`
outside the config file.

## 10. Recent and favorites

Both live in `$XDG_CONFIG_HOME/micro-manager/`, in separate files so that the
high-frequency writes of one cannot clobber the other.

```
$XDG_CONFIG_HOME/micro-manager/recent.json
$XDG_CONFIG_HOME/micro-manager/favorites.json
```

> These are arguably state rather than configuration, and a strict XDG reading
> would put `recent.json` under `XDG_STATE_HOME`. This specification requires
> `XDG_CONFIG_HOME` for both so that every implementation and both front ends
> agree on one location; interoperability matters more here than orthodoxy.

```json
{ "schemaVersion": 1,
  "entries": [
    { "projectId": "9f2a7c1e4b60", "path": "/abs/path/micro-manager",
      "name": "Sample One", "lastOpened": "2026-07-29T09:14:00Z",
      "themeName": "Sample One — Dark" }
  ] }
```

`favorites.json` uses the same shape plus `"order": <int>` and an optional
`"label"` that overrides the displayed name. `lastOpened` is a `TIMESTAMP`:
ISO 8601, extended format, with a UTC designator.

Behavior:

1. Opening a project MUST move it to the front of `recent`, updating
   `lastOpened`. An entry already present is moved, never duplicated.
2. `recent` retains `ui.recentMaxStored` entries and displays
   `ui.recentCount`.
3. `favorites` is user-ordered, displays `ui.favoritesCount`, and MUST be
   reorderable by drag using the same mechanism and attributes as §7.
4. A favorite MUST also appear in recent if recently opened. The lists are
   independent.
5. An entry whose path no longer resolves MUST be rendered with
   `data-missing="true"` and MUST NOT be silently removed — a project on an
   unmounted drive is not a deleted project. Offer explicit removal.
6. Both files MUST be written atomically (temp file plus rename) and MUST
   tolerate concurrent writers, since two front ends may run at once.
7. Neither list may contain a secret; paths only, and no item content.

Both lists are reachable from `project-switcher-menu` and Home. Adding and
removing a favorite is `project-card-favorite-toggle`, which MUST use
`aria-pressed` to expose state.

## 11. Accessibility

Requirements below are normative and double as testability guarantees.

1. Every interactive element MUST be keyboard reachable, in DOM order, with a
   visible focus ring drawn from `--mm-color-border-focus`.
2. Colour MUST NOT be the sole carrier of meaning. Priority, state, and outcome
   MUST each have a text or `aria-label` equivalent, which is what lets a test
   assert them without reading pixels.
3. Columns are `role="list"`, cards `role="listitem"`, the board's live region
   `aria-live="polite"`.
4. Drag operations MUST announce start, target change, and result through the
   live region.
5. Dialogs trap focus and restore it to the invoking element on close.
6. The item panel of §5.6 MUST focus its title field the moment it opens for
   a **new** item — typing should need no click first. Opening it for an
   existing item leaves focus alone.
7. `prefers-reduced-motion` MUST disable non-essential animation.
8. Contrast between `fg.default` and `bg.base`, and between `accent.fg` and
   `accent.base`, MUST meet WCAG AA (4.5:1). The theme editor MUST warn when a
   token pair fails; it MUST NOT block saving.

## 12. Conformance and the external test suite

A conforming implementation:

1. serves every route in §4 with the specified semantics;
2. renders every required `data-testid` and `data-*` attribute in §5;
3. uses the `mm-` class convention of §5.1;
4. exposes every theme token as the CSS custom property of §8.4 and styles
   exclusively through them;
5. implements §7 drag and drop including the keyboard equivalent;
6. reads and writes the theme, config, recent, and favorites files exactly as
   specified in §8–§10;
7. provides every required operation of §6.1;
8. passes the external conformance suite with no implementation-specific
   configuration beyond a base URL and a fixture directory.

The suite is normative in the sense that it may only exercise what this document
specifies: it MUST locate elements exclusively by documented `data-testid`
values, MUST assert state exclusively via documented `data-*` attributes and CSS
custom properties, and MUST NOT depend on text content except where this
document fixes it. If the suite needs something this document does not specify,
the document is incomplete — that is a spec change, not a test workaround.

Fixtures are micro-manager directories checked into the suite. A fixture MUST
validate clean under the format spec unless it exists to test violation
reporting, in which case it MUST document which invariant it breaks.

## 13. TUI addendum

The TUI is specified in `spec-tui.md`. It is bound to what this document
fixes:

- the operation set of §6.1;
- the `color` and `brand.ascii` theme tokens (§8.3, §8.6) and the resolution
  order of §8.7, including live re-theming on project switch;
- the configuration files, recent list, and favorites list of §9 and §10,
  byte-compatible with the GUI;
- the move-mode key bindings of §7.4, which were specified as the keyboard
  equivalent of dragging precisely so the TUI inherits a proven interaction
  rather than inventing one.

The TUI specification MUST NOT introduce a theme token or list file the GUI does
not already understand. Terminal-specific values belong under the theme's `tui`
group (§8.3) and the config's `tui` object (§9.2), both of which the GUI ignores
by construction and preserves on write.

---

## Appendix A: testid index

```
app  app-header  app-main  app-nav  app-status
brand  brand-logo  brand-name
project-switcher  project-switcher-menu  project-switcher-favorites
project-switcher-recent  project-switcher-all
search-input  nav-board  nav-report  nav-check  nav-settings
status-wip  status-counts  status-check
toast-region  toast-<n>  dialog-root  dialog-<name>

home  favorites-list  recent-list  project-open  project-init
project-card-<projectId>  project-card-name  project-card-path
project-card-wip  project-card-favorite-toggle

board  board-column-<slug>  board-column-<slug>-toggle  board-column-done
board-column-<slug>-header  -title  -count  -wip  -add  -body  (-wip only on a
WIP-capped column; every column carries -add except version 1's
board-column-working, which has no --add --stage working)
board-column-done-show-all
item-<ID>  item-<ID>-title  item-<ID>-id  item-<ID>-prio
item-<ID>-tags  item-<ID>-tag-<tag>  item-<ID>-detail-indicator
item-<ID>-tickler  item-<ID>-menu  item-<ID>-action-<operation>
drop-placeholder

item-panel  item-panel-title  item-form  item-field-<field>
item-field-stage  item-field-reason  item-tickler
tickler-kind  tickler-date  tickler-weekday  tickler-ordinal
tickler-monthday  tickler-time  tickler-dest
item-save  item-cancel  item-actions  item-action-<operation>
item-notes  item-plan  item-meta  subtask-<n>

report  report-controls  report-period  report-since  report-until
report-group-by  report-include-wip  report-include-stage  report-body
report-group-<key>  report-item-<ID>  report-copy

check  check-run  check-results  check-violation-<n>

audit  audit-unavailable  audit-disabled  audit-empty
audit-entries  audit-entry-<n>

settings  settings-theme  settings-theme-select  settings-theme-source
settings-theme-edit  settings-theme-export  settings-theme-import
settings-theme-clear  settings-lists  settings-recent-count
settings-favorites-count  settings-wip  settings-wip-row-<slug>
settings-wip-limit-<slug>  settings-migrate  settings-migrate-run
settings-audit  settings-audit-toggle  settings-audit-view
settings-save
settings-scan  settings-scan-roots  settings-scan-root-<n>
settings-scan-root-<n>-remove  settings-scan-root-add
settings-scan-max-depth  settings-scan-follow-symlinks  settings-scan-hidden
settings-scan-excludes  settings-scan-rescan  settings-scan-status
projects-root-<n>
```

## Appendix B: CSS custom property index

```
--mm-color-bg-base  --mm-color-bg-raised  --mm-color-bg-sunken
--mm-color-bg-overlay
--mm-color-fg-default  --mm-color-fg-muted  --mm-color-fg-subtle
--mm-color-fg-inverted
--mm-color-border-default  --mm-color-border-strong  --mm-color-border-focus
--mm-color-accent-base  --mm-color-accent-fg  --mm-color-accent-muted
--mm-color-state-ready  --mm-color-state-blocked  --mm-color-state-someday
--mm-color-state-working  --mm-color-state-done
--mm-color-prio-high  --mm-color-prio-med  --mm-color-prio-low
--mm-color-feedback-success  --mm-color-feedback-warning
--mm-color-feedback-danger   --mm-color-feedback-info
--mm-color-selection-bg  --mm-color-selection-fg
--mm-color-drag-valid    --mm-color-drag-invalid

--mm-font-family-ui  --mm-font-family-mono
--mm-font-size-{xs,sm,md,lg,xl}  --mm-font-weight-{normal,medium,bold}
--mm-font-lineheight-{tight,normal,loose}
--mm-space-{0,1,2,3,4,5,6,8}  --mm-radius-{none,sm,md,lg,full}
--mm-border-width-{thin,thick}  --mm-shadow-{none,sm,md,lg}
--mm-motion-duration-{fast,normal,slow}
--mm-motion-easing-{standard,enter,exit}
--mm-density-{compact,normal,comfortable}
```

`--mm-color-state-<slug>` is not in the index above because it is not a
fixed property: it is OPTIONAL and minted per custom stage a theme chooses
to give its own color (§8.3), one property per declared `state.<slug>`
token, falling back to `--mm-color-accent-base` when a stage has none. The
five `--mm-color-state-*` properties above stay the fixed, always-required
set regardless of how many custom stages a board declares.

## Appendix C: route index

```
view   /  /projects  /p/:projectId  /p/:projectId/board
       /p/:projectId/item/:itemId  /p/:projectId/new
       /p/:projectId/report  /p/:projectId/check  /p/:projectId/settings
       /settings  /settings/theme  /settings/themes
       /settings/themes/:themeId  /about

api    /api/v1/health
       /api/v1/projects  /api/v1/projects/:projectId
       /api/v1/projects/:projectId/fingerprint
       /api/v1/projects/:projectId/items
       /api/v1/projects/:projectId/items/:itemId
       /api/v1/projects/:projectId/items/:itemId/{move,start,pause,finish,
                                                 block,unblock,note}
       /api/v1/projects/:projectId/detail/:itemId
       /api/v1/projects/:projectId/{report,check,wip,config,theme}
       /api/v1/{config,theme,themes,recent,favorites,events,scan}
       /api/v1/themes/import  /api/v1/themes/:themeId/export
```
