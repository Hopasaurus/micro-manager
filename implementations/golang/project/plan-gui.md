# Plan — the GUI service

    Status: draft
    Date:   2026-07-30
    Scope:  implementations/golang/, spec-gui.md
    Items:  T-0041, T-0042, T-0046 – T-0070

The order in which `spec-gui.md` gets built, and why that order.

[architecture.md](architecture.md) already fixes *how* — Echo v5, htmx,
`html/template`, server-rendered fragments, SSE over a fingerprint poller. This
document does not restate it. It sequences the work, names the decisions that
must be taken before code is written, and records what is deliberately not being
built yet.

Specs are normative. Where this document appears to contradict one, the spec
wins.

---

## 1. Where things stand

The library and the CLI are done: every operation of `spec-tools.md` §5.1 plus
`--block`, `--unblock`, `--note`, `--wip` and `--find`, with `--dry-run`,
`--json` and `--porcelain` across all of them. 39 items closed, `## Ready`
empty until now.

Four things the UI needs and the library does not yet have:

| Gap | Spec | Item |
|---|---|---|
| Directory fingerprint | `spec-tools.md` §2.4 | T-0046 |
| `projectId` derivation | `spec-gui.md` §3.1 | T-0047 |
| Config and theme load, merge, resolve | `spec-gui.md` §8, §9 | T-0041 |
| Recent and favorites files | `spec-gui.md` §10 | T-0048 |
| `status`, `next`, `search` | `spec-tools.md` §5.2 | T-0042 |

All five are **library** work, not web work. They are shared with the TUI, and
`spec-gui.md` §13 binds the TUI to exactly these files and tokens. Building any
of them inside `internal/web` would have to be undone later.

## 2. Phases

Each phase ends somewhere the work can stop without leaving a half-built
surface. `## Ready` is ordered to match, so the top of the list is genuinely the
next thing that can be started.

Every item carries a `phase-N` tag, so a phase is a query rather than a range of
IDs — which matters because Phase 0 contains two items numbered below the rest:

```bash
mm --list --tag phase-0
```

### Phase 0 — library prerequisites (T-0046, T-0047, T-0041, T-0048, T-0042)

No HTTP, no templates. Five additions to `mm/`, each with the same test
discipline as everything already there: round-trip fidelity, unknown-key
preservation, and no environment access.

Ends when: the library can fingerprint a directory, name it, resolve its theme
and config, maintain the two list files, and answer a search.

### Phase 1 — the service exists and is safe (T-0049, T-0050, T-0051, T-0052, T-0053, T-0054)

`cmd/mm-ui` starts, refuses to bind anywhere but loopback, serves `/api/v1/health`
and a not-found view, and has a renderer that emits identical markup for a
fragment and for the same fragment inside its page.

The bind guard (T-0050) is second in the phase on purpose. `spec-gui.md` §9.6
gives the service unauthenticated read and write access to the user's files; the
protection that substitutes for authentication goes in before there is anything
worth reaching.

The httptest harness (T-0054) comes before the views rather than after, because
every view item afterwards is expected to arrive with tests, and a harness
written at the end gets written to fit what was built.

Ends when: `go run ./cmd/mm-ui` serves, refuses `0.0.0.0`, rejects a foreign
`Origin`, resolves a `projectId` to a store, and maps every library error to the
status of §4.3.

### Phase 2 — the board works (T-0055, T-0056, T-0057, T-0058, T-0059)

The app shell, the board, the item panel, every mutating operation, and drag and
drop with its keyboard equivalent. This is the phase that makes the tool usable,
and it is where the DOM contract is either honoured or quietly broken — every
`data-testid` and `data-*` attribute of §5.5 and §5.6 lands here.

T-0059 (drag and drop) is last in the phase and depends on T-0058 being real:
§6.2 requires every drag to have a non-drag equivalent, so the item menu and the
panel actions must exist and work *before* the accelerator over them is written.
Building the drag first would make the menu an afterthought, which is the one
outcome the spec names three separate reasons to avoid.

Ends when: a person can run the service against their own micro-manager
directory and use it instead of the CLI for a day.

### Phase 3 — the rest of the views (T-0060, T-0061, T-0062, T-0063, T-0064)

Report, check, home and `/projects`, settings for both scopes, and theming.
These are independent of each other and can be taken in any order; the listed
order is by how often the view is used.

Theming (T-0064) is last of the five because the shell already emits the tokens
from Phase 2 — this item is the *editor*, the library, and import/export, not
the mechanism.

Ends when: every route in Appendix C serves its view.

