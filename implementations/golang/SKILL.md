---
name: micro-manager
description: Work with micro-manager todo directories — plain-markdown task tracking using backlog.md, working.NN.md, done.md and details/, with WIP slots and machine-checkable invariants. Use when a directory named micro-manager, .micro-manager, µmanager or .µmanager is present; when reading or editing backlog.md, working.NN.md, done.md or details/T-NNNN.md; when running the mm CLI; or when asked to add, start, pause, block, finish, validate, or report on todo items in a project.
---

# micro-manager

A todo system stored as plain markdown. Every file is readable by a human in a
text editor and parseable by a machine with a handful of regexes. Nothing needs
a tool; a tool is just faster than editing by hand.

All paths and commands below are relative to the **repository root**.

`project/SKILL.md` is the source of truth for this file.
`.claude/skills/micro-manager/SKILL.md` is a symlink to it, so the installed
skill tracks edits automatically. `implementations/golang/SKILL.md` is a copy —
after editing, run `cp project/SKILL.md implementations/golang/SKILL.md`.

## Status — read this first

The **file format and the validator work today.** `check.sh` and `find.sh` are
present and functional at the repository root.

The **`mm` CLI does not exist yet.** It is specified in
`project/spec-tools.md` but not implemented. Until it is, perform operations by
editing the files directly using the recipes in *Operations by hand* below, and
validate with `check.sh`. Do not invent `mm` invocations or claim to have run
them.

## Install

### What works now

Requires `bash` and `awk` — both present on macOS and Linux by default.

```bash
chmod +x check.sh find.sh
./find.sh          # list every micro-manager directory below here
./check.sh --all   # validate all of them
```

`check.sh` with no arguments offers an interactive menu of discovered
directories. `./check.sh <dir>` checks one. Exit code 0 means clean, 1 means an
invariant was violated, 2 means a usage or setup problem.

### The CLI

```bash
cd implementations/golang
go build -o bin/mm ./cmd/mm
sudo install -m 0755 bin/mm /usr/local/bin/mm   # or put it anywhere on PATH
mm --version
```

Build into `bin/`, **not** `-o mm`. That path is the library package directory,
and `go build -o <existing-directory>` does not fail — it writes the binary
*inside* it, as `mm/mm`, where nothing ignores it and it gets committed.

## Find the directories

A micro-manager directory is recognized by **name alone**. Four conventional
names, and both Unicode micro signs are matched (U+00B5 and U+03BC render
identically and are trivially confused):

```
micro-manager    .micro-manager    µmanager    .µmanager
```

```bash
./find.sh                        # from the current directory
./find.sh ~/code ~/work          # from specific roots
./find.sh --exclude build .      # add to the prune list
```

Dotted variants are real and common — never skip hidden directories when
searching. `find.sh` prunes `.git`, `.claude`, `node_modules` and similar, and
does not descend into a directory it has already matched. Matching is on name
alone, so anything else named `micro-manager` will be listed and then rejected
by `check.sh` as not a todo directory.

## Directory layout

```
<micro-manager dir>/
  structure.md        human documentation, not validated
  backlog.md          everything not started
  working.01.md       one file per WIP slot; the file count IS the WIP limit
  working.02.md
  done.md             everything finished or cancelled, newest first
  details/
    T-0042.md         long-form description for one item
    _template.md      files starting with _ are templates, exempt from checks
```

An item lives in **exactly one** of `backlog.md`, a working file, or `done.md`.
Moving is cut-and-paste between files, never a copy. Detail files are the
exception: they never move, so the long text survives every transition.

Other files may be present (`theme.json`, `config.json`) — they belong to the
GUI/TUI and are ignored by the format and the validator.

## The item line

Every item in `backlog.md` and `done.md` is exactly one line:

```
- [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
```

```
- [BOX] [ID] TITLE | key:value | key:value | ...
```

- `BOX` — a space for open, `x` for closed. Open lines only in `backlog.md`,
  closed lines only in `done.md`.
- `ID` — `T-` plus exactly four digits, zero-padded. Permanent, never reused,
  never renumbered.
- `TITLE` — one line, **must not contain `|`**.
- Fields — separated by ` | ` (space pipe space), split at the first `:`. Order
  does not matter. Unknown keys are legal and **must be preserved** when an item
  moves.

Regex to recognize one:

```
^- \[[ x]\] \[T-[0-9]{4}\] .
```

### Fields

| Key | Values | Notes |
|---|---|---|
| `prio` | `high` `med` `low` | absent means `med` |
| `tags` | `infra,ci` | comma-separated, **no spaces** |
| `created` `started` `done` | `YYYY-MM-DD` | ISO 8601, and a real date: `2026-02-31` is rejected |
| `outcome` | `shipped` `cancelled` `obsolete` | required in `done.md` |
| `blocked` | free text | required in `## Blocked`, forbidden elsewhere |
| `detail` | `details/T-0042.md` | must match the item's own ID |

## The files

**`backlog.md`** — frontmatter carries `project` (the human name, required) and
`next_id` (the ID to hand out next). Three sections, all required even when
empty, in this order:

```
## Ready      order is meaningful; top of the list is what you pick next
## Blocked    every item here needs a blocked: field
## Someday    not committed to
```

