# micro-manager — data file format specification

    Spec version: 1
    Date:         2026-07-29
    Status:       stable
    Applies to:   backlog.md, working.NN.md, done.md, details/<ID>.md

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
| `details/<ID>.md` | no | §5.4 |
| `details/_*.md` | no | §5.4.1 |
| `structure.md` | no | §5.5 |
| `done-YYYY.md` | no | §5.6 |
| `details-YYYY/<ID>.md` | no | §5.6 |

Directory naming is specified in Appendix B. Any other file in the directory is
outside this specification and MUST be ignored by conforming readers.

The two archive entries are specified but **not validated**: §5.6 says what
they hold and how they are written, and no invariant of §7 reads them.

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
| `SCHEDULE` | a tickler schedule expression — a calendar date, a weekday optionally prefixed by an ordinal, or a month day, each with an optional `@HH:MM` wall time (not the `TIME` token: no seconds, no offset) | `2026-09-01`, `mon@08:00`, `first-mon@08:00`, `15@08:00`, `last@08:00` |
| `TAG` | one or more of `A-Z a-z 0-9 . _ -` | `ci`, `infra-2` |
| `TAGLIST` | one or more `TAG`, comma-separated, no spaces | `infra,ci` |
| `SLUG` | one to sixteen ASCII characters: a lowercase letter, then lowercase letters, digits or hyphens | `py`, `python-impl` |
| `LINKLIST` | one or more `SLUG` `:` `ID`, comma-separated, no spaces, where `ID` takes the generic form shared by every declared grammar — `[A-Z]{1,4}-[0-9]{1,15}` (§3.3.2, §6) | `py:T-0012,go:T-0003` |
| `DETAILPATH` | `details/` + `ID` + `.md` | `details/T-0042.md` |
| `SLOT` | one or more ASCII digits, zero-padded to the directory's width | `01`, `003` |
| `NULL` | the literal four characters `null` | `null` |

`ID` values are zero-padded to the directory's width. `T-42` is invalid;
`T-0042` is correct.

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

The `tickler` field is the one sanctioned exception (§3.3, §6): its `SCHEDULE`
value MAY carry an `@HH:MM` wall time inside the expression. That time is part
of the schedule's grammar, read only by the process evaluating the schedule in
its own zone — it is not a time-of-day field, and the checker validates its
shape without ever consulting a clock (§10.1).

#### 3.3.2 The ID grammar is directory-configurable

The default grammar is the prefix `T` and width 4 (§3.3); every directory that
declares nothing else uses it. A directory MAY declare its own grammar with two
optional keys in `backlog.md` frontmatter (§5.1):

| Key | Value | Default |
|---|---|---|
| `id_prefix` | one to four ASCII letters, all uppercase (`A-Z`) | `T` |
| `id_width` | one to fifteen ASCII digits | `4` |

Rules:

1. **One grammar per directory.** The directory declares a single prefix and a
   single width; every ID in the directory — item lines, working-file
   frontmatter, `next_id`, and `details/` filenames — uses it. A directory
   containing IDs in more than one grammar is invalid. (Per-task prefixes are a
   possible future extension; they are NOT part of this spec version.)
2. **Either key MAY appear alone**; the other then takes its default. The
   prefix is one to four ASCII letters and MUST be all uppercase — `t` and
   `Tt` are invalid. Matching is exact: in a directory declaring `T`, the ID
   `t-0042` is invalid.
3. **The declared prefix is one to four ASCII letters.** An ID is the prefix, a
   hyphen, then exactly `id_width` ASCII digits, zero-padded. `id_width` is
   one to fifteen ASCII digits. Width 3–6 is RECOMMENDED; a checker SHOULD
   warn — not fail — on any other width within that bound. The ladder in
   full: widths 1–2 warn, 3–6 are RECOMMENDED, 7–15 warn, and a width above
   15 is **invalid** — a reader MUST refuse it loudly (rule 4), never
   silently accept it. The cap is exactly where the simplest reader stops
   being exact: at 16 digits the reference checker's mawk arithmetic silently
   rounds, so the uniform refusal keeps every reader's comparison exact
   (Appendix A).
4. **Readers MUST read the declaration before interpreting any ID.** A reader
   that cannot honor a declared grammar MUST refuse loudly — report a version
   mismatch and exit — never silently misparse.
5. **`next_id` stays one monotonic counter** (§7, I2), in the declared grammar.
   Width `W` caps the counter space at `10^W − 1` items for W ≤ 15,
   generalizing the default's 9999-item cap.
