# micro-manager pi plugin specification

A pi extension that brings a micro-manager board into pi as structured tools,
commands, and per-turn context — without the agent ever editing the markdown
by hand.

Companion to `spec-tools.md` (the `mm` CLI this plugin drives) and
`spec-file-format.md` (the data the CLI maintains). This spec describes the
plugin's contract with both pi and `mm`; it says nothing about the data
format, which is the other specs' business.

## 1. Scope

A **pi plugin** here means a pi extension (TypeScript module) loaded from a pi
extension location, per pi's own extension documentation. Its job is to make a
micro-manager directory — the `backlog.md` / `working.NN.md` / `done.md` /
`details/` board of `spec-file-format.md` — a first-class thing an agent can
work with:

- **Tools** the model calls to read and mutate the board, with typed
  parameters and structured results.
- **A `/mm` command** a human uses in the TUI, mirroring the same operations.
- **Per-turn context**: the board's state is injected into the system prompt
  so the agent knows what is in work and what is next without a tool call.
- **Session state**: which board the session is pinned to survives `session_start`
  and follows branches.

The plugin is an *agent-side* companion. It is not a replacement for the `mm`
CLI, not a GUI, not a server, and it never touches the board's files itself.

## 2. Architecture

### 2.1 The plugin drives `mm`; it never reads or writes the files

The ten invariants, the transaction envelope, atomic writes, and `next_id`
accounting live in the library and are reached through the `mm` CLI
(`spec-tools.md`). The plugin MUST invoke `mm` for every board interaction —
reading or writing the markdown directly would re-implement the validator,
which is precisely the corruption path the format's design exists to prevent.
A plugin that edits `backlog.md` by hand is not conforming.

Consequences:

- **Requires a conforming `mm` on PATH** (`spec-tools.md` §11). The plugin MUST
  verify `mm` exists at session start (`mm --version`, or first use) and MUST
  degrade loudly but gracefully when it does not: tools return a "no `mm` on
  PATH" error, context injection is skipped, and the plugin says what to
  install rather than pretending.
- **The plugin parses `--json`** (`spec-tools.md` §9.2). Every invocation uses
  the JSON envelope; the plugin reads `ok`, `errors[].code`, `directory`,
  `result`, and `changes`, and maps them onto tool results.
- **The CLI is the contract, not the plugin's guess.** Where this spec says
  "the plugin calls `mm --op`", the exact switches and their meanings are
  `spec-tools.md`'s; this spec pins only how the plugin uses them.

### 2.2 The plugin is a guest in the agent

It holds no locks, runs no background timers or watchers (`session_start`
aside — see §5), and must never mutate the board except in direct response to
a tool call or a `/mm` command. No event handler, context injection, or
`agent_settled` hook ever writes. The board changes only when the agent or the
user asks for a change. This is the same discipline `spec-tools.md` §5.3.1
applies to archiving: nothing happens on its own initiative.

### 2.3 Relationship to the portable skill

`skills/micro-manager-cli/SKILL.md` teaches an agent to drive `mm` through
`bash`. This plugin provides the same operations as structured tools, which is
strictly better for the agent: parameters are validated by schema, results are
typed and compact, errors carry `mm`'s exit-code taxonomy, and the board path
is pinned once instead of re-resolved per command. When both are present, the
plugin's tools MUST be preferred — the agent calls `mm_start`, it does not
shell out to `mm --start` — and the plugin's tool guidelines say so. The skill
remains the fallback for environments that cannot run extensions.

## 3. Deployment and discovery

### 3.1 Where the plugin lives

Installed into a pi extension location:

```
~/.pi/agent/extensions/micro-manager/       # global
.pi/extensions/micro-manager/               # project-local (after trust)
```

The directory form (`index.ts` entry point, helper modules) is REQUIRED; the
plugin is too large for a single file. npm dependencies are permitted but MUST
be limited to `typebox` and the pi packages; the plugin has no other runtime
dependencies.

