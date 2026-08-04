---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0169
updated: 2026-08-04
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

- [ ] [T-0044] Implement migrate for older directory layouts | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0045] Implement stats: throughput, cycle time, WIP over time | prio:low | tags:library,report | created:2026-07-29
- [ ] [T-0167] CLI: wire the optional operations from spec-tools.md §5.3, starting with --archive [--before YYYY-MM] | prio:low | tags:cli | created:2026-08-04
- [ ] [T-0168] Library: archive MUST move detail files into details-YYYY/, not strand them (spec-file-format.md §5.6) | prio:med | tags:library,ops | detail:details/T-0168.md | created:2026-08-04

## Blocked

- [ ] [T-0135] CSS transition polish on swap (fade .htmx-added content) | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0135.md | created:2026-08-02 | blocked:report Phase 3; only if Phase 1 still feels abrupt
- [ ] [T-0136] Morph the theme shell swap to preserve focus and scroll | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0136.md | created:2026-08-02 | blocked:report Phase 3; needs idiomorph (T-0128); measure shell-swap cost first
- [ ] [T-0132] Library: expose per-file stamps for region-level change detection | prio:med | tags:library,flicker-fix,phase-2 | detail:details/T-0132.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0133] Broker: publish per-column events from a per-file diff; detail-body edits fire nothing | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0133.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0134] Per-column fragment routes and hx-triggers (gated on T-0131) | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0134.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead

## Someday

- [ ] [T-0143] Spec: name the compiled-in library themes in spec-gui.md §8.7 (micro-manager-lite) | prio:low | tags:format,spec | created:2026-08-03
- [ ] [T-0142] Theme editor: edit the dark palette (colorDark) alongside the light | prio:med | tags:gui,theme | detail:details/T-0142.md | created:2026-08-03
- [ ] [T-0144] Done only shows the count up to the limit | prio:med | tags:bug | detail:details/T-0144.md | created:2026-08-03
- [ ] [T-0145] Make a way to show all done items | prio:med | tags:new-feature,spike | detail:details/T-0145.md | created:2026-08-03
- [ ] [T-0162] Open the new-item panel through an htmx swap like the card title link | prio:low | tags:gui,flicker-fix | detail:details/T-0162.md | created:2026-08-04
- [ ] [T-0164] Tickler service | prio:med | tags:spike | detail:details/T-0164.md | created:2026-08-04
- [ ] [T-0165] Refine settings and theme config. | prio:med | detail:details/T-0165.md | created:2026-08-04
