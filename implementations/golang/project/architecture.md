# Architecture — Go implementation

    Status: draft
    Date:   2026-07-29
    Scope:  implementations/golang/

How the Go implementation is put together. The specifications in
`../../../project/` define *what* must happen; this document records *how* this
implementation does it, and pins the choices that are not free to vary between
packages.

Specs are normative. Where this document appears to contradict one, the spec
wins and this document has a bug.

---

## 1. Artifacts

One library, three binaries, all in one module.

```
                      +---------------------------+
                      |          mm/              |
                      |   the library — all       |
                      |   domain logic lives here |
                      +-------------+-------------+
                                    |  compiled in
          +-------------------------+-------------------------+
          |                         |                         |
   +------v------+          +-------v-------+         +-------v-------+
   |  cmd/mm     |          |  cmd/mm-ui    |         |  cmd/mm-tui   |
   |  CLI        |          |  GUI service  |         |  terminal UI  |
   |             |          | Echo+htmx+SSE |         |  (later)      |
   +------+------+          +-------+-------+         +-------+-------+
          |                         |                         |
          +-------------------------+-------------------------+
                                    |
                          +---------v----------+
                          |   markdown files   |
                          +--------------------+
```

Per `spec-tools.md` §2.1 the front ends **compile the library in**. `mm-ui` MUST
NOT shell out to `mm`; `mm-tui` MUST NOT speak HTTP to `mm-ui`. They are peers
over the same files, and §7 of that spec is what keeps concurrent use safe.

## 2. Package layout

```
implementations/golang/
  go.mod
  mm/                      THE LIBRARY — importable, never internal/
    store.go               Open, Store, per-directory operations
    item.go                Item, Slot, Directory, Date, Prio, State…
    parse.go               frontmatter, item lines, sections
    write.go               atomic writes, round-trip fidelity
    validate.go            I1–I10, one implementation
    discover.go            DiscoveryOptions / DiscoveryResult
    report.go              period resolution, report building
    config.go              config + theme load and merge
    errors.go              the error taxonomy
  cmd/
    mm/main.go             CLI entry point
    mm-ui/main.go          GUI service entry point
    mm-tui/main.go         TUI entry point (later)
  internal/
    cli/                   flag parsing, rendering, exit codes
    web/                   everything in §4
    tui/                   later
  testdata/                fixture micro-manager directories
  micro-manager/           this implementation's own todo directory
  project/                 planning documents, including this one
```

`mm/` is not under `internal/` on purpose: three binaries and, eventually,
external consumers import it.

`internal/web` and `internal/cli` are wrappers. Nothing in them may contain a
domain rule. The test is mechanical: if a function there would also be needed by
the TUI, it is in the wrong package.

## 3. Dependency policy

Standard library first. Every dependency needs a reason recorded here.

| Dependency | Used by | Reason |
|---|---|---|
| `github.com/labstack/echo/v5` | `internal/web` | HTTP routing, middleware, error handling |
| `github.com/labstack/echo/v5/middleware` | `internal/web` | Recover, request ID, gzip (see §4.5) |
| htmx (vendored JS) | `internal/web/static` | Server-driven interactivity without a SPA |
| `htmx-ext-sse` (vendored JS) | `internal/web/static` | SSE extension; a separate file from htmx core |

That is the whole list. The library `mm/` MUST have **zero** third-party
dependencies — it is compiled into every front end, and a transitive dependency
there is one the TUI and CLI pay for too.

### Echo v5

Framework reference notes, verified against primary sources, live in
[architecture-echo-v5.md](architecture-echo-v5.md) — including the parts of the
v5 API that were *not* verified and should be checked before use. That file is
generic and reusable; this section records only what this project depends on.

v5 is the current released major line. Module path
`github.com/labstack/echo/v5`, middleware at `.../v5/middleware`.

One API difference from v4 matters for everyone writing a handler: **the context
is a pointer type, `*echo.Context`, not v4's interface.**

```go
e := echo.New()
e.GET("/", func(c *echo.Context) error {
    return c.JSON(http.StatusOK, map[string]string{"message": "Hello, World!"})
})
e.Start(":7717")
```

Do not copy v4 handler signatures from older examples; they will not compile.

### Vendored JavaScript