**Source-tree naming caution.** This repository's own `find.sh`/`check.sh`
recognize a *todo directory* by name alone, including the name `micro-manager`
(see `project/SKILL.md`). The plugin's source in this repository MUST
therefore live under a different directory name — it is `plugins/pi-mm/`, and
only the *installed* copy in pi's extension directory is named
`micro-manager`. The destination name is the plugin's identity for `/reload`
and for humans; the source name is an implementation detail.

(`implementations/typescript/` was the obvious home when this was written and
is no longer: that workspace is the TypeScript *implementation* of the format —
library, CLI, service, web client, with its own build and its own rules — and
the plugin is an agent-side companion that drives the Go `mm`. Putting it there
would enrol it in a build it has nothing to do with.)

### 3.2 Board resolution

The plugin MUST resolve the board through `mm`, never by its own directory
scanning — `mm` implements the "refuse rather than guess" rule
(`spec-tools.md` §4). Resolution order:

1. **A pin**: the plugin's configured `board` path (§9), or the path set by
   `/mm board PATH` / `mm_board` in this session.
2. **`MM_DIR`**: `mm` honors it; the plugin passes it through untouched.
3. **`mm`'s own discovery**: a bare `mm --status` uses the nearest directory
   above the working directory, then a downward search.

The resolved path is read from the first invocation's envelope
(`directory.path`) and **pinned in session state** (§7). Every later call
passes `--dir <pinned>` so mid-session re-resolution cannot flip boards when
two are nearby.

When `mm` refuses to choose (ambiguous, exit 3), the plugin MUST surface the
candidates (`mm_find`) and report that a pin is required — a tool error for
the agent, a `ctx.ui.notify` plus a picker for the `/mm` command. The plugin
MUST NOT guess among them.

#### 3.2.1 Listing and showing boards

The plugin MUST provide a way to list the boards it can see and to show the
one it is operating on — a human or an agent facing an unknown workspace needs
to know what is out there before it can choose:

- **`mm_find` lists located boards.** It runs `mm --find` and returns every
  discovered board with its path and `project` name, marking the board
  currently in use (the §3.2 resolution) so the list answers "which one am I
  on?". An empty list is a valid result — "no boards located" — never an
  error. The ambiguity case above uses the same listing to name the
  candidates.
- **The board in use is always knowable.** `mm_board` with no path returns
  the pinned board: path, `project`, and how it resolved (pin / `MM_DIR` /
  discovery). `mm_status` names the directory it read. In the TUI the plugin
  SHOULD keep a persistent status line (`ctx.ui.setStatus`) showing the board
  in use — set at session start and whenever the pin changes, cleared when no
  board is resolved — so a human sees at a glance what the agent is operating
  on. Context injection (§6) always names the board too.

Both capabilities are tool surface (§4.2) and command surface (§5); the model
and the human see the same facts.

### 3.3 Initializing a board

The plugin MUST be able to create a board (`mm_init` / `/mm init`), because a
plugin that can only operate on boards that already exist cannot bootstrap a
new workspace. `mm_init` maps to `mm --init` (`spec-tools.md` §5.1.1) and
MUST expose at least `project`, `dir`, `slots`, `slot_width`, `prefix`, and
`description`:

- `project` — required; the board's `project` name.
- `dir` — where the board is created; absent means `mm`'s default.
- `slots` / `slot_width` — the working-file count and digit width.
- `prefix` — the board's ID prefix, set at creation. It MUST match the
  format's `id_prefix` grammar (one to four uppercase ASCII letters,
  `spec-file-format.md` §3.3.2) and is passed as `--prefix P`. A prefix makes
  the board's IDs its own (`G-0001` on a gardening board, never confused with
  the default `T`); it is the one decision that cannot be re-made cleanly
  later, so it belongs at `--init`.
- `description` — the board's first-paragraph description (§3.4), passed as
  `--description TEXT` and written into the fresh `structure.md`; a board can
  be born with its purpose stated.

`--prefix` requires an `mm` build whose `--init` accepts it (`spec-tools.md`
§5.1.1 does not name it yet — the plugin MUST gate on the installed build and
degrade to the §4.3 exit-2 messaging when the switch is unknown, rather than
inventing its own way to set the prefix). A freshly created board is pinned
for the session (§3.2).

### 3.4 Board description and status summary

