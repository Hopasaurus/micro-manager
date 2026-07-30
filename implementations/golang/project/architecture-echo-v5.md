# Echo v5 — reference notes

    Verified:  2026-07-29
    Sources:   see §8
    Scope:     generic; no project-specific content. Copy freely.

Working notes on the Echo web framework, v5, and on server-sent events with it.
Written to be lifted into any Go project — nothing here is specific to the one
it was written in.

**Read §7 before relying on anything.** This file separates what was checked
against primary sources from what was not. The unverified list is the useful
half: it says where to look before you trust your memory of v4.

---

## 1. Version and module paths

v5 is the current released major line — not a beta, not a branch.

```
go get github.com/labstack/echo/v5
```

| Import | Contents |
|---|---|
| `github.com/labstack/echo/v5` | core: `Echo`, `Context`, routing, `Start` |
| `github.com/labstack/echo/v5/middleware` | bundled middleware |

The `/v5` suffix is part of the module path, as Go requires for major versions
above 1. Code and examples written for `github.com/labstack/echo/v4` will not
compile against it unchanged — see §3.

## 2. Minimal server

```go
package main

import (
    "net/http"

    "github.com/labstack/echo/v5"
)

func main() {
    e := echo.New()

    e.GET("/", func(c *echo.Context) error {
        return c.JSON(http.StatusOK, map[string]string{"message": "Hello, World!"})
    })

    e.Start(":1323")
}
```

## 3. What changed from v4

Verified difference, and the one that breaks every copied example:

**The handler context is a pointer type.**

```go
// v4
func(c echo.Context) error      // interface

// v5
func(c *echo.Context) error     // pointer to struct
```

Consequences worth knowing before you start:

- Any helper with a `echo.Context` parameter needs its signature updated.
- Test doubles that implemented the v4 `Context` interface no longer work;
  there is no interface to implement. Construct a real context instead.
- Middleware signatures that mention the context change the same way.
- A nil context is now representable, where the interface form made that a
  typed-nil footgun instead. Neither is pleasant; the pointer form at least
  fails obviously.

Most v4 tutorial code, blog posts, and LLM-recalled snippets predate this. When
something does not compile and the error mentions `Context`, this is why.

## 4. Server-sent events

### 4.1 Echo has no built-in SSE support

There is no `c.SSE()`, no event type in the library, and no middleware. Echo's
own documentation demonstrates SSE using standard `net/http` mechanisms and
example code you copy into your project. Treat every type in §4.3 as *yours*,
not the framework's.

This is fine — SSE is a simple wire format — but budget for owning the code.

### 4.2 Response headers

```go
w := c.Response()
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("X-Accel-Buffering", "no")   // if anything nginx-shaped is in front
```

### 4.3 An event type

The shape Echo's cookbook demonstrates — copy it, it is not library API:

```go
type Event struct {
    ID      []byte
    Data    []byte
    Event   []byte
    Retry   []byte
    Comment []byte
}

func (e *Event) MarshalTo(w io.Writer) error { /* … */ }
```

`MarshalTo` must honor the SSE wire format, and the part that catches people:
**every line of a multi-line payload needs its own `data:` prefix.** A payload
containing a newline that is written as one `data:` line is silently truncated
at the newline by the browser. If you push HTML fragments over SSE, this is the
detail you will get wrong.

### 4.4 Flushing

Nothing reaches the client until you flush:

```go
rc := http.NewResponseController(w)
// after each event:
rc.Flush()
```

`http.NewResponseController` (Go 1.20+) is the current way; it works through
wrapper `ResponseWriter`s that a type assertion to `http.Flusher` would miss —
which matters, because Echo wraps the writer.

### 4.5 Detecting disconnect

```go
case <-c.Request().Context().Done():
    // client went away
    return nil
```

Watch the request context. Do **not** rely on write errors to notice a departed
client: they arrive late, or never, and you leak a goroutine per abandoned tab
in the meantime.

### 4.6 `WriteTimeout` must be 0

The one that bites, and the reason to read this file at all:

```go
e.Server.WriteTimeout = 0     // set in BeforeServeFunc
```

SSE connections are long-lived by definition. Any server write deadline
eventually fires and kills the stream. The symptom is a connection that works
perfectly for exactly `WriteTimeout` and then drops — which reads as a flaky
network or a flaky client, and sends you debugging the wrong layer.

If the rest of the server needs a write timeout, run the SSE endpoint on a
separate server instance rather than disabling the timeout globally.

### 4.7 Compression must skip the route

Gzip middleware buffers, and buffering is the one thing a stream cannot
tolerate. Configure a skipper so the event route is never compressed.

The failure mode is nasty: everything looks correct — connection opens, no
errors, handler runs, events written — and the client simply never receives
anything. Test for it explicitly.

### 4.8 Shape of a complete handler

```go
func sseHandler(c *echo.Context) error {
    w := c.Response()
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    sub := broker.Subscribe()
    defer broker.Unsubscribe(sub)

    rc := http.NewResponseController(w)
    for {
        select {
        case ev := <-sub.C:
            if err := ev.MarshalTo(w); err != nil {
                return nil          // client gone mid-write
            }
            rc.Flush()

        case <-c.Request().Context().Done():
            return nil
        }
    }
}
```

