# Working with `mm` (this project)

How to drive the task board for the Howmet documentation effort. `mm` is a
plain-Markdown todo system; this file is the **how-to**, and [`structure.md`](structure.md)
is the **format spec** (files, the item line, the ten invariants). Read this to
work day-to-day; read `structure.md` when you need to know exactly what a field
means or edit the raw Markdown by hand.

- **Board location:** `/Users/davidhoppe/Hopasaurus/Rock/Howmet/Howmet-code/micro-manager`
- **The plan this board tracks:** `../Project-notes/final-docs/project/re-org.md`
- **Binary:** `mm` (on `PATH`); `mm --version` → `mm 0.2.0`.

`mm` runs from anywhere as long as it can find this directory (it searches upward
from the current dir). To be explicit, add `--dir` or set `MM_DIR`:

```bash
export MM_DIR=/Users/davidhoppe/Hopasaurus/Rock/Howmet/Howmet-code/micro-manager
# or per-command:
mm --status --dir /Users/davidhoppe/Hopasaurus/Rock/Howmet/Howmet-code/micro-manager
```

---

## The mental model

- Every task is one line in **`board.md`** (open) or **`done.md`** (closed) — never
  both. Long-form content lives in **`details/T-NNNN.md`**, which never moves.
- A task has an **ID** (`T-0007`), permanent and never reused. (Removing a task
  does *not* reclaim its number — `next_id` only ever climbs.)
- A task sits in a **stage**, set by its `stage:` field, not its position:
  `someday` → `ready` → `working` → `done` (with `blocked` off to the side).
- Fields are ` | `-separated `key:value` pairs. **Unknown keys are legal and
  preserved** — that's how the project-specific `parent:` field below works.

## Everyday commands

```bash
mm --status                 # one screen: stage counts, next, oldest
mm --next                   # top of Ready (what to pick up next)
mm --list                   # everything open, in stage order
mm --show T-0006            # print one item + its detail file
mm --search "translator"    # substring/regex over titles, tags, details
mm --check                  # validate the board against the ten invariants
mm --stats                  # throughput, cycle time, work in flight, tags
```

Moving work through the stages:

```bash
mm --start  T-0001          # ready  -> working  (sets started:)
mm --pause  T-0001          # working -> ready   (keeps started:)
mm --finish T-0001 --outcome shipped   # -> done.md (outcome: shipped|cancelled|obsolete)
mm --block  T-0003 --reason "waiting on hardware access"
mm --unblock T-0003
```

Editing and annotating:

```bash
mm --edit T-0005 --prio high --tag docs        # change fields; --tag accumulates
mm --edit T-0005 --title "New title"           # retitle (also updates the detail file)
mm --note T-0006 "Found: PlotFileMgr owns the on-card ring buffer."
```

`mm --note` appends a **dated** entry under a `## Notes` section in the task's
detail file (it does not touch the other sections). See the notes-vs-implementation
convention below.

Adding tasks:

```bash
# one task, with a detail stub created and linked automatically:
mm --add "Wire VERSION into build.sh" --tag high-level --stage ready --prio high \
        --detail --no-edit

# many at once (titles only, one per line) — set fields afterward with --edit:
mm --add-many <<'EOF'
Split registers: pressure-stages.md
Split registers: flow-stages.md
EOF
```

`--detail --no-edit` creates `details/T-NNNN.md` from the template and sets the
`detail:` field without opening an editor (important in a non-interactive shell).

---

## This project's conventions

### High-level tasks and splitting

The board starts with **high-level tasks** (tagged `high-level`), one per major
workstream in the reorg plan. Each is deliberately coarse and is meant to be
**split into detailed subtasks when you start it**.

Each high-level detail file has two project-specific placeholders:

- `## Implementation notes` — the curated write-up of decisions, findings, and
  gotchas discovered while working the task.
- `## Tasks split from this` — the list of child task IDs spun off from it.

**A high-level task stays open until every task split from it is closed.** Treat
that list as its definition-of-done checklist.

### The splitting workflow

1. `mm --start T-0006` — move the high-level task to `working`.
2. Create the detailed children, linking each back to the parent with a **`parent:`
   field** (an unknown key `mm` preserves, so `mm --search T-0006` finds them all):

   ```bash
   id=$(mm --add "screens/recipe/params.md field map" --tag sub --stage ready \
              --detail --no-edit --json | python3 -c 'import sys,json;print(json.load(sys.stdin)["result"]["id"])')
   # add the parent link (mm has no flag for arbitrary fields; append it to the line):
   #   … | tags:sub | parent:T-0006 | detail:details/<id>.md | created:…
   ```

   You can add `parent:T-0006` by editing the child's line in `board.md` directly
   (plain Markdown — allowed), or just rely on the parent's list as the record.
3. Record each child ID under `## Tasks split from this` in the parent's detail
   file (this is the canonical record).
