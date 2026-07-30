# micro-manager — data file format specification

    Spec version: 1
    Date:         2026-07-29
    Status:       stable
    Applies to:   backlog.md, working.NN.md, done.md, details/T-NNNN.md

This document specifies the on-disk formats a micro-manager directory uses. It
is the normative reference for anyone writing a parser, a generator, or a second
implementation of `check.sh`.

For how the files are *used* — the lifecycle, the operations, why the system is
shaped this way — see `structure.md` inside any micro-manager directory. Where
this document and `structure.md` disagree about a format detail, this document
wins.

For the tooling that manipulates these files — the library, its command line
wrapper, and their behavioral contract — see `spec-tools.md`. It depends on this
document; a tool that must violate this specification to do its job is wrong,
not the data.

---

## 1. Scope

A **micro-manager directory** contains:

| Path | Required | Specified in |
|---|---|---|
| `backlog.md` | yes | §5.1 |
| `working.NN.md` | yes, one or more | §5.2 |
| `done.md` | yes | §5.3 |
| `details/T-NNNN.md` | no | §5.4 |
| `details/_*.md` | no | §5.4.1 |
| `structure.md` | no | §5.5 |

Directory naming is specified in Appendix B. Any other file in the directory is
outside this specification and MUST be ignored by conforming readers.

## 2. Terminology

**MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** carry their
RFC 2119 meanings.

- **Item** — one unit of tracked work, identified by an ID.
- **Working file** — a `working.NN.md` file; one WIP slot.
- **WIP limit** — the number of working files in a directory.
- **Item line** — the single-line serialization of an item (§4.2).
- **Frontmatter** — the YAML-ish header block at the top of every file (§4.1).
- **Reader** — software that parses these files.
- **Writer** — software that produces or modifies them.
- **Checker** — software that validates them; `check.sh` is the reference
  implementation.

## 3. Lexical conventions

### 3.1 Encoding

Files MUST be UTF-8. A byte-order mark MUST NOT be present. Readers MUST NOT
assume any character is single-byte; parsers that index by byte offset (as the
reference implementation does when slicing the fixed item-line prefix) are safe
only because that prefix is pure ASCII by construction — see §4.2.

### 3.2 Lines

Lines are terminated by LF (`U+000A`). CRLF MUST NOT be used. Files SHOULD end
with a final newline. Trailing whitespace on a line is not significant and
SHOULD be stripped by writers.

### 3.3 Tokens

| Token | Definition | Example |
|---|---|---|
| `ID` | `T-` followed by exactly four ASCII digits | `T-0042` |
| `DATE` | ISO 8601 calendar date, extended format | `2026-07-29` |
| `MONTH` | ISO 8601 calendar date reduced to year and month | `2026-07` |
| `WEEK` | ISO 8601 week date, year and week only | `2026-W30` |
| `TIME` | ISO 8601 wall time, extended format | `09:14:00` |
| `TIMESTAMP` | ISO 8601 combined date and time with a UTC offset | `2026-07-29T09:14:00Z` |
| `TAG` | one or more of `A-Z a-z 0-9 . _ -` | `ci`, `infra-2` |
| `TAGLIST` | one or more `TAG`, comma-separated, no spaces | `infra,ci` |
| `DETAILPATH` | `details/` + `ID` + `.md` | `details/T-0042.md` |
| `SLOT` | one or more ASCII digits, zero-padded to the directory's width | `01`, `003` |
| `NULL` | the literal four characters `null` | `null` |

`ID` values are zero-padded to four digits. `T-42` is invalid; `T-0042` is
correct. The four-digit width caps a directory at 9999 items; a future spec
version may widen it, so readers SHOULD match `T-` followed by digits and treat
a width other than four as a version mismatch rather than crashing.

#### 3.3.1 Dates, times and timestamps are ISO 8601

**Every date, time and timestamp anywhere in a micro-manager directory MUST be
ISO 8601, in the extended format, with the separators shown above.** This is
normative for the four files of §5, and for every adjacent file a tool writes
into the directory — themes, configuration, lists (`spec-gui.md` §8–§10) — and
for every value a tool emits over an API.

Specifically:

1. **Extended format only.** ISO 8601 also permits the basic format —
   `20260729`, `0914`, `2026W30` — and this specification does **not**. The files
   are hand-edited and hand-read; the hyphens and colons are what make a
   mistyped field obvious at a glance.
