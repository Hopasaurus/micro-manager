# micro-manager — terminal user interface specification

    Spec version: 1
    Date:         2026-07-31
    Status:       draft
    Depends on:   spec-file-format.md (v1), spec-tools.md (v1), spec-gui.md (v1)

This document specifies the micro-manager terminal user interface: a keyboard-
driven front end for a lighter workflow than the web GUI. It is language- and
framework-agnostic; several independent implementations are expected.

`spec-gui.md` §13 reserved the shared ground. This document fills it in and does
not re-decide it: the operation set, the theme colour tokens, the configuration
files, and the recent and favorites lists are shared, byte for byte, with the
GUI. Where this document appears to differ from the GUI on shared material, the
GUI spec wins and this document has a bug.

**On testing.** The GUI specification pins down DOM locators and routes so an
external suite can drive every implementation. There is no end-to-end test
plan for the TUI at this time, so this document specifies no locator contract,
no test mode, and no conformance suite. §12 is a checklist, not a harness. The
one hook that would make automation possible later — the command line of §7.5 —
is required anyway, for reasons that stand on their own.

---

## 1. Scope

Normative: the screen inventory, layout rules, the keyboard model, terminal
capability handling, theming as it differs from the GUI, and the shared
configuration and list files.

Non-normative: visual arrangement beyond what this document fixes, framework
and rendering library choice, and packaging.

Out of scope: the file formats (`spec-file-format.md`), operation semantics
(`spec-tools.md`), and everything web-specific in `spec-gui.md` — DOM, CSS,
routes, and the HTTP API have no analogue here.

## 2. Architecture

### 2.1 The TUI links the library

Per `spec-tools.md` §2.1, the TUI is a **binary with the library compiled in**.
It MUST NOT invoke the `mm` CLI, spawn it as a subprocess, or parse its output.
It MUST NOT talk to the GUI service over HTTP. All three front ends are peers
over the same library and the same files.

```
   +---------------+   +---------------+   +---------------+
   |  mm  (CLI)    |   |  UI service   |   |  TUI          |
   |  +---------+  |   |  +---------+  |   |  +---------+  |
   |  | library |  |   |  | library |  |   |  | library |  |
   |  +----+----+  |   |  +----+----+  |   |  +----+----+  |
   +-------|-------+   +-------|-------+   +-------|-------+
           +-------------------+-------------------+
                               |
                      +--------v---------+
                      |  markdown files  |
                      +------------------+
```

Unlike the GUI there is no client/server split and no untrusted layer: the
process that renders is the process that holds the library. Validation still
belongs to the library — a TUI MUST NOT reimplement a rule to decide whether a
key press is legal, and MUST surface the library's error rather than
pre-empting it with its own.

### 2.2 Relationship to the GUI

Shared, and specified in `spec-gui.md` rather than here:

| Concern | Where |
|---|---|
| Operation coverage | §6 here, mapping to `spec-gui.md` §6.1 |
| Theme file, locations, resolution order | `spec-gui.md` §8.2, §8.7 |
| `color` tokens | `spec-gui.md` §8.3 |
| Branding | `spec-gui.md` §8.6 |
| System and project config | `spec-gui.md` §9 |
| Recent and favorites | `spec-gui.md` §10 |

Not shared, and specified here: terminal capability handling (§3), the screen
model (§4), the keyboard model (§7), colour degradation (§8.2).

A TUI MUST NOT write a theme, config, or list file the GUI cannot read, and MUST
preserve keys it does not understand when rewriting one (§9).

### 2.3 Freshness

The CLI, the GUI service, and a text editor may all change a directory while the
TUI is displaying it.

1. The TUI MUST poll the library fingerprint (`spec-tools.md` §2.4) at
   `ui.pollIntervalMs` (default 5000) and redraw when it changes.
2. It MUST work with polling alone; a filesystem watcher is an optional
   optimization and MUST NOT change behavior when absent.
3. It SHOULD suspend polling while a modal prompt holds an uncommitted edit,
   and MUST re-check on commit — writing over someone else's change is worse
   than a stale display.
