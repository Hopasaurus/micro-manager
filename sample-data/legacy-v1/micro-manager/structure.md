---
doc: structure
version: 1
updated: 2026-07-29
---

# Structure

A small-file todo system. Every file is plain Markdown: readable as-is in any
editor, and parseable with a handful of regexes. Nothing here needs a tool to
work — a tool is just faster than editing by hand.

## Files

| File | Holds | Cardinality |
|---|---|---|
| `structure.md` | This document. The spec. | 1 |
| `backlog.md` | Everything not started. | many items |
| `working.NN.md` | One in-progress item each, with its notes. | 1 or more files |
| `done.md` | Everything finished or cancelled, newest first. | many items |
| `details/T-####.md` | Long-form description for one item. | 0 or 1 per item |

These live together in one directory. That directory can sit anywhere and
be named anything — `micro-manager`, `.micro-manager`, `µmanager` and
`.µmanager` are the conventional names, and `find.sh` at the top of the tree
locates every one of them. `check.sh`, alongside it, validates whichever you
point it at.

An item lives in exactly one of `backlog.md`, one working file, or `done.md` at
any time. That single-home rule is the core invariant — never copy an item,
always move it.

Detail files are the exception that proves it: they never move. An item's line
travels between files while its detail file stays at a fixed path, so the
long-form text survives every transition without being re-pasted.

## The item line

Every item in `backlog.md` and `done.md` is one line:

```
- [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
```

Grammar:

```
- [BOX] [ID] TITLE | key:value | key:value | ...
```

- **BOX** — `` (space) for open, `x` for closed. Open lines only in
  `backlog.md`; closed lines only in `done.md`.
- **ID** — `T-` plus four digits, zero-padded. Permanent. Never reused, never
  renumbered. The ID is what makes an item trackable across files.
- **TITLE** — free text, one line, no `|` character. Imperative mood reads best
  ("Fix the deploy script", not "Deploy script is broken").
- **fields** — ` | ` separated `key:value` pairs. Order is not significant.
  Unknown keys are allowed and must be preserved when an item moves.

Parsing regex:

```
^- \[( |x)\] \[(T-\d{4})\] ([^|]+?)(?: \| (.*))?$
```

Then split group 4 on ` | ` and each part on the first `:`.

### Fields

| Key | Values | Where | Meaning |
|---|---|---|---|
| `prio` | `high` `med` `low` | anywhere | Priority. Default `med` if absent. |
| `tags` | comma-separated, no spaces | anywhere | Free-form labels. |
| `created` | `YYYY-MM-DD` | anywhere | When the item was written down. |
| `started` | `YYYY-MM-DD` | working, done | When it entered a working file. |
| `done` | `YYYY-MM-DD` | done | When it was closed. |
| `outcome` | `shipped` `cancelled` `obsolete` | done | How it closed. Required in `done.md`. |
| `blocked` | free text, no `|` | backlog | Why it can't start yet. |
| `detail` | path, e.g. `details/T-0042.md` | anywhere | Long-form description. See below. |

Dates are always ISO 8601: `YYYY-MM-DD`, and a real day of a real month —
`2026-02-31` is rejected. Month headings in `done.md` are ISO 8601 too:
`YYYY-MM`. No other date form is accepted anywhere, and nothing in these files
records a time of day. Values never contain `|`.

## Detail files

The item line is deliberately one line — it stays scannable, and a backlog of
two hundred items stays readable. Anything longer than a title goes in a detail
file, and the line points at it:

```
- [ ] [T-0042] Fix the deploy script | prio:high | detail:details/T-0042.md | created:2026-07-29
```

One file per item, named for its ID, in `details/`. Create it only when there's
something to say; most items never need one. The file:

```markdown
---
doc: detail
id: T-0042
title: Fix the deploy script
updated: 2026-07-30
---

# T-0042 — Fix the deploy script

## Context
## Requirements
## Open questions
## References
```

Frontmatter is the machine-readable half: `id` must match the filename and the
item line pointing here, and `title` must match that line's title — those two
duplications are what let a checker catch a detail file that drifted away from
its item.

The body is yours. The four headings above are a starting shape, not a
requirement — delete what you don't need, add what you do. Prose, diagrams,
pasted logs, transcripts, code blocks, whatever the item actually needs.
`details/_template.md` holds a copy to start from; files beginning with `_` are
templates and are ignored by the checker.

Detail files are permanent. When an item completes, its file stays where it is —
`done.md` keeps the `detail:` field, so a closed item still links to everything
you learned while doing it. If a detail file grows past what one file should
hold, keep it as the entry point and link out to siblings from `## References`
rather than splitting the pointer.