**The board description** is the first paragraph of the directory's
`structure.md` — the format's human-documentation file (spec-file-format.md
§5.5: free prose, not validated, not parsed, optional). "First paragraph"
means the first run of consecutive non-blank lines after any leading title
heading. An absent `structure.md` or an empty paragraph is "no description" —
a valid state, never an error.

- **Reading** rides through `mm`: `mm --status` carries the description in
  its JSON envelope, and `mm_status` / `mm_board` format it (truncated with
  `…` beyond a display cap). The plugin reads `structure.md` only through
  `mm`, like every other board file (§2.1).
- **Writing** is `mm_describe TEXT` / `/mm describe TEXT`, mapping to a new
  `mm --describe` operation (spec-tools.md §5.3): it writes or replaces the
  first paragraph of `structure.md`. Like `--prefix` (§3.3), the plugin MUST
  gate on the installed build's support and degrade to the §4.3 exit-2
  messaging when the operation is absent — it MUST NOT write `structure.md`
  itself.
- `mm --init` MAY accept a `--description` in the same build that adds
  `--describe`, so a board is born with one.

**The board status summary** is what `mm --status` already is — WIP `n/N`,
per-section counts, the in-work item(s), the top of Ready. `mm_board` with no
path shows it together with the identity and description, so one call answers
"show me this board": what it is, what it says about itself, and what is
happening on it right now. `mm_status` remains the deeper one-screen; context
injection (§6) carries a one-line summary every turn.

## 4. Invocation model

### 4.1 One tool, one `mm` operation

Every tool maps to exactly one CLI operation and one `mm` subprocess. A tool
MUST NOT chain operations ("start and finish" is two tool calls), because each
mutation is an independent transaction and a chain hides which step failed.

Common rules:

- **Parameters** are declared with `typebox` schemas and map onto the CLI
  switches 1:1. No parameter the CLI does not accept.
- **Every invocation passes `--json`** and, once the board is pinned, `--dir`.
- **Timeout**: every subprocess is bounded (default 30 s, §9). A timed-out
  `mm` is reported as an error, never awaited forever.
- **Content vs details**: the tool result's `content` is the *formatted*
  outcome the model reads (compact, terminal-friendly, no raw JSON);
  `details` carries the parsed envelope — `directory`, `result`, `changes`,
  `warnings` — for state reconstruction (§7) and for the TUI renderer.
- **Exit codes** map per §4.3.
- A **no-board** state (resolution failed, or `mm` absent) makes every board
  tool return an error naming the cause and the remedy, never a fake empty
  board.

### 4.2 Tool index

Required (MUST provide all):

| Tool | `mm` op | Parameters (→ switches) | Result |
|---|---|---|---|
| `mm_init` | `--init` | `project` (required), `dir`, `slots`, `slot_width`, `prefix`, `description` | A new board at `directory.path`; pinned for the session. `prefix` sets the ID grammar (§3.3) — gate on the build's `--prefix` support. `description` seeds `structure.md` (§3.4). |
| `mm_find` | `--find` | — | Every located board: path, `project`, in-use marker. "No boards" is a result, not an error. |
| `mm_status` | `--status` | — | One screen: `directory.path`/`project`, WIP `n/N`, per-section counts, oldest untouched Ready item, board description when set (§3.4). |
| `mm_next` | `--next` | — | The top of `## Ready`. Non-zero exit when empty → an explicit "nothing ready" result, not an error. |
| `mm_list` | `--list` | `section` (ready/blocked/someday), `state` (backlog/working/done/all), `prio`, `tag`, `limit` (default 50, capped at 200) | Filtered items in on-disk order — MUST NOT re-sort; `## Ready` order is the user's prioritisation (`spec-tools.md` §5.1.3). |
| `mm_show` | `--show` | `id`, `detail` (bool) | Every field, current state (file, slot if working), optional detail body. |
| `mm_board` | `--status` | `path` (optional) | Resolve or re-pin the board; no path returns the pinned board — identity, description, and the live status summary in one view (§3.2.1, §3.4). |
| `mm_describe` | `--describe` | `text` (required) | Write/replace the board description's first paragraph in `structure.md` (§3.4). Gate on the build like `--prefix`. |
| `mm_add` | `--add` | `title`, `section`, `prio`, `tags[]`, `top` (bool), `detail_text` | The new item **including its ID** — the handle for everything after. |
| `mm_edit` | `--edit` | `id`, `title`, `prio`, `tags[]` (add), `untags[]`, `set` (key=value pairs) | The updated item. MUST reach unregistered keys via `--set` (`spec-tools.md` §5.1.5). |
| `mm_move` | `--move` | `id`, `section`, `position`, `top`/`end` (bool) | The item at its new position. |
| `mm_start` | `--start` | `id` | The item now in a slot, or a WIP-limit error carrying the current slots (§4.3). |
| `mm_pause` | `--pause` | `id` | The item back in backlog. |
| `mm_finish` | `--finish` | `id`, `outcome` (shipped/cancelled/obsolete), `closing_note` | The item in `done.md`; the closing note lands in its detail file. |
| `mm_note` | `--note` | `id`, `text` | The appended dated entry. |
| `mm_remove` | `--remove` | `id`, `confirmed` (bool, REQUIRED) | Deletion. Guarded per §8.2. |
| `mm_check` | `--check` | — | Validation result: violations with `path:line`, or clean. |

