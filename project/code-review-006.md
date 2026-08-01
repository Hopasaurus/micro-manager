# Code review 006 — pending changes on `main`

    Date:    2026-07-31
    Scope:   Uncommitted working-tree changes plus 12 untracked new files in
             implementations/golang: the JSON API package (internal/web/api),
             the SSE broker and freshness routes, the /about view, contrast
             warnings in the theme editor, ParseConfigFile/ParseProjectList/
             WriteProjectList in mm, and six new test files.
    Method:  Single careful diff pass. `git diff HEAD` for tracked files, full
             reads of every new file, `go build`, `go vet`, `go test ./...`,
             plus three throwaway probe tests to confirm suspected defects.
    Outcome: 7 findings — 6 CONFIRMED (reproduced), 1 PLAUSIBLE. One blocks the
             suite (26 failing tests), one silently disables the theme-refresh
             feature this change exists to deliver, and two can destroy a user's
             theme file.
    Tooling: Conducted in Claude Code using claude-opus-5.

---

## 1. Diff under review

No upstream is configured for `main`, so the review scope is the working tree
against `HEAD`.

| Area | Files |
|---|---|
| JSON API (new package) | `internal/web/api/{api,detail,events,files,items,lists,projects,report,respond}.go` |
| SSE | `internal/web/broker.go`, `internal/web/freshness.go`, `api/events.go` |
| Wiring | `internal/web/{routes,server,registry,api_service}.go` |
| Views | `internal/web/about.go`, `templates/{about,layout,check}.html`, `templates/partials/board.html`, `render.go` |
| Theme | `internal/web/theme.go` (contrast warnings) |
| Library | `mm/config.go`, `mm/list.go` (parse-from-bytes + atomic list write) |
| Tests (new) | `a11y_test.go`, `about_test.go`, `api_handlers_test.go`, `audit_test.go`, `sse_test.go`, `testmode_test.go`, `harness_test.go` |

`go build ./...` and `go vet ./...` are both clean. Every failure found is
behavioral.

---

## 2. Findings

### F1 — `projectIDOf` compares uncanonicalized paths; 26 tests fail (CONFIRMED)

`implementations/golang/internal/web/api_handlers_test.go:90`

On darwin `t.TempDir()` returns `/var/folders/…` while `mm.Discover` resolves
symlinks and returns `/private/var/folders/…`. The comparison `d.Path == path`
is therefore never true, and the helper falls through to
`t.Fatalf("discovery did not find %s", path)`.

Because every new test file routes through this one helper, the whole new
surface is red:

    go test ./internal/web/   →  26 failures
    (a11y_test, about_test, api_handlers_test, audit_test, sse_test, testmode_test)

The same defect appears a second time at line 137,
`strings.HasPrefix(d.Path, ts.Dirs[0])`, which fails `TestAPIDiscovery` with
`after refresh, expected at least 2 projects under the root, got 0`.

Verified: canonicalizing both sides with `mm.CanonicalPath` at both lines turns
the entire suite green. The patch was applied to confirm, then reverted — the
file on disk is byte-identical to the authored version.

Worth noting the codebase already documents this exact hazard, in
`registry.go:405` on `under()`:

> Discovery returns resolved paths — on macOS `/var/folders/...` comes back as
> `/private/var/folders/...`

The rule was known; the new tests just didn't apply it.

---

### F2 — `hx-target="#app"` matches no element; theme refresh never swaps (CONFIRMED)

`implementations/golang/internal/web/templates/layout.html:35`

The app root is declared as `<div data-testid="app" class="mm-app" …>` — there
is no `id` attribute anywhere on it. The new theme-refresh wiring targets
`#app`, a selector that resolves to nothing.

htmx raises `htmx:targetError` and aborts the swap. So: the poller detects the
`theme.json` mtime change, publishes the `theme` event, the browser receives it,
htmx fires — and nothing happens. The tab never re-themes, which is precisely
the §8.7 guarantee the shell-swap design exists to provide.