htmx and `htmx-ext-sse` are **vendored into `internal/web/static/`**, not loaded
from a CDN, and embedded with `embed.FS`. The service binds to loopback and must
work with no network at all; a CDN reference would also leak the fact that the
tool is running to a third party. The SSE extension is a separate script and
MUST be loaded after htmx core.

## 4. The GUI: Echo v5 + htmx + html/template

### 4.1 The shape

Server-rendered HTML. Handlers call the library, render a template, and return
markup. htmx turns links, forms, and buttons into partial-page requests that
swap fragments in place. There is no client-side application state, no build
step, and no JavaScript framework.

This is chosen because `spec-gui.md` §2.1 already requires it in substance: the
browser client is *not trusted with domain rules*, and validation, WIP-limit
enforcement, and move legality are decided by the library on the server. A
server-rendered stack makes that the path of least resistance instead of a rule
to be enforced against a client that would rather cache state.

### 4.2 Layout of `internal/web`

```
internal/web/
  server.go        Echo instance, middleware chain, bind guard (§4.9)
  routes.go        the route table — mirrors spec-gui.md §4 exactly
  render.go        echo.Renderer over html/template, template cache
  errors.go        library error -> HTTP status / HTML fragment (§4.8)
  handlers_home.go
  handlers_board.go
  handlers_item.go
  handlers_report.go
  handlers_check.go
  handlers_settings.go
  handlers_theme.go
  api/             JSON handlers for /api/v1 (§4.7)
  templates/
    layout.html          app shell: header, main, status, toast region
    home.html
    projects.html
    board.html
    report.html
    check.html
    settings.html
    partials/
      column.html        one board column
      item-card.html     one item card — the DOM contract lives here
      item-panel.html
      toast.html
      dialog-*.html
      status-bar.html
  static/
    htmx.min.js          vendored
    htmx-ext-sse.js      vendored, loaded after htmx core
    mm.js                drag/drop + keyboard move + busy tracking only
    mm.css               styles, all colours via CSS custom properties
```

### 4.3 Templates and the DOM contract

`spec-gui.md` §5 fixes `data-testid` values, `data-*` state attributes, and
`mm-` class names as contract. Server-side templates are the reason to be
optimistic about holding that contract: **each element in the spec is emitted by
exactly one template**, so `item-card.html` is the single place an item card's
markup exists, for the board, the report, search results, and every htmx partial
swap.

Rules:

- A partial rendered on its own MUST be byte-identical to the same partial
  rendered inside a full page. htmx swaps fragments in, and a fragment that
  differs from its first-render form breaks the external test suite in a way
  that only shows up after an interaction.
- `data-*` state attributes are computed in the handler and passed to the
  template, never derived in JavaScript. State the client invents is state the
  server cannot be held to.
- Template functions for the repetitive attribute groups (`itemAttrs`,
  `columnAttrs`) keep the contract in one place and out of every template.

### 4.4 htmx patterns

| Interaction | Mechanism |
|---|---|
| Open item panel | `hx-get="/p/{id}/item/{itemId}"`, `hx-target="#panel"`, `hx-push-url="true"` |
| Save item | `hx-patch` on the form, swap the panel and the card |
| Start / pause / finish | `hx-post` to the action route, swap the affected columns |
| Filter the board | `hx-get` on change, `hx-push-url="true"` — URL stays addressable |
| Status bar, WIP counts | out-of-band swap (`hx-swap-oob`) on every mutating response |
| Toasts | `HX-Trigger` response header, or an OOB swap into `toast-region` |
| Live updates | SSE via `/api/v1/events` — see §4.5 |
| Freshness backstop | a slow `every 30s` poll on the same trigger, in case the stream dies |

`hx-push-url` is what satisfies §4.1 rule 1 — every reachable view state must be
addressable. Opening an item, filtering, and choosing a report period all change
the URI, and a direct load of that URI must produce the same state. That means
**every htmx route must also serve a full page** when requested without the
`HX-Request` header. `render.go` handles this centrally: one handler, two
renderings, chosen by the header.

Out-of-band swaps are how the status bar, WIP counters, and column counts stay
truthful after a mutation without a full reload. Every mutating handler returns
its primary fragment plus the OOB fragments its change invalidated.

### 4.5 Server-sent events

SSE is part of the design, not a later optimization. The CLI, a text editor, and
a second browser tab all write to the same files; without a push channel the UI
is only ever as fresh as its poll interval.