4. Work the children. When all are in `done.md`, finish the parent:
   `mm --finish T-0006 --outcome shipped`.
5. If a high-level task is actively blocked waiting on its children but you're not
   touching it, `mm --pause` it back to `ready` (or leave it `working` if you're
   interleaving) — your call; the rule that matters is it doesn't get finished early.

### Notes vs. implementation notes

- Use **`mm --note`** for quick, timestamped findings as you go — they land in the
  detail file's `## Notes` section, newest work appended.
- Periodically **promote** the durable ones into `## Implementation notes` (edit the
  file directly) so the curated section stays readable. `## Notes` is the running
  log; `## Implementation notes` is the summary a future reader wants.

### Linking to the plan

Every high-level detail file references
`../Project-notes/final-docs/project/re-org.md` and the specific sections/decisions
it implements. Keep that link current when you split — children should cite the
same plan section (and any prior-note source) they draw from.

### Content caveats to carry into tasks

These live in the plan; repeated here because they bite task work directly:

- Document the canonical **`WindowParams.cpp`** — **not** `WindowParams-refactored.cpp`
  (an experiment, not shipped).
- Facts lifted from the earlier `Project-notes/*.md` are **unverified** until
  re-checked against `DCS2b-only/` — their line numbers are approximate.
- Screen visuals come from the `screen-translator/` simulation (hardware
  screenshots are impractical); its code changes happen in that repo, not here.

---

## Hygiene

- Run **`mm --check`** after any batch of hand edits — it enforces the ten
  invariants (IDs unique and below `next_id`, open/closed in the right file, every
  `detail:` path exists and matches, etc.).
- Never reuse or renumber an ID; never copy a task between files — move it.
- Titles must not contain `|`; tags have no spaces; dates are real calendar dates.
- `mm --json` / `--porcelain` give machine-readable output; `--dry-run` computes a
  change and writes nothing (good for checking a command before running it).

## Quick reference

| Want to… | Command |
|---|---|
| See where things stand | `mm --status` |
| Pick the next task | `mm --next` |
| Start / pause / finish | `mm --start ID` · `mm --pause ID` · `mm --finish ID --outcome shipped` |
| Log a finding | `mm --note ID "…"` |
| Change fields | `mm --edit ID --prio high --tag docs` |
| Block with a reason | `mm --block ID --reason "…"` |
| Add a task (+ detail stub) | `mm --add "…" --tag high-level --stage ready --detail --no-edit` |
| Find related items | `mm --search "T-0006"` (finds children via `parent:`) |
| Validate the board | `mm --check` |

---

## Findings & feature ideas for `mm`

Rough edges hit while using `mm 0.2.0` on this project, and features that would
help. Workarounds are in place today; these are notes for improving the tool.

### 1. Set an arbitrary field

Unknown keys like `parent:T-0006` are legal and preserved, but there's no command
to set one — you have to hand-edit the line in `board.md`. Something like
`mm --edit T-0007 --set parent=T-0006` (or `--field parent=T-0006`) would let the
tool manage custom fields it doesn't know about, making the `parent:` linking
convention above a one-command operation instead of a manual edit.

### 2. `--edit --stage` silently ignores the change (and there's no `--review`)

`mm --edit ID --stage review` returns `ok:true` but **does not change the stage** —
no error, no warning. Stage is only moved by `--start` / `--pause` / `--finish`,
and there is no operation to move an item into a custom stage such as `review`
(which the board declares in its `stages:` list). Two fixes worth making:

- Make `--edit --stage <declared-stage>` actually move the item (or fail loudly if
  a transition isn't allowed) rather than succeeding silently.
- Add a general way to move into any declared stage — e.g. `mm --move-to ID review`
  — so stages like `review` are reachable without hand-editing.

**Workaround today:** set `stage:review` (or any declared stage) directly on the
item's line in `board.md`; it's just a field, and `mm --check` still passes.

### 3. Detail-view: watch for updates, and merge concurrent edits (multi-phase)

In the `mm` UI, the **item detail view should watch for changes** while it's open —
**both the detail file (`details/T-NNNN.md`) and `board.md`**, since an item's
fields (stage, tags, prio, `parent:`, etc.) live on the board line, not in the
detail file. When either is updated (e.g. by a `mm --note`, a `mm --edit`/`--start`,
another session, or an external editor), it should **notify the user that the
content changed and offer to reload**. It should also detect when the view has
**unsaved local edits** and, rather than silently overwriting or discarding,
**offer a merge with conflict resolution**. This is likely a **multi-phase change**
for `mm`:

- Phase 1 — detect an external change to the open detail file and prompt to reload
  (safe when there are no local edits).
- Phase 2 — detect local unsaved edits vs. an external change and warn before
  either side is lost.
- Phase 3 — an actual three-way merge / conflict-resolution flow so both sets of
  changes can be reconciled.