6. **Additive, not a version bump.** Directories without the keys behave
   byte-identically to spec version 1; `version: 1` is unchanged. A reader of
   the current spec MUST accept a default-grammar directory exactly as before.

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
3. A line with no `:` MUST be ignored. (Git conflict-marker lines are the one
   exception: a line whose first non-blank characters are exactly `<<<<<<<`,
   `=======`, or `>>>>>>>` is invalid here as in every data file — §5.1 — and
   MUST be refused loudly, never ignored.)
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

1. A line is a candidate item line if it matches `^- \[[ x]\] \[ID\] .`
   where `ID` is the directory's declared grammar (§3.3.2) — the default
   instantiation is `^- \[[ x]\] \[T-[0-9]{4}\] .`. Note the trailing `.`,
   which requires a non-empty title.
2. For the default grammar the prefix `- [_] [T-NNNN] ` is fixed-width and
   pure ASCII: the box is at character 4, the ID at characters 8–13, and the
   title begins at character 16. Readers MAY rely on those offsets ONLY when
   the directory declares no `id_prefix`/`id_width`; otherwise the ID is
   located by token (rule 1) and the title is everything up to the first
   `" | "`.
3. Everything after the title's start — character 16 under the default
   grammar, the first `" | "` otherwise (rule 2) — is split on the
   three-character sequence `" | "` (space, pipe, space). The first part is
   the title; each remaining part is a field.
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
| `id_prefix` | one to four uppercase letters — the ID prefix (§3.3.2); absent means `T` | no |
| `id_width` | one to fifteen ASCII digits — the ID digit width (§3.3.2); absent means `4` | no |
| `board` | `SLUG` — the board's link identity for cross-board `refs` (§5.1, §6); absent means the directory is not a link target | no |
| `updated` | `DATE` | no |

`project` MUST be non-empty and MUST NOT be `NULL`. It is otherwise free text on
a single line: no length limit, no character restrictions beyond §4.1's parsing
rules. It is the only human-meaningful identity a micro-manager directory has —
discovery finds directories by path (Appendix B), and a tool presenting several
of them SHOULD show `project` rather than, or alongside, the path.

`project` is not an identifier. Two directories MAY carry the same `project`
value, and nothing binds it to the directory name.

`id_prefix` and `id_width`, when present, MUST each match §3.3.2 — `id_prefix`
is one to four ASCII letters, all uppercase, `id_width` is one to fifteen
ASCII digits. `next_id`
MUST use the declared grammar (or the default when neither key is present).

`board` is the directory's link identity: the `SLUG` that item-line `refs`
fields (§6) use to name this directory from another one. It is the one board
handle that is content rather than location — `project` is unvalidated free
text (§10) and `projectId` is derived from the canonical path (spec-gui.md
§3.1), so a moved or renamed directory keeps its slug but not its id. The
slug is therefore the form a cross-board link uses to survive a change in
file hierarchy (plan-board-links.md).

Lowercase is deliberate: every ID prefix is uppercase (§3.3.2 rule 2), so the
slug namespace and the ID namespace are disjoint by case and a slug can never
be mistaken for an ID, nor an ID for a slug. The key is optional — a directory
without `board` is not a link target — and the value is human-chosen and
human-stable: moving, renaming, or restructuring around the directory never
changes it, and changing it is a deliberate act that breaks every link to this
directory (a findable edit: the slug appears only in this frontmatter and in
`refs:` values). Like `project`, it is not globally unique — two directories
MAY declare the same slug, and a reader that finds two MUST refuse to resolve
a link to it rather than guess (§10).

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
  This is the only section an item carrying `tickler` (§6) MAY sit in, and a
  `tickler` item MUST also carry `created` — a never-fired recurring schedule
  anchors its first fire on `created`, and without the anchor a fresh
  `mon@08:00` written on a Tuesday would be judged overdue against the epoch
  and fire for the Monday that already passed.

Non-item content (prose, comments, blank lines) MAY appear anywhere and MUST be
ignored by readers.

Git conflict markers are the one reserved exception, and the rule applies to
every data file in the directory — `backlog.md`, `working.NN.md`, `done.md`,
and `details/` — not just to this one. A line whose first non-blank characters
are exactly `<<<<<<<`, `=======`, or `>>>>>>>` (the three shapes git writes
into a file whose merge conflicted) is invalid, and a conforming reader MUST
refuse it loudly — report the file and line — never ignore it (§4.1, §8). A
half-resolved merge must never be indistinguishable from valid prose. Ordinary
prose that merely contains `<` or `>` is unaffected.

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
| `refs` | `LINKLIST` or `NULL` | `NULL` |
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

