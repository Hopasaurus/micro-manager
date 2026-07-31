# Session 003 — retrospective

    Date:    2026-07-30
    Scope:   planning the GUI, then phases 0, 1 and 2 of it
    Outcome: 26 items written, 16 shipped. 2,412 lines of new library across
             7 files; a working web service — 2,856 lines of Go, 1,599 of
             templates, CSS and JavaScript; 456 passing tests; 1 commit.
             `mm-ui` serves a usable board against this repository's own
             todo directory.
    Open:    phases 1 and 2 are uncommitted. One spec inconsistency is
             recorded and not acted on (§3).

---

## 1. What happened

Six requests, each one a phrase:

| Request | What it produced |
|---|---|
| "Make a plan & tasks … the spec is here" | `plan-gui.md`, 26 items (T-0046–T-0071), 9 detail files |
| "add phase tags to each item" | `phase-0`…`phase-4`, so a phase is a query rather than an ID range |
| "Work on the phase-0 items" | fingerprint, `projectId`, config, theme, lists, status, search |
| "Commit the working tree changes" | one commit, `a013fc4`, on a new branch |
| "Do phase-1" | `cmd/mm-ui`, guards, renderer, registry, error mapping, harness |
| "Implement phase-2" | shell, board, item panel, mutations, drag and drop |

The rhythm from session 002 held unchanged: start the item, implement, test,
validate with both checkers, write the decisions into `details/T-NNNN.md`, close
it. Two WIP slots were used properly for the first time — scaffold with its bind
guard, shell with its board — where one item genuinely cannot be finished without
the other existing.

## 2. What the session found

### The bugs the tests caught

**`project()` returned `(nil, nil)`.** The helper rendered the not-found view and
returned whatever rendering produced, which on success is `nil`. Every caller
read "no error, carry on" and dereferenced a nil store. The signature now means
*a nil store is the stop signal*, and the error is only how rendering went. This
is the shape of the mistake, not a typo: a function that both **handles** a case
and **reports** one needs two channels, and Go's single error return invites
collapsing them.

**Echo v5's router errors are an unexported type.** `errors.As(err, &echo.HTTPError{})`
does not match them, so every unrouted path returned 500 instead of 404.
`echo.StatusCode(err)` is the API that covers both. Found by the scaffold's
"an unknown route is a 404" test — written before there were any interesting
routes, which is the whole argument for writing it then.

**`canonical()` resolved symlinks before making paths absolute.** A relative path
has nothing to resolve, and the working directory prepended afterwards may itself
run through a symlink: `/var` → `/private/var` gave one directory two canonical
forms. It was already affecting discovery's deduplication, silently, and was
found by a `projectId` test that had no reason to care about symlinks.

**`mm.Section` values are capitalised.** `Ready`, because they name the
`## Ready` heading. The DOM contract fixes them lowercase, so every
`board-column-*` testid was wrong until the conversion was put at one boundary.
Two spellings of one concept will always exist here; what matters is that
exactly one function knows about both.

### The bugs only a browser caught

**Every item menu rendered open.** The markup carries `hidden`, and a test
asserted the attribute was present — but `.mm-item__actions { display: flex }`
silently beats the user agent's `[hidden] { display: none }`. *An attribute
assertion is not a visibility assertion*, and no amount of Go testing would have
said so.

**The service serves the JavaScript it was built with.** Assets are embedded
with `embed.FS`, so editing `mm.js` and reloading changes nothing until
`go build` runs again. The first keyboard test ran against a 227-line script
while the source was already 505 — and reported, accurately and uselessly, that
move mode did not engage. `Options.Dev` re-reads templates from disk; static
assets have no equivalent.

### The pattern

Session 002 concluded that *running the thing found what reading it did not.*
This session extends it: **looking at the thing found what running it did not.**
Both browser findings were invisible to a passing test suite, and both would have
reached the external conformance suite of §12 as flaky, hard-to-place failures.

## 3. The spec inconsistency — raised, then fixed

`spec-gui.md` §8.4 says every `color` and `gui` token becomes a CSS custom
property, and §8.3 lists `gui.density.{compact,normal,comfortable}`. **Appendix B
did not list `--mm-density-*`.**

The implementation emitted them anyway, on the grounds that §8.4 is the normative
sentence and the appendix is an index. `TestCSSPropertyNamesMatchTheSpecIndex`
parses Appendix B out of the spec file itself and asserts every name in it is
emitted — deliberately a *subset* check, so both could be true until the spec
settled.

**Applied at the end of this session**, on the user's instruction: Appendix B now
carries the three names. The implementation did not change, and the test stays a
subset check — §8.4 is what binds, so an index that falls behind again should
fail on what it omits, not on what the implementation correctly adds.

The finding was raised rather than fixed in the first place because session 002's
retrospective asked for exactly this judgement call to be handed back (§5, item
5). That worked: the question was asked, and answered, in one round trip. It is
worth continuing to ask — **a contradiction between a normative sentence and an
index is cheap to surface and expensive to guess at.**