Recommended (SHOULD provide when the CLI does):

| Tool | `mm` op | Notes |
|---|---|---|
| `mm_add_many` | `--add-many` | Add many items in ONE transaction, one per line (`spec-tools.md` §5.2.1). The item lines are written to `mm`'s **stdin** — the plugin does not create a file for them, here or anywhere (§8.1). A rejected batch adds nothing, so the error is reported as-is and NOT retried line by line: a partial batch is exactly what the operation exists to prevent. |
| `mm_block` / `mm_unblock` | `--block` / `--unblock` | Sugar over move+`blocked:`. |
| `mm_search` | `--search` | Titles, tags, detail bodies; reports state per hit. |
| `mm_report` | `--report` | Weekly summary. |
| `mm_tick` | `--tick` | The tickler service (`spec-tools.md` §5.3) — expose only when the installed `mm` has it, and NEVER auto-run it; a bare `mm_tick`/`/mm tick` run is the only trigger. |
| `mm_archive` | `--archive` | MUST surface the I1/I2 coverage warning verbatim; never run on a schedule from the plugin. |

### 4.3 Exit codes and error mapping

| `mm` exit | Plugin behavior |
|---|---|
| 0 | Success. |
| 1 | Invariant violation: the board is broken or the mutation would break it. Report `errors[]` verbatim (path:line); suggest `mm_check`. |
| 2 | Usage: the installed `mm` does not implement this operation (a minimally conforming build). Report "operation not available in this `mm`", not a plugin bug. |
| 3 | Not found / ambiguous: unknown ID, or resolution refused. For ambiguity, list candidates and require a pin (§3.2). |
| 4 | Precondition: WIP limit reached, wrong state, missing guard. The plugin MUST surface the remedy the CLI names — for `mm_start` at the limit, the current slots — so the agent can act (finish, pause, or `--wip`). |
| 5 | Concurrent modification: the board changed under the plugin. Return an error telling the agent the read was stale and to re-read before retrying. |
| 6 | I/O or environment failure. |

Errors become `isError: true` results with the code and message in both
`content` and `details`. A mutation that fails MUST be reported as failed even
when some part of the envelope is non-empty — a partial success is the one
state a transaction may not produce (`spec-tools.md` §7).

## 5. Commands

One command, `mm`, with the operation as its first argument — a human-facing
mirror of the tools, sharing the same argv builders and formatters:

```
/mm status | next | check | find
/mm init --project NAME [--dir PATH] [--slots N] [--slot-width W] [--prefix P] [--description TEXT]
/mm board [PATH]        # no PATH: identity + description + status summary
/mm describe TEXT
/mm context [on|off]
/mm list [--section S] [--state S] [--prio P] [--tag T] [--limit N]
/mm show ID [--detail]
/mm add TITLE [--section S] [--prio P] [--tag T]... [--top] [--detail-text TEXT]
/mm add-many [--section S] [--prio P] [--tag T]... [--top]   # then one item per line
/mm edit ID [--title T] [--prio P] [--tag T]... [--set K=V]...
/mm move ID [--section S] [--position N | --top | --end]
/mm start ID | /mm pause ID
/mm finish ID [--outcome shipped|cancelled|obsolete] [--closing-note TEXT]
/mm note ID TEXT
/mm remove ID          # confirms interactively, then passes --force
/mm tick | /mm archive [--before YYYY-MM]
/mm help
```

Rules:

- The command MUST print the same formatted results the tools return, so a
  human and an agent see the same facts.
- For interactive confirmations the command uses `ctx.ui.confirm` when
  `ctx.hasUI`; in headless modes a guarded operation fails rather than
  guessing (§8.2).
- The command MUST resolve the board exactly like the tools (§3.2) and MUST
  NOT run when no board is found — it prints the resolution failure and stops.
- `/mm find` lists located boards and marks the one in use — the command form
  of `mm_find` (§3.2.1).
- `/mm board` with no PATH prints the pinned board's identity, description,
  and status summary — the command form of `mm_board` (§3.4).
- `/mm describe TEXT` sets the board description through `mm --describe`
  (§3.4); it fails rather than touching `structure.md` when the operation is
  absent from the installed build.
- `/mm init` creates a board and pins it for the session; `--prefix` is
  validated against the `id_prefix` grammar before invocation (§3.3).
- `/mm context off` disables context injection for the session (§6); the
  setting is session state, not configuration.

## 6. Context injection

When a board is resolved and injection is enabled, `before_agent_start`
appends a compact block to the system prompt. Purpose: the agent knows the
board's state at the start of every turn without spending a tool call — and
knows *which* board it is pinned to.

```
[mm] /srv/boards/todos — Sample One — wip 1/4 · ready 3 · blocked 1 · someday 5
     in work:  T-0018  Migrate the build cache
     next:     T-0169  Spec: tickler fields and grammar (high)
```

Rules:

- MUST be data only: project, WIP, per-section counts, the in-work item(s),
  and the top of Ready. No commentary, no "please remember" prose.
- MUST be ≤ 8 lines (configurable, §9). A board that would overflow is
  truncated with an explicit `…`; the full picture is one `mm_status` away.
- MUST NOT include done items or archive contents.
- MUST refresh after any plugin mutation (the injected block must never
  describe a board the plugin just changed).
- MAY cache between turns otherwise — the block is a snapshot, and the tool
  calls the agent makes afterwards are authoritative.
- MUST NOT inject anything when no board is resolved or when `mm` is absent —
  an empty injection is better than a false one.
- MAY include the board description (one line) after the path when set
  (§3.4) — still data, still counted against the ≤ 8 lines.
- MUST NOT inject a board the project has not been trusted to read, when the
  plugin runs project-local (§9).

The tool guidelines (`promptGuidelines`) carry the behavior this spec expects:
"use mm_start when you begin work on an item and mm_finish when it is done";
"re-read with mm_status or mm_list after a Concurrent error"; "prefer these
tools over shelling out to mm".

## 7. Session state and branching

The plugin keeps exactly one piece of durable state: the **board pin** —
`{ path, project }`. It follows pi's documented state-management pattern:
the pin travels in tool-result `details`, and `session_start` reconstructs it
by scanning the active branch for the last `mm_status`/`mm_board` result.
Branches therefore get their own pin, and a fork that changes the board does
not leak into the other branch.

- `pi.appendEntry("mm:board", ...)` is a MAY for a rendered board card in the
  transcript; the branch-details pattern is the normative store.
- A pin set by `/mm board PATH` or config (§9) is the *session* pin; it
  overrides resolution order and is re-asserted on `session_start`.
- On `session_shutdown` the plugin releases nothing (it holds no resources)
  and MUST not flush any state to the board.

## 8. Safety

### 8.1 Never touch the files

Repeated from §2.1 because it is the load-bearing rule: the plugin reads and
writes micro-manager directories **only through `mm`**. A test that greps the
plugin's source for the board filenames and finds none is a conformance
smoke-test worth having.