2. **`DATE` MUST be a real calendar date.** `2026-02-31`, `2026-11-31` and
   `2027-02-29` are all syntactically well-formed and all invalid, and a
   conforming checker MUST reject them. Leap years follow the proleptic
   Gregorian rule ISO 8601 specifies: divisible by 4, except centuries not
   divisible by 400.
3. **`TIMESTAMP` MUST carry an explicit UTC designator or offset**, and SHOULD
   use `Z`. A timestamp with no offset is ambiguous the moment it crosses a
   machine, and these files are synchronised between machines by whatever the
   user already uses.
4. **Reduced precision is ISO 8601, not a deviation.** `MONTH` is a calendar
   date truncated to month precision, which §5.3 uses as a group heading, and
   `WEEK` is a week date truncated to week precision, which report periods use
   (`spec-tools.md` §5.1.11). Neither is a departure from the standard.
5. **No other form is permitted anywhere**: no `29/07/2026`, no `July 29 2026`,
   no epoch seconds, no ordinal dates (`2026-210`), no fractional seconds, no
   leap-second value `60`, no week-with-weekday form (`2026-W30-3`).

`TIME` and `TIMESTAMP` are defined here for the whole system but are **not used
by the four files of §5**. Those are deliberately date-granular: an item's
`created`, `started` and `done` are dates, and §5.3 groups by month, so nothing
in the data model needs a clock. A tool MUST NOT introduce a time-of-day field
into these files without a spec revision — see §10 for what that granularity
costs and why it is still the right trade.

## 4. Common structures

### 4.1 Frontmatter

Every file specified here MUST begin with a frontmatter block: line 1 is exactly
`---`, and the block is terminated by the next line that is exactly `---`.

```
---
key: value
key: value
---
```

Frontmatter is a **flat map of string keys to string values**, not general YAML.
Specifically:

1. A line is a key/value pair if it contains `:`. The key is everything before
   the **first** `:`; the value is everything after it.
2. Key and value are trimmed of leading and trailing spaces and tabs.
3. A line with no `:` MUST be ignored.
4. If the trimmed value starts and ends with `"`, those two characters are
   removed. No other unescaping occurs.
5. For every key except `title`, a trailing comment — whitespace, `#`, then any
   text to end of line — is stripped from the value. `title` is exempt because
   titles legitimately contain `#` (`Fix issue #42`).
6. Duplicate keys: last one wins.
7. Nested structures, multi-line values, and anchors MUST NOT be used. Values
   that look like YAML collections (see `tags` in §5.2) are opaque strings to a
   conforming reader.

Writers SHOULD emit keys in the order given in each file's schema.

### 4.2 Item line

An item line serializes one item on exactly one line:

```
- [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
```

Grammar (regex terminals, `SP` = one space `U+0020`):

```
item-line   = "- [" box "] [" ID "] " title *( " | " field )
box         = SP / "x"
title       = 1*( %x20-7E / non-ASCII )   ; no "|", no CR, no LF
field       = key ":" value
key         = 1*( ALPHA / DIGIT / "_" / "-" )
value       = *( any character except "|", CR, LF )
```

Parsing rules:

1. A line is a candidate item line if it matches
   `^- \[[ x]\] \[T-[0-9]{4}\] .` — note the trailing `.`, which requires a
   non-empty title.
2. The prefix `- [_] [T-NNNN] ` is fixed-width and pure ASCII: the box is at
   character 4, the ID at characters 8–13, and the title begins at character 16.
   Readers MAY rely on those offsets.
3. Everything from character 16 to end of line is split on the three-character
   sequence `" | "` (space, pipe, space). The first part is the title; each
   remaining part is a field.
4. Each field is split at its **first** `:`. Key and value are trimmed.
5. A field part with no `:` is an error.
6. A repeated key within one item line is an error.
7. Field order is NOT significant. Writers SHOULD use the canonical order in
   §6.1; readers MUST NOT depend on it.

The title MUST NOT contain `|`. See §10 for the one case where violating this
is not reliably detected.

### 4.3 Section headings

A line beginning with `## ` opens a section; the section name is the rest of the
line, trimmed. Sections are flat — a section runs until the next `## ` line or
end of file. `# ` (level 1) headings are titles and carry no meaning. Headings
below level 2 are not used in the three item files.

## 5. File schemas

### 5.1 `backlog.md`

Holds every item not yet started.

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `backlog` | yes |
| `version` | spec version, currently `1` | yes |
| `project` | human name of what this directory tracks | yes |
| `next_id` | `ID` — the ID to assign to the next new item | yes |
| `updated` | `DATE` | no |