### Choosing where text goes

Four places can hold prose, and mixing them up is the easiest way to lose track
of something:

- **The title** — one line, always. What the item *is*.
- **`details/T-####.md`** — the durable description. Written mostly before
  starting, and true regardless of who picks the item up. Survives completion.
- **`working.NN.md` → `## Notes`** — the running log while you work. Dated entries,
  discarded on completion. If a note turns out to matter permanently, move it
  into the detail file before closing the item.
- **`done.md`** — the outcome, in fields only. No prose.

Rule of thumb: if it would still be worth reading a year from now, it belongs in
the detail file.

## backlog.md

Frontmatter carries two things beyond bookkeeping:

- `project` — the human name of what this directory tracks. Required, and the
  one field that identifies a directory as more than its path. `check.sh` shows
  it in the menu and in every result line, which is what makes a tree of several
  todo directories navigable.
- `next_id` — the ID to hand out for the next new item. Increment it on every
  add; never decrement it, even after deletions.

Three sections, fixed names and order:

- `## Ready` — startable now. Keep this list ordered by what you'd pick next;
  the ordering is meaningful, top of list wins ties.
- `## Blocked` — real work, but waiting on something. Every line here carries a
  `blocked:` field naming what it waits on.
- `## Someday` — not committed to. No ordering implied.

Sections may be empty but must not be deleted; a parser is allowed to assume all
three headings exist.

## working.NN.md

One file per WIP slot, numbered from `01`. **The number of these files is the
WIP limit.** Want to work on three things at once? Create `working.02.md` and
`working.03.md`. Want to go back to one? Delete them — once they're idle.

There is no `wip_limit:` setting anywhere, and nothing has to enforce a limit,
because each file holds at most one item and you cannot start an item without a
free file to put it in. The constraint is the shape of the directory rather than
a number some tool has to check against.

Files MUST be numbered contiguously from 1, with no gaps, and MUST all use the
same number of digits. Two digits is the convention — `working.01.md` — but any
consistent width works: `working.1.md` through `working.3.md` is valid, and so
is `working.001.md` through `working.012.md`. What is never valid is mixing
widths in one directory, because `working.1.md` and `working.01.md` would then
both claim slot 1.

Slots are interchangeable. Nothing requires the lowest free one be used first,
though filling them in order keeps things predictable.

Zero or one item per file. The frontmatter is the machine-readable half:

```yaml
status: working   # or: idle
id: T-0042
title: Fix the deploy script
prio: high
tags: infra,ci
detail: details/T-0042.md   # or: null
created: 2026-07-29
started: 2026-07-30
```

Every field here is written exactly as it would be on an item line — `tags` is
the same comma-separated list in both places, not a YAML list. An item moving
between files is a copy, never a conversion. `null` in this frontmatter means
what an absent field means on an item line, and every field is validated here
with the same rules it gets there.

Two fields are required while `status: working`: `title`, because a nameless
item in progress is unrecoverable, and `started`, because the Start operation is
what sets it. The rest may be `null`. When `status: idle`, all of them must be.

When `status: idle`, `id` is `null` and the body sections are emptied.

The body is the human half — the reason this file exists separately from a line
in `backlog.md`. Fixed sections:

- `## Task` — what done looks like, in a sentence or two. The acceptance test.
  When `detail` is set, open this section with a link to that file so the long
  form is one click away.
- `## Plan` — subtask checkboxes, `- [ ]` / `- [x]`. Subtasks are scratch work:
  they have no IDs and are discarded on completion. Only the parent item
  survives into `done.md`.
- `## Notes` — running log. Prefix entries with a date.
- `## Blockers` — anything currently in the way. Non-empty here is the signal to
  either resolve it or move the item back to `## Blocked` in `backlog.md`.

Pick a WIP limit deliberately and change it deliberately. Raising it is creating
a file, which is easy — that's the point, and also the risk. The limit is only
worth having if adding a slot is a decision rather than a reflex when something
new looks urgent.

## done.md

Newest first. Grouped under `## YYYY-MM` headings, newest month at the top; a
new month means a new heading. Within a month, newest at the top of the list.

Every line is closed (`- [x]`) and carries `done:` and `outcome:`. Cancelled
work is kept, not deleted — `outcome:cancelled` with a note on the reason in the
title or a `why:` field. The record of what you decided not to do is worth as
much as the record of what you shipped.