`spec-gui.md` §2.3 still requires the UI to work correctly with **polling
alone**, so SSE is a fast path layered over a design that is already correct
without it.

#### The endpoint

`GET /api/v1/events` — one stream per browser tab, scoped to the open project.

Echo has **no built-in SSE support**; this is plain `net/http` inside a handler:

```go
func (h *Handler) Events(c *echo.Context) error {
    w := c.Response()
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("X-Accel-Buffering", "no")

    sub := h.broker.Subscribe(projectID)
    defer h.broker.Unsubscribe(sub)

    rc := http.NewResponseController(w)
    for {
        select {
        case ev := <-sub.C:
            if _, err := ev.MarshalTo(w); err != nil {
                return nil
            }
            rc.Flush()
        case <-c.Request().Context().Done():
            return nil          // client went away
        }
    }
}
```

Three things that will silently break the stream if missed — see
[architecture-echo-v5.md](architecture-echo-v5.md) §4 for the detail:

1. **`WriteTimeout` must be 0** for the server, set in `BeforeServeFunc`. SSE
   connections are long-lived; any write deadline eventually kills them and the
   symptom looks like a flaky client.
2. **Gzip middleware must skip this route.** Compression buffers, and buffering
   is the one thing a stream cannot tolerate. Add a `Skipper` for
   `/api/v1/events`.
3. **Flush after every event** via `http.NewResponseController(w).Flush()`.
   Without it the events sit in the response buffer.

Disconnect is detected through `c.Request().Context().Done()` — not by write
errors, which arrive late or never.

#### Events carry notifications, not content

The htmx SSE extension offers two ways to use a stream: `sse-swap="name"`, which
swaps the event's payload directly into the DOM, and
`hx-trigger="sse:name"`, which fires a normal htmx request when the event
arrives.

**This implementation uses `hx-trigger`, and does not use `sse-swap`.** The
event body is a notification; the fragment is then fetched over the normal
route. Reasons, in order of weight:

1. **One rendering path.** The fragment that arrives after an SSE nudge is
   produced by the same handler that serves the poll, the direct load, and the
   post-mutation swap. With `sse-swap` there would be a second path that renders
   the same markup, and §4.3 requires every partial to be byte-identical
   wherever it comes from.
2. **Polling parity is free.** The same element, the same route, only the
   trigger differs — which is what makes "works with polling alone" true by
   construction rather than by a second code path nobody exercises.
3. **No multi-line escaping.** SSE requires every line of a `data:` payload to
   carry its own `data:` prefix. HTML fragments are multi-line, so pushing
   markup means getting that exactly right, forever, for every template.

The cost is one extra round trip per change, on a loopback connection, for a
tool with a single user. That is the correct trade.

#### Wiring

One `EventSource` per tab. `hx-ext` and `sse-connect` go on the app root;
children subscribe by name:

```html
<div data-testid="app" hx-ext="sse" sse-connect="/api/v1/events?project={{.ProjectID}}">
  <section data-testid="board"
           hx-get="/p/{{.ProjectID}}/board?fragment=1"
           hx-trigger="sse:board from:body, every 30s"
           hx-swap="outerHTML">…</section>
  <footer data-testid="app-status"
          hx-get="/p/{{.ProjectID}}/status"
          hx-trigger="sse:status from:body, every 30s"
          hx-swap="outerHTML">…</footer>
</div>
```

The `every 30s` clause is the backstop: if the stream dies and the extension's
exponential-backoff reconnect has not yet recovered, the view still converges.
When SSE is disabled by config the same elements render with `every 5s` and
nothing else changes.

Event names are a small closed set — `board`, `status`, `check`, `theme` — so a
change touches only the region it affects. The broker derives them from what
actually changed rather than publishing one firehose event.

#### Broker

`internal/web/broker.go`: a per-project subscriber registry, fed by a single
goroutine per open project that polls the library fingerprint
(`spec-tools.md` §2.4) and publishes named events on change. It is a fan-out
over an existing signal, not a second source of truth.

Bounded, buffered channels per subscriber; a subscriber that cannot keep up is
dropped and its client reconnects. A slow reader must never stall a mutation.

