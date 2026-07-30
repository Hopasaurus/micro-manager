---
doc: backlog
version: 1
project: micro-manager — Go implementation
next_id: T-0046
updated: 2026-07-29
---

# Backlog

Implementation of `project/spec-tools.md` in Go: the library, then the `mm` CLI
wrapper over it. See [structure.md](structure.md) for the line format, and
`../AGENTS.md` for the working rules.

`## Ready` is ordered by dependency — top of the list is genuinely the next
thing that can be started.

## Ready

- [ ] [T-0035] Implement dry-run across every mutation | prio:med | tags:cli | created:2026-07-29
- [ ] [T-0036] Implement the JSON envelope | prio:med | tags:cli,output | created:2026-07-29
- [ ] [T-0037] Implement block, unblock and note | prio:med | tags:library,ops | created:2026-07-29
- [ ] [T-0038] Implement porcelain output | prio:low | tags:cli,output | created:2026-07-29
- [ ] [T-0039] Integrate EDITOR and VISUAL for detail editing | prio:low | tags:cli | created:2026-07-29

## Blocked

- [ ] [T-0041] Implement theme and config loading for the UI service | prio:low | tags:library,ui | created:2026-07-29 | blocked:only the UI service needs it; nothing consumes it until spec-gui.md is being built

## Someday

- [ ] [T-0042] Implement status, next, search and find | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0043] Implement archive for rolling months into done-YYYY.md | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0044] Implement migrate for older directory layouts | prio:low | tags:library,ops | created:2026-07-29
- [ ] [T-0045] Implement stats: throughput, cycle time, WIP over time | prio:low | tags:library,report | created:2026-07-29
- [ ] [T-0040] Pin the published module path in go.mod | prio:low | tags:setup | created:2026-07-29 | deferred:the module does not need to be published yet
