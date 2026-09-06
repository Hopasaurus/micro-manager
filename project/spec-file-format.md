# micro-manager — data file format specification

    Product version: 0.2.1
    Spec version: 2
    Date:         2026-08-20
    Status:       draft
    Applies to:   board.md, done.md, details/<ID>.md
    Supersedes:   version 1 (backlog.md + working.NN.md); see Appendix C for
                  the shape that superseded, and spec-tools.md §5.3 for the
                  migration mechanism

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
| `board.md` | yes | §5.1 |
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

A directory at spec version 1 — `backlog.md` plus one or more `working.NN.md`
files — is a valid, readable board under the *previous* version of this
document, not this one. §9 states what a version-2 reader owes a version-1
directory; Appendix C summarizes the version-1 shape for migration authors;
`spec-tools.md` §5.3 specifies the migration mechanism itself.

## 2. Terminology

**MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** carry their
RFC 2119 meanings.

- **Item** — one unit of tracked work, identified by an ID.
- **Stage** — the value of an item's `stage:` field: where it sits on the
  board (§5.1). A directory declares its own set of stages and their order
  (§5.1.1); `someday`, `ready`, `blocked`, and `working` are the default
  set, not a closed vocabulary.
- **WIP limit** — an optional per-stage cap declared in a `wip.<slug>`
  frontmatter key (§5.1.3); absent means that stage is uncapped.
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
| `STAGE` | a `SLUG` that is also a member of the directory's declared `stages` (§5.1.1) | `ready`, `review` |
| `LINKLIST` | one or more `SLUG` `:` `ID`, comma-separated, no spaces, where `ID` takes the generic form shared by every declared grammar — `[A-Z]{1,4}-[0-9]{1,15}` (§3.3.2, §6) | `py:T-0012,go:T-0003` |
| `DETAILPATH` | `details/` + `ID` + `.md` | `details/T-0042.md` |
| `NULL` | the literal four characters `null` | `null` |

`ID` values are zero-padded to the directory's width. `T-42` is invalid;
`T-0042` is correct.

#### 3.3.1 Dates, times and timestamps are ISO 8601

**Every date, time and timestamp anywhere in a micro-manager directory MUST be
ISO 8601, in the extended format, with the separators shown above.** This is
normative for the files of §5, and for every adjacent file a tool writes
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

`TIME` and `TIMESTAMP` are defined here for the whole system but are used by
only three fields in the files of §5: `created`, `started`, and a file's own
`updated` (§6, §5.1–§5.4). Each of those MAY be a plain `DATE` or the combined
`TIMESTAMP` form — the time is OPTIONAL, never required, and a reader MUST
accept either shape wherever one of these three fields appears. `done` and
`tickled` stay `DATE`-only: `done.md` groups by month (§5.3), and neither
field has ever needed finer precision. A tool MUST NOT introduce a
time-of-day form on any OTHER field without a spec revision — see §10 for
what the remaining date-only granularity costs and why it is still the right
trade there.

A time on `created`/`started`/`updated` is written and read verbatim; nothing
in this data model *computes* with it. Every calculation this format defines
— cycle time, flight time, month grouping, sorting — reads the calendar date
half only and stays exactly as date-granular as before. The time exists for
provenance (a hand-added "I actually started this at 14:30," a tool that
knows its own wall clock), not for the format's own arithmetic; a reader MAY
ignore it entirely and lose nothing §5–§7 requires. A conforming writer
MUST NOT truncate a time it did not need to change — round-tripping an item
through any operation preserves whatever precision was already there,
exactly like an unregistered field (§9).

The `tickler` field is the one sanctioned exception (§3.3, §6): its `SCHEDULE`
value MAY carry an `@HH:MM` wall time inside the expression. That time is part
of the schedule's grammar, read only by the process evaluating the schedule in
its own zone — it is not a time-of-day field, and the checker validates its
shape without ever consulting a clock (§10.1).

#### 3.3.2 The ID grammar is directory-configurable

The default grammar is the prefix `T` and width 4 (§3.3); every directory that
declares nothing else uses it. A directory MAY declare its own grammar with two
optional keys in `board.md` frontmatter (§5.1):

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
   byte-identically to a directory that never declares them; the directory's
   format version is unchanged by declaring them. A reader of the current
   spec MUST accept a default-grammar directory exactly as before.

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
- [ ] [T-0042] Fix the deploy script | stage:ready | prio:high | tags:infra,ci | created:2026-07-29
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
8. A reader MAY tolerate and retain a legacy unregistered key that does not
   match the `key` grammar, so the item remains readable and the field can be
   removed. A writer MUST NOT introduce or replace such a key, but an unrelated
   mutation MAY preserve it verbatim.

The title MUST NOT contain `|`. See §10 for the one case where violating this
is not reliably detected.

### 4.3 Section headings