When the file grows unwieldy, trailing month groups MAY be archived to
`done-YYYY.md` in the same directory, together with the detail files of the
items in them. §5.6 defines the layout and what it costs.

### 5.4 `details/<ID>.md`

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

The one path out of `details/` is archiving. When an item's month group leaves
`done.md`, its detail file leaves with it, to `details-YYYY/` (§5.6) — moved,
not deleted, and still readable by anything that follows the archived item's
own `detail` field.

#### 5.4.1 Template files

A file in `details/` whose name begins with `_` is a template. Templates are
exempt from every constraint in §5.4 and from invariant I9. `_template.md` is
conventional.

### 5.5 `structure.md`

Human documentation. Not validated, not parsed, no required format. Its presence
is optional; its absence changes nothing for a reader.

### 5.6 Archives: `done-YYYY.md` and `details-YYYY/`

`done.md` is the file nothing ever leaves. Every finish, cancellation and
abandonment lands there and stays, which is the point (§7, I1) and also means
the file only grows. A directory MAY therefore **archive** completed months:
whole month groups move out of `done.md` into `done-YYYY.md`, and the detail
files of the items in them move out of `details/` into `details-YYYY/`, both in
the same directory.

```
micro-manager/
  done.md          2026-08, 2026-07 …   the live record
  done-2025.md     2025-12 … 2025-01    archived month groups
  details/         T-0198.md …          detail files of live items
  details-2025/    T-0007.md …          detail files of archived items
```

`YYYY` is four ASCII digits and is taken from the month heading being moved,
never from today's date: the group `2025-03` goes to `done-2025.md` and its
detail files to `details-2025/`, whenever the archive is run.

1. **An archive file has `done.md`'s schema** (§5.3): the same frontmatter with
   `doc: done`, month groups newest first, closed item lines carrying `done`
   and `outcome`. A person opening one finds the file they already know how to
   read, and one parser serves both.

2. **Archives are not validated.** `done-YYYY.md` and `details-YYYY/` are
   outside I1–I10 (§7). A checker MUST NOT read them, and specifically MUST NOT
   report a file in `details-YYYY/` as an I9 orphan or an archived `detail`
   value as an I8 violation. This costs no new checker code, and that is not an
   accident: I8 and I9 name `details/` exactly, so `details-2025/` is already
   invisible to a checker that reads the spec literally.

3. **A detail file travels with its item, and its `detail` field is rewritten.**
   An archived item carrying `detail:details/<ID>.md` MUST end up with the file
   at `details-YYYY/<ID>.md` and the archived line reading
   `detail:details-YYYY/<ID>.md`, in the same operation. Half a move — a line
   in `done-2025.md` pointing into `details/` where the file no longer is, or a
   file in `details-2025/` that no line names — is a broken archive, and by
   rule 2 nothing will tell you.

   Moving the file is what keeps the *live* directory clean: leaving it in
   `details/` when its item is gone from `done.md` is an I9 orphan, reported
   for as long as the archive exists.

4. **Restoring is the same move backwards, and fails loudly.** Pasting a group
   back into `done.md` MUST be accompanied by moving each
   `details-YYYY/<ID>.md` back to `details/<ID>.md` and rewriting `detail`.
   Doing only half of it is caught, because the restored line is validated
   again: I8 reports a `detail` that is not `details/<ID>.md`, or one that is
   and names a file that is not there. Archiving is reversible by hand; it is
   not reversible by *half* a hand, and that is the cost rule 3 buys with.

5. **Archiving never recycles an ID.** An archive MUST NOT change `next_id`.
   Archived IDs are retired exactly as I2 requires, and a tool that derives
   `next_id` from what it can see MUST read the archives too — otherwise it
   hands out an ID that a `done-YYYY.md` already holds, and by rule 2 no
   checker will ever notice (§10.5).

6. **Archiving is a policy, not a schedule the format sets.** The format says
   what an archive looks like and nothing about when to make one. Cutoffs and
   automation belong to a tool (`spec-tools.md` §5.3), which MUST NOT archive
   without being asked: the operation moves data out of the checked set, and
   that is not something to discover after the fact.