`project` MUST be non-empty and MUST NOT be `NULL`. It is otherwise free text on
a single line: no length limit, no character restrictions beyond §4.1's parsing
rules. It is the only human-meaningful identity a micro-manager directory has —
discovery finds directories by path (Appendix B), and a tool presenting several
of them SHOULD show `project` rather than, or alongside, the path.

`project` is not an identifier. Two directories MAY carry the same `project`
value, and nothing binds it to the directory name.

**Body**

Exactly three sections MUST be present, with these names, in this order:

```
## Ready
## Blocked
## Someday
```

Any other `## ` heading is an error. A section MAY be empty but MUST NOT be
removed; readers MAY assume all three exist.

Every item line in this file MUST have box `" "` (open). Item lines MUST appear
inside one of the three sections — never before the first heading.

- `## Ready` — the order of items is **significant**; earlier means higher
  priority within equal `prio`. Items here MUST NOT carry `blocked`.
- `## Blocked` — every item MUST carry a `blocked` field.
- `## Someday` — order is not significant. Items MUST NOT carry `blocked`.

Non-item content (prose, comments, blank lines) MAY appear anywhere and MUST be
ignored by readers.

### 5.2 `working.NN.md`

Each working file holds zero or one in-progress item. Unlike the other two
files, the item is carried in the **frontmatter**, not as an item line.

#### 5.2.1 The working file set and the WIP limit

A directory MUST contain at least one file named `working.` + `SLOT` + `.md`.

**The number of working files in a directory is that directory's WIP limit.**
The limit is structural, not declared: no key anywhere states it, and no
enforcement step is required, because each file holds at most one item and an
item can only be started into a free file. A reader computes the limit by
counting files and the current WIP by counting files with `status: working`.

Constraints on the set:

1. Slot numbers MUST be the contiguous sequence 1..N, where N is the number of
   working files. No gaps, no duplicates, and numbering starts at 1 — `00` is
   not a valid slot.
2. Every slot number in one directory MUST use **the same number of digits**.
   Two digits is the RECOMMENDED width (`working.01.md`), but any width is
   valid provided it is uniform: `working.1.md`..`working.3.md` conforms, and
   so does `working.001.md`..`working.012.md`. Mixing widths within a directory
   MUST be rejected — `working.1.md` and `working.01.md` name the same slot.
3. Widths do not have to match between directories. Width is a per-directory
   property, discovered by reading the filenames, never declared.
4. A file named exactly `working.md` is NOT a working file. It is the pre-slot
   name from an earlier revision of this format; a checker SHOULD report it as
   requiring migration to `working.01.md` rather than silently ignoring it.
5. A file matching `working.*.md` whose middle segment is not all digits is an
   error, not an ignorable file.

Slots are interchangeable. A writer MAY use any idle slot and SHOULD prefer the
lowest-numbered one. Nothing depends on which slot an item occupies, and an item
MAY be moved between slots freely.

Changing the limit is creating or deleting a file. A working file being deleted
MUST be idle and MUST be the highest-numbered one, or constraint 1 breaks.

#### 5.2.2 Frontmatter and body

**Frontmatter**

| Key | Value when `status: working` | Value when `status: idle` |
|---|---|---|
| `doc` | `working` | `working` |
| `version` | `1` | `1` |
| `status` | `working` | `idle` |
| `id` | `ID` — required | `NULL` |
| `title` | the item's title — required | `NULL` |
| `started` | `DATE` — required | `NULL` |
| `prio` | `high` / `med` / `low`, or `NULL` | `NULL` |
| `tags` | `TAGLIST` or `NULL` | `NULL` |
| `detail` | `DETAILPATH` or `NULL` | `NULL` |
| `created` | `DATE` or `NULL` | `NULL` |

`status` MUST be exactly `working` or `idle`.

When `status: working`, `id`, `title`, and `started` MUST be non-`NULL`. `title`
is required because an item in progress with no title cannot be returned to the
backlog intact; `started` is required because entering this file is the event
that defines it. Every other field MAY be `NULL`.

When `status: idle`, every item field — `id`, `title`, `prio`, `tags`, `detail`,
`created`, `started` — MUST be `NULL`.

Every value in this frontmatter uses the **same lexical form as the
corresponding item-line field** (§6). `tags` is a `TAGLIST` here exactly as it
is on an item line — `tags: infra,ci`, not a YAML flow sequence. Consequently
moving an item between files is a copy of each value, never a conversion, and
one validator covers both representations.

