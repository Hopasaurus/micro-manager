# micro-manager — pi plugin

A [pi](https://github.com/earendil-works/pi) extension that makes a
micro-manager board something an agent works with through typed tools, a `/mm`
command, and per-turn context — instead of editing markdown by hand.

Specification: [`project/spec-pi-mm-plugin.md`](../../project/spec-pi-mm-plugin.md).
The plugin drives the `mm` CLI for every board interaction and never reads or
writes the board's files itself (§2.1).

## Status

Complete. The extension loads, checks for `mm`, resolves and pins a board, and
provides the whole tool surface of spec §4.2 — required first:

| Tool | What it answers |
|---|---|
| `mm_status` | WIP, counts, what is in work, what is next |
| `mm_next` | the top of `## Ready` |
| `mm_list` | items in on-disk order, filtered |
| `mm_show` | one item in full, optionally with its detail file |
| `mm_board` | which board is in use, how it resolved, and its status |
| `mm_find` | every board that can be located, the one in use marked |
| `mm_check` | the ten invariants, violations as `path:line` |
| `mm_add` | add an item, and report the ID it was assigned |
| `mm_edit` | change fields in place, including unregistered keys |
| `mm_move` | reposition, or move between backlog sections |
| `mm_note` | append a dated note |
| `mm_init` | create a board and pin it for the session |
| `mm_describe` | set the board's description, through `mm` |
| `mm_start` | move an item into a working slot |
| `mm_pause` | return a working item to the backlog |
| `mm_finish` | close an item into `done.md` with an outcome |
| `mm_remove` | delete an item — guarded three times over |

That is the **whole required tool set** of spec §4.2. `mm_remove` needs
`confirmed: true` (a schema default of `false` makes a missing parameter a
refusal), asks the user as well when there is a UI, and only then passes
`--force` — the CLI's own guard being the third line rather than the first.

The **recommended set** is registered too, but only for the operations the
installed `mm` actually has — the plugin reads its `--help` once at session
start and gates on what it lists:

| Tool | What it does |
|---|---|
| `mm_add_many` | add a whole list in one transaction, one item per line |
| `mm_block` / `mm_unblock` | move an item to blocked with a reason, or back to ready |
| `mm_search` | substring or regex over titles, tags and detail bodies |
| `mm_report` | what closed in a period, with the period it resolved |
| `mm_tick` | fire the board's due someday schedules, once |
| `mm_archive` | roll old closed months out into a per-year archive |

`mm_add_many` is the one to reach for when the user hands over a list — notes
from a meeting, a pasted checklist. It is a single transaction: a bad line adds
nothing at all, so there is never a half-added batch to reconcile, and the items
travel to `mm` on a pipe because the plugin creates no files.

Neither `mm_tick` nor `mm_archive` ever runs on its own — no timer, no event
handler, no "while I'm here". Calling the tool is the only trigger, both take
`dry_run` so you can see what a run would do, and `mm_archive` passes `mm`'s
warning about archived items leaving the ID pool through word for word.

## `/mm`

The same operations for a human, sharing the tools rather than mirroring them —
so what you see is byte-for-byte what the agent sees:

```
/mm status | next | check | find
/mm board [PATH]                      show the board in use, or pin one
/mm init --project NAME [--dir P] [--slots N] [--prefix P] [--description T]
/mm list [--section S] [--state S] [--prio P] [--tag T] [--limit N]
/mm show ID [--detail]
/mm add "TITLE" [--section S] [--prio P] [--tag T]... [--top]
/mm edit ID [--title T] [--set K=V]...   /mm move ID [--top | --position N]
/mm start ID | /mm pause ID | /mm finish ID [--outcome O]
/mm note ID TEXT                      /mm remove ID     (asks first)
/mm add-many [--section S] [--prio P] [--tag T]... [--top]
    …then one item per line, below the command
/mm block ID "REASON"                 /mm unblock ID [--end]
/mm search "QUERY" [--field F]... [--state S] [--regex] [--limit N]
/mm report [PERIOD] [--since D] [--until D] [--group-by G] [--include-wip]
/mm tick [--dry-run]                  /mm archive [--before YYYY-MM] [--dry-run]
/mm describe TEXT                     /mm context [on|off]
/mm help
```

An operation whose tool this build does not have says so — it names the
operation the installed `mm` is missing rather than half-running it.

## Per-turn context

When a board is pinned, each turn's system prompt gains one compact block, so
the agent starts knowing what is in flight without spending a tool call:

```
[mm] /srv/boards/todos — Sample One — wip 1/4 · ready 3 · blocked 1 · someday 5
     in work:  T-0018  Migrate the build cache
     next:     T-0169  Spec: tickler fields and grammar (high)
```

Data only, at most 8 lines, never done items, refreshed after anything the
plugin changes, and **nothing at all** when there is no board, no `mm`, or an
untrusted project — an empty injection beats a false one. Turn it off for a
session with `/mm context off`, or by default with `context: false` in the
config.

## Configuration

| File | Scope |
|---|---|
| `~/.pi/agent/mm-plugin.json` | global, always read |
| `.pi/mm-plugin.json` | project-local, **only when the project is trusted** |

| Key | Default | Meaning |
|---|---|---|
| `board` | — | pin a board, overriding resolution |
| `context` | `true` | per-turn injection |
| `contextLines` | `6` | max injected lines (capped at 8) |
| `timeoutMs` | `30000` | subprocess bound |
| `confirmRemove` | `true` | `mm_remove`'s interactive prompt |

A key of the wrong type is named and ignored rather than coerced, and an
unknown key is reported — a config that appears to work and does nothing is the
worst outcome.

## Requirements

- **pi** ≥ 0.82.
- **`mm` on PATH.** Build it from this repository:

  ```bash
  cd implementations/golang
  go build -o bin/mm ./cmd/mm
  install -m 0755 bin/mm /usr/local/bin/mm   # or anywhere on PATH
  mm --version
  ```

  Without it the plugin loads, warns once, and refuses to pretend: there is no
  fallback that parses the markdown.

## Install

pi auto-discovers extensions from `~/.pi/agent/extensions/<name>/index.ts`
(global) and `.pi/extensions/<name>/index.ts` (project-local, after the project
is trusted). **The installed directory is named `micro-manager`** — that name
is the plugin's identity for `/reload` and for humans.

```bash
# from the repository root
npm --prefix plugins/pi-mm install
ln -s "$PWD/plugins/pi-mm" ~/.pi/agent/extensions/micro-manager
```

A symlink keeps the installed copy in step with the checkout; `cp -R` works
too. Either way pi reads `package.json`'s `pi.extensions` and loads
`src/index.ts`.

To load it for one run without installing:

```bash
pi -e ./plugins/pi-mm/src/index.ts
```

## Why the source directory is not called `micro-manager`

This repository recognizes a *todo directory* by name alone — `micro-manager`,
`.micro-manager`, `µmanager` and three more (see `project/SKILL.md`). A source
directory with that name would be picked up by `find.sh` and then reported by
`check.sh` as a malformed board. So the source lives at `plugins/pi-mm/` and
only the installed copy carries the name (spec §3.1).

## Development

```bash
npm install          # in this directory
npm test             # node --test, no framework, no build step
npm run typecheck    # tsc --noEmit; pi itself runs the TypeScript through jiti
```

Runtime dependencies are limited to `typebox` and the pi packages (spec §3.1).
The test runner is node's own, and the tests spawn real shims rather than
faking `spawn` — what they are checking is what happens when running a program
goes wrong.

`src/conformance.test.ts` is the spec §10 checklist: the two conformance
tiers, a sweep that runs every tool at its widest and asserts that no switch
reached `mm` which the spec does not name, that one tool call is one operation,
that no lifecycle handler mutates anything, and the §8.1 smoke test that the
plugin never opens a board file.

`src/mm-contract.test.ts` goes further and drives the **real** `mm`, so the
runner is checked against the CLI rather than against a fixture's idea of it.
It skips with a reason when `mm` is not on PATH, so the suite still runs on a
machine that has never built the Go implementation:

```bash
npm test                       # 172 pass, 7 skipped without mm
PATH=/path/to/mm/bin:$PATH npm test   # 179 pass
```
