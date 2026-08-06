---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0202
updated: 2026-08-06
---

# Backlog

Implementation of `project/spec-tools.md` in Go: the library, then the `mm` CLI
wrapper over it, then the GUI service of `project/spec-gui.md`. See
[structure.md](structure.md) for the line format, `../AGENTS.md` for the working
rules, `../project/architecture.md` for how the implementation is put together,
and `../project/plan-gui.md` for the phasing of T-0046 onward. T-0128 onward
(tagged `flicker-fix`, phased `phase-1`…`phase-3`) follow
`../project/research-app-fllicker.md`, which finds the sources of refresh
flicker and sequences the fixes.

`## Ready` is ordered by dependency — top of the list is genuinely the next
thing that can be started.

## Ready

- [ ] [T-0180] Workflow tools: mm_start, mm_pause, mm_finish | prio:med | tags:plugin | detail:details/T-0180.md | created:2026-08-05
- [ ] [T-0181] mm_remove with the double guard | prio:med | tags:plugin | detail:details/T-0181.md | created:2026-08-05
- [ ] [T-0192] Make sure the UI has a place to enter the time when setting a wakeup type. | prio:med | detail:details/T-0192.md | created:2026-08-06

## Blocked

- [ ] [T-0135] CSS transition polish on swap (fade .htmx-added content) | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0135.md | created:2026-08-02 | blocked:report Phase 3; only if Phase 1 still feels abrupt
- [ ] [T-0136] Morph the theme shell swap to preserve focus and scroll | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0136.md | created:2026-08-02 | blocked:report Phase 3; needs idiomorph (T-0128); measure shell-swap cost first
- [ ] [T-0132] Library: expose per-file stamps for region-level change detection | prio:med | tags:library,flicker-fix,phase-2 | detail:details/T-0132.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0133] Broker: publish per-column events from a per-file diff; detail-body edits fire nothing | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0133.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0134] Per-column fragment routes and hx-triggers (gated on T-0131) | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0134.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead

## Someday

- [ ] [T-0165] Refine settings and theme config. | prio:med | detail:details/T-0165.md | created:2026-08-04
- [ ] [T-0182] /mm command surface | prio:med | tags:plugin | detail:details/T-0182.md | created:2026-08-05
- [ ] [T-0183] Context injection and configuration | prio:med | tags:plugin | detail:details/T-0183.md | created:2026-08-05
- [ ] [T-0184] Recommended tools: mm_block/unblock, mm_search, mm_report, mm_tick, mm_archive | prio:low | tags:plugin | detail:details/T-0184.md | created:2026-08-05
- [ ] [T-0185] Conformance tests: runner, exit codes, §8.1 smoke test | prio:med | tags:plugin | detail:details/T-0185.md | created:2026-08-05
- [ ] [T-0187] mm cli: add the ability to bulk add tasks | prio:med | tags:new-feature | created:2026-08-05
- [ ] [T-0189] Need a way to bulk move from someday to ready | prio:med | created:2026-08-06
- [ ] [T-0190] Need a way to make drag scroll down | prio:med | detail:details/T-0190.md | created:2026-08-06
- [ ] [T-0191] update the spec so that if backlog.md and done.md are missing it skips the directory. | prio:med | detail:details/T-0191.md | created:2026-08-06
- [ ] [T-0194] detect multiple mm directories in the same directory | prio:med | tags:needs-refinement | detail:details/T-0194.md | created:2026-08-06
- [ ] [T-0195] slim down the skill | prio:med | tags:needs-refinement | detail:details/T-0195.md | created:2026-08-06
- [ ] [T-0196] think about removing the spec from the project files | prio:med | tags:needs-refinement | detail:details/T-0196.md | created:2026-08-06
- [ ] [T-0193] This will wake up at 6:44 | prio:med | tags:test | detail:details/T-0193.md | created:2026-08-06 | tickler:2026-08-06@06:44
- [ ] [T-0197] GUI: the monthly Wake-up control cannot express last (spec-gui §5.6) | prio:med | tags:gui,spec | detail:details/T-0197.md | created:2026-08-06
- [ ] [T-0198] test3 | prio:med | created:2026-08-06 | tickler:2026-08-06@09:32
- [ ] [T-0199] tie you shoes | prio:med | created:2026-08-06 | tickler:mon@07:00
- [ ] [T-0200] later | prio:med | created:2026-08-06 | tickler:2026-08-20
- [ ] [T-0201] make sure ui is local and storage is time zoned | prio:med | created:2026-08-06
