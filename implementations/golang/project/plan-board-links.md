# Plan — cross-board links that survive a change in file hierarchy

    Status: proposed
    Date:   2026-08-02
    Scope:  spec-file-format.md §5.1/§6, spec-gui.md §3, both validators, the GUI
    Items:  T-0104 (this spike, plan only); proposes T-0125 (spec), T-0126
            (validators), T-0127 (GUI)

How one board's item can reference another board's item, in a form that
survives a change in file hierarchy. The spike's one stated requirement —
"Make sure the links would be able to survive a change in file hierarchy" —
turns out to be the whole design question, because every identity this system
already has except one is derived from the file hierarchy and breaks when it
changes.

Specs are normative. Where this document appears to contradict one, the spec
wins; T-0125 lands the changes this plan proposes.

---

## 1. The question

Two boards (two micro-manager directories — the Go implementation's board, the
Python one's). An item in one needs to say "this depends on T-0012 over in the
Python board", in a way a machine can read and the GUI can render clickable.
The link must still mean the same thing after:

- the item moves between homes as it progresses (I1: backlog → working →
  done — its line is cut from one file and pasted into another),
- the format's internal file set changes (a future revision moves files,
  `done.md` rolls into `done-YYYY.md` archives, §10 limitation 5),
- **the boards' directories are moved or renamed in the repo tree** — this is
  "a change in file hierarchy" and it is the requirement that filters the
  candidates below.

The format has no server and no registry; discovery is a directory walk
(find.sh). A link therefore has to be resolvable from the data alone, by a
reader that knows nothing beyond the board it is standing in and, at most, the
set of boards in the same tree.

## 2. What identity is in this system (measured)

An item's identity is **(board, ID)**. Inside one board the ID is stable for
the item's whole life: I1 moves the item between homes but never changes its
ID, and I2 forbids reuse. The Go implementation's `mm fix` renumbers only in a
collision repair (T-0123), and then a detail file is renamed with it — the one
case where a link would go stale (see decision 6).

The board half is the problem. A board has exactly three handles today, and
two of them are location-derived:

| Handle | Where it lives | Survives an item moving homes | Survives the format's file set changing | Survives the board's directory moving |
|---|---|---|---|---|
| `project` (free text) | `backlog.md` frontmatter | yes | yes | **yes** |
| `projectId` (`sha256(canonicalPath)[0:12]`) | derived, spec-gui.md §3.1 | yes | yes | **no — measured** |
| the directory path | the filesystem | yes | yes | **no** |

Measured on 2026-08-02: `ProjectID("/tmp/…/001/team-a/micro-manager")` =
`0b53479ae264`; after the directory moves to `team-b/todos/micro-manager` the
same call returns `8031b21be7bf`. The id is defined as a hash of the canonical
path, so it cannot survive a move by construction. The spec-gui.md §3.1
comment is explicit: the id is "derived, never parsed; nothing reconstructs a
path from it" — a front end resolves it by looking it up among the directories
it knows, and a moved board simply stops being found under its old id (the
same class of event §10 rule 5 already handles for favorites: rendered missing,
never silently dropped).

`project` survives a move because it is content, not location — but §10
limitation 8 leaves it unconstrained free text: it cannot be validated beyond
non-emptiness, and nothing prevents two directories claiming the same name. It
is the right *kind* of handle and the wrong *shape*.

So the requirement "survive a change in file hierarchy" selects, among the
existing handles, exactly one: a **content-declared identity**. Nothing else in
the system qualifies.

## 3. The candidates

### A. Prose — mention the other board in the title or detail body

"Depends on T-0012 in the Python board." Survives everything, including the
apocalypse. Not machine-readable: the GUI cannot render it as a link without
heuristics, no tool can check it, and nothing survives — *as a link* — at all.
The task asks for links that "would be able to survive a change in file
hierarchy", which presumes there IS a link. A is the null answer and the
fallback; it is what the format has today.

### B. Path + ID — `../python/micro-manager/backlog.md#T-0012`

Machine-readable, but fails the requirement on both axes it would be expected
to pass: the target item *moves files* as it progresses (the `#` anchor moves
from backlog.md to a working file to done.md), and the board's directory moves
break the path outright. The file hierarchy IS the link. Rejected by the
task's own sentence.

### C. projectId + ID — `9f2a7c1e4b60:T-0012`

Machine-readable, needs no spec change (an unregistered field, §9), survives
item moves and format revisions, and the GUI already owns the resolution
machinery (look up the id among known directories; render missing per §10 rule
5). But the measured fact in §2 stands: the id is path-derived, so the one
change the task names — the hierarchy — is exactly the one it does not survive.
It is a *route*, not an *identity*: stable against path *spelling* (relative
vs absolute, symlinks, NFC, trailing separators), unstable against path
*truth* (a move). Useful as a resolution hint, insufficient as the link.