## 6. Field registry

Fields valid on an item line. A reader encountering an unregistered key MUST
accept it, and a writer moving an item between files MUST preserve it verbatim
(§9).

| Key | Value | Required | Valid in | Notes |
|---|---|---|---|---|
| `prio` | `high` / `med` / `low` | no | all | Absent means `med`. |
| `tags` | `TAGLIST` | no | all | No spaces. Identical form in working-file frontmatter. |
| `refs` | `LINKLIST` | no | all | Cross-board references, §6. Never validated for resolution (§9). |
| `created` | `DATE` | no | all | When the item was written down. |
| `started` | `DATE` | no | working, done | Set on entering a working file; survives a pause. |
| `done` | `DATE` | **yes** in `done.md` | done | |
| `outcome` | `shipped` / `cancelled` / `obsolete` | **yes** in `done.md` | done | |
| `blocked` | free text, no `|` | **yes** in `## Blocked` | backlog | Forbidden in `## Ready` and `## Someday`. |
| `tickler` | `SCHEDULE` | no | `## Someday` only | Fires when the schedule's next instant arrives — a bare date is one-shot (the item moves to Ready); a weekday or monthday spec recurs, making the item a prototype that spawns a new Ready item on each fire. Placement and the `created` requirement: §5.1. |
| `tickled` | `DATE` | no | all | Date the item's `tickler` last fired. Audit trail; harmless after a manual move. |
| `detail` | `DETAILPATH` | no | all | MUST equal `details/<this item's ID>.md`. In an archived file it is `details-YYYY/<ID>.md` instead (§5.6); archived files are not validated. |

`refs` names items in *other* directories — each element is a target board's
`board` slug (§5.1), a colon, and the target item's ID. The ID half uses the
generic form every declared grammar shares (`[A-Z]{1,4}-[0-9]{1,15}`, §3.3.2):
a local reader can check the *shape* of a link without knowing the target
directory's declared grammar, and a link's target grammar is exactly the thing
a per-directory checker cannot know. Resolution — whether the named board and
item exist — is deliberately NOT this directory's business (§9): a stale or
ambiguous link never invalidates a directory.

### 6.1 Canonical field order

Writers SHOULD emit fields in this order. Readers MUST NOT require it.

```
prio, tags, refs, detail, created, started, blocked, tickler, tickled, done, outcome, <unregistered...>
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
  decremented, so IDs are never reused. All IDs — including `next_id` — use
  the directory's declared grammar (§3.3.2), and the comparison is over the
  digit portion at the declared width.
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
  A `tickler` value is checked for shape only — it MUST match the `SCHEDULE`
  grammar of §3.3 and MUST NOT be evaluated: no clock enters the format, and a
  schedule can never make a directory invalid because a clock disagrees (§10.1).
  An item carrying `tickler` MUST sit in `## Someday` and MUST also carry
  `created` (§5.1); `tickled` is a valid `DATE` wherever it appears.
  `backlog.md` frontmatter carries a non-empty `project` (§5.1).
- **I8 — Detail references resolve.** Every `detail` value equals
  `details/<ID>.md` for the ID that carries it, and that file exists.
- **I9 — Detail files are claimed exactly once.** Every file in `details/` not
  beginning with `_` is referenced by exactly one item, and its frontmatter `id`
  and `title` match that item's ID and title exactly.
  I8 and I9 name the live files and only those: `done-YYYY.md` is not read and
  `details-YYYY/` is not `details/`, so an archive is outside both (§5.6).
- **I10 — The working file set is well formed.** At least one working file
  exists; slot numbers are the contiguous sequence 1..N; every slot number uses
  the same digit width; no file is named `working.md` (§5.2.1).

## 8. Conformance

**A conforming reader** implements §3 and §4, recognizes every schema in §5,
tolerates unregistered fields and unknown frontmatter keys, never treats a
`- [` line in a working file as an item, and refuses git conflict-marker lines
in every data file rather than ignoring them (§5.1).

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
- **Links are advisory, never load-bearing.** A `refs` value (§6) is checked
  for shape and nothing else: a per-directory validator cannot see other
  directories, so whether a link's target exists is never validated here, and
  a stale or ambiguous link MUST NOT invalidate a directory the way a broken
  `detail` reference does. Resolution is the business of a tool with a
  tree-wide view (the GUI, discovery, a future sweep), and that tool MUST
  refuse to resolve an ambiguous slug rather than guess (§5.1).
