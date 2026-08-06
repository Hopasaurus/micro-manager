# micro-manager — pi plugin

A [pi](https://github.com/earendil-works/pi) extension that makes a
micro-manager board something an agent works with through typed tools, a `/mm`
command, and per-turn context — instead of editing markdown by hand.

Specification: [`project/spec-pi-mm-plugin.md`](../../project/spec-pi-mm-plugin.md).
The plugin drives the `mm` CLI for every board interaction and never reads or
writes the board's files itself (§2.1).

## Status

The skeleton and the runner: the extension loads, checks for `mm` at session
start, says what to install when it is missing, and can drive one `mm`
operation with the exit codes mapped to typed failures. **No tools and no `/mm`
command yet** — board resolution (T-0177) and the tool surface (T-0178 onward)
land next. Installing it now gets you the health check and nothing else.

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

`src/mm-contract.test.ts` goes further and drives the **real** `mm`, so the
runner is checked against the CLI rather than against a fixture's idea of it.
It skips with a reason when `mm` is not on PATH, so the suite still runs on a
machine that has never built the Go implementation:

```bash
npm test                       # 30 pass, 1 skipped without mm
PATH=/path/to/mm/bin:$PATH npm test   # 31 pass
```