`NULL` in this frontmatter is equivalent to the field being absent from an item
line. A writer serializing this item to `backlog.md` or `done.md` MUST omit
every key whose value is `NULL`, and a writer filling this frontmatter from an
item line MUST write `NULL` for every field the line omits.

**Body**

Four sections, in this order, all REQUIRED even when empty:

```
## Task
## Plan
## Notes
## Blockers
```

`## Plan` contains subtask checkboxes — `- [ ]` and `- [x]` lines with no ID.

> **Critical parsing rule.** Lines matching `^- \[` in a working file are
> subtasks, NOT item lines, and MUST NOT be parsed as items. Subtasks are
> unstructured by design: no ID, no fields, discarded when the item closes.

### 5.3 `done.md`

Holds every closed item, newest first.

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `done` | yes |
| `version` | `1` | yes |
| `updated` | `DATE` | no |

**Body**

Sections are month groups. Every `## ` heading MUST be a `MONTH`
(`^[0-9]{4}-[0-9]{2}$`); any other heading is an error. Groups are ordered
newest first, and items within a group are ordered newest first.

Every item line in this file MUST:

- have box `"x"` (closed),
- carry a `done` field,
- carry an `outcome` field,
- appear under a month heading whose value equals the first 7 characters of its
  `done` field.

Cancelled and obsolete work is recorded here, not deleted (§7, note under I1).

When the file grows unwieldy, trailing years MAY be moved to `done-YYYY.md` in
the same directory. Those files are outside this specification and are not
validated.

### 5.4 `details/T-NNNN.md`

Optional long-form description for exactly one item. The filename MUST be the
item's `ID` plus `.md`.

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `detail` | yes |
| `id` | `ID`, equal to the filename stem | yes |
| `title` | byte-identical to the referencing item's title | yes |
| `updated` | `DATE` | no |

The `id` and `title` duplication is deliberate: it is the only mechanism by
which a detail file that has drifted from its item can be detected.

**Body**

Unconstrained. Any Markdown. `structure.md` suggests `## Context`,
`## Requirements`, `## Open questions`, and `## References`, but no heading is
required and readers MUST NOT depend on any.

A detail file's lifetime is independent of its item's location: the item line
moves between `backlog.md`, a working file, and `done.md` while the detail file
stays at a fixed path. Detail files are never deleted on completion.

#### 5.4.1 Template files

A file in `details/` whose name begins with `_` is a template. Templates are
exempt from every constraint in §5.4 and from invariant I9. `_template.md` is
conventional.

### 5.5 `structure.md`

Human documentation. Not validated, not parsed, no required format. Its presence
is optional; its absence changes nothing for a reader.

## 6. Field registry

Fields valid on an item line. A reader encountering an unregistered key MUST
accept it, and a writer moving an item between files MUST preserve it verbatim
(§9).

| Key | Value | Required | Valid in | Notes |
|---|---|---|---|---|
| `prio` | `high` / `med` / `low` | no | all | Absent means `med`. |
| `tags` | `TAGLIST` | no | all | No spaces. Identical form in working-file frontmatter. |
| `created` | `DATE` | no | all | When the item was written down. |
| `started` | `DATE` | no | working, done | Set on entering a working file; survives a pause. |
| `done` | `DATE` | **yes** in `done.md` | done | |
| `outcome` | `shipped` / `cancelled` / `obsolete` | **yes** in `done.md` | done | |
| `blocked` | free text, no `|` | **yes** in `## Blocked` | backlog | Forbidden in `## Ready` and `## Someday`. |
| `detail` | `DETAILPATH` | no | all | MUST equal `details/<this item's ID>.md`. |

### 6.1 Canonical field order

Writers SHOULD emit fields in this order. Readers MUST NOT require it.

```
prio, tags, detail, created, started, blocked, done, outcome, <unregistered...>
```

## 7. Cross-file constraints

These hold across the directory as a whole. The reference checker verifies all
ten; the identifiers match the numbering in `structure.md`.

- **I1 — One home per ID.** Every ID appears in exactly one of `backlog.md`,
  one working file (frontmatter `id`), or `done.md` — across *all* working
  files, so the same item cannot occupy two slots. An item is moved, never
  copied.
  Consequence: closing an item is a deletion from one file and an insertion into
  another, and cancelled work must be written to `done.md` rather than deleted,
  or its ID vanishes from the directory.