4. When a refresh changes the focused item, the TUI MUST keep focus on that item
   by ID and note the change in the status line. It MUST NOT let focus jump to a
   different item because positions shifted underneath it.

## 3. Terminal environment

### 3.1 Capability detection

Detection order for colour depth, first conclusive answer wins:

1. `--color=never|auto|always` on the command line.
2. `NO_COLOR` set and non-empty → monochrome, regardless of terminal support.
3. `config.tui.color` (§9).
4. `COLORTERM` containing `truecolor` or `24bit` → 24-bit.
5. terminfo / `TERM` capability lookup → 256 or 16.
6. Not a terminal (piped or redirected) → monochrome, no control sequences.

Four rendering tiers MUST be supported: **24-bit**, **256**, **16**, and
**monochrome**. §8.2 specifies how theme colours degrade into each.

Monochrome is not a degraded curiosity; it is what a user gets over a bad SSH
link, and every piece of information conveyed by colour MUST remain available
through text or attributes (§11).

### 3.2 Size, resize, and reflow

- Minimum usable size is **80×24**. At or above it, the full board renders.
- Between **60×16** and 80×24, the board MUST collapse to a single focused
  column with an indicator showing position in the column list. All operations
  remain available.
- Below **60×16**, the TUI MUST render only a message naming the current and
  required size, and MUST recover automatically when the terminal grows.
- `SIGWINCH` MUST be handled and the UI reflowed without losing: the focused
  item, scroll position within a column, an open prompt and its contents, or
  move-mode state (§7.3).

### 3.3 Screen, cursor, and signals

1. Use the alternate screen buffer; restore the primary buffer on exit.
2. Hide the cursor except while a text field has focus.
3. Restore terminal state — buffer, cursor visibility, mouse reporting, raw
   mode, bracketed paste — on **every** exit path: clean quit, `SIGINT`,
   `SIGTERM`, `SIGHUP`, and an unhandled internal error. A crash that leaves an
   unusable terminal is a defect of the same severity as data loss.
4. On `SIGTSTP`, restore the terminal before suspending and re-initialize on
   `SIGCONT`.
5. Enable bracketed paste where available so a pasted multi-line title cannot
   trigger key bindings.

### 3.4 Unicode and ASCII fallback

Box drawing and symbols MUST have an ASCII fallback, selected when the locale is
not UTF-8, when `config.tui.ascii` is true, or when `--ascii` is given.

| Role | Unicode | ASCII |
|---|---|---|
| Column border | `│ ─ ┌ ┐ └ ┘` | `\| - + + + +` |
| Focus marker | `▸` | `>` |
| Selected item | `●` | `*` |
| Detail indicator | `¶` | `@` |
| Blocked marker | `⨯` | `x` |
| Priority high/med/low | `▲ ■ ▼` | `^ = v` |
| Move-mode insertion point | `▬` | `~` |
| Truncation | `…` | `...` |

Implementations MUST measure display width by grapheme cluster and East Asian
width, not byte or code-point count. Titles are user text and will contain
emoji, CJK, and combining marks; a naive width calculation corrupts every column
to the right of the error.

### 3.5 Mouse

OPTIONAL. Where implemented:

- click to focus an item or column; double-click opens the item panel;
- wheel scrolls the focused column;
- press-and-drag performs a move, following the same legality rules as §7.3;
- mouse reporting MUST default to **off**, because it disables the terminal's
  own text selection, and MUST be toggleable at runtime and via
  `config.tui.mouse`.

No operation may be mouse-only.

## 4. Screen model

### 4.1 Screens

| Screen | Purpose | Spec |
|---|---|---|
| Board | Primary working view | §5.1 |
| Item | Full view and edit of one item | §5.2 |
| Report | Completed work over a period | §5.3 |
| Check | Validation results | §5.4 |
| Home | Recent and favorites; the entry screen with no project open | §5.5 |
| Settings | Project and system configuration | §5.6 |
| Theme | Theme selection and editing | §5.6 |
| Help | Key binding reference | Appendix A |
| Palette | The command line | §7.5 |

### 4.2 Navigation

Screens form a stack. Opening pushes; `q` and `Esc` pop. Popping the last screen
quits, with confirmation if an edit is uncommitted.