### Phase 4 — freshness, machines, and conformance (T-0065, T-0066, T-0067, T-0068, T-0069, T-0070)

The JSON API, SSE, test mode, accessibility, `/about`, and the contract audit.

T-0065 (the API) is high priority despite arriving late: it is required by §4.2,
it is what a script drives, and it is the second consumer that proves no domain
rule has leaked into a template. It is *not* the UI's data source — the UI
renders server-side (`architecture.md` §4.7).

T-0070 (the audit test) is deliberately at the end, because it asserts against
the finished surface, and deliberately *in* this module rather than left to the
external suite, because a Go test that fails in two seconds is cheaper than a
browser run and will exist sooner.

Ends when: `go test ./...` proves every testid in Appendix A, every custom
property in Appendix B, and every route in Appendix C is served.

## 3. Decisions to take before the code that depends on them

Each of these has a wrong default that is expensive to reverse.

1. **NFC normalisation for `projectId`** (T-0047, before Phase 0 ends). macOS
   hands back decomposed filenames; without normalisation the same directory
   gets two different ids on two platforms. The stdlib has no NFC, and `mm/` is
   required to have zero third-party dependencies. Either implement the narrow
   case, or accept `golang.org/x/text` in the library and record the reason in
   `architecture.md` §3. Not a decision to discover halfway through Phase 2.
2. **HTML5 drag events versus pointer events** (T-0059, before Phase 2 ends).
   The §5.5 markup says `draggable="true"`, which implies HTML5 DnD; HTML5 DnD
   is also the hardest thing for an automation harness to synthesize. Whatever
   the external suite can drive is the constraint that decides this.
3. **Where `--dry-run` surfaces in the HTML views** (`architecture.md` §9
   question 2). The API must accept it. The views have no obvious place for it.
   A preview inside the destructive dialogs is the current guess; decide during
   T-0058 rather than leaving it open.
4. **Projects outside every scan root** (T-0052, T-0062). §5.3 groups
   `/projects` by scan root, which leaves no place for a project reachable only
   through favorites. Either it gets a group of its own or it is reachable from
   Home alone. Pick one and write it down.
5. **Pin the Echo version.** v5.3.1 is the current release; `go.mod` should name
   it rather than floating. Note that T-0040 (module path) is still deferred and
   unrelated.

## 4. Risks

| Risk | Containment |
|---|---|
| The DOM contract drifts silently | T-0070 parses the spec's own appendices; T-0054 asserts fragment parity from Phase 1 |
| A domain rule leaks into `internal/web` or `mm.js` | The mechanical test from `AGENTS.md`: if the TUI would need it, it is in the wrong package. §7.2's transition table is the one permitted duplication |
| The service is reachable from the network | T-0050 before any real surface exists; `allowRemote` unreachable from the UI by construction |
| Two front ends strip each other's settings | Unknown-key preservation is a requirement of T-0041 and T-0048 with a round-trip test, not a review item |
| A theme edit or a CLI write goes unnoticed | Fingerprint polling (T-0046) is the floor; SSE (T-0066) is a fast path over a design that is already correct without it |
| Optimistic drag updates corrupt the board | §7.1 forbids applying a drop without confirmation unless it can be fully reverted, `data-position` included; asserted in T-0059 |

## 5. Definition of done

`AGENTS.md` §"Definition of done" applies unchanged — build, test, vet, oracle
agreement with `check.sh`, round-trip, no unexplained dependency.

Four additions for anything in `internal/web`:

1. Every element the spec names carries its exact `data-testid`, and no required
   testid is renamed, omitted, or conditionally dropped.
2. Every partial renders byte-identically standalone and inside its page.
3. `data-busy="false"` means the view fully reflects server state — the external
   suite waits on it, so anything less makes every test in that suite flaky.
4. Nothing hard-codes a colour that a theme token covers.

## 6. Not tracked here

- **The external conformance suite** of `spec-gui.md` §12. It drives a running
  server, is not a Go test, and is shared with the Python, TypeScript and Erlang
  implementations. It belongs above `implementations/`, in its own directory
  with its own fixtures.
- **The TUI** (`spec-tui.md`). `cmd/mm-tui` and `internal/tui` stay reserved.
  Nothing in this plan may push a rule into `internal/web` that the TUI would
  also need.
- **Packaging and launch UX** — service supervision, a menu-bar launcher, opening
  a browser on start. No spec covers any of it.
- **T-0043 – T-0045** (archive, migrate, stats) and **T-0040** (module path),
  which remain in `## Someday` and are unrelated to the UI.