- **I2 — ID ceiling.** Every ID in the directory is numerically less than
  `backlog.md`'s `next_id`. `next_id` increases monotonically and is never
  decremented, so IDs are never reused.
- **I3 — Box matches file.** `backlog.md` contains only `- [ ]` item lines;
  `done.md` contains only `- [x]` item lines.
- **I4 — Working coherence.** *Every* working file has `status: working` with a
  valid, unique, non-`NULL` `id`, a non-`NULL` `title`, and a `started` date; or
  `status: idle` with every item field `NULL`. Applied per file.
- **I5 — Blocked items state why.** Every item under `## Blocked` carries
  `blocked`; no item elsewhere in `backlog.md` does.
- **I6 — Done items are dated and filed.** Every item in `done.md` carries
  `done` and `outcome`, and sits under a month heading matching its `done` date.
- **I7 — Well-formed values.** Dates are valid ISO 8601 calendar dates per
  §3.3.1 — the lexical form AND a real day of a real month; `prio`, `outcome`, and
  `tags` values are drawn from their vocabularies; no field value contains `|`;
  no key is repeated within an item line. `prio`, `tags`, `created`, and
  `started` are validated identically wherever they appear — item line or
  working-file frontmatter — since §5.2 gives them the same lexical form in both.
  `backlog.md` frontmatter carries a non-empty `project` (§5.1).
- **I8 — Detail references resolve.** Every `detail` value equals
  `details/<ID>.md` for the ID that carries it, and that file exists.
- **I9 — Detail files are claimed exactly once.** Every file in `details/` not
  beginning with `_` is referenced by exactly one item, and its frontmatter `id`
  and `title` match that item's ID and title exactly.
- **I10 — The working file set is well formed.** At least one working file
  exists; slot numbers are the contiguous sequence 1..N; every slot number uses
  the same digit width; no file is named `working.md` (§5.2.1).

## 8. Conformance

**A conforming reader** implements §3 and §4, recognizes every schema in §5,
tolerates unregistered fields and unknown frontmatter keys, and never treats a
`- [` line in a working file as an item.

**A conforming writer** additionally emits the required frontmatter keys and
sections for each file, maintains `next_id`, preserves unregistered fields when
moving an item, and produces output that satisfies §7.

**A conforming checker** verifies I1–I10 and reports each violation with a
`path:line: message` location. It exits non-zero when any violation is found.

Byte-for-byte round-tripping is NOT required. A writer MAY normalize field order,
whitespace, and section spacing. It MUST NOT drop fields, reorder `## Ready`, or
renumber IDs.

## 9. Extensibility and versioning

`version: 1` in each file's frontmatter is the format version, not a content
revision. Bump it only for an incompatible format change.

Forward compatibility rules:

- Unregistered field keys on an item line are **valid**. Readers accept them;
  writers preserve them across moves. This is the intended extension point — a
  new field needs no spec change to start being used.
- Unknown frontmatter keys are **valid** and MUST be ignored, not rejected.
- Unknown `## ` headings are an **error** in `backlog.md` and `done.md`. Section
  vocabulary is closed in both files.
- The WIP limit is expressed only as a file count. A future revision MUST NOT
  add a `wip_limit` key without also deciding which of the two wins; extensions
  MUST NOT introduce one.
- Reserved for future use, MUST NOT be redefined by extensions: `id`, `status`,
  `next_id`, `doc`, `version`.

A reader encountering `version` greater than the version it implements SHOULD
report a version mismatch rather than parse the file speculatively.

## 10. Known limitations

Documented deliberately; a second implementation is not expected to fix them
without a spec revision.

1. **Timestamps are date-granular.** The four files of §5 record dates, never
   times (§3.3.1). Two items finished on the same day have no recorded order
   beyond their position in the month group, and cycle time is measured in whole
   days. This is deliberate — a clock in a hand-edited file is a field people get
   wrong, and month grouping is the only ordering `done.md` actually needs — but
   it does mean the format cannot answer "which did I finish first" within a day.
2. **A pipe in a title is usually, not always, caught.** `Fix a | b` splits into
   a title and a bogus field `b`, which fails the key:value rule and is
   reported. But `Fix a | b: c` splits into a title and a well-formed
   unregistered field `b:c` — accepted silently, and the title is silently
   truncated. §4.2 forbids `|` in titles; a writer is the only thing enforcing
   it.
