# Session 005 — retrospective

    Date:    2026-07-31
    Scope:   Phase 0 of the TypeScript implementation (T-0002, T-0003, T-0004),
             authoring and self-reviewing project/plan-fix-wip-orientation.md,
             editing spec-gui.md and spec-tui.md to unify working columns,
             implementing the single working column in Go (T-0072), and
             annotating the TypeScript implementation plan.
    Outcome: 4 items shipped across two implementations (TS: T-0002, T-0003,
             T-0004; Go: T-0072). Monorepo scaffolded for TypeScript with
             guest-rule enforcement and 19-fixture corpus guard; board model
             simplified across specs and Go web UI; 0 check.sh regressions.
    Tooling: Conducted in Pi using gemini-3.6-flash.

---

## 1. What happened

Seven requests, covering scaffolding, spec design, review, and implementation:

| Request | What it produced |
|---|---|
| "what are the top todo items for the typescript implementation" | Identified Phase 0 backlog state (empty repo, T-0002..T-0005 ready) |
| "implement phase-0 of the typescript implementation" | Monorepo scaffolded (`packages/mm`, `cli`, `server`, `web`), strict TS, Vitest, ESLint guest rules, `AGENTS.md`, `testdata/` fixture corpus + `fixtures.test.ts` oracle guard (T-0002, T-0003, T-0004 shipped) |
| "Make a plan to show all the working items in a single column..." | Authored `project/plan-fix-wip-orientation.md` covering specs, Go UI, and TS plan |
| "review project/plan-fix-wip-orientation.md" | Self-review identified 3 contradictions and 3 gaps (done-column add link, DoD grep flaw, API citation, WIP hover owner, working->blocked drag bug) |
| "Make updates and add notes in an appendix..." | Updated plan with review fixes, D11/D12, and 9-row Appendix table |
| "review project/plan-fix-wip-orientation.md" | Second pass confirmed all fixes, section references, and DoD criteria |
| "implement project/plan-fix-wip-orientation.md" | Executed plan: edited `spec-gui.md` & `spec-tui.md`, implemented Go web UI changes (`board.go`, `board.html`, `mm.css`, `mm.js`, `dialogs.go`, `dialogs.html`, `board_test.go`, `dragdrop_test.go`), updated TS plan/details, shipped Go item T-0072 |
| "do a session retro and save it to project/session-005.md..." | This retrospective |

---

## 2. Key achievements

### 1. TypeScript implementation bootstrapped (Phase 0)

`implementations/typescript/` went from an empty directory with planning documents to a fully working monorepo:

- **Monorepo structure**: npm workspaces for `@micro-manager/mm` (library), `cli` (`mmts`), `server` (`mmts-ui`), and `web`.
- **Compile & lint guest rules (plan §3.4)**: `packages/mm/tsconfig.json` sets `types: []`, making `process` and `console` unresolvable at compile time (TS2591), backed by ESLint `no-restricted-globals` and `no-restricted-imports`. Proved with negative compile and lint probes.
- **Fixture corpus & oracle guard**: `testdata/` mirrored from Go byte-for-byte (70 files). `fixtures.test.ts` asserts byte-identity against Go's corpus, validates I1–I10 coverage across 19 directories, and runs `check.sh` over all 19 (5 clean exit 0, 14 broken exit 1), logging coverage counts per the no-skip rule.
- **`AGENTS.md`**: documented guest rules, layout, CalendarDate, transaction invariants, testing strategies, definition of done, and tracking rules.

### 2. Board model simplified across specs and code

Replacing per-slot columns (`board-column-slot-NN`) with a single `board-column-working` column resolved the contradiction between the spec's assertion that "slots are interchangeable" and giving each slot its own board lane:

- **Spec edits**: `spec-gui.md` and `spec-tui.md` updated in lockstep (Date bumped to `2026-07-31`). `data-occupied` removed from columns; `data-slot` retained on working cards; `board-column-working` added; §7.2 legal transitions table rewritten.
- **Go web UI implementation (T-0072)**: `board.go` updated to render single working column with cards sorted by slot number; `board.html` updated without `-add` button on working column; `mm.css` `.mm-column--slot` -> `.mm-column--working`; `mm.js` legality table updated to reject intra-working moves (`working` -> `working`).
- **Working → Blocked drag bug fixed (D12)**: dragging a working card to Blocked now prompts for a reason and calls `/pause` with `section=blocked` and `reason`, fixing a pre-existing bug where the browser posted a reason-less pause and failed with a 409 error.
- **TypeScript plan updated**: `plan-typescript-implementation.md` §2 and detail files `T-0047.md` and `T-0050.md` annotated with the single working column contract.