### 8.2 Removal is guarded, twice

`mm_remove`:

1. Requires `confirmed: true` in the tool schema — the model must explicitly
   assert it has the user's go-ahead. A schema default of `false` makes a
   missing parameter a refusal.
2. When `ctx.hasUI`, prompts with `ctx.ui.confirm` naming the item and its
   home, and proceeds only on an affirmative answer.
3. Only then passes `--force` — the CLI's own guard (`spec-tools.md` §5.1.6)
   is the third line, not the first.

The `/mm remove` command confirms the same way. In headless modes without a
UI, step 2 is skipped and the explicit `confirmed` is the whole guard.

### 8.3 Nothing on its own initiative

No event handler mutates the board (§2.2). In particular the plugin MUST NOT
auto-start, auto-finish, or auto-tick when a turn ends; it may *suggest* in a
tool guideline, never act.

### 8.4 Timeouts and concurrency

Every `mm` subprocess is time-bounded (§4.1). Exit 5 (concurrent
modification) is surfaced with the re-read instruction (§4.3). The plugin
MUST NOT retry a failed mutation automatically — the board's state is the
user's to re-confirm.

## 9. Configuration

Config is JSON. Read in two places:

| File | Scope | Trusted check |
|---|---|---|
| `~/.pi/agent/mm-plugin.json` | global | none needed |
| `.pi/mm-plugin.json` | project | MUST be ignored unless `ctx.isProjectTrusted()` — pi's project-trust model governs project-local config, and the plugin must not read an untrusted project's pin. |

Keys:

| Key | Type | Default | Meaning |
|---|---|---|---|
| `board` | string (path) | — | Pin a board; overrides resolution order (§3.2). |
| `context` | bool | `true` | Per-turn injection (§6). |
| `contextLines` | int | 6 | Max injected block lines. |
| `timeoutMs` | int | 30000 | Subprocess bound. |
| `confirmRemove` | bool | `true` | Master switch for §8.2's interactive prompt (still requires `confirmed: true`). |

Environment: `MM_DIR` is honored by passing it through untouched (§3.2). The
plugin defines no other environment.

## 10. Conformance

**Minimally conforming**: §2.1, §3.2, §3.3, §4.1–§4.3 for the required set
`mm_init`, `mm_find`, `mm_status`, `mm_next`, `mm_list`, `mm_show`,
`mm_board`, `mm_describe`, `mm_add`, `mm_start`, `mm_pause`, `mm_finish`; §6; §8.1; §8.3;
§9's global config.

**Fully conforming**: additionally `mm_edit`, `mm_move`, `mm_note`,
`mm_remove` with §8.2, `mm_check`, the `/mm` command surface of §5, and §7's
branching state.

A conforming plugin MUST NOT add tools that change the meaning of the
operations defined here, MUST NOT write the data files, and MUST NOT run `mm`
with any switch this spec does not name.

## 11. Worked examples

```text
user:  start a board for the gardening project
agent: mm_init "gardening" --dir ~/boards/garden --prefix G \
         --description "Garden chores and seasonal projects"
       → board created at ~/boards/garden, IDs are G-0001…, pinned

user:  what is this board?
agent: mm_board → ~/boards/garden — gardening — "Garden chores and seasonal
       projects" — wip 0/1 · ready 2 · someday 1

user:  which boards can you see?
agent: mm_find → 3 boards — in use: ~/boards/garden (gardening)

user:  what should I work on next?
agent: mm_next → "T-0169  Spec: tickler fields and grammar (high)"

user:  capture this: the deploy script ate the logs again
agent: mm_add "Deploy script ate the logs again" --prio high --tag infra
       → T-0174 added to Ready (bottom)

user:  okay, working on T-0169
agent: mm_start T-0169 → slot 01
       ... (turns later) ...
agent: mm_finish T-0169 --outcome shipped --closing-note "grammar in §3.3"
       → done.md, 2026-08

user:  start T-0018
agent: mm_start T-0018 → WIP limit reached (1/1)
       slot 01  T-0169  (in work)          # remedy surfaced, not a mystery

user:  /mm board /srv/boards/other
       → pinned: /srv/boards/other — "Other Project"   # session pin

user:  drop T-0175, it was a duplicate
agent: mm_remove T-0175 confirmed=true → confirm dialog → T-0175 removed
```

