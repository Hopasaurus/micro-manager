---
doc: working
version: 1
status: working
id: T-0216
title: Reload scan roots into the live UI registry
prio: high
tags: bug,gui
detail: details/T-0216.md
created: 2026-08-19
started: 2026-08-19
refs: null
---

# Working

Nothing in progress. Pick an item from [backlog.md](backlog.md) — see the
**Start** operation in [structure.md](structure.md).

## Task

Reload scan roots into the live UI registry

See [details/T-0216.md](details/T-0216.md).

## Plan

## Notes

**2026-08-19** — Implemented live scan-config replacement with synchronized registry cache invalidation, shared startup/runtime root expansion, and correct removal of expanded roots (including final-root [] persistence). Added settings/API/expansion regression tests. Verified go test ./..., go build ./..., go vet ./..., race-focused tests, and check.sh --all. Left Working for review.

## Blockers
