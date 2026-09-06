---
doc: structure
version: 2
updated: 2026-09-05
---

# micro-manager — Go implementation

A small-file todo system. Every file is plain Markdown: readable as-is in any
editor, and parseable with a handful of regexes. Nothing here needs a tool to
work — a tool is just faster than editing by hand.

- [x] Allow `mm --refresh-structure` to update generated documentation.

## Authority

This file is read-only orientation: it is optional documentation, not board
state, and validators do not parse it. Careful manual edits to the data files
are supported, but `mm` is safer because it preserves cross-file invariants.

## Files

| Path | Purpose |
|---|---|
| `board.md` | Open items in one ordered list; each item carries `stage:`. |
| `done.md` | Finished and cancelled items grouped by month. |
| `details/<ID>.md` | Optional durable description for one item. |
| `done-YYYY.md`, `details-YYYY/` | Optional archives, outside validation. |
| `audit.md` | Optional append-only action log. |

## Item lines

```text
- [ ] [T-0042] Fix the deploy script | stage:ready | prio:high | tags:infra,ci | created:2026-07-29
```

Open items use `- [ ]`; done items use `- [x]`. Fields are
` | `-separated `key:value` pairs. Unknown fields are legal and must survive
moves. Stages and any `wip.<stage>` caps are declared in `board.md` frontmatter.

## Safe manual edits

- An ID has exactly one home: `board.md` or `done.md`. Move; never copy.
- `next_id` only rises. IDs are permanent and never reused or renumbered.
- Renaming an item also requires the matching detail `title` to change.
- A detail path, filename, frontmatter `id`, and owning item ID must agree.
- Preserve unknown fields. Tags contain no spaces; dates are real ISO dates.
- Run `mm --check` after editing data files directly.

## Commands and more help

Use `mm --help` for the current command list and
`mm --help --OPERATION` for one operation. This file explains storage;
help explains actions.

Normative documentation:

- Repository: https://github.com/Hopasaurus/micro-manager
- Format: `project/spec-file-format.md`
- Tools: `project/spec-tools.md`

When this overview and a specification disagree, the format specification wins.

<!-- mm:user-notes:begin -->
## User notes

Add board-specific notes here. `mm --refresh-structure` preserves everything
between these markers exactly.
<!-- mm:user-notes:end -->