`g` is the go-to prefix: `g h` Home, `g b` Board, `g r` Report, `g c` Check,
`g s` Settings, `g t` Theme, `g ?` Help. A go-to **replaces** the current screen
rather than pushing, so the stack cannot grow without bound.

Help and Palette are overlays: they render above the current screen and do not
push onto the stack.

### 4.3 Chrome

Every screen shows:

- **Header** — brand short name or project name, the project's theme is the
  primary signal of which project is open (§8.5), and the screen name.
- **Status line** — WIP as `n/N`, counts by section, validation state, and a
  transient message area. Validation state MUST be visible without opening the
  Check screen.
- **Key hint line** — the four to six most relevant bindings for the current
  screen and mode. It MUST update when entering move mode, a prompt, or a
  filter.

## 5. Screens

### 5.1 Board

The primary view. Columns in this order, matching the GUI:

1. Ready
2. Blocked
3. Someday
4. one Working column for all working items, ordered by slot number
5. Done

Each column shows a title, a count, and its items. The focused column MUST be
visually distinct in every colour tier, including monochrome (§11).

An item row shows, in a fixed order: priority marker, ID, title, tag list,
detail indicator, and blocked marker where applicable. Title is truncated last,
after tags. Working item rows additionally show the slot number and `started`.

Columns scroll independently. The focused item MUST remain visible, with at
least one row of context above and below where the column is long enough.

Filters (`/` search, and filter keys) narrow the visible items and MUST show an
active-filter indicator in the status line, since a filtered board that looks
unfiltered is how people conclude their data is gone.

### 5.2 Item

Full view of one item: every field, current state and slot, `## Task`,
`## Plan` subtasks, `## Notes`, and the detail file body when one exists.

Editing is field-by-field: focus a field, `Enter` to edit, `Esc` to abandon,
`Ctrl-S` to commit. The detail body opens in `$VISUAL` or `$EDITOR` on `o`; the
TUI MUST restore the terminal before spawning the editor and re-initialize
after, and MUST re-read from disk on return rather than trusting its cached copy.

### 5.3 Report

Renders the same content as `mm --report`, with the resolved period and its
source shown in the header — switch, config, or default. Period selection cycles
with `p`, grouping with `G`.

`y` copies the paste-ready markdown to the system clipboard via OSC 52 where the
terminal supports it, and otherwise writes it to a file and reports the path.
The report is the artifact people paste elsewhere; a TUI that can only display
it has done half the job.

### 5.4 Check

Lists violations as `file:line: message` with the `I1`–`I10` identifier, sorted
by path then numeric line. `Enter` on a violation jumps to the offending item
where one exists. `r` re-runs.

### 5.5 Home

Favorites first, then recent, each capped at its configured display count
(`spec-gui.md` §10), then all discovered projects grouped by scan root (§9).
Shows for each: name, path, and WIP. Entries whose path no longer resolves are
marked and MUST NOT be silently removed.

The discovered list MUST show when it was last scanned, and MUST say so when the
walk was truncated by a limit — a partial scan looks exactly like a missing
project otherwise.

`Enter` opens, `f` toggles favorite, `d` removes from the list after
confirmation, `o` opens a path directly, `n` initializes a new directory.

Where the resolved theme provides `brand.ascii` and the terminal is wide enough,
Home displays it.

### 5.6 Settings and Theme

Field-per-line forms with an explicit scope indicator — every screen MUST state
which file a change will be written to. System-scoped keys (`ui.recentCount`,
`ui.favoritesCount`, `ui.recentMaxStored`, `server.*`) MUST NOT be offered in
project scope.

The Theme screen lists the theme library, marks the active theme and its source,
and offers select, edit, export, import, and clear. Editing MUST apply live so
the screen is its own preview.

### 5.7 Prompts and messages

- **Prompts** — single-line input at the bottom of the screen, `Enter` commits,
  `Esc` cancels.
- **Confirmations** — required for `--remove` (`spec-tools.md` §5.1.6), for
  quitting with uncommitted edits, and for overwriting on theme import. A
  confirmation MUST require an explicit affirmative key, never bare `Enter` on a
  default-yes prompt.