- Reserved for future use, MUST NOT be redefined by extensions: `id`, `status`,
  `next_id`, `doc`, `version`, `id_prefix`, `id_width`, `board`.

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
   The tickler keeps the discipline: its schedule expression may name an
   `@HH:MM` wall time (§3.3), but that time lives inside the expression, read
   only by the evaluating process in its own zone. The checker validates the
   schedule's *shape*, never its meaning, so a schedule cannot make a directory
   invalid because a clock disagrees — and two processes with different zones
   see the same data while their judgement of "due" can differ by up to an
   hour, which is exactly the wall-clock behaviour a "Monday at 08:00" promise
   implies.
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
5. **Archives are unvalidated, and archived IDs leave the pool.** Once a month
   group is moved out of `done.md` (§5.6), its items are invisible to the
   checker: I1 no longer notices an ID that exists both in the archive and in
   `backlog.md`, and I2 no longer counts one against `next_id`. Their detail
   files do not become I9 orphans, because §5.6 moves them to `details-YYYY/`
   and out of I8 and I9's reach — but that is the same invisibility, not an
   exemption from it: nothing checks that an archived line and its archived
   detail file still agree, or that the file is there at all. Archiving trades
   checking for size, and the trade stays safe only while `next_id` keeps
   rising (I2, §5.6 rule 5), the one rule that still spans both halves.
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
9. **`board` slugs are not globally unique.** The format has no server and no
   global registry; even a tree-wide sweep cannot see boards in other
   repositories. Two directories MAY declare the same `board` slug (§5.1). A
   resolver that finds two known boards with one slug MUST refuse to resolve
   links to it rather than guess, naming both boards — the same discipline a
   repair applies to a tie it cannot decide.
10. **A `refs` link can dangle.** The target item may be renumbered by a
    collision repair in its own directory (a repair is directory-local by
    design), or the target directory may move or vanish. Validators never
    check resolution (§9), so a stale link costs exactly one rendered "missing
    target" in a front end that resolves — never a board error.

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
SCHEDULE        ^([0-9]{4}-[0-9]{2}-[0-9]{2}|(first|second|third|fourth|last)-(mon|tue|wed|thu|fri|sat|sun)|mon|tue|wed|thu|fri|sat|sun|0[1-9]|[12][0-9]|3[01]|last)(@([01][0-9]|2[0-3]):[0-5][0-9])?$
                                             (the DATE alternative must be a real calendar date, per the DATE note)
TAGLIST         ^[A-Za-z0-9._-]+(,[A-Za-z0-9._-]+)*$
SLUG            ^[a-z][a-z0-9-]{0,15}$
LINKLIST        ^[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15}(,[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15})*$
DETAILPATH      ^details/T-[0-9]{4}\.md$
working file    ^working\.[0-9]+\.md$        (uniform width per directory)
archive file    ^done-[0-9]{4}\.md$          (informative; never validated)
archive detail  ^details-[0-9]{4}/T-[0-9]{4}\.md$
                                             (informative; never validated)
prio            ^(high|med|low)$
outcome         ^(shipped|cancelled|obsolete)$
frontmatter end ^---$
section heading ^##SP
```

Implementations targeting awk variants without interval-expression support
should expand the width term (`{4}` for the default, `{W}` for a declared
width) to that many `[0-9]`, as the reference checker does.

The regexes above instantiate the default grammar (prefix `T`, width 4). For a
directory declaring `id_prefix`/`id_width` (§3.3.2), substitute the declared
prefix for `T` and the declared width for `4` — `ID` becomes `^P-[0-9]{W}$`
with the declared `P` (one to four letters) and `W` (one to fifteen digits),
and the same substitution applies to the item
line, item capture, and `DETAILPATH` patterns.

The last two patterns are given for readers that follow an archived item into
`done-YYYY.md` (§5.6). They are informative: no checker matches them, because
no checker reads those files.

The width bound is the reference checker's arithmetic. check.sh runs on mawk's
double, which is exact for every integer of up to 15 digits
(`10^15 − 1 = 999,999,999,999,999 < 2^53`) and silently rounds at 16
(`"9999999999999999" + 0` evaluates to `1e16`) — the same limit as
JavaScript's `number`. The I2 comparison (§7) runs in that arithmetic, so a
width above 15 would be a comparison the reference checker cannot make
exactly; that is why §3.3.2 rule 3 makes 16+ invalid everywhere, not merely
unrecommended.

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
