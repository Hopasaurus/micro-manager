---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0151
updated: 2026-08-03
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


- [ ] [T-0149] Drops over the drop placeholder silently never fire (upward drags) | prio:med | tags:gui,dnd | detail:details/T-0149.md | created:2026-08-03
- [ ] [T-0111] Close the drag-behaviour test gap: non-displacing placeholder and/or a JS test runner (T-0110 follow-up) | tags:gui,test,tech-debt | detail:details/T-0111.md | created:2026-08-01
- [ ] [T-0139] Conditional polling (D2): poll only while the SSE stream is down | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0139.md | created:2026-08-02
- [ ] [T-0142] Theme editor: edit the dark palette (colorDark) alongside the light | prio:med | tags:gui,theme | detail:details/T-0142.md | created:2026-08-03
- [ ] [T-0143] Spec: name the compiled-in library themes in spec-gui.md §8.7 (micro-manager-lite) | prio:low | tags:format,spec | created:2026-08-03
- [ ] [T-0148] Check if flicker fixes need to be applied to more paths | prio:med | tags:spike | detail:details/T-0148.md | created:2026-08-03
- [ ] [T-0144] Done only shows the count up to the limit | prio:med | tags:bug | detail:details/T-0144.md | created:2026-08-03
- [ ] [T-0145] Make a way to show all done items | prio:med | tags:new-feature,spike | detail:details/T-0145.md | created:2026-08-03
- [ ] [T-0147] Add 'move to top' & 'move to bottom' to card context menu. | prio:med | tags:new-feature | created:2026-08-03
- [ ] [T-0146] When saving after edit return to board | prio:med | tags:bug | detail:details/T-0146.md | created:2026-08-03
- [ ] [T-0150] Flicker follow up | prio:med | tags:bug | detail:details/T-0150.md | created:2026-08-03

## Blocked

- [ ] [T-0135] CSS transition polish on swap (fade .htmx-added content) | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0135.md | created:2026-08-02 | blocked:report Phase 3; only if Phase 1 still feels abrupt
- [ ] [T-0136] Morph the theme shell swap to preserve focus and scroll | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0136.md | created:2026-08-02 | blocked:report Phase 3; needs idiomorph (T-0128); measure shell-swap cost first
- [ ] [T-0132] Library: expose per-file stamps for region-level change detection | prio:med | tags:library,flicker-fix,phase-2 | detail:details/T-0132.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0133] Broker: publish per-column events from a per-file diff; detail-body edits fire nothing | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0133.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0134] Per-column fragment routes and hx-triggers (gated on T-0131) | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0134.md | created:2026-08-02 | blocked:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead

## Someday

- [ ] [T-0043] Implement archive for rolling months into done-YYYY.md | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0044] Implement migrate for older directory layouts | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0045] Implement stats: throughput, cycle time, WIP over time | prio:low | tags:library,report | created:2026-07-29
- [ ] [T-0040] Pin the published module path in go.mod | prio:low | tags:setup | created:2026-07-29 | deferred:the module does not need to be published yet