3. **Frontmatter is not YAML.** §4.1 describes a flat-map parser. A file that is
   valid YAML but violates §4.1 (nested maps, block scalars, single-quoted
   values) will be misread. This is why `tags` is a `TAGLIST` rather than a YAML
   flow sequence: a flat-map parser cannot read `[infra, ci]` as a list, so the
   list form would be an unvalidated string pretending to be structured data.
4. **Ordering is a convention, not a constraint.** Nothing verifies that
   `## Ready` is in priority order, that month groups in `done.md` descend, or
   that items within a group descend.
5. **`done-YYYY.md` archives are unvalidated.** Once a month group is moved out
   of `done.md`, its items leave the ID pool, and I1 and I2 no longer see them.
6. **A subtask cannot be validated.** Because `- [` lines in a working file are
   unstructured by design (§5.2), a malformed one is indistinguishable from
   prose.
7. **The WIP limit cannot be exceeded, only mis-set.** Because the limit is the
   file count, no state can violate it — starting a fourth item with three slots
   is impossible rather than invalid. The trade-off is that raising the limit is
   as cheap as `cp`, so nothing records that it was raised or why. `done.md`
   shows what was finished, never how many slots were open at the time.
8. **`project` is unconstrained.** Being free text, it cannot be validated
   beyond non-emptiness. Nothing detects a `project` that no longer describes
   what the directory holds, and nothing prevents two directories claiming the
   same name.

---

## Appendix A: reference regexes

POSIX ERE. `SP` is a literal space.

```
item line       ^- \[[ x]\] \[T-[0-9]{4}\] .
item capture    ^- \[( |x)\] \[(T-[0-9]{4})\] ([^|]+?)( \| (.*))?$
field separator SP\|SP                        (literal " | ")
ID              ^T-[0-9]{4}$
DATE            ^[0-9]{4}-[0-9]{2}-[0-9]{2}$      (see note)
MONTH           ^[0-9]{4}-[0-9]{2}$
WEEK            ^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$
TIME            ^([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$
TIMESTAMP       ^[0-9]{4}-[0-9]{2}-[0-9]{2}T([01][0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9](Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$
TAGLIST         ^[A-Za-z0-9._-]+(,[A-Za-z0-9._-]+)*$
DETAILPATH      ^details/T-[0-9]{4}\.md$
working file    ^working\.[0-9]+\.md$        (uniform width per directory)
prio            ^(high|med|low)$
outcome         ^(shipped|cancelled|obsolete)$
frontmatter end ^---$
section heading ^##SP
```

Implementations targeting awk variants without interval-expression support
should expand `{4}` to `[0-9][0-9][0-9][0-9]`, as the reference checker does.

**Note on `DATE`.** The pattern above accepts the lexical form only. Calendar
validity — month `01`–`12`, the correct number of days for that month, and the
leap-year rule of §3.3.1 — CANNOT be expressed as a readable regex and MUST be
checked in code. A conforming checker that matches only the pattern is not
conforming: `2026-02-31` passes the regex and is an invalid date. The same
applies to `MONTH`, whose month component must be `01`–`12`.

## Appendix B: directory naming

A micro-manager directory is recognized by name alone; contents are not
inspected during discovery. Recognized names:

| Name | Bytes of the leading sign |
|---|---|
| `micro-manager` | — |
| `.micro-manager` | — |
| `µmanager` | `C2 B5` — U+00B5 MICRO SIGN |
| `.µmanager` | `C2 B5` |
| `μmanager` | `CE BC` — U+03BC GREEK SMALL LETTER MU |
| `.μmanager` | `CE BC` |

Both codepoints are recognized because they are visually identical in nearly
every font and are trivially confused when typing. Writers SHOULD create
directories using U+00B5 MICRO SIGN. Readers MUST accept either.

Discovery is recursive from a search root, and dot-prefixed variants MUST be
found — a discovery implementation that skips hidden directories is
non-conforming, since two of the four conventional names begin with a dot.

Discovery MUST NOT descend into a directory it has matched: a micro-manager
directory nested inside another is undefined, and pruning at the match keeps a
deep tree cheap to walk.

Matching is on **name only**, so a directory that merely shares the name — a
source repository called `micro-manager`, a skill or plugin directory — will be
reported by discovery and then rejected by a checker as not a todo directory.
Discovery implementations therefore SHOULD prune well-known directories that
never contain projects (`.git`, `.claude`, `node_modules`, `vendor`, `target`,
`dist`, `build`, `.venv`) and SHOULD let the user add to that list. This is a
convenience, not a correctness rule: the authority on whether a directory is a
micro-manager directory remains its contents.
