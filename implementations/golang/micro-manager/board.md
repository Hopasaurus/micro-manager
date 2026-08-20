---
doc: board
version: 2
project: micro-manager — Go implementation
next_id: T-0236
updated: 2026-08-20
wip.working: 4
---

# Board

- [ ] [T-0165] Refine settings and theme config. | stage:someday | prio:med | detail:details/T-0165.md | created:2026-08-04
- [ ] [T-0189] Need a way to bulk move from someday to ready | stage:someday | prio:med | created:2026-08-06
- [ ] [T-0190] Need a way to make drag scroll down | stage:someday | prio:med | detail:details/T-0190.md | created:2026-08-06
- [ ] [T-0195] slim down the skill | stage:someday | prio:med | tags:needs-refinement | detail:details/T-0195.md | created:2026-08-06
- [ ] [T-0196] think about removing the spec from the project files | stage:someday | prio:med | tags:needs-refinement | detail:details/T-0196.md | created:2026-08-06
- [ ] [T-0199] tie your shoes | stage:someday | prio:med | created:2026-08-06 | tickler:mon@07:00 | tickled:2026-08-17
- [ ] [T-0201] make sure ui is local and storage is time zoned | stage:someday | prio:med | created:2026-08-06
- [ ] [T-0192] Investigate caching, was: Make sure the UI has a place to enter the time when setting a wakeup type. | stage:someday | prio:med | detail:details/T-0192.md | created:2026-08-06
- [ ] [T-0208] time for code & coffee | stage:someday | prio:med | created:2026-08-07 | tickler:fri@08:30 | tickled:2026-08-14
- [ ] [T-0209] a single button for board and global settings is confusing | stage:someday | prio:med | tags:needs-refinement,spike | created:2026-08-07
- [ ] [T-0212] Testing around directories | stage:someday | prio:med | detail:details/T-0212.md | created:2026-08-08
- [ ] [T-0230] Columns M4: finish CLI/GUI/TUI surface, retire v1 mutation support | stage:ready | prio:med | tags:columns,migration,golang | detail:details/T-0230.md | created:2026-08-20
- [ ] [T-0216] Reload scan roots into the live UI registry | stage:blocked | prio:high | tags:bug,gui | detail:details/T-0216.md | created:2026-08-19 | started:2026-08-19 | reason:needs review and test
- [ ] [T-0135] CSS transition polish on swap (fade .htmx-added content) | stage:blocked | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0135.md | created:2026-08-02 | reason:report Phase 3; only if Phase 1 still feels abrupt
- [ ] [T-0136] Morph the theme shell swap to preserve focus and scroll | stage:blocked | prio:low | tags:gui,flicker-fix,phase-3 | detail:details/T-0136.md | created:2026-08-02 | reason:report Phase 3; needs idiomorph (T-0128); measure shell-swap cost first
- [ ] [T-0132] Library: expose per-file stamps for region-level change detection | stage:blocked | prio:med | tags:library,flicker-fix,phase-2 | detail:details/T-0132.md | created:2026-08-02 | reason:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0133] Broker: publish per-column events from a per-file diff; detail-body edits fire nothing | stage:blocked | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0133.md | created:2026-08-02 | reason:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
- [ ] [T-0134] Per-column fragment routes and hx-triggers (gated on T-0131) | stage:blocked | prio:med | tags:gui,flicker-fix,phase-2 | detail:details/T-0134.md | created:2026-08-02 | reason:NO-GO from T-0131 (2026-08-02): premise unmet - idle traffic is 100% backstop, 0 events; doneLimit caps payload. Do T-0139 (D2) instead