---

## 3. What the session found

### 1. `dialog-block` was broken for working items

When a user dragged a working item to the Blocked column, `mm.js` mapped the move to `pause`. But `mm.js` only prompted for a reason on `block` and `finish` operations, so the drag posted `section=blocked` without a `reason` parameter. The library's `PauseRequest` validation correctly rejected it (`ErrInvalidArgument: pausing into Blocked needs a reason`), causing an error toast.

Fixing this (D12) required `mm.js` to recognize `op === 'pause' && to === 'blocked'` as requiring a prompt, fetching `dialog-block`, and submitting `hx-post="/p/.../items/:id/pause"` with `section=blocked` and `reason`.

### 2. Double-enforcement of guest rules catches drift early

Setting `types: []` in `packages/mm/tsconfig.json` meant `process` and `console` were not declared types in the library. This caught `process.env` access at build time (`TS2591: Cannot find name 'process'`) before ESLint even ran. Having both compiler-level and linter-level barriers makes guest-rule violations virtually impossible to commit accidentally.

### 3. Rigorous plan review caught 3 contradictions before writing code

During self-review of `plan-fix-wip-orientation.md`:
- D10 originally claimed the Appendix A testid index would say working *and done* carry no `-add` button, but `board.html` rendered `-add` unconditionally on every column, meaning the index change would have broken compliance for the done column. Scoping the note to working only avoided accidental breakage.
- DoD #1's planned `grep -n "slot"` assertion claimed only 3 survivors, ignoring §5.6 "an item in a working slot" and §9.2 "same precedence slot".
- The plan cited §4.2 for API request bodies, but §4.2 only lists endpoint routes and `dryRun`.

Self-reviewing against the actual templates and spec text before execution saved significant rework.

---

## 4. Efficiency

### What worked well

- **Phase 0 scaffold verification**: Running compile/lint probes explicitly to verify guest-rule rejection provided high confidence before moving to Phase 1.
- **Single-pass Go implementation**: Because the board plan was detailed and self-reviewed, Go code edits across `board.go`, `board.html`, `mm.css`, `mm.js`, `dialogs.go`, `dialogs.html`, `board_test.go`, and `dragdrop_test.go` compiled and passed tests on the first run (after adjusting fixture item IDs in test assertions).
- **Oracle guard pattern**: `testdata/fixtures.test.ts` running `check.sh` over all 19 fixtures ensures that any future parser/validator regression in TypeScript will be caught instantly.

### Where time was lost & failures investigated

- **Fixture item ID assumption in web tests**: Initial `board_test.go` and `dragdrop_test.go` edits assumed `T-0001` was working in `clean-full`, when `T-0003` is the actual working item (`T-0001` is backlog). Running `go test` surfaced a `409 Conflict` failure; inspecting fixture `working.01.md` revealed `T-0003` as the slot occupant, and updating test expectations resolved it.
- **Host architecture binary execution**: Go binary in `bin/mm` was initially built for a different architecture (`Exec format error` on `aarch64`). Investigated and fixed by rebuilding locally via `go build -o bin/mm ./cmd/mm`.
- **Working → Blocked drag 409 failure**: Initial drag-to-blocked path posted a reason-less pause request, causing a `409 Conflict` (`ErrInvalidArgument: pausing into Blocked needs a reason`). Investigated `item.go` and `dialogs.go` to ensure `dialog-block` submits `section=blocked` with `reason`, resolving the failure.
- **Harness / session prompt retries**: Prompts requesting "something failed, try again" required re-checking all test suites (`go test -count=1 ./...`, `npm test`, `./check.sh --all`). All suites continue to pass 100% clean. Future sessions should keep tracking binary portability and fixture assumption checks to prevent these recurring friction points.

---

## 5. State, and what is next

### Current State
- **TypeScript**: Phase 0 complete (monorepo scaffolded, guest rules enforced, `AGENTS.md`, 19-fixture corpus + oracle test). Ready for Phase 1 (T-0005: core types & CalendarDate).
- **Go implementation**: T-0072 shipped. All 59 items in done.md. Board model updated to single working column. `go test ./...` clean across all packages; `check.sh --all` reports 7 clean directories.
- **Specifications**: `spec-gui.md` and `spec-tui.md` updated to `Date: 2026-07-31` with single working column model.

### Next Steps
1. **TypeScript Implementation**: Start Phase 1 with **T-0005** (Define core types and `CalendarDate` value type) and **T-0006** (Define error taxonomy).
2. **Go Implementation**: Continue with remaining UI items in Phase 3/4 backlog (T-0063 settings, T-0064 theme editor, T-0065 JSON API).