### D. Content slug + ID — `py:T-0012`, slug declared in `backlog.md` frontmatter

The board declares its own short name — `board: py` — as content. A link is
the slug, a colon, and the target item's ID. The slug is the one handle in
§2's table that survives the hierarchy change, because it is not derived from
anything. It costs a spec change (reserve the key, define the shape), human
responsibility for uniqueness, and an honest answer about dangling links —
each addressed in §5.

## 4. What the format already gives us, for free

Three facts make D cheap rather than visionary:

1. **The extension point is built in.** §9: unregistered item-line fields are
   valid, and "a writer moving an item between files MUST preserve it
   verbatim". The Go implementation does exactly that: `Extra` fields survive
   every move — `RenderItemLine` re-emits them after the registered fields
   (mm/itemline.go), `op_start` carries them into the working-file frontmatter
   (mm/op_start.go:55), and `op_start_test.go:148` pins `owner:dana` surviving
   a start. A link field needs no writer work anywhere.
2. **Links are writable today.** `mm --edit ID --set KEY=VALUE` reaches any
   key "including ones this tool does not know" (usage, OpEdit) — verified in
   `op_update.go` (`setExtra`/`removeExtra`). No CLI change is needed for v1.
3. **The format already solved "several of the same thing" once.** `tags` is a
   comma-separated `TAGLIST`, no spaces, precisely because frontmatter and item
   lines are a flat map, not YAML (§10.3). A link list is the same shape:
   `refs:py:T-0012,go:T-0003`. The parser, the validator vocabulary check, and
   the writers' canonical-order machinery all have a precedent to copy.

## 5. Decisions

1. **Links never contain paths.** A link names a board and an item, never a
   file. This is the non-negotiable consequence of the task's requirement, and
   it rules out B outright.

2. **A board's link address is a content-declared slug: `board` in
   `backlog.md` frontmatter.** Shape: lowercase `[a-z][a-z0-9-]{0,15}` — one
   to sixteen characters, ASCII, starting with a letter. Lowercase keeps the
   slug namespace disjoint from the ID-prefix namespace by construction: every
   ID prefix is uppercase (spec-file-format.md §3.3.2 rule 2, matching is
   case-exact), so no slug can be mistaken for an ID and no ID for a slug.
   The key is optional — a board without `board:` is not a link target — and
   the value is human-chosen and human-stable: moving the directory, renaming
   it, or restructuring the repo never touches it. Changing a slug is a
   deliberate act and a findable one (the slug appears in exactly two kinds of
   place: its own frontmatter and `refs:` values; `grep -rn 'board:'` and
   `grep -rn 'refs:'` find every instance).

3. **A cross-board link is `refs`, a comma-separated LINKLIST of `SLUG:ID`
   elements.** `refs` joins the §6 field registry; its value is a LINKLIST
   (identical lexical rule to TAGLIST: comma-separated, no spaces); each
   element is a target board slug, a colon, and the target item's ID. The ID
   half uses the generic ID shape `[A-Z]{1,4}-[0-9]{1,15}` — the intersection
   of every declared grammar (§3.3.2: uppercase prefix of 1–4 letters, width
   1–15) — so a local reader can check the *shape* without knowing the target
   board's declared grammar. Canonical field order (§6.1): immediately after
   `tags`, both being lists. One ref per element; multiple targets are one
   comma-joined value, exactly as tags does it.