- **Messages** — transient in the status line; severity conveyed by both colour
  and a text prefix.
- **Errors** — an error that ends an operation MUST persist until dismissed or
  superseded. `WipLimitReached` MUST list the current occupants and the three
  remedies, matching the CLI and the GUI.

## 6. Operation coverage

The TUI MUST provide every required operation in `spec-tools.md` §5.1 and SHOULD
provide every recommended one in §5.2.

| Operation | Reachable from |
|---|---|
| `--init` | Home, `n` |
| `--add` | Board, `a` |
| `--list` | Board, with filters |
| `--show` | Item screen |
| `--edit` | Item screen fields |
| `--remove` | `x`, with confirmation |
| `--move` | Move mode (§7.3) |
| `--start` | `s`, or move mode into a slot column |
| `--pause` | `S`, or move mode out of a slot column |
| `--finish` | `f`, or move mode into Done |
| `--report` | Report screen |
| `--check` | Check screen and the status line |
| `--block` / `--unblock` | `b` / `B`, or move mode |
| `--note` | `n` on the Item screen |
| `--wip` | Settings |
| `--status` | Status line |
| `--next` | Top item of Ready |
| `--search` | `/` |
| `--find` | Home, scanning the configured roots (§9) |

Anything without a binding MUST be reachable from the command line (§7.5). That
rule is what guarantees parity: a new library operation is usable from the TUI
the day it exists, before anyone chooses a key for it.

## 7. Keyboard model

### 7.1 Principles

1. Every operation is reachable by keyboard alone. There is no other input
   method this specification requires.
2. Bindings are modal only where §7.3 says so. A key that means different things
   on different screens is fine; a key that silently changes meaning on the same
   screen is not.
3. Arrow keys MUST work everywhere `hjkl` does. Neither is an alias the user has
   to discover.
4. `Esc` always cancels the innermost thing: a prompt, then a mode, then an
   overlay, then the screen.
5. Bindings are remappable via `config.tui.keys` (§9). Defaults below are what
   an implementation ships with, not a hard-coded contract.

### 7.2 Global and board bindings

Global:

| Key | Action |
|---|---|
| `?` | Help overlay |
| `:` | Command line (§7.5) |
| `/` | Search |
| `Esc` | Cancel innermost |
| `q` | Pop screen; quit from Home |
| `Ctrl-C` | Quit, with confirmation if an edit is uncommitted |
| `Ctrl-P` | Project switcher |
| `Ctrl-R` | Force refresh |
| `g` + `h b r c s t ?` | Go to screen (§4.2) |

Board:

| Key | Action |
|---|---|
| `h` `l` `←` `→` | Focus previous / next column |
| `j` `k` `↓` `↑` | Focus previous / next item |
| `g g` / `G` | First / last item in column |
| `Ctrl-D` / `Ctrl-U` | Half-page down / up |
| `Enter` | Open Item screen |
| `Space` | Enter move mode (§7.3) |
| `a` | Add item to the focused column |
| `e` | Edit focused item |
| `s` / `S` | Start / pause |
| `f` | Finish, prompting for outcome |
| `b` / `B` | Block (prompts for reason) / unblock |
| `n` | Add a note |
| `o` | Open the detail file in `$EDITOR` |
| `d` | Show detail |
| `x` | Remove, with confirmation |
| `y` | Copy item ID |
| `F` | Filter menu |

`s`, `S`, `f`, `b`, `B`, and `x` MUST be disabled — with a status-line reason
naming the error code — when illegal for the focused item, rather than silently
doing nothing. A key that appears to do nothing is indistinguishable from a
broken program.

### 7.3 Move mode

Move mode is the terminal's equivalent of the GUI's drag and drop, and inherits
the bindings `spec-gui.md` §7.4 reserved for exactly this purpose.

`Space` on a focused item enters move mode. While in it:

| Key | Action |
|---|---|
| `j` `k` `↓` `↑` | Change insertion index within the target column |
| `h` `l` `←` `→` | Change target column |
| `Enter` | Commit |
| `Esc` | Cancel, restoring the item's original position and focus |

Requirements:

1. The item under the cursor MUST be visually distinct, and an insertion marker
   MUST show where it would land.
2. The key hint line MUST change on entry, since the meaning of the arrow keys
   has changed.
3. An illegal target MUST be shown as illegal — with a reason — and MUST refuse
   to commit rather than silently reverting to the last legal target.
4. Legal transitions are exactly those in `spec-gui.md` §7.2: reorder within a
   column; between backlog sections; backlog into a slot (`--start`); slot into
   backlog (`--pause`); backlog or slot into Done (`--finish`). Slot-to-slot and
   anything out of Done are illegal.
5. Transitions requiring input — a block reason, a finish outcome — MUST prompt
   on commit. Cancelling the prompt cancels the move.
6. On rejection, restore the original position, keep focus on the item, and show
   the error. For `WipLimitReached`, show occupants and remedies.

### 7.4 Text entry

Prompts and fields support: `Ctrl-A`/`Ctrl-E` line start/end, `Ctrl-W` delete
word, `Ctrl-U` clear, `Ctrl-K` kill to end, and bracketed paste. Tab completion
is RECOMMENDED for IDs, tags, and theme names.

### 7.5 Command line

`:` opens a command line accepting the full operation vocabulary, named to match
`spec-tools.md` switches without the leading dashes:

```
:add Fix the deploy script --prio high --tag infra
:start T-0042
:finish T-0042 --outcome cancelled
:move T-0042 --position 3
:report --period last-week --group-by outcome
:wip 3
:theme sample-one-dark
:open /path/to/micro-manager
:check
:help
:q
```

Requirements:

1. Every library operation MUST be invocable here, including any this document
   gives no key binding.
2. Argument names and values MUST match the CLI exactly. A user who knows `mm`
   knows this.
3. Errors surface as in §5.7; the command line MUST retain the failed text for
   editing rather than clearing it.
4. History is per-session and MUST NOT be persisted — command lines contain item
   titles, and those belong in the project, not a shell-history-shaped file in
   the user's home directory.

## 8. Theming

### 8.1 Shared contract

The theme file, its locations, the token taxonomy, branding, and the resolution
order are specified in `spec-gui.md` §8 and are **not** restated here. A TUI:

- MUST read the same files, from the same locations, with the same precedence
  and the same per-token fallback to the built-in default;
- MUST use the `color` group (`spec-gui.md` §8.3);
- MUST read `tui.ansi256`, `tui.ansi16`, and `tui.attrs` where present;
- MUST ignore the `gui` group without error or warning;
- MUST preserve every key it does not understand when it writes a theme.

The `color` group was deliberately sized to what a terminal can render. If a TUI
needs a colour the group does not have, that is a request to change
`spec-gui.md` §8.3 — not a licence to invent a TUI-only token.

### 8.2 Colour degradation

Per rendering tier (§3.1):

| Tier | Source |
|---|---|
| 24-bit | The token's hex value, directly. |
| 256 | `tui.ansi256[token]` if present; otherwise the nearest 256-palette colour to the hex. |
| 16 | `tui.ansi16[token]` if present; otherwise the nearest of the 16 ANSI colours. |
| monochrome | No colour. Distinctions carried by attributes and text (§11). |

Derivation MUST be deterministic, and SHOULD use nearest-neighbour in CIELAB —
nearest-in-RGB produces visibly wrong hues on the 16-colour palette.

A theme author who cares about terminals sets `tui.ansi256` and `tui.ansi16`
explicitly; automatic derivation is a floor, not a substitute.

### 8.3 Attributes

`tui.attrs` maps a token to terminal attributes — `bold`, `dim`, `italic`,
`underline`, `reverse`, `strikethrough`. Implementations MUST apply those a
terminal supports and MUST silently skip those it does not.

Attributes are also the monochrome fallback: in monochrome, an implementation
MUST apply `tui.attrs` and MUST substitute its own attribute scheme for tokens
that have none, so that state, priority, and focus stay distinguishable.

### 8.4 Branding

`brand.short` appears in the header. `brand.ascii` appears on Home when the
terminal is wide enough; it MUST be truncated or omitted rather than wrapped.
`brand.logo` and `brand.icon` are raster or SVG assets and MUST be ignored.