A line beginning with `## ` opens a section; the section name is the rest of the
line, trimmed. Sections are flat — a section runs until the next `## ` line or
end of file. `# ` (level 1) headings are titles and carry no meaning. Headings
below level 2 are not used in either item file.

This mechanism serves `done.md`'s month groups (§5.3). `board.md` does not use
section headings — `stage:` (§5.1.6, §6) is the sole source of truth for where
an item sits — and any `## ` line found there is ordinary non-item content,
not a section (§5.1.6).

## 5. File schemas

### 5.1 `board.md`

Holds every item that is not yet done — every declared stage except the
terminal state, which is `done.md` (§5.3). By default that means `someday`,
`ready`, `blocked`, and `working`; a directory MAY declare additional stages
(§5.1.1).

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `board` | yes |
| `version` | spec version, currently `2` | yes |
| `project` | human name of what this directory tracks | yes |
| `next_id` | `ID` — the ID to assign to the next new item | yes |
| `id_prefix` | one to four uppercase letters — the ID prefix (§3.3.2); absent means `T` | no |
| `id_width` | one to fifteen ASCII digits — the ID digit width (§3.3.2); absent means `4` | no |
| `board` | `SLUG` — the board's link identity for cross-board `refs` (§5.1, §6); absent means the directory is not a link target | no |
| `updated` | `DATE` or `TIMESTAMP` (§3.3.1) | no |
| `stages` | comma-separated `STAGE` list, order-significant (§5.1.1); absent means `someday,ready,blocked,working` | no |
| `stage_labels` | comma-separated `slug:Label` pairs (§5.1.2); absent means every label is derived from its slug | no |
| `wip.<slug>` | a positive integer WIP cap for stage `<slug>` (§5.1.3); zero or more keys | no |
| `tickler_stages` | comma-separated `SOURCE->DEST` pairs (§5.1.4); absent means `someday->ready` | no |
| `needs_reason` | comma-separated `STAGE` list (§5.1.5); absent means `blocked` | no |
| `audit` | `true` or `false` (§5.1.8); absent means `false` | no |

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

The `board` frontmatter key and the `board.md` filename name unrelated
concepts that happen to share a word: the key is a link-identity slug (above);
the filename is simply what this file is called. A directory MAY declare
`board: foo` inside `board.md`, with no relationship between the two implied.

#### 5.1.1 `stages`: the set and its order

`stages` is a comma-separated, order-significant list of `STAGE` values — a
`SLUG` (§3.3) naming one stage. Order is column order, left to right, for any
front end that renders stages as columns (`spec-gui.md` §5.5, `spec-tui.md`
§5.1).

The list defines the directory's **entire** stage vocabulary: every `stage:`
value on an item line (§6), every key in `wip.<slug>` (§5.1.3), every
`SOURCE`/`DEST` in `tickler_stages` (§5.1.4), every value in `needs_reason`
(§5.1.5), and every key in `stage_labels` (§5.1.2) MUST be a member of
`stages` — I7 checks this (§7, §5.1.7). A board cannot cap, schedule, route,
label, or place an item in a stage it hasn't declared.

Absent, `stages` defaults to `someday,ready,blocked,working` — reproducing
version 1's four sections/states, in the same order, for a directory that
never customizes it.