4. **Links are advisory, never load-bearing.** This is the format's no-server
   promise applied to references. check.sh validates the *shape* of `refs`
   values (the LINKLIST grammar and each element's `SLUG:ID` form) — nothing
   more, because a per-directory checker cannot see other boards, and a stale
   or ambiguous link must not invalidate a board the way a broken I8 detail
   reference does. Resolution — does the target exist — is the GUI's job, where
   a tree-wide view exists (discovery, favorites, recent), and a dead link
   renders with the §10 rule 5 missing treatment (`data-missing="true"`), the
   same policy as a favorite on an unmounted drive. The GUI is the only place
   that knows enough to resolve, and it must be the only place that pretends to.

5. **`board` uniqueness is human responsibility, with defined failure
   behaviour.** Two boards may declare the same slug — the format cannot
   enforce global uniqueness without a server, and even a tree-wide sweep
   cannot see boards in other repos. A resolver that finds two known boards
   with one slug MUST refuse to resolve rather than guess, naming both boards
   — the same discipline `mm fix` applies to a tie (T-0123): never invent an
   answer a human should give. check.sh does not check uniqueness; a future
   tree-wide link sweep (T-0127's possible follow-up, not in v1) may report it.

6. **`mm fix` stays directory-local, and the consequence is owned.** A
   collision repair in the *target* board renumbers an item; a `refs:` value
   pointing at it dangles. The repair cannot sweep other boards (it is
   directory-local by design, T-0123 decision 4), and it must not pretend
   otherwise. The dangling link renders missing in the GUI and the human fixes
   the one value. This is the same tolerance as decision 4: links are
   advisory, and the cost of a stale one is a rendered "missing target", not a
   broken board. The inverse case — a renumber *in this* board — is safe: a
   renumbered item's own `refs:` fields move with it verbatim (§9, fact 1 in
   §4), because the link value is data on the item line.

7. **`projectId` stays the GUI's route identity; the slug is the data
   identity.** The two coexist and do not compete. Routes keep addressing
   directories by projectId; links name boards by slug. The GUI resolves a
   slug by scanning the directories it knows for a matching declared `board`
   (the directory summary gains the field), and then addresses the target by
   its own projectId. Nothing reconstructs a path from a slug — the slug is
   resolved through discovery, exactly as projectId is resolved today.

## 6. What lands as a result

- **T-0125 (spec, proposed)**: spec-file-format.md gains `board` in §5.1's
  `backlog.md` frontmatter schema (slug shape, optional, human-stable) and
  `refs` in §6's field registry (LINKLIST of `SLUG:ID`, canonical position
  after `tags`, generic-ID shape check). §9's extensibility section gains the
  advisory-links sentence: link resolution is never validation, and a stale or
  ambiguous link never invalidates a directory. §10's known-limitations list
  gains the two owned gaps: slug uniqueness is not enforced, and `mm fix` in
  the target board can dangle a ref.
- **T-0126 (validators, proposed)**: check.sh and the Go validator accept a
  declared `board` (shape-validated) and validate the shape of `refs` values
  — LINKLIST grammar, each element `SLUG:ID` with the generic ID form — as a
  format-level rule; a broken-refs fixture (bad shape, bad slug) joins the
  corpus and the oracle test asserts lockstep. Neither validates resolution.
- **T-0127 (GUI, proposed)**: the directory summary and item rendering gain
  `board`/`refs`; a `refs:` value renders as a clickable link resolved against
  the known-directories set; a slug that resolves to nothing renders with the
  §10 rule 5 missing treatment; an ambiguous slug renders as ambiguous with
  both boards named. No CLI change at all — `--edit --set refs:…` and
  `--unset refs` already work (fact 2 in §4).
- **SKILL.md** gains a "linking boards" note with the post-move ritual: move
  the directory, `grep -rn 'refs:'` to find every link to it, update the one
  slug if it changed. (Folded into T-0125; not a separate item.)
- **This plan flips to decided** when T-0125–T-0127 land, exactly as
  plan-git-support.md flipped when T-0121–T-0123 shipped.

## 7. Why not the alternatives

- **Prose only (A)**: survives everything because it means nothing
  structurally. The task's phrasing presumes a link that survives; prose is
  the status quo that the spike exists to question, and it gives the GUI
  nothing to render and tools nothing to check.
- **Paths (B)**: the file hierarchy IS the link, which is the one thing the
  requirement says it must survive. The target item moves files as it
  progresses, so even a stable directory breaks it within a week of writing.
- **projectId (C)**: measured — the id is `sha256(canonicalPath)`, and a moved
  board gets a new id (0b53479ae264 → 8031b21be7bf). It survives every change
  the task does not name and fails the one it does. It remains useful as the
  GUI's route handle (decision 7), which is the honest role for a
  location-derived identifier.
- **A registered uniqueness invariant (two boards may not share a `board:`)**
  : needs a tree-wide or global view the format does not have; even a
  tree-wide check cannot see other repos, and a server contradicts "micro".
  The format's answer to "cannot enforce" is "define the failure behaviour"
  (decision 5), not "pretend it cannot happen".
- **A UUID or hash as the slug**: human-hostile in a hand-edited file,
  typo-prone in a field value, and no more unique than a short slug in
  practice — both rely on human care, and a slug a human can read and type is
  worth more than one they cannot. The slug is a name, not a nonce.

## 8. Definition of done

- A link is written `refs:SLUG:ID`, the slug is declared in the target board's
  `backlog.md` frontmatter, and neither contains a path — a link survives the
  target item moving homes (I1), a format revision that moves files, and the
  target board's directory being moved or renamed (the slug is content).
- Both validators accept a well-formed `board` and `refs` and reject a
  malformed one, in lockstep over a fixture; neither validates resolution, and
  a stale link never invalidates a directory.
- The GUI renders a `refs:` value clickable when the slug resolves, missing
  (data-missing) when it does not, and ambiguous with both boards named when
  two known boards share the slug.
- The measured fact that anchors the design — projectId changes on move, the
  slug does not — is reproducible from this document.