### 8.5 The theme identifies the project

The reason a theme lives in the data directory (`spec-gui.md` §8.1) applies more
strongly in a terminal, where there is no window chrome, no tab title, and no
favicon: **the colours are the only ambient signal of which project is open.**

Therefore:

1. Switching projects MUST re-resolve the theme and repaint immediately.
2. The header MUST show the project name at all times; the theme reinforces it
   but never replaces it, because a monochrome terminal has no theme to read.
3. A project with no theme MUST fall back to the system theme, and the TUI
   SHOULD indicate on the Settings screen that no project theme is set — an
   ambient signal that silently means "default" is worth knowing about.

### 8.6 Export and import

Same semantics as `spec-gui.md` §8.8: export is one self-contained JSON file
with assets inlined; import validates fully, applies atomically, preserves
unknown keys, and never silently overwrites on an `id` collision. Import and
export are reachable from the Theme screen and from `:theme`.

## 9. Configuration

The TUI reads and writes the same files as the GUI (`spec-gui.md` §9), with the
same merge rules and the same system-scoped key restrictions.

Terminal-specific settings live under a `tui` object, the config counterpart to
the theme's `tui` group. The GUI ignores it; the TUI ignores `ui.board` and any
other key with no terminal meaning.

```json
{
  "schemaVersion": 1,
  "ui":  { "pollIntervalMs": 5000, "density": "compact" },
  "scan": { "roots": ["~/code"], "maxDepth": 6, "includeHidden": true },
  "tui": {
    "color": "auto",
    "ascii": false,
    "mouse": false,
    "columnWidth": 32,
    "showKeyHints": true,
    "keys": { "start": "s", "pause": "S", "finish": "f" }
  }
}
```

**Discovery scope.** `scan` is the shared block specified in `spec-gui.md` §9.5,
and the TUI MUST honor it identically: the same roots, the same depth and
exclusion rules, hidden directories included by default, no descent into a
matched directory, results deduplicated by canonical path and marked partial
when a limit is hit. A user who configures roots in one front end MUST see the
same project list in the other.

TUI-specific handling:

1. `--root PATH`, repeatable, overrides `scan.roots` for one run without writing
   config. This is the terminal equivalent of pointing the GUI at a different
   tree, and is the expected way to use the TUI against a checkout you do not
   want in your permanent roots.
2. `:scan` re-runs discovery; `:scan add PATH` and `:scan remove PATH` edit the
   configured roots and persist them.
3. Because a terminal has no window-focus event, `scan.rescanOnFocus` has no
   meaning here and MUST be ignored. Discovery refreshes on `:scan`, on entering
   Home, and on `Ctrl-R`.
4. Scanning MUST NOT block input. A scan in progress MUST show in the status
   line and MUST be cancellable with `Esc`.
5. Roots are edited on the Settings screen in system scope only, matching
   `spec-gui.md` §9.5 rule 1.

**The TUI MUST NOT open a listening socket.** It has no network surface, no
binding configuration, and MUST ignore `server.*` entirely — those keys belong
to the GUI service (`spec-gui.md` §9.6) and MUST be preserved untouched when the
TUI rewrites a config file.

`tui.keys` maps action names — the operation names of `spec-tools.md` §5 plus
the navigation actions of §7.2 — to key descriptors. An unknown action name or
an unparseable descriptor MUST be reported as a warning naming the key, and MUST
NOT prevent startup with the remaining bindings intact.

**Both front ends MUST preserve keys they do not understand when rewriting any
shared file** — config, theme, recent, or favorites. Without that, running the
TUI once would strip a GUI-only setting, and running the GUI once would strip it
back. This is the single most likely way two front ends over one file corrupt
each other's state.

## 10. Recent and favorites

Exactly as `spec-gui.md` §10, including file locations, shape, ordering, the
separation of stored count from displayed count, and the requirement that a
missing path be marked rather than removed.

TUI specifics:

1. Opening a project from Home, `Ctrl-P`, or `:open` MUST update `recent`.
2. Favorites are reorderable from Home using move mode (§7.3), which is the
   terminal equivalent of the GUI's drag reordering.
