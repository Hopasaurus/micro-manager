# Implementation plan: stage-field board redesign (v1 → v2)

    Status:   plan, ready to execute
    Depends:  notes/add-columns.md — 21 decisions, research closed, read that
              for the *why*; this file is only the *in what order*

---

## M0 — Spec patch (blocks everything below)

`spec-file-format.md`, `spec-tools.md`, `spec-gui.md`, `spec-tui.md`. No Go
changes in this milestone.

- [ ] `spec-file-format.md`: `version: 2`; `backlog.md` → `board.md` (decision
      5) — its body drops the three fixed `## ` sections (dec 1) for a flat
      `stage:`-ordered list; `working.NN.md` retired entirely
- [ ] Field registry: `blocked:` renamed to `reason:` (dec 17), valid on any
      stage, required only where `needs_reason` lists it (dec 18); new item
      field `tickler_dest` (dec 15)
- [ ] New `board.md` frontmatter keys: `stages` (default
      `someday,ready,blocked,working`, dec 4), `stage_labels`, `wip.<slug>`
      (map shape, absent = unlimited incl. `working`, dec 3 + 10),
      `tickler_stages` as `SOURCE->DEST` pairs (default `someday->ready`,
      dec 15), `needs_reason` (default `blocked`, dec 14)
- [ ] Referential-integrity rule: any slug in `wip.*`/`tickler_stages`/
      `needs_reason`/`stage_labels` must be a member of `stages`
- [ ] Invariants: I1 simplifies to `board.md`/`done.md`; I4 and I10 retired;
      I5 and I7 generalized per `needs_reason`/`tickler_stages`
- [ ] `details/<ID>.md` (§5.4) carve-out: `## Plan` is normative
      subtask-checkbox syntax when present (dec 2); detail file MAY now be
      auto-created by `--note`/`--subtask`, not only `--pause`
- [ ] Migration mechanism + version-history table (§7.1 of the research
      note), seeded with the `1 → 2` entry
- [ ] `spec-tools.md`: `--migrate` generalized to the chained/versioned
      mechanism; longevity policy stated (mutating ops refuse pre-migration
      with a named-version error; read ops MAY still work, implementer's
      choice); `--start`/`--pause`/`--finish` extended to treat any declared
      stage as a legal source/destination; `--report --include-wip`/
      `--stats` generalize to repeatable `--include-stage=SLUG`, `--include-
      wip` kept as shorthand (dec 7); WIP check keyed to `wip.<slug>`
      generically; library API gets a `Stage` type, directory-context
      parsing (dec 13)
- [ ] `spec-gui.md`: column list generalized from the fixed five to
      `stages:` order, with a documented DOM pattern for a stage column
      (`board-column-stage-<slug>`); `state.<slug>` theme tokens, optional,
      fallback `accent.base` (dec 8); drag-and-drop legality table extended
- [ ] `spec-tui.md`: mirror the column/theming changes; keyboard model
      unaffected in shape
- [ ] Full-text sweep of all four specs for `backlog.md`/`working.NN.md`/
      `## Ready`/`## Blocked`/`## Someday` mentions the research note didn't
      already work through, to catch anything missed

## M1 — Go: v2 support + migration step, fixtures only

No changes to the live board (`implementations/golang/micro-manager`) or the
CLI/GUI/TUI's day-to-day surface beyond what `--migrate` needs. Build and
test against `sample-data/` plus new v2 fixtures.

- [ ] `item.go`: `State` narrows to `board`/`done` (`StateBoard`, not
      `StateBacklog`, dec 16); `Stage` as a non-enum `string` type, no
      built-in constants (dec 12); `ParseStage(s, declared)` needs directory
      context, same pattern as `id_prefix`/`id_width` (dec 13)
- [ ] `parsefile.go`/`write.go`: `board.md` reader/writer (flat list, no
      section headings); `reason:` field; the five new frontmatter keys
- [ ] `validate.go`: updated I1/I4/I5/I7/I10; the referential-integrity
      check for `wip.*`/`tickler_stages`/`needs_reason`/`stage_labels`
- [ ] `op_start.go`/`op_pause.go`/`op_finish.go`/`op_move.go`: generalized
      for arbitrary stages; WIP check against `wip.<slug>`
- [ ] `op_tick.go`: `tickler_stages` (`SOURCE->DEST`) and `tickler_dest`
      override, for both one-shot and recurring fires (dec 15)
- [ ] New `migrate.go`: a registered step chain (`[]MigrationStep`, just one
      entry — `1→2` — for now); `Store.Migrate(dryRun)`; folds
      `working.NN.md` into `board.md`, moves `## Notes`/`## Plan`/
      `## Blockers` into `details/<ID>.md`, renames the file, rewrites its
      opening description; warns on uncommitted git changes
- [ ] New v2 test fixtures (parallel to `sample-data/sample1-3`) exercising
      custom stages, `tickler_stages`, `needs_reason`, `wip.<slug>` caps
- [ ] Migration round-trip test: a captured v1 board with real notes/
      subtasks/blockers in a working file, asserting nothing is lost

## M2 — Prove the migration step against real data

- [ ] Branch or scratch-copy `implementations/golang/micro-manager`
- [ ] `mm --migrate --dry-run` against the copy; read the report
- [ ] Run for real on the copy; `mm --check` the result
- [ ] Spot-check that working-file notes/subtasks/blockers actually landed
      in the right `details/<ID>.md` files
- [ ] Fix whatever the real board's ~200 items surface that the synthetic
      fixtures in M1 didn't

## M3 — Cutover the live board

- [ ] `git status` clean in `implementations/golang/micro-manager`; commit
      anything pending first
- [ ] Capture this pre-migration state as the permanent v1 fixture (dec 21)
- [ ] Run `mm --migrate` for real against the live directory
- [ ] Verify: `mm --check`, `mm --status`, spot-check a few known items
      (e.g. this session's own T-0217–T-0225) render correctly
- [ ] From here on, day-to-day board work goes through the v2-supporting
      binary normally

## M4 — Finish the operation surface, retire v1 mutation support

- [ ] CLI: `--include-stage`, generalized `--wip` (per-stage), any other
      modifier surface the spec patch (M0) added
- [ ] GUI (`internal/web/board.go`): dynamic columns from `stages:`,
      `state.<slug>` theming with `accent.base` fallback
- [ ] TUI: mirror, if/when the TUI is implemented
- [ ] Mutating ops refuse cleanly (named-version error) against any
      directory still on v1
- [ ] Decide and act, per-directory, for everything else in the repo that
      isn't the live board or the deliberately-kept v1 fixture:
      `implementations/typescript/.micro-manager`, `sample-data/*` — migrate
      each, or mark it a deliberate second v1 fixture (dec 21), not an
      oversight