**`working.NN.md`** — zero or one item, carried in the **frontmatter**, not as
an item line. Two-digit numbers by convention; any consistent width works, but
all files in a directory must use the same width and be numbered 1..N with no
gaps.

```yaml
---
doc: working
version: 1
status: working          # or: idle
id: T-0042
title: Fix the deploy script
prio: high
tags: infra,ci           # same TAGLIST form as an item line, not a YAML list
detail: details/T-0042.md
created: 2026-07-29
started: 2026-07-30
---
```

When `status: idle`, every one of those item fields must be `null`. When
`status: working`, `id`, `title` and `started` must be set.

Body sections `## Task`, `## Plan`, `## Notes`, `## Blockers` are always
present. **`- [ ]` lines in a working file are subtasks, not items** — they have
no IDs and must never be parsed as items.

**`done.md`** — items grouped under `## YYYY-MM` headings, newest month first,
newest item first within a month. Every line closed, with `done:` and
`outcome:`. Cancelled work is recorded here, not deleted.

**`details/T-NNNN.md`** — optional long-form description. Frontmatter `id` must
equal the filename stem and `title` must match the item line exactly; that
duplication is the only way drift gets detected.

## Operations by hand

### Add

1. Read `next_id` from `backlog.md` frontmatter.
2. Append the item line to the **bottom** of `## Ready`, with `created:` today.
3. Increment `next_id`.
4. If it needs more than a title: copy `details/_template.md` to
   `details/<ID>.md`, set its `id` and `title` to match, and add
   `detail:details/<ID>.md` to the line.

### Start

1. Find a working file with `status: idle`. **If every slot is occupied, stop —
   the WIP limit is reached.** Do not create a new working file to make room;
   that silently raises the limit, which is the one thing it exists to prevent.
2. Remove the line from `backlog.md`.
3. Copy its fields verbatim into the working file's frontmatter, set
   `status: working` and `started:` to today.
4. Seed `## Task`, linking the detail file if there is one.

Write the working file **before** removing the backlog line. A crash between the
two then duplicates the item, which the validator catches, instead of losing it.

### Pause

1. Write the item back as a line at the **top** of `## Ready`, keeping
   `started:`.
2. Move anything from `## Notes` worth keeping into the detail file — it exists
   nowhere else and is about to be discarded.
3. Reset the working file to `status: idle` with every item field `null`.

Subtasks are scratch and are discarded.

### Finish

1. Write the line at the top of the current month group in `done.md` as `- [x]`,
   preserving every field and adding `done:` (today) and `outcome:`
   (`shipped` unless cancelled or obsolete). Create the `## YYYY-MM` heading if
   absent, in newest-first position.
2. Move anything worth keeping out of `## Notes` into the detail file.
3. Reset the working file to idle.

Works from `backlog.md` directly too — closing something you never started is
normal, and `outcome:cancelled` is how you abandon work without deleting it.

### Block / unblock

Move the line between `## Ready` and `## Blocked`, adding or dropping the
`blocked:` field.

## Validate

```bash
./check.sh --all
```

Run it after any hand edit. It reports `file:line: message` and exits non-zero
on any violation.

## Hard rules

Breaking any of these produces a directory the validator rejects.

1. An ID appears in exactly one file. Never copy an item — move it.
2. Every ID is below `next_id`. Never decrement `next_id`, never reuse an ID,
   never renumber.
3. `backlog.md` holds only `- [ ]` lines; `done.md` only `- [x]` lines.
4. A working file is `status: working` with `id`, `title` and `started` set, or
   `status: idle` with every item field `null`.
5. Every item under `## Blocked` has a `blocked:` field; no other backlog item
   does.
6. Every item in `done.md` has `done:` and `outcome:`, under a month heading
   matching its `done:` date.
7. Values are well-formed: dates are ISO 8601 `YYYY-MM-DD` **and** real calendar
   dates, `tags` is comma-separated with no spaces, no field value contains `|`.
8. Every `detail:` path is `details/<that item's ID>.md` and the file exists.
9. Every non-`_` file in `details/` is referenced by exactly one item, and its
   frontmatter `id` and `title` match that item.
10. Working files share a digit width and are numbered 1..N with no gaps.

Also, when moving an item between files:

- **preserve fields you do not recognize** — unknown keys are the format's
  extension point;
- **do not reorder `## Ready`** as a side effect of anything else;
- **do not reformat lines you did not need to change.**

## The CLI, once it exists

One command, switches select the operation, exactly one operation per
invocation:

```bash
mm --add "Fix the deploy script" --prio high --tag infra --detail
mm --add "Rotate the leaked token" --top
mm --move T-0042 --position 3
mm --start T-0042
mm --pause T-0031
mm --finish T-0042 --outcome shipped
mm --report --group-by outcome        # defaults to last complete ISO week
mm --check --all
```

`--dry-run` works on every mutation. `--json` emits a machine envelope.

## Specifications

| File | Covers |
|---|---|
| `project/spec-file-format.md` | On-disk formats, tokens, the ten invariants |
| `project/spec-tools.md` | Library API, CLI surface, transactions, exit codes |
| `project/spec-gui.md` | Web UI, DOM contract, routes, theming, discovery, binding |
| `project/spec-tui.md` | Terminal UI, keyboard model, colour degradation |
| `structure.md` (in each directory) | Lifecycle and operations, for humans |

The format spec wins any disagreement about what a file may contain.