When the file gets unwieldy, cut trailing months into `done-YYYY.md` in the same
directory and leave the current year here. Take their detail files with them,
into `details-YYYY/`, and rewrite the archived `detail:` fields to match — a
detail file left in `details/` after its item is gone from `done.md` is an
orphan the checker will report from then on. Archives are not validated, so
neither half is checked once it has left; restoring is the same move backwards.

## Operations

**Add** — read `next_id` from `backlog.md` frontmatter, write the item line into
`## Ready` (or `## Someday`) with `created:` set to today, then increment
`next_id`. If the item needs more than a title, create `details/T-####.md` from
`details/_template.md` and add the `detail:` field.

**Describe** — to add long-form text to an existing item: copy
`details/_template.md` to `details/<ID>.md`, fill in `id` and `title` to match
the item line, and add `detail:details/<ID>.md` to that line. Works wherever the
item lives, including after completion.

**Start** — requires a working file with `status: idle`; if every one is
occupied you are at your WIP limit, and the choice is to finish something, pause
something, or deliberately raise the limit. Remove the line from `backlog.md`,
fill the chosen file's frontmatter from its fields (`detail:` included,
verbatim), add `started:` with today's date, write `## Task` from the title plus
whatever context you have, and leave the other sections empty.

**Pause / put back** — copy the frontmatter fields back into a `backlog.md` item
line, keeping `started:`. Drop the subtasks, but carry anything from `## Notes`
worth keeping into the item's tail — an unrecoverable note is the main risk of
pausing. Reset that working file to `status: idle`.

**Complete** — write the item into the current month's group at the top of
`done.md` as `- [x]` with `done:` and `outcome:` added, preserving all other
fields (`detail:` included). Before resetting, move anything from `## Notes`
worth keeping into the detail file — this is the last moment those notes exist.
Then reset that working file to `status: idle`.

**Block** — move the line from `## Ready` to `## Blocked` and add a `blocked:`
field. Unblocking is the reverse, dropping the field.

**Cancel** — same as complete, with `outcome:cancelled`. Works from either
`backlog.md` or a working file.

**Raise the WIP limit** — create the next working file in sequence, copying an
idle one so the frontmatter and section skeleton are right.

**Lower the WIP limit** — delete the highest-numbered working file. It MUST be
idle first, or you are deleting an in-progress item; and it MUST be the highest,
or the numbering develops a gap.

## Tooling

Two scripts sit at the top of the tree, above whatever directories they act on:

- `find.sh [--exclude NAME]... [ROOT ...]` — prints every todo directory it can
  find, one per line, sorted. Matches on directory name only: `micro-manager`,
  `.micro-manager`, `µmanager`, `.µmanager`. It prunes `.git`, `.claude`,
  `node_modules` and similar, and does not descend into a directory it has
  matched. Exits 1 when it finds none.
- `check.sh [-a|--all] [DIR ...]` — validates the invariants below. With no
  arguments it asks `find.sh` what exists and offers a numbered menu; with one
  or more `DIR` it checks exactly those; with `--all` it checks everything
  `find.sh` returns. Exits 1 if any directory violates an invariant, so it drops
  straight into a pre-commit hook or CI step as `check.sh --all`.

The menu needs a terminal. Without one — a hook, a pipeline — `check.sh` refuses
to guess: it lists what it found and exits 2, so an unattended run can never
silently check the wrong directory.

## Invariants

`check.sh` verifies all of these against a todo directory and exits non-zero on
any violation:

1. Every ID matches `T-\d{4}` and appears in exactly one file.
2. No ID is greater than or equal to `next_id` in `backlog.md`.
3. `backlog.md` contains only `- [ ]` lines; `done.md` only `- [x]` lines.
4. Every working file has `status: working` with a non-null `id`, `title` and
   `started`, or `status: idle` with every item field `null`.
5. Every line in `## Blocked` has a `blocked:` field.
6. Every line in `done.md` has `done:` and `outcome:`, and sits under a `##`
   month heading matching its `done:` date.
7. `prio`, `tags`, and the dates are well-formed wherever they appear — on an
   item line and in working-file frontmatter alike. Dates are valid ISO 8601
   calendar dates, so an impossible one like `2026-02-31` is a violation. No
   field value contains `|`. `backlog.md` frontmatter has a non-empty `project`.
8. Every `detail:` path exists, lives in `details/`, and is named for the ID
   that points at it.
9. Every file in `details/` not starting with `_` is referenced by exactly one
   item, and its frontmatter `id` and `title` match that item.
10. At least one `working.NN.md` exists; they are numbered contiguously from 1
    with no gaps, and every number uses the same digit width. A plain
    `working.md` is reported as a name needing migration.