---

## Appendix A: tool index

| Tool | Parameters | `mm` argv | Content (formatted) | `details` |
|---|---|---|---|---|
| `mm_init` | project, dir, slots, slot_width, prefix, description | `--init --project N [--dir P] [--slots N] [--slot-width W] [--prefix P] [--description TEXT] --json` | new board path + project, pinned | envelope |
| `mm_find` | — | `--find --json` | located boards, in-use marked | envelope |
| `mm_status` | — | `--status --json [--dir P]` | the one-screen summary | envelope |
| `mm_next` | — | `--next --json [--dir P]` | top item or "nothing ready" | envelope |
| `mm_list` | section, state, prio, tag, limit | `--list --json …` | items, on-disk order | envelope |
| `mm_show` | id, detail | `--show ID [--detail] --json …` | fields + state (+ body) | envelope |
| `mm_board` | path? | `--status --json [--dir P]` | pinned board: identity + description + summary | envelope |
| `mm_describe` | text | `--describe TEXT --json [--dir P]` | updated first paragraph | envelope |
| `mm_add` | title, section, prio, tags, top, detail_text | `--add … --json …` | new item + ID | envelope |
| `mm_add_many` | items[], section, prio, tags, top | `--add-many … --json …`, items on stdin | every new item + ID | envelope |
| `mm_edit` | id, title, prio, tags, untags, set | `--edit ID … --json …` | updated item | envelope |
| `mm_move` | id, section, position, top, end | `--move ID … --json …` | item at new position | envelope |
| `mm_start` | id | `--start ID --json …` | item in slot, or slots at limit | envelope |
| `mm_pause` | id | `--pause ID --json …` | item back in backlog | envelope |
| `mm_finish` | id, outcome, closing_note | `--finish ID … --json …` | item in done.md | envelope |
| `mm_note` | id, text | `--note ID TEXT --json …` | appended entry | envelope |
| `mm_remove` | id, confirmed | `--remove ID --force --json …` | deletion summary | envelope |
| `mm_check` | — | `--check --json [--dir P]` | violations or clean | envelope |

## Appendix B: `mm` switches used

Cross-references into `spec-tools.md`; the plugin relies on the CLI
contract, not on these lists staying frozen:

- Ops: `--init` (§5.1.1), `--describe` (§5.3), `--status` (§5.2), `--next`
  (§5.2), `--list` (§5.1.3), `--show` (§5.1.4), `--add` (§5.1.2), `--edit`
  (§5.1.5), `--move` (§5.1.7), `--start` (§5.1.8), `--pause` (§5.1.9),
  `--add-many` (§5.2.1),
  `--finish` (§5.1.10), `--remove` (§5.1.6), `--check` (§5.1.12),
  `--block`/`--unblock` (§5.2), `--note` (§5.2), `--search` (§5.2),
  `--report` (§5.1.11), `--tick`/`--archive` (§5.3), `--find` (§5.2),
  `--wip` (§5.2).
- Modifiers: `--dir`, `--json`, `--force`, `--top`, `--end`, `--position`,
  `--section`, `--state`, `--prio`, `--tag`, `--untag`, `--set`, `--limit`,
  `--detail`, `--detail-text`, `--outcome`, `--closing-note`, `--before`.
- Exit codes and the `--json` envelope: §9.2, §10.

## Appendix C: the exit-code table

Repeated from §4.3 as a reference table; 0–6 are `spec-tools.md` §10's codes,
and the plugin MUST NOT reinterpret them.

| Code | Name | Plugin surface |
|---|---|---|
| 0 | success | ok result |
| 1 | invariant violation | errors verbatim; suggest `mm_check` |
| 2 | usage | "operation not in this mm build" |
| 3 | not found / ambiguous | unknown ID; or candidates + pin required |
| 4 | precondition | remedy surfaced (slots at WIP limit, etc.) |
| 5 | concurrent | stale read; re-read and retry |
| 6 | I/O | environment failure |