`TestSSEStreamAttributesInShell` passes because it asserts only that the
attribute *strings* appear in the body, never that the target resolves.

Confirming the convention: `mm.js:22` selects the same element with
`document.querySelector('[data-testid="app"]')`, not by id.

Fix: add `id="app"` to the div, or use `hx-target="this"`.

---

### F3 — `importTheme` writes the request envelope over the system theme (CONFIRMED)

`implementations/golang/internal/web/api/files.go:455`

The bare-document fallback is meant to forgive a client that PUTs the theme as
the whole body. It also swallows the wrapped envelope when the `theme` key is
missing:

```go
if req.Theme == nil {
    var doc rawDocument
    if json.Unmarshal(body, &doc) == nil && len(doc) > 0 && doc["theme"] == nil {
        req.Theme = doc          // ← the envelope itself
    }
}
if req.Theme == nil {            // ← can now never fire
    return fmt.Errorf("%w: a theme document is required", mm.ErrInvalidArgument)
}
```

`POST /api/v1/themes/import` with body `{"destination":"system"}` — a client that
simply forgot the `theme` key — assigns `req.Theme = {"destination":"system"}`.
Probed against `mm`:

    ParseTheme OK: id="" name="" appearance=""
    Validate warnings: 0
    Bytes: { "destination": "system" }

So it parses, validates clean, and `t.Save` writes `{"destination":"system"}`
over the user's system `theme.json`, destroying it. Destination `project`
clobbers a project's `theme.json` the same way. Only `library` is safe, because
the empty `t.ID` is rejected first.

The fallback needs to require that the document actually look like a theme — at
minimum, that it carries no `destination`/`dryRun` keys.

---

### F4 — `sample-one-dark` exports the micro-manager builtin (CONFIRMED)

`implementations/golang/internal/web/api/files.go:557`

```go
switch themeID {
case "micro-manager", "sample-one-dark":
    return mm.BuiltinTheme()
}
```

`mm/theme.go:552` shows `BuiltinTheme()` is `ID: "mm-default"`,
`Name: "micro-manager"`, `Appearance: auto`. There is no `sample-one-dark` theme
anywhere in `mm` — grep finds the string only in a `theme_test` id-validation
list.

`GET /api/v1/themes` (line 406) advertises `sample-one-dark` as a distinct dark
theme; `GET /api/v1/themes/sample-one-dark/export` then serves the default auto
theme under the filename `sample-one-dark.mm-theme.json`.

Secondary mismatch in the same listing: it advertises id `micro-manager` while
the document it yields carries id `mm-default`.

`theme.go`'s pre-existing `resolveExportTheme` has the same shape; the new API
surface inherits it rather than introducing it, but it ships the bug on a new
public endpoint.

---

### F5 — Deleting a project theme publishes no theme event (CONFIRMED)

`implementations/golang/internal/web/broker.go:166`

```go
if mt := themeStamp(store); !mt.IsZero() && mt != lastTheme {
```

`themeStamp` returns the zero time when `os.Stat` fails. A user who deletes
`theme.json` to fall back to the system theme trips the `!mt.IsZero()`
short-circuit before the inequality is ever evaluated: no event is published and
`lastTheme` is never reset. Open tabs keep rendering the deleted theme's tokens
until a manual reload.

The appearing case works — zero → non-zero passes the guard. Only the
disappearing case is dropped.

Currently masked by F2, since no theme event reaches the DOM either way. Both
need fixing before theme refresh works end to end.

---

### F6 — `deleteThemeFile` ignores `dryRun` and deletes anyway (CONFIRMED)

`implementations/golang/internal/web/api/files.go:363`

`DELETE /api/v1/theme?dryRun=true` calls `os.Remove` unconditionally and returns
`noContent`. The client asked to compute the change without writing and instead
loses the file irrecoverably.