## 4. Decisions taken, and by whom

Three decisions in this session were the user's, and each changed what got
written:

1. **The zero-dependency rule was relaxed.** `projectId` needs NFC
   normalisation, the stdlib has none, and `architecture.md` §3 said `mm/` MUST
   have zero third-party dependencies. Asked; answered "prefer permissively
   licensed dependencies". `golang.org/x/text` is in, and §3 now records a
   licence per dependency and says what the higher bar on `mm/` actually means.
   Hand-rolling Unicode normalisation would have put a correctness-critical
   implementation under the one identifier every route rests on.
2. **htmx was vendored.** ~60 KB of third-party JavaScript committed to the
   repository. Asked before downloading anything; answered yes, with the SSE
   extension. Versions are pinned and recorded: htmx 2.0.7 (0BSD),
   `htmx-ext-sse` 2.2.4 (BSD-2-Clause).
3. **Two open questions from `plan-gui.md` were resolved in passing**, and both
   are written down where the next reader will look: a project reachable only
   through favorites gets its own group in `/projects` (T-0052), and drag and
   drop uses HTML5 events rather than pointer events, because §5.5's markup
   settles it and the keyboard path carries identical attributes (T-0059).

The pattern worth keeping: **ask before adding a dependency or a file to the
repository; decide everything else and write down why.**

## 5. Efficiency

### Where the session lost time — my side

**Fixture assumptions in tests, ~8 wasted runs.** Repeatedly wrote tests that
assumed a fixture had a free WIP slot, or an item with a given ID, and found out
from the failure. `clean-full` has one slot and it is busy; `clean-multi-slot`
has one ready item, not four. **Reading the fixture first costs one command and
saves two round trips**, every time.

**Test expectations written before seeing the output.** The search tests listed
expected hits without accounting for the item `busySlot` puts in a slot; the
column-order test's regex matched `board-column-ready-header` as well as
`board-column-ready`. Both were my error, not the code's, and both cost a cycle.

**Python heredocs again.** Session 002 already recorded this. They were right for
the bulk moves — extracting the board templates into `partials/` — and wrong for
the single edits I used them for out of momentum. `Edit` produces a reviewable
diff and keeps the language server current.

**One misplaced browser click.** Clicking a button by screenshot coordinates
missed; the flow worked when driven through the DOM. For verifying behaviour,
`javascript_tool` is more reliable than synthesized clicks — and for *seeing*
whether something looks right, the screenshot is the only tool that works. They
answer different questions.

### Where the session was efficient — worth repeating

- **Phase tags.** `mm --list --tag phase-1` answered "what is left" instantly,
  and survived two items being promoted out of order into phase 0.
- **Running the service against this repository's own todo directory.** The
  board rendered 55 done items and 12 ready ones on the first try, which is a
  scale no fixture provides. It is also how the hidden-menu bug surfaced.
- **A scratch copy for anything that writes.** The keyboard-move test performed a
  real pause; it did so against a throwaway copy of `clean-multi-slot`, not
  against the live backlog.
- **Detail files written at close.** Every non-obvious decision in 16 items has
  a paragraph explaining it, and the cost was zero because the finish step
  demanded it anyway.

### What you could do differently

1. **Answer the spec question in §3.** It is the same question session 002
   raised, and it will come up in every remaining phase — the settings and theme
   items touch the parts of `spec-gui.md` most likely to contradict themselves.
2. **Say when to commit.** Phases 1 and 2 are uncommitted, 19 files of working
   tree, because commits were asked for explicitly once. "Commit at the end of
   each phase" would remove the question.
3. **Keep the requests phrase-sized.** "Do phase-1" carried more than a
   paragraph would have, because the items already hold the requirements — the
   same finding as session 002's "batch by item ID", now proven at a larger
   grain.
4. **Consider whether the external suite of §12 should exist before phase 4.**
   Both browser-only bugs would have been caught by it. It is not in any
   backlog, it is shared with the three unbuilt implementations, and T-0070's
   in-repo audit test is deliberately not a substitute.

## 6. State, and what is next

Phase 0, 1 and 2 are done. `## Ready` holds phases 3 and 4, in dependency order:

```
T-0060  report view                    T-0065  the JSON API
T-0061  check view                     T-0066  SSE broker and event stream
T-0062  home and projects              T-0067  test mode
T-0063  settings, both scopes          T-0068  accessibility
T-0064  theme editor and library       T-0069  about
                                       T-0070  the contract audit test
                                       T-0071  --status/--next/--search in the CLI
```

`## Blocked` is empty. `## Someday` still holds archive, migrate, stats and the
module path.

Not tracked anywhere, and unchanged from session 002:

- **The external conformance suite** of `spec-gui.md` §12 — now with two
  concrete bugs it would have caught.
- **The other three implementations.** `python/` and `erlang/` are empty.
  `typescript/` acquired a `.micro-manager` directory during this session that I
  did not create; it validates clean and is untracked.
- **The `--detail` namespace collision**, still a real defect in a shipped spec
  with nothing tracking it.