The extension's lifecycle events — `htmx:sseOpen`, `htmx:sseError`,
`htmx:sseClose` — drive a connection indicator in the status bar, so a dead
stream is visible rather than silently stale.

### 4.6 What htmx does not do: drag and drop

`spec-gui.md` §7 requires drag and drop with accurate mid-gesture DOM attributes
— `data-dragging`, `data-drop-target`, `data-drop-allowed`, `data-drop-index`,
`data-drop-reason`, and a `drop-placeholder` element — plus a keyboard move mode
with identical attributes. htmx has no answer for this, and neither does any
amount of server rendering.

`static/mm.js` is the only hand-written JavaScript in the project. It contains:

1. HTML5 drag-and-drop event handling on item cards and column bodies.
2. The keyboard move mode of §7.4, sharing one code path with the pointer drag
   so both maintain the same attributes.
3. `data-busy` tracking, wired to `htmx:beforeRequest` and `htmx:afterSettle`
   (and `htmx:responseError`), since that attribute is the quiescence signal the
   external suite waits on.

It contains no domain logic beyond one thing: the **static transition table** of
§7.2, replicated so hover feedback is instant. `spec-gui.md` §2.1 explicitly
permits the client to replicate rules to disable controls early, provided the
server's answer is authoritative — so the table decides what the cursor looks
like, and the server decides whether the drop happens. On rejection the client
restores the pre-drag DOM including every card's `data-position`.

The drop itself is issued through `htmx.ajax()` so the response flows through
the same swap and OOB machinery as every other mutation. No parallel update
path.

Keep this file small and dependency-free. If it grows past a few hundred lines,
something belongs on the server that has drifted onto the client.

### 4.7 Views and API side by side

`spec-gui.md` §4 requires both view routes and a JSON API under `/api/v1`. They
are separate handler sets over the same library calls:

- `internal/web/handlers_*.go` — HTML, for the browser.
- `internal/web/api/` — JSON, for third-party automation.

The API is not the UI's data source. The UI renders server-side; the API exists
because the spec requires it and because scripting the service is useful. Both
call the same `mm.Store` methods, so behavior cannot diverge.

Every mutating API endpoint accepts `dryRun: true`, mirroring `--dry-run`.

### 4.8 Errors

One Echo `HTTPErrorHandler` maps library errors to responses:

- **status** from `spec-gui.md` §4.3 — `NotFound`→404, `WipLimitReached`→409,
  `PreconditionFailed`→412, `InvariantViolation`→422, `Concurrent`→409, …
- **body** depends on the caller: JSON envelope for `/api/v1`, an HTML toast or
  dialog fragment for an htmx request, a full error page otherwise.

The error `code` string appears in every form, so a client branches on the name
rather than the status. `WipLimitReached` renders `dialog-wip-limit` with the
current occupants and the three remedies; it is the one error whose presentation
is most of its value.

### 4.9 Binding and local-only access

`spec-gui.md` §9.6 is a hard requirement, implemented in `server.go` before
Echo starts listening:

1. Resolve the bind address, default `127.0.0.1:7717`.
2. **Verify it is loopback** (`127.0.0.0/8` or `::1`) and refuse to start
   otherwise, naming the address — unless `server.allowRemote` is explicitly
   true. `0.0.0.0` and `::` are refused by the same check.
3. `allowRemote` is settable only from the config file or `--allow-remote`.
   There is no route and no template that can change it; a setting that removes
   a protection must not be reachable from the surface it defends.
4. Middleware rejects requests whose `Host` is not `localhost`, `127.0.0.1`,
   `[::1]`, or a configured hostname → 421; and state-changing requests with a
   foreign `Origin` → 403. This is DNS-rebinding defence, not ceremony: any page
   the user visits can issue requests to loopback.
5. `Content-Security-Policy` restricts `connect-src` to `'self'`. With htmx
   vendored and no CDN, the policy has nothing legitimate to allow.
6. Unix domain socket via `server.socket` where configured.

No authentication is specified anywhere, which is precisely why the above is not
optional.

### 4.10 Theming

Themes resolve in the library (`mm/config.go`), per `spec-gui.md` §8.7: project
`theme.json`, then system, then built-in, falling back **per token**.

The server renders resolved tokens as CSS custom properties on the app root, and
`mm.css` styles exclusively through them (`--mm-color-*`, `--mm-space-*`, …).
Nothing hard-codes a colour that a token covers.

