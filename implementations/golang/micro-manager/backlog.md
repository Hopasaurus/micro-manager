---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0119
updated: 2026-08-01
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


- [ ] [T-0116] check.sh and the Go validator agree on the declared ID grammar (fixture corpus) | tags:check,format | detail:details/T-0116.md | created:2026-08-01
- [ ] [T-0117] GUI: non-default prefix and width end to end | tags:gui | detail:details/T-0117.md | created:2026-08-01
- [ ] [T-0118] Per-task ID prefixes: per-prefix counters and I2 per counter (gated on the spec decision) | tags:format,spike | detail:details/T-0118.md | created:2026-08-01
- [ ] [T-0101] Explore how micro-manager could interoperate with git, consider how merge conflicts with overlapping task IDs could be solved. | prio:med | tags:spike | detail:details/T-0101.md | created:2026-07-31
- [ ] [T-0104] Explore how micro-manager could link between boards. | prio:med | tags:spike | detail:details/T-0104.md | created:2026-07-31
- [ ] [T-0109] Consider breaking up the htmx updates to reduce flicker | prio:med | tags:spike | detail:details/T-0109.md | created:2026-07-31
- [ ] [T-0111] Close the drag-behaviour test gap: non-displacing placeholder and/or a JS test runner (T-0110 follow-up) | tags:gui,test,tech-debt | detail:details/T-0111.md | created:2026-08-01

## Blocked

## Someday

- [ ] [T-0043] Implement archive for rolling months into done-YYYY.md | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0044] Implement migrate for older directory layouts | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0045] Implement stats: throughput, cycle time, WIP over time | prio:low | tags:library,report | created:2026-07-29
- [ ] [T-0040] Pin the published module path in go.mod | prio:low | tags:setup | created:2026-07-29 | deferred:the module does not need to be published yet