Fan out through a broker with **bounded** per-subscriber channels, and drop a
subscriber that cannot keep up rather than blocking the publisher. One slow
browser tab must never stall the thing producing events.

## 5. Pairing with htmx

The htmx SSE extension is a **separate script** from htmx core and must load
after it. Available via CDN (`htmx-ext-sse` on jsDelivr), direct download, or
`npm install htmx-ext-sse`.

| Attribute | Meaning |
|---|---|
| `hx-ext="sse"` | enable the extension on an element and its subtree |
| `sse-connect="<url>"` | open an `EventSource` to this URL |
| `sse-swap="<name>"` | swap the payload of this named event into the DOM |
| `sse-close="<name>"` | close the connection when this event arrives |
| `hx-trigger="sse:<name>"` | fire a normal htmx request when this event arrives |

```html
<div hx-ext="sse" sse-connect="/events" sse-swap="message">
    Contents updated in real-time with SSE messages
</div>
```

Lifecycle events: `htmx:sseOpen`, `htmx:sseError`, `htmx:sseClose` — the close
event carries a reason of `nodeMissing`, `nodeReplaced`, or a message-triggered
close. The extension adds exponential-backoff reconnection on top of the
browser's own `EventSource` retry.

One connection serves a whole subtree: put `hx-ext`/`sse-connect` on a wrapper
and let many children listen for different named events. Browsers cap concurrent
connections per origin, so do not open one stream per widget.

### 5.1 `sse-swap` vs `hx-trigger` — pick deliberately

Two genuinely different architectures:

**`sse-swap`** — the event payload *is* the HTML, swapped in directly. One round
trip. Costs: the server now renders fragments in two places (the normal route
and the event publisher), and you own the multi-line `data:` escaping of §4.3
forever, for every template.

**`hx-trigger="sse:name"`** — the event is a bare notification; htmx then fetches
the fragment over the normal route. Costs one extra round trip. Buys: exactly
one rendering path, and polling parity for free, since falling back to
`hx-trigger="every 5s"` changes nothing but the trigger.

Default to `hx-trigger` unless the extra round trip is genuinely expensive.
The one-rendering-path property is worth more than a round trip on any
low-latency link, and it is what keeps a no-SSE fallback honest instead of
theoretical.

### 5.2 Keep a slow poll as a backstop

```html
hx-trigger="sse:board from:body, every 30s"
```

Streams die — sleep/wake, proxies, laptop lids. The extension reconnects with
backoff, but a slow poll alongside the SSE trigger guarantees convergence even
while it is backing off, at negligible cost.

## 6. Checklist for a new SSE endpoint

- [ ] `Content-Type: text/event-stream`, `Cache-Control: no-cache`
- [ ] Flush after every event via `http.NewResponseController`
- [ ] Multi-line payloads prefix every line with `data:`
- [ ] `WriteTimeout = 0` on the serving instance (§4.6)
- [ ] Compression middleware skips the route (§4.7)
- [ ] Disconnect watched via `Request().Context().Done()` (§4.5)
- [ ] Bounded subscriber channels; slow subscribers dropped
- [ ] Broker goroutines exit when the last subscriber leaves
- [ ] `htmx-ext-sse` loaded *after* htmx core
- [ ] A no-SSE path exists and is exercised, not assumed

## 7. Not verified — check before relying on these

Everything above was read from primary sources on the date at the top. The
following were **not**, and v4 habits are unreliable here:

- `echo.Renderer` interface shape, and how `html/template` is wired in v5
- `HTTPErrorHandler` signature and the shape of `echo.HTTPError`
- The `middleware` package API: `Skipper` type, `GzipConfig`, `RecoverConfig`,
  CORS and CSRF configuration
- Group and route registration beyond the basics; route naming and reverse
  routing
- `Binder` and `Validator` interfaces
- Static file serving and `embed.FS` integration
- Graceful shutdown API
- Logger: whether `e.Logger` survives v5 unchanged
- The full `Context` method set beyond the pointer change of §3

To check quickly:

```bash
go doc github.com/labstack/echo/v5
go doc github.com/labstack/echo/v5 Context
go doc github.com/labstack/echo/v5/middleware
```

or read <https://pkg.go.dev/github.com/labstack/echo/v5>.

## 8. Sources

Fetched 2026-07-29:

- <https://github.com/labstack/echo> — version and release status
- <https://echo.labstack.com/> — documentation home
- <https://echo.labstack.com/guide/quickstart/> — minimal server, handler
  signature, module paths
- <https://echo.labstack.com/cookbook/sse/> — SSE: absence of built-in support,
  headers, `Event`/`MarshalTo`, flushing, disconnect detection, `WriteTimeout`
- <https://htmx.org/extensions/sse/> — the htmx SSE extension

Re-verify against these before starting a new project. Framework docs move, and
a stale reference note is worse than none because it is believed.