No stage is structurally distinguished from any other by this key. What made
`working` special in version 1 is now config: its WIP cap is a `wip.working`
key like any other stage's (§5.1.3), not a physical file count. One thing
about `working` stays hardcoded rather than becoming configurable: `started`
is required whenever `stage:working` (§7, folded in from version 1's I4) —
that marks *when work began*, which is what `working` means, not a policy a
board sets.

#### 5.1.2 `stage_labels`: display name, decoupled from the stored slug

`stage_labels` is a comma-separated list of `slug:Label` pairs, each split on
its *first* `:` (the same rule §4.1 uses one level up, for frontmatter itself)
— so a label MAY contain a colon, but not a comma. Only stages worth
overriding need an entry; it is sparse, not a full enumeration.

A slug with no entry gets a label derived mechanically: title-case, hyphens
become spaces (`code-review` → `Code Review`). This is what makes the key
optional: a directory that never sets it, or sets it for only one stage,
still gets a sensible label for every other stage.

`stage:` values are never rendered verbatim by a conforming front end;
`stage_labels` — or its absence, and the derivation rule above — is what a
person actually sees. Renaming a column is therefore a frontmatter edit that
touches zero item lines.

#### 5.1.3 `wip.<slug>`: per-stage WIP caps

A `wip.<slug>` key sets an integer cap on how many items may sit in stage
`<slug>` at once. `<slug>` MUST be a member of `stages` (§5.1.1). A stage
with no `wip.<slug>` key is **uncapped** — including `working`: version 1's
structural one-WIP-slot-per-file guarantee does not carry forward as a
default; a directory that wants a cap sets it explicitly, `wip.working: N`.

The cap is a **checked** invariant, not the physical impossibility version 1
achieved by having no free working file to write into (§10). A mutation that
would exceed a declared cap is refused before it is written; `--check`
(`spec-tools.md` §5.1.12) reports a directory that somehow has more anyway
(§10 note 7).

#### 5.1.4 `tickler_stages` and `tickler_dest`: where a schedule lives and where a fire lands

`tickler_stages` is a comma-separated list of `SOURCE->DEST` pairs — `SOURCE`
is a stage a `tickler:` field is legal in and that `--tick` scans; `DEST` is
where that stage's fires land by default. Both MUST be members of `stages`.
A `SOURCE` MUST NOT repeat (one destination per source — the same "repeated
key is an error" shape §4.2 rule 6 already applies within one item line).
Once the key is present, every entry MUST be a complete `SOURCE->DEST` pair;
a bare slug with no arrow is invalid. Absent, `tickler_stages` defaults to
the single pair `someday->ready`, reproducing version 1's behavior exactly.

`tickler` (§6) is valid only on an item whose current stage is a `SOURCE`
named in `tickler_stages` — generalizing version 1's "MUST sit in
`## Someday`" (I7). `created` remains a required companion whenever
`tickler` is present (§6), unchanged and unrelated to which stage carries it.

`tickler_dest` (§6) is an item-line field, a `STAGE` value, valid only where
`tickler` is valid. When present it overrides that one item's destination —
for both a one-shot move and a recurring spawn alike, since it is the item
being routed either way, not a property of the schedule's shape. When absent,
the item uses its current stage's `tickler_stages` entry.

#### 5.1.5 `needs_reason` and `reason`

`needs_reason` is a comma-separated `STAGE` list naming which stages require
the `reason` field (§6) on every item that sits in them. Absent, it defaults
to `blocked` alone, reproducing version 1's I5 for a directory that never
customizes it.

`reason` (renamed from version 1's `blocked`, §6, §10) is valid on **any**
stage — required where `needs_reason` lists the item's current stage,
optional everywhere else. It behaves like `prio` or `tags`: legal anywhere,
sometimes required, and — unlike version 1's `blocked`, and unlike `tickler`
(§5.1.4) — it is NOT dropped automatically when an item leaves a
`needs_reason` stage. It has the same shape as `tickled` (§6): kept as
historical once it is no longer load-bearing.

#### 5.1.6 Body: item lines, no sections

`board.md`'s body is a flat list of item lines (§4.2). Version 1's three
fixed `## Ready`/`## Blocked`/`## Someday` sections (§4.3) do not carry
forward; `stage:` (§6) is the sole source of truth for where an item sits. A
writer MAY still group items physically by stage for a human reading the raw
file, but SHOULD keep `## ` headings out of it entirely — §4.3's heading
mechanism is retained for `done.md`'s month groups (§5.3), not reused here,
and any `## ` line found in `board.md` is ordinary non-item content.

Every item line in this file MUST have box `" "` (open) and MUST carry a
`stage:` field whose value is a member of `stages` (§5.1.1). Order within a
stage is the file's own order; a writer appends a newly-started or
newly-moved item to the end of its stage's run.

Non-item content (prose, comments, blank lines) MAY appear anywhere and MUST be
ignored by readers.

Git conflict markers are the one reserved exception, and the rule applies to
every data file in the directory — `board.md`, `done.md`, and `details/` —
not just to this one. A line whose first non-blank characters are exactly
`<<<<<<<`, `=======`, or `>>>>>>>` (the three shapes git writes into a file
whose merge conflicted) is invalid, and a conforming reader MUST refuse it
loudly — report the file and line — never ignore it (§4.1, §8). A
half-resolved merge must never be indistinguishable from valid prose. Ordinary
prose that merely contains `<` or `>` is unaffected.

#### 5.1.7 Referential integrity

Every stage slug named outside of `stages` itself — in an item line's
`stage:`, in a `wip.<slug>` key, in a `tickler_stages` entry, in
`needs_reason`, or in `stage_labels` — MUST be a member of `stages`. A board
can't cap, schedule, route, label, or place an item in a stage it hasn't
declared. This is I7's `stage:` rule (§7), stated once here because five
different keys share it rather than repeating it five times.

#### 5.1.8 `audit`: an opt-in action log

`audit` is a boolean (`true` or `false`; absent means `false`) that enables
`audit.md` (§5.7) for this directory. Version 2 only — the key lives in
`board.md`, and a version-1 directory has none.

Turning it on does not retroactively construct history: `audit.md` records
only what happens **after** it is enabled, the same way a server access log
says nothing about requests before it started. Turning it back off does not
delete what has already been recorded — it only stops new entries; the
existing file (and its history) is untouched, exactly like disabling a
feature never implies erasing its output. `audit.md`'s own presence or
absence is never `--check`'s business either way (§5.7).

### 5.2 (retired) — `working.NN.md`

Version 1 held in-progress items in per-slot `working.NN.md` files, one item
per file, carried in frontmatter rather than as an item line. **Version 2
folds that state into `board.md` via `stage:working` (§5.1) and retires this
file type entirely: a version-2 directory MUST NOT contain a `working.NN.md`
file.**

This section number is intentionally left retired rather than reused or
removed, so that every cross-reference to a numbered section elsewhere in
this document set stays valid across the version boundary. See Appendix C
for the version-1 shape this replaces, and `spec-tools.md` §5.3 for the
migration mechanism.

### 5.3 `done.md`

Holds every closed item, newest first.

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `done` | yes |
| `version` | spec version, currently `2` | yes |
| `updated` | `DATE` or `TIMESTAMP` (§3.3.1) | no |

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
| `updated` | `DATE` or `TIMESTAMP` (§3.3.1) | no |

The `id` and `title` duplication is deliberate: it is the only mechanism by
which a detail file that has drifted from its item can be detected.

**Body**

Unconstrained. Any Markdown. `structure.md` suggests `## Context`,
`## Requirements`, `## Open questions`, and `## References`, but no heading is
required and readers MUST NOT depend on any.

**One exception: `## Plan`, when present, is normative subtask-checkbox
syntax**, not free-form prose. Lines matching `^- \[` under a `## Plan`
heading in a detail file are subtasks — no ID, no fields, unstructured
content inside the checkbox — and MUST NOT be parsed as item lines, the same
"critical parsing rule" version 1 stated for a working file's `## Plan`
(§5.2, retired). This is where that content lives now: `spec-tools.md`'s
`--note` and `--subtask`/`--subtask-done` write here directly, creating the
file on first use if it doesn't exist — a detail file is no longer purely
optional the moment an item is first noted or subtasked. Every other heading
in a detail file stays exactly as unconstrained as before.

A detail file's lifetime is independent of its item's location: the item line
moves between stages within `board.md`, and to `done.md`, while the detail
file stays at a fixed path. Detail files are never deleted on completion.

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

### 5.7 `audit.md`: an opt-in, append-only action log

Present only when `board.md`'s `audit` key (§5.1.8) is or has ever been
`true`. Version 2 only.

**Frontmatter**

| Key | Value | Required |
|---|---|---|
| `doc` | `audit` | yes |
| `version` | spec version, currently `2` | yes |

**Body**

One line per changed field, oldest first, in the order operations produced
them — an append-only log, never reordered and never rewritten once
written:

```
TIMESTAMP " | id:" ID " | field:" FIELD " | value:" VALUE
```

```
2026-08-27T14:32:10Z | id:T-0251 | field:stage | value:working
2026-08-27T14:32:10Z | id:T-0251 | field:started | value:2026-08-27
2026-08-27T14:35:02Z | id:T-0091 | field:detail | value:
```

- `TIMESTAMP` is the combined date-time form of §3.3, always carrying an
  explicit offset (§3.3.1 rule 3) — the moment the write committed, not the
  item's own `created`/`started`/`updated`, which may carry an entirely
  different time or none at all.
- `FIELD` is a field name from the registry (§6) — `stage`, `prio`,
  `created`, `started`, and so on — one line per field an operation actually
  changed, not one line per operation. A move that changes both `stage` and
  `started` (entering `working`) produces two lines with the same
  `TIMESTAMP` and `ID`. A pure reorder, which changes no field, produces no
  line at all.
- `VALUE` is that field's new value after the change, rendered exactly as it
  would appear on the item line (empty when a field was cleared, e.g. a
  `reason` dropped by leaving a `needs_reason` stage).
- **A detail-file body change — a note, an edit to its prose, its creation
  or removal — logs `field:detail` with `VALUE` left blank.** The body is
  unstructured Markdown (§5.4); there is no single "new value" to record,
  only the fact that the file changed. `ID` still names the item the detail
  file belongs to.
- **An item's removal logs `field:removed` with `VALUE` left blank**, rather
  than one line per field trailing off to empty — the item is gone, not
  edited down to nothing, and that distinction is worth keeping visible.
- A change with no `ID` — a board-level edit such as a `wip.<slug>` cap or
  the `audit` key itself — is not an item action and is never logged here.

**Never validated.** `audit.md` is a log, not board state: it carries no
constraint from §7, `--check` neither requires nor inspects it, and I1–I10
say nothing about it, the same relationship an archive has to the files it
came from (§5.6). A conforming reader MUST NOT reject a directory for
`audit.md`'s absence, presence, or content. A writer MUST NOT rewrite or
reorder an existing line — appending is the only mutation this file ever
receives.

## 6. Field registry

Fields valid on an item line. A reader encountering an unregistered key MUST
accept it, and a writer moving an item between files MUST preserve it verbatim
(§9).

| Key | Value | Required | Valid in | Notes |
|---|---|---|---|---|
| `stage` | `STAGE` | **yes** in `board.md` | board | Where the item sits (§5.1.1). MUST be a member of the directory's declared `stages`. `done.md` items carry no `stage` — they've left the board entirely. |
| `prio` | `high` / `med` / `low` | no | all | Absent means `med`. |
| `tags` | `TAGLIST` | no | all | No spaces. |
| `refs` | `LINKLIST` | no | all | Cross-board references, §6. Never validated for resolution (§9). |
| `created` | `DATE` or `TIMESTAMP` (§3.3.1) | no | all | When the item was written down. |
| `started` | `DATE` or `TIMESTAMP` (§3.3.1) | no | board, done | Required whenever `stage:working`; survives a later stage change. |
| `done` | `DATE` | **yes** in `done.md` | done | |
| `outcome` | `shipped` / `cancelled` / `obsolete` | **yes** in `done.md` | done | |
| `reason` | free text, no `|` | **yes** where the item's `stage` is listed in `needs_reason` (§5.1.5) | all | Renamed from version 1's `blocked` (§10). Valid on any stage; required only where `needs_reason` lists it — unlike version 1, NOT forbidden elsewhere. |
| `tickler` | `SCHEDULE` | no | a `SOURCE` stage named in `tickler_stages` (§5.1.4) only | Fires when the schedule's next instant arrives — a bare date is one-shot; a weekday or monthday spec recurs, making the item a prototype that spawns a new item on each fire. Placement, the `created` requirement, and where a fire lands: §5.1.4. |
| `tickler_dest` | `STAGE` | no | same stages as `tickler` | Overrides that item's default fire destination from `tickler_stages` (§5.1.4), for both a one-shot move and a recurring spawn. |
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
stage, prio, tags, refs, detail, created, started, reason, tickler, tickler_dest, tickled, done, outcome, <unregistered...>
```

## 7. Cross-file constraints

These hold across the directory as a whole. The reference checker verifies all
ten; the identifiers match the numbering in `structure.md`.

- **I1 — One home per ID.** Every ID appears in exactly one of `board.md` or
  `done.md`. An item is moved, never copied.
  Consequence: closing an item is a deletion from one file and an insertion into
  another, and cancelled work must be written to `done.md` rather than deleted,
  or its ID vanishes from the directory.
- **I2 — ID ceiling.** Every ID in the directory is numerically less than
  `board.md`'s `next_id`. `next_id` increases monotonically and is never
  decremented, so IDs are never reused. All IDs — including `next_id` — use
  the directory's declared grammar (§3.3.2), and the comparison is over the
  digit portion at the declared width.
- **I3 — Box matches file.** `board.md` contains only `- [ ]` item lines;
  `done.md` contains only `- [x]` item lines.
- **I4 — retired.** Version 1's "working coherence" (per-working-file
  frontmatter consistency) has no target left to constrain now that
  `working.NN.md` is retired (§5.2). Its live content — `started` required
  whenever `stage:working` — folds into I7. This identifier is not reused.
- **I5 — Reason required where declared.** Every item whose `stage` is a
  member of `needs_reason` (§5.1.5; default `blocked`) carries `reason`.
  Unlike version 1's `blocked`, `reason` is not forbidden on a stage outside
  `needs_reason` — it MAY be present there too, simply not required (§5.1.5).
- **I6 — Done items are dated and filed.** Every item in `done.md` carries
  `done` and `outcome`, and sits under a month heading matching its `done` date.
- **I7 — Well-formed values.** Dates are valid ISO 8601 calendar dates per
  §3.3.1 — the lexical form AND a real day of a real month; `prio`, `outcome`, and
  `tags` values are drawn from their vocabularies; no field value contains `|`;
  no key is repeated within an item line. `prio`, `tags`, `created`, and
  `started` are validated identically wherever they appear; `created`,
  `started`, and a file's own `updated` MAY additionally carry the optional
  time §3.3.1 sanctions for those three fields alone (`DATE` or `TIMESTAMP`).
  A `tickler` value is checked for shape only — it MUST match the `SCHEDULE`
  grammar of §3.3 and MUST NOT be evaluated: no clock enters the format, and
  a schedule can never make a directory invalid because a clock disagrees
  (§10.1). `tickled` and `done` stay `DATE`-only wherever they appear.
  `board.md` frontmatter carries a non-empty `project` (§5.1).
  Additionally: every item line in `board.md` carries a `stage:` value that is
  a member of the directory's declared `stages` (§5.1.1); `started` is
  required whenever `stage:working` (folded in from version 1's I4);
  `tickler` is valid only on an item whose stage is a `SOURCE` named in
  `tickler_stages`, and MUST also carry `created` (§5.1.4); `tickler_dest`,
  where present, MUST be a member of `stages`; every slug named in
  `wip.<slug>`, `tickler_stages`, `needs_reason`, or `stage_labels` MUST be a
  member of `stages` (§5.1.7).
- **I8 — Detail references resolve.** Every `detail` value equals
  `details/<ID>.md` for the ID that carries it, and that file exists.
- **I9 — Detail files are claimed exactly once.** Every file in `details/` not
  beginning with `_` is referenced by exactly one item, and its frontmatter `id`
  and `title` match that item's ID and title exactly.
  I8 and I9 name the live files and only those: `done-YYYY.md` is not read and
  `details-YYYY/` is not `details/`, so an archive is outside both (§5.6).
- **I10 — retired.** Version 1's working-file-set well-formedness (slot
  contiguity, uniform width, at least one file) has no target left to
  constrain now that `working.NN.md` is retired (§5.2). This identifier is
  not reused.

I4 and I10 are retired, not renumbered away: every other document in this
set that names an invariant by number (`data-invariant="I9"`, "an I9 orphan,"
and so on) stays correct across the version boundary without an edit.

## 8. Conformance

**A conforming reader** implements §3 and §4, recognizes every schema in §5,
tolerates unregistered fields and unknown frontmatter keys, never treats a
`- [` line inside a detail file's `## Plan` section as an item, and refuses
git conflict-marker lines in every data file rather than ignoring them
(§5.1.6).

**A conforming writer** additionally emits the required frontmatter keys for
each file, maintains `next_id`, preserves unregistered fields when moving an
item, and produces output that satisfies §7.

**A conforming checker** verifies the invariants of §7 (I1, I2, I3, I5, I6,
I7, I8, I9 — I4 and I10 are retired, §7) and reports each violation with a
`path:line: message` location. It exits non-zero when any violation is found.

Byte-for-byte round-tripping is NOT required. A writer MAY normalize field
order and whitespace. It MUST NOT drop fields, reorder items within a
stage's own run, or renumber IDs.

## 9. Extensibility and versioning

`version: 2` in `board.md`'s and `done.md`'s frontmatter is the format
version, not a content revision. Bump it only for an incompatible format
change — this document's own transition from version 1 is the first such
change, described in Appendix C.

Forward compatibility rules:

- Unregistered field keys on an item line are **valid**. Readers accept them;
  writers preserve them across moves. This is the intended extension point — a
  new field needs no spec change to start being used.
- Unknown frontmatter keys are **valid** and MUST be ignored, not rejected.
- Unknown `## ` headings are an **error** in `done.md` only. `board.md` does
  not use section headings at all (§5.1.6); any `## ` line there is ordinary
  ignorable content, not a candidate for a closed vocabulary.
- **A per-stage WIP cap is expressed only as `wip.<slug>` frontmatter
  (§5.1.3).** A future revision MUST NOT add a competing cap mechanism
  without deciding which wins; extensions MUST NOT introduce one.
- **Links are advisory, never load-bearing.** A `refs` value (§6) is checked
  for shape and nothing else: a per-directory validator cannot see other
  directories, so whether a link's target exists is never validated here, and
  a stale or ambiguous link MUST NOT invalidate a directory the way a broken
  `detail` reference does. Resolution is the business of a tool with a
  tree-wide view (the GUI, discovery, a future sweep), and that tool MUST
  refuse to resolve an ambiguous slug rather than guess (§5.1).
- Reserved for future use, MUST NOT be redefined by extensions: `id`,
  `status`, `next_id`, `doc`, `version`, `id_prefix`, `id_width`, `board`,
  `stage`, `stages`, `stage_labels`, `tickler_stages`, `needs_reason`, and
  the `wip.` key prefix. `status` is retired alongside `working.NN.md` (§5.2)
  but stays reserved rather than becoming available for reuse.
  A reader MAY preserve a legacy item-line field using one of these names so it
  can be removed; a writer MUST NOT introduce or replace one.

A reader encountering `version` greater than the version it implements SHOULD
report a version mismatch rather than parse the file speculatively. A reader
encountering `version` *less* than the version it implements — a version-1
directory read by a version-2-or-later implementation — MUST NOT silently
treat it as the current version; see Appendix C and `spec-tools.md` §5.3 for
what it owes that directory instead.

## 10. Known limitations

Documented deliberately; a second implementation is not expected to fix them
without a spec revision.

1. **Every calculation stays date-granular, even where a time is allowed.**
   `created`, `started` and `updated` MAY carry a time (§3.3.1), but `done`
   and `tickled` never do, and nothing in this format's own arithmetic reads
   the optional time even where it is present — cycle time, flight time and
   month grouping all still work in whole days. Two items finished on the
   same day have no recorded order beyond their position in the month group.
   This is deliberate — a clock in a hand-edited file is a field people get
   wrong, and month grouping is the only ordering `done.md` actually needs —
   but it does mean the format cannot answer "which did I finish first"
   within a day, and a time on `created`/`started` is provenance a reader MAY
   ignore, not an input to any rule in §7. The tickler keeps the same
   discipline one level further in: its schedule expression may name an
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
4. **Ordering is a convention, not a constraint.** Nothing verifies that a
   stage's items are in priority order, that month groups in `done.md`
   descend, or that items within a group descend.
5. **Archives are unvalidated, and archived IDs leave the pool.** Once a month
   group is moved out of `done.md` (§5.6), its items are invisible to the
   checker: I1 no longer notices an ID that exists both in the archive and in
   `board.md`, and I2 no longer counts one against `next_id`. Their detail
   files do not become I9 orphans, because §5.6 moves them to `details-YYYY/`
   and out of I8 and I9's reach — but that is the same invisibility, not an
   exemption from it: nothing checks that an archived line and its archived
   detail file still agree, or that the file is there at all. Archiving trades
   checking for size, and the trade stays safe only while `next_id` keeps
   rising (I2, §5.6 rule 5), the one rule that still spans both halves.
6. **A subtask cannot be validated.** Because `- [` lines inside a detail
   file's `## Plan` section are unstructured by design (§5.4), a malformed
   one is indistinguishable from prose.
7. **A per-stage WIP cap can be violated, not just mis-set.** Version 1's
   limit could only ever be mis-set, never exceeded — it was a file count, and
   starting a fourth item with three slots was structurally impossible
   (§5.1.3). Version 2's `wip.<slug>` cap is a **checked** invariant instead:
   a hand edit, a force-merge, or a bug can leave more items in a capped
   stage than its cap allows, and nothing about the file format itself
   prevents writing that directly. `--check` reports it; nothing stops the
   file from existing in that state first. This is the cost of a cap that can
   apply to any declared stage rather than only to one physically-limited
   file set (§5.1.3).
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
STAGE           ^[a-z][a-z0-9-]{0,15}$       (same pattern as SLUG; membership
                                             in the directory's declared
                                             `stages` is not expressible as a
                                             regex — see the DATE note below)
STAGES          ^STAGE(,STAGE)*$
STAGE_LABELS    ^STAGE:[^,]+(,STAGE:[^,]+)*$
TICKLER_STAGES  ^STAGE->STAGE(,STAGE->STAGE)*$
NEEDS_REASON    ^STAGE(,STAGE)*$
WIP key         ^wip\.STAGE$                 (a frontmatter KEY pattern, not
                                             a value)
LINKLIST        ^[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15}(,[a-z][a-z0-9-]{0,15}:[A-Z]{1,4}-[0-9]{1,15})*$
DETAILPATH      ^details/T-[0-9]{4}\.md$
archive file    ^done-[0-9]{4}\.md$          (informative; never validated)
archive detail  ^details-[0-9]{4}/T-[0-9]{4}\.md$
                                             (informative; never validated)
prio            ^(high|med|low)$
outcome         ^(shipped|cancelled|obsolete)$
frontmatter end ^---$
section heading ^##SP
```

Appendix C carries the version-1 `working file` filename pattern — a version-2
reader has no reason to recognize it except while migrating one.

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

A micro-manager directory is recognized by its name, and then by one probe of
its contents — the emptiness test below. Recognized names:

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

### The emptiness test

A name match alone is not enough to make a directory a board. A directory whose
name matches but that contains **none of `board.md`, `backlog.md`, or
`done.md`** is NOT a micro-manager directory, and discovery MUST skip it
silently: it is not listed, not counted, and not checked.

`backlog.md` stays part of the probe alongside `board.md` even though it is
version 1's filename: discovery has to find a version-1 directory too, or a
person can never be offered the migration that turns it into one (§9,
Appendix C, `spec-tools.md` §5.3). A directory found only by `backlog.md`
is a version-1 board; one found by `board.md` is version 2; a directory
should never have both (that is the sibling-collision case below, not a
version transition), and a checker that finds both SHOULD say so rather than
picking one silently.

The probe, and the rule stated as *none present* rather than *some missing*,
on purpose:

- **None present** — nothing here was ever a board. A source repository
  called `micro-manager`, a skill package, an empty directory someone made by
  hand: reporting these as broken todo directories is noise in exactly the
  place a person is scanning for real problems, and the noise is permanent
  because nobody will ever "fix" a source tree into a board.
- **`done.md` alone, or `board.md`/`backlog.md` alone** — a board that has
  lost a file. This is a REAL failure, and the most alarming kind: something
  deleted half a project. It MUST still be discovered and MUST still be
  reported. A rule that skipped it would make a half-deleted board disappear
  from the checker at the exact moment it needs attention.
- **Both `board.md` and `backlog.md` present** — not a lost file but a stuck
  migration or a hand-mistake; report it the same way a sibling-name collision
  is reported (below), since it is the same shape of ambiguity: two candidate
  identities for one directory.

**The skip applies to DISCOVERY, never to a directory the user named.** A path
given explicitly — `--dir`, `MM_DIR`, an argument to a checker — that fails the
test MUST be an error saying it is not a micro-manager directory. Silence there
would report success for a command that did nothing, which is worse than the
noise this rule removes.

`board.md`/`backlog.md` and `done.md` are the right probe because they are
the files a board cannot lack by design at either version: whichever of
`board.md` or `backlog.md` applies carries `project` and `next_id` (§5.1),
and `done.md` is where every closed item lives (§5.3), unchanged across the
version boundary. Working files were never part of the test even in version
1 — a directory with `working.01.md` and none of the others is a wreck worth
reporting, not an absence worth skipping.

Discovery implementations SHOULD additionally prune well-known directories that
never contain projects (`.git`, `.claude`, `node_modules`, `vendor`, `target`,
`dist`, `build`, `.venv`) and SHOULD let the user add to that list. That pruning
is a convenience and an optimisation; the emptiness test above is the rule.

### Two boards in one place

**One parent directory holds at most one micro-manager directory.** Two of the
recognized names side by side — `micro-manager` and `.micro-manager` is the
pair that actually happens — is a mistake, and a checker MUST report it.

It is a mistake because nothing joins them. Two boards in one place share no
`next_id`, so both start at `T-0001` and the same ID means two different items;
`--report` over the project covers one of them; and a person who adds an item
today has no way to know which board yesterday's went into. It is usually the
residue of a rename that copied instead of moving, or of two tools disagreeing
about which spelling to create.

**It is not an invariant.** I1–I10 are properties of one directory's files, and
a conforming reader MUST be able to validate a board it was handed without
reading the directory above it. A collision is a property of the PARENT, so it
is a **checker and discovery finding** — reported by the tools that already
look at the parent, and absent from the per-directory validation that mutations
run before they commit (`spec-tools.md` §8). Making it I11 would oblige every
reader to walk upward, which nothing else in this format requires.

What the tools MUST do:

- **A checker** reports it against each colliding directory and exits non-zero,
  the same as any other finding. Against each, rather than once against the
  parent, because a person who checks one board has to be told — and because a
  finding no result carries is one a machine caller cannot see.
- **Discovery** lists both, and SHOULD say they collide. Hiding one would pick a
  winner, which is the one thing the format never does with ambiguity.
- **Resolution** already refuses: two candidates at the resolving step is the
  ambiguity of `spec-tools.md` §4, and it fails with both paths listed rather
  than guessing.

The fix is the user's: keep one directory, move any items worth keeping into it
by hand — they carry their own IDs, and two boards' IDs overlap, so a merge is
a decision no tool can make.

---

## Appendix C: version 1 → version 2

This document describes version 2. For version 1's full text, see this
document's history at the commit immediately prior to this revision — this
appendix is a compact reference for migration authors and version-1 readers,
not a restatement.

**What changed, in one paragraph.** `backlog.md` (three fixed
`## Ready`/`## Blocked`/`## Someday` sections) and one-or-more
`working.NN.md` files (one in-progress item each, carried in frontmatter)
merge into a single `board.md`, where every item carries an explicit
`stage:` field (§5.1). `blocked:` is renamed `reason:` and its placement
restriction relaxes (§5.1.5). Five new `board.md` frontmatter keys —
`stages`, `stage_labels`, `wip.<slug>`, `tickler_stages`, `needs_reason` —
generalize what version 1 hardcoded to specific stage names (§5.1.1–§5.1.5).
`done.md`, `details/<ID>.md` (plus one addition, §5.4), `structure.md`, and
the archive files are unchanged.

**Concordance, for a migration reader:**

| Version 1 | Version 2 equivalent |
|---|---|
| `backlog.md`, `doc: backlog` | `board.md`, `doc: board` |
| `## Ready` section | `stage:ready` |
| `## Blocked` section, `blocked:` field | `stage:blocked`, `reason:` field |
| `## Someday` section | `stage:someday` |
| `working.NN.md`, `status: working` | a `stage:working` item line in `board.md` |
| `working.NN.md`, `status: idle` | (no equivalent — the file does not exist) |
| working file's `## Task`/`## Plan`/`## Notes`/`## Blockers` | `details/<ID>.md`, same headings (§5.4) |
| WIP limit = working file count | `wip.working` frontmatter key (§5.1.3); absent = uncapped |
| `tickler:` valid only in `## Someday` | valid in any `SOURCE` named in `tickler_stages` (§5.1.4) |
| a fire always lands in `## Ready` | lands per `tickler_stages`'s `DEST`, or an item's own `tickler_dest` |
| I4 (working coherence) | retired (§7); its live content folds into I7 |
| I10 (working file set well-formed) | retired (§7) |
| `working\.[0-9]+\.md` filename pattern | not part of version 2's grammar |

**Migration.** `spec-tools.md` §5.3 specifies the general, versioned
`--migrate` mechanism and this transition's specific step. A version-2
reader encountering a version-1 directory MUST refuse mutating operations
with a version-mismatch error naming both versions (§9) rather than
misparsing it; `--migrate`, `--check`, and read-only operations MAY still
work against a version-1 directory, at an implementation's discretion (§9,
`spec-tools.md` §5.3).