Switching projects re-renders the shell with new values and updates
`data-theme-name` / `data-theme-source`. Because the properties are inlined in
the served HTML rather than fetched, there is no flash of the previous theme —
the requirement in §8.7 is satisfied by construction rather than by timing.

## 5. Concurrency

`spec-tools.md` §2.3 requires the library to satisfy the stricter of the two
front ends, and the GUI service is the stricter one.

- Echo handles each request on its own goroutine, so `mm.Store` must be safe for
  concurrent use. Internal synchronization, per the RECOMMENDED path — a mutex
  per open directory. Operations are short and file-bound.
- No operation blocks indefinitely. Any lock has a bounded wait and surfaces
  `ErrConcurrent` rather than hanging a request.
- The CLI, the service, and a text editor may all write at once. Size-and-mtime
  detection at write time (`spec-tools.md` §7 rule 5) is the mechanism; the
  service does not get a privileged position.
- Discovery results are cached with an explicit rescan, since fingerprint
  polling detects change *within* a project and never a new project appearing.
- SSE holds one goroutine per open connection plus one fingerprint poller per
  open project (§4.5). Both exit on `Request().Context().Done()` and on the last
  subscriber leaving; neither may hold a `Store` lock while blocked on a send.

## 6. The TUI

Deferred. `cmd/mm-tui` and `internal/tui` are reserved. It links the same
library, shares the config, theme, recent and favorites files byte-for-byte, and
opens no listening socket (`spec-tui.md` §2.1, §9).

Nothing in the GUI design may push a rule into `internal/web` that the TUI would
also need. The transition table replicated in `mm.js` is the one deliberate
duplication, and it duplicates a table that is fixed in the spec rather than
logic that could drift.

## 7. Testing

Library tests carry the weight; the web layer gets thin coverage over routing,
error mapping, and the bind guard.

- **Round-trip** — parse every fixture, write it back unchanged, assert bytes
  identical. Cheapest possible guard against dropping unregistered fields.
- **`check.sh` as oracle** — run the Go validator and the reference `check.sh`
  over the same directories and assert they agree on violation sets. Two
  implementations of ten invariants will diverge; this finds it.
- **Fragment parity** — render each partial standalone and inside its page,
  assert identical markup (§4.3).
- **Bind guard** — table test over addresses, asserting refusal for everything
  non-loopback without `allowRemote`.
- **SSE** — connect to `/api/v1/events` with `httptest`, mutate a fixture, and
  assert the expected named event arrives and the stream stays open. Also assert
  the gzip skipper is in effect for that route: a compressed event stream is the
  failure mode that looks like everything working until nothing updates.
- **Hygiene** — grep `mm/` for `os.Exit`, `fmt.Print*`, `os.Getenv`; fail if
  present.

The external browser conformance suite of `spec-gui.md` §12 is out of scope for
this module's tests. It drives a running server and is not a Go test.

## 8. Build

```bash
go build ./...
go test ./...
go vet ./...

go run ./cmd/mm --check --all
go run ./cmd/mm-ui --bind 127.0.0.1 --port 7717
```

Templates and static assets are embedded with `embed.FS`, so each binary is a
single file with no runtime asset path. A `-dev` flag MAY read them from disk
instead, for editing templates without recompiling.

## 9. Open questions

1. **`html/template` vs `templ`** — `html/template` is stdlib, needs no build
   step, and its contextual escaping is the right default for user-supplied
   titles. `templ` gives compile-time checking of the DOM contract, at the cost
   of a code generation step. Starting with `html/template`; revisit if template
   drift becomes a real source of contract violations.
2. **Where `--dry-run` surfaces in the UI** — the API requires it; the HTML
   views have no obvious place for it. Possibly a preview in destructive
   dialogs. Undecided.
3. **Event granularity** — `board`, `status`, `check`, `theme` is a guess at the
   right split (§4.5). If board re-fetches turn out to dominate, a per-column
   event may be worth it. Measure before splitting.
4. **Multi-project streams** — one `EventSource` per tab is scoped to the open
   project. The Home and `/projects` views show many projects at once and
   currently poll. Whether they warrant a stream is unresolved.

Resolved, recorded so they are not re-litigated: Echo v5 is released and is the
target (§3); SSE is in scope from the start (§4.5).