3. Writes MUST be atomic — temp file plus rename — because the GUI service may
   be running against the same files.

## 11. Legibility in reduced environments

1. **Nothing may be conveyed by colour alone.** Priority, item state, outcome,
   blocked status, focus, and move mode MUST each be distinguishable in
   monochrome through a glyph, an attribute, or text. This is not only
   accessibility: it is what makes the TUI usable over a link that has mangled
   `TERM`.
2. Focus MUST be visible in every tier; a focus marker glyph is required, not a
   colour change alone.
3. `NO_COLOR` MUST be honored regardless of terminal capability.
4. Contrast between `fg.default` and `bg.base` SHOULD meet WCAG AA where the
   theme controls both. A terminal's own background may override the theme's,
   and an implementation MUST NOT assume its background colour took effect.
5. Text MUST remain selectable and copyable by the terminal's own mechanisms
   when mouse reporting is off, which is the default (§3.5).
6. No information may depend on colour-blind-unsafe pairs alone; red/green
   distinctions MUST carry a glyph or attribute difference too.

## 12. Conformance

There is no external conformance suite for the TUI and none is planned at this
time. This is a checklist for implementers, not a harness.

A conforming implementation:

1. links the library directly and never shells out to the CLI or the GUI (§2.1);
2. provides every required operation of §6, with the command line (§7.5) as the
   backstop for anything unbound;
3. implements the keyboard model of §7, including move mode with the bindings
   inherited from `spec-gui.md` §7.4;
4. supports all four colour tiers and degrades per §8.2;
5. reads and writes theme, config, recent, and favorites files compatibly with
   the GUI, preserving unknown keys (§9);
6. re-resolves and repaints the theme on project switch (§8.5);
7. restores terminal state on every exit path, including signals and internal
   errors (§3.3);
8. conveys nothing by colour alone (§11).

Should end-to-end testing be added later, the natural driver is the command line
of §7.5, which is required independently. A future revision would need to add a
deterministic mode — disabled animation, fixed terminal size, a stable frame
dump — and this document does not specify one.

---

## Appendix A: default key bindings

```
global   ?  help                    :  command line
         /  search                  Esc  cancel innermost
         q  pop screen              Ctrl-C  quit
         Ctrl-P  project switcher   Ctrl-R  refresh
         g h|b|r|c|s|t|?  go to screen

board    h l ← →   focus column     j k ↓ ↑  focus item
         gg G      first / last     Ctrl-D Ctrl-U  half page
         Enter     open item        Space    move mode
         a add     e edit           s start  S pause    f finish
         b block   B unblock        n note   o $EDITOR  d detail
         x remove  y copy id        F filter

move     j k ↓ ↑   insertion index  h l ← →  target column
         Enter     commit           Esc      cancel

item     Enter edit field           Ctrl-S commit      Esc abandon
         o open detail in $EDITOR

report   p period  G group-by       w toggle wip       y copy markdown

home     Enter open                 f favorite         d remove entry
         o open path                n init              R rescan roots

text     Ctrl-A start   Ctrl-E end   Ctrl-W del word
         Ctrl-U clear   Ctrl-K kill to end
```

## Appendix B: colour token rendering

All tokens from `spec-gui.md` §8.3 `color`. Monochrome column gives the required
fallback when no `tui.attrs` entry exists.

| Token group | 24-bit / 256 / 16 | Monochrome fallback |
|---|---|---|
| `bg.*` | background fill | none; rely on borders |
| `fg.default` | text | normal |
| `fg.muted`, `fg.subtle` | text | `dim` |
| `border.focus` | focused column border | focus marker glyph |
| `accent.*` | selection, active screen | `reverse` |
| `state.ready/blocked/someday/working/done` | column titles, item state | column position plus text label |
| `prio.high/med/low` | priority marker | `▲ ■ ▼` glyphs (`^ = v` in ASCII) |
| `feedback.success/warning/danger/info` | status messages | text prefix `ok:` `warn:` `error:` `info:` |
| `selection.*` | focused item row | `reverse` |
| `drag.valid/invalid` | move-mode insertion marker | marker glyph present or absent, plus reason text |
