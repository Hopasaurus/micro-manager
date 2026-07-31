---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0072
updated: 2026-07-30
---

# Backlog

Implementation of `project/spec-tools.md` in Go: the library, then the `mm` CLI
wrapper over it, then the GUI service of `project/spec-gui.md`. See
[structure.md](structure.md) for the line format, `../AGENTS.md` for the working
rules, `../project/architecture.md` for how the implementation is put together,
and `../project/plan-gui.md` for the phasing of T-0046 onward.

`## Ready` is ordered by dependency — top of the list is genuinely the next
thing that can be started.

## Ready


- [ ] [T-0060] Render the report view with period controls and copy as markdown | prio:med | tags:ui,view,phase-3 | created:2026-07-30
- [ ] [T-0061] Render the check view and the status bar violation count | prio:med | tags:ui,view,phase-3 | created:2026-07-30
- [ ] [T-0062] Render home and the projects list with favorites | prio:med | tags:ui,view,phase-3 | created:2026-07-30
- [ ] [T-0063] Render the settings views for the system and project scopes | prio:med | tags:ui,view,phase-3 | created:2026-07-30
- [ ] [T-0064] Build the theme editor, library, import and export | prio:med | tags:ui,theme,phase-3 | detail:details/T-0064.md | created:2026-07-30
- [ ] [T-0065] Implement the JSON API under /api/v1 with dryRun on every mutation | prio:high | tags:ui,api,phase-4 | created:2026-07-30
- [ ] [T-0066] Implement the SSE broker, the event stream and the polling backstop | prio:med | tags:ui,events,phase-4 | created:2026-07-30
- [ ] [T-0067] Implement test mode for MM_UI_TEST and the mm-test parameter | prio:med | tags:ui,test,phase-4 | created:2026-07-30
- [ ] [T-0068] Meet the accessibility requirements of the UI spec | prio:med | tags:ui,a11y,phase-4 | created:2026-07-30
- [ ] [T-0069] Render the about view with the three spec versions | prio:low | tags:ui,view,phase-4 | created:2026-07-30
- [ ] [T-0070] Audit the templates against the testid, property and route indexes | prio:med | tags:ui,test,phase-4 | detail:details/T-0070.md | created:2026-07-30
- [ ] [T-0071] Add the --status, --next and --search switches to the CLI | prio:med | tags:cli,ops | created:2026-07-30

## Blocked

## Someday

- [ ] [T-0043] Implement archive for rolling months into done-YYYY.md | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0044] Implement migrate for older directory layouts | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0045] Implement stats: throughput, cycle time, WIP over time | prio:low | tags:library,report | created:2026-07-29
- [ ] [T-0040] Pin the published module path in go.mod | prio:low | tags:setup | created:2026-07-29 | deferred:the module does not need to be published yet