Every other mutating endpoint in the package threads `req.DryRun || dryRun(c)`
into the library call — `putThemeFile:351`, `putConfigAt:134`,
`importTheme:522`, `putWip`, `putDetail`, and all seven item operations.
`items.go`'s own header states the invariant:

> Every mutating endpoint accepts dryRun and returns the change set without
> writing (spec-tools.md §3.4).

This handler is the sole exception.

---

### F7 — `wg.Add` can race `wg.Wait` at shutdown (PLAUSIBLE)

`implementations/golang/internal/web/broker.go:74`

`Subscribe` calls `b.wg.Add(1)` under `b.mu`; `close()` calls `b.wg.Wait()`
without taking `b.mu` and with no `closed` flag to gate new subscriptions.

`Server.Start` returns → `broker.close()` cancels `rootCtx` and blocks in
`Wait()`. An in-flight `GET /api/v1/events` reaches `Subscribe`, finds no
existing subscriber for its project, and calls `Add(1)`. If the last poller has
already exited so the counter is 0 while `Wait` is blocked, the runtime panics
with `sync: WaitGroup misuse: Add called concurrently with Wait` and takes the
process down during shutdown.

Not reproduced — the interleaving is narrow — but nothing in the code prevents
it. A `closed bool` checked under `b.mu`, returning an already-closed channel
when set, removes the window.

---

## 3. Verified clean

Things checked and found correct, recorded so a later pass need not redo them:

- **Gzip skipper.** `server.go:159` skips `/api/v1/events`, and `sse_test.go`
  proves the skipper is doing the work by asserting `/api/v1/health` *is*
  gzipped under the same `Accept-Encoding`.
- **`WriteTimeout = 0`.** Set in `Start`'s `BeforeServeFunc` (`server.go:234`),
  as `api/events.go`'s contract comment requires.
- **Flush discipline.** `http.NewResponseController` flushes the `: connected`
  preamble and every event.
- **Subscriber drop path.** A full buffer deletes and closes the subscriber; the
  handler's `defer unsubscribe()` then finds the map entry already gone, and the
  last removal cancels the poller. No double-close, no leaked poller.
- **`publish`'s `goto next`.** Legal Go (jump outward, no declarations skipped),
  and it compiles and vets clean.
- **Registry locking.** `rescanRaw` takes the write lock and releases it before
  `rescan` takes the read lock for `view()`; `rawResult` reads under RLock.
  `roots()`/`scan` are set once in `newRegistry` and never mutated. The slice
  returned by `rawResult` aliases registry state, but `rememberPath` only
  appends beyond the reader's `len`, so there is no torn read.
- **`mm` round-trip helpers.** `ParseConfigFile` and `ParseProjectList` share the
  exact parse `LoadConfigFile`/`LoadProjectList` run, so a document the API
  accepts is one the service can load. `WriteProjectList` parses before writing
  and writes atomically.
- **Error envelope.** `writeError` keys on the `/api/` path prefix, so the new
  package's returned library errors land in the §4.3 JSON envelope without the
  API package mapping anything itself.

---

## 4. Recommended order

1. **F1** — unblocks the suite; nothing else can be validated until it's green.
2. **F2 + F5** — together they make theme refresh actually work.
3. **F3 + F6** — both destroy user theme files; small, contained fixes.
4. **F4** — wrong bytes served, no data loss.
5. **F7** — hardening.

---

## 5. Observations

The comment density in this diff is unusually high and unusually load-bearing —
several comments state invariants precisely enough to review *against*. Two
findings came directly from that: F6 contradicts `items.go`'s own header, and F1
contradicts `registry.go:405`. That is a good property for a codebase to have.

The gap it doesn't close is between *asserting an attribute is present* and
*asserting the behavior works*. F2 is exactly that gap: the shell carries
`hx-target="#app"`, the test confirms the string is there, and the feature is
dead. A test that renders the shell and resolves the selector against the
emitted markup would have caught it.
