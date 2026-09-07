package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// InitRequest creates a new micro-manager directory (spec-tools.md §5.1.1).
//
// Init always creates a version-2 directory (board.md) — the spec describes
// no version-1 output for --init at all, and no version modifier exists to
// ask for one (T-0241). Version 1 remains fully supported for directories
// that already exist; it is simply no longer what a fresh one is born as.
type InitRequest struct {
	// Project is the human name of the directory, and is required: I7 makes it
	// the one piece of frontmatter a backlog cannot do without.
	Project string

	// Wip is the fresh board's wip.working cap. Zero means UNCAPPED: no
	// wip.working key is written at all (spec-tools.md §5.1.1 - "this is the
	// one place version 2 does not reproduce version 1's out-of-the-box
	// behavior by default"). A caller wanting version 1's old default of one
	// concurrent item passes Wip: 1 explicitly.
	Wip int

	// SlotWidth is version-1-only (initV1's working-file digit width). A
	// version-2 board has no working files, so Init ignores it; it survives
	// only for initV1's test-only callers.
	SlotWidth int

	// IDPrefix is the declared id_prefix (spec-file-format.md §3.3.2): one to
	// four uppercase ASCII letters. Empty means "T" and writes no id_prefix
	// key, so a default init declares no grammar of its own (rule 6).
	IDPrefix string

	// IDWidth is the declared id_width: 1 to 15. Zero means 4 and writes no
	// id_width key. 3-6 is RECOMMENDED; other widths within 1-15 are warned
	// about, never refused, and 16+ is invalid everywhere (§3.3.2 rule 3).
	IDWidth int

	// Description seeds structure.md's first paragraph (spec-tools.md
	// §5.1.1), replacing its default prose. Supplying it makes structure.md's
	// SHOULD a MUST: it is written even when NoStructure is also set, since
	// there is nowhere else for a supplied description to live.
	Description string

	// NoStructure skips structure.md. The spec only SHOULD-writes it, but a
	// directory without it is a format nobody can read without a tool, which is
	// the opposite of the point. Overridden by a non-empty Description.
	NoStructure bool

	DryRun bool
}

// Init creates a micro-manager directory at path and returns a Store over it.
//
// The directory itself may already exist and hold anything else; what it may NOT
// hold is any file this would write. Merging into a half-built directory would
// mean guessing which of the two layouts is authoritative.
//
// Always version 2 (see InitRequest's doc comment). Version-1 creation
// (backlog.md/working.NN.md) survives only as the unexported initV1, reachable
// solely from this package's own tests, which still need to construct
// version-1 fixtures to test version-1 behavior the CLI and library keep
// supporting on directories that already exist.
func Init(path string, req InitRequest, today Date) (*Store, TxResult, error) {
	project, err := validatedProjectName(req.Project)
	if err != nil {
		return nil, TxResult{}, err
	}
	if req.Wip < 0 {
		return nil, TxResult{}, fmt.Errorf(
			"%w: a WIP cap cannot be negative", ErrInvalidArgument)
	}
	g, err := resolvedIDGrammar(req.IDPrefix, req.IDWidth)
	if err != nil {
		return nil, TxResult{}, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, TxResult{}, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}

	writeStructure := !req.NoStructure || req.Description != ""
	files := map[string]string{
		"board.md":   renderInitBoard(project, today, g, req.Wip),
		"done.md":    renderInitDone(today, 2),
		templateName: initTemplate,
	}
	order := []string{"board.md", "done.md", templateName}
	if writeStructure {
		files["structure.md"] = renderManagedStructureV2(project, today, req.Description, g, defaultStructureNotes())
		order = append(order, "structure.md")
	}

	// Nothing may be overwritten. An existing file here is a directory that is
	// already something else, and merging into it would mean choosing which
	// layout wins.
	for _, name := range order {
		if _, err := os.Stat(filepath.Join(abs, name)); err == nil {
			return nil, TxResult{}, fmt.Errorf(
				"%w: %s already exists in %s", ErrAlreadyExists, name, path)
		}
	}

	// Pre-commit validation, on the same terms as every mutation: parse what is
	// about to be written and refuse to create a directory this package's own
	// checker would reject.
	if vs := validateInitV2(abs, files); len(vs) > 0 {
		return nil, TxResult{}, &InvariantError{Violations: vs}
	}

	var ws writeSet
	var res TxResult
	res.DryRun = req.DryRun
	for _, name := range order {
		ws.Add(filepath.Join(abs, name), []byte(files[name]), stamp{missing: true})
		res.Changes = append(res.Changes, Change{Kind: ChangeCreated, File: name})
	}
	res.Files = ws.Paths()

	if req.DryRun {
		return nil, res, nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, res, fmt.Errorf("%w: creating %s: %v", ErrIO, path, err)
	}
	if err := ws.Commit(); err != nil {
		return nil, res, err
	}
	s, err := Open(abs)
	if err != nil {
		return nil, res, err
	}
	return s, res, nil
}

// validatedProjectName is Init's shared name check (v1 and v2 alike).
func validatedProjectName(name string) (string, error) {
	project := strings.TrimSpace(name)
	if project == "" || project == "null" {
		return "", fmt.Errorf(
			"%w: a directory needs a project name", ErrInvalidArgument)
	}
	// Frontmatter values are read with a trailing comment stripped, so a name
	// containing " #" would come back shortened - and the file would fail to
	// describe itself the moment it was written.
	if strings.ContainsAny(project, "\n\r") || strings.Contains(project, " #") ||
		strings.Contains(project, "\t#") {
		return "", fmt.Errorf(
			"%w: a project name may not contain a newline or a comment marker (' #')",
			ErrInvalidArgument)
	}
	return project, nil
}

// resolvedIDGrammar is Init's shared grammar check (v1 and v2 alike): the ID
// grammar (§3.3.2). Both keys are optional and default independently; what
// Init writes is a directory that declares its own grammar, validated here the
// same way the parsers validate a hand-written one.
func resolvedIDGrammar(prefix string, width int) (IDGrammar, error) {
	g := DefaultIDGrammar()
	if prefix != "" {
		if !ValidIDPrefix(prefix) {
			return g, fmt.Errorf(
				"%w: id_prefix must be one to four uppercase letters (A-Z), got %q",
				ErrInvalidArgument, prefix)
		}
		g.Prefix = prefix
	}
	if width != 0 {
		if width < 1 || width > 15 {
			// The shared cap of §3.3.2 rule 3 (T-0120): at 16 digits the
			// narrowest readers silently round, so no implementation honors
			// it, and Init must not create a directory the checkers reject.
			return g, fmt.Errorf(
				"%w: id_width must be 1 to 15, got %d", ErrInvalidArgument, width)
		}
		g.Width = width
	}
	return g, nil
}

// validateInitV2 runs the real validator over the version-2 files Init is
// about to write, mirroring migrateOneToTwo's own pre-write validation.
func validateInitV2(path string, files map[string]string) []Violation {
	board, bvs := parseBoard("board.md", []byte(files["board.md"]))
	done, dvs := parseDoneG("done.md", []byte(files["done.md"]), board.grammar)
	m := &dirModel{path: path, board: board, done: done, details: map[string]*detailFile{}}
	// The template is exempt from I9 by name, so it is deliberately not added
	// to m.details - doing so would report the file the format requires as an
	// orphan.
	vs := append(bvs, dvs...)
	vs = append(vs, m.validate()...)
	return vs
}

// renderInitBoard is board.md for a fresh version-2 directory: no stages/
// stage_labels/tickler_stages/needs_reason keys, since Init offers no
// modifier to customize any of them and their absence already means the
// documented defaults (parseStageConfig, DefaultStageConfig).
func renderInitBoard(project string, today Date, g IDGrammar, wip int) string {
	fm := NewFrontmatter()
	fm.Set("doc", "board")
	fm.Set("version", "2")
	fm.Set("project", project)
	fm.Set("next_id", string(g.NewID(1)))
	if g.Prefix != "T" {
		fm.Set("id_prefix", g.Prefix)
	}
	if g.Width != 4 {
		fm.Set("id_width", strconv.Itoa(g.Width))
	}
	if wip > 0 {
		fm.Set("wip.working", strconv.Itoa(wip))
	}
	fm.Set("updated", today.String())
	return fm.Render() + `
# Board

Everything not started or done, in stage order. See structure.md for the
line format.

` + stageComment("someday") + `

` + stageComment("ready") + `

` + stageComment("blocked") + `

` + stageComment("working") + `
`
}

// renderInitStructureV2 is structure.md for a fresh version-2 directory.
// description, when non-empty, replaces the default prose as the first
// paragraph after the title (spec-tools.md §5.1.1).
func renderInitStructureV2(project string, today Date, description string) string {
	intro := `A todo directory in plain Markdown. Every file is readable in any editor and
parseable with a handful of regexes — a tool is faster than editing by hand, but
nothing here needs one.`
	if description != "" {
		intro = description
	}
	return "---\ndoc: structure\nversion: 2\nupdated: " + today.String() + `
---

# ` + project + `

` + intro + `

## Files

| File | Holds |
|---|---|
| ` + "`board.md`" + ` | Every item not archived, one flat ordered list. |
| ` + "`done.md`" + ` | Everything finished or cancelled, newest first. |
| ` + "`details/T-NNNN.md`" + ` | Long-form description for one item. |

An item lives in **exactly one** of ` + "`board.md`" + ` or ` + "`done.md`" + ` at a time.
Moving it is cut-and-paste, never a copy. Detail files are the exception: they
never move, so the long text survives every transition.

## The item line

` + "```" + `
- [ ] [T-0042] Fix the deploy script | stage:ready | prio:high | tags:infra,ci | created:2026-07-29
` + "```" + `

- Box — a space for open, ` + "`x`" + ` for closed. Open lines only in
  ` + "`board.md`" + `, closed lines only in ` + "`done.md`" + `.
- ID — ` + "`T-`" + ` plus four digits. Permanent: never reused, never renumbered.
- Title — one line, and it must not contain ` + "`|`" + `.
- Fields — ` + "` | `" + ` separated ` + "`key:value`" + ` pairs. Order does not matter, and
  unknown keys are legal and must be preserved when an item moves.

| Key | Values |
|---|---|
| ` + "`stage`" + ` | one of the board's declared stages — default ` + "`someday`" + `, ` + "`ready`" + `,
  ` + "`blocked`" + `, ` + "`working`" + ` — required on every open item |
| ` + "`prio`" + ` | ` + "`high`" + ` ` + "`med`" + ` ` + "`low`" + ` — absent means ` + "`med`" + ` |
| ` + "`tags`" + ` | comma separated, no spaces |
| ` + "`created`" + ` ` + "`started`" + ` ` + "`done`" + ` | ` + "`YYYY-MM-DD`" + ` |
| ` + "`outcome`" + ` | ` + "`shipped`" + ` ` + "`cancelled`" + ` ` + "`obsolete`" + ` — required in done.md |
| ` + "`reason`" + ` | free text — required on a stage listed in ` + "`needs_reason`" + `
  (default: ` + "`blocked`" + `), legal on any stage |
| ` + "`detail`" + ` | ` + "`details/T-0042.md`" + ` — must match the item's own ID |
| ` + "`tickler`" + ` | a wake-up schedule; only on a stage listed as a ` + "`tickler_stages`" + `
  source (default: ` + "`someday`" + `) |
| ` + "`tickler_dest`" + ` | overrides where a fired schedule lands, instead of that
  stage's ` + "`tickler_stages`" + ` default |
| ` + "`tickler_paused`" + ` | ` + "`true`" + ` suppresses this item's schedule until resumed |

## board.md

Frontmatter carries ` + "`project`" + ` and ` + "`next_id`" + `, the ID to hand out next. The
body is a **flat, order-significant list** — no ` + "`## `" + ` sections; where an item
sits is entirely its own ` + "`stage:`" + ` field, not its position in the file. A
writer groups items under informational ` + "`<!-- stage:SLUG -->`" + ` comments for a
human reading the raw file, including comments for empty stages. The comments
do not determine stage membership: the item line's ` + "`stage:`" + ` field remains
authoritative.

Per-stage WIP caps are declared as ` + "`wip.<slug>`" + ` frontmatter keys (absent
means uncapped); a fresh board only ever declares ` + "`wip.working`" + `, and only
when asked for one at creation.

## done.md

Items grouped under ` + "`## YYYY-MM`" + ` headings, newest month first, newest item
first within a month. Every line closed, with ` + "`done:`" + ` and ` + "`outcome:`" + `.

## Operations by hand

**Add** — read ` + "`next_id`" + `, append the line to the bottom of its stage's run
with today's ` + "`created:`" + `, increment ` + "`next_id`" + `.

**Start** — move the line to ` + "`stage:working`" + `, set ` + "`started:`" + ` to today.
*If working is capped and already full, stop* — that is the WIP limit doing
its job.

**Pause** — move the line back to a backlog stage (` + "`ready`" + ` by default),
keeping ` + "`started:`" + `. A destination listed in ` + "`needs_reason`" + ` needs its
` + "`reason:`" + ` field set already — pausing does not prompt for one.

**Finish** — write the line at the top of the current month group in
` + "`done.md`" + ` as ` + "`- [x]`" + ` with ` + "`done:`" + ` and ` + "`outcome:`" + `, preserving every
other field (` + "`stage:`" + ` included, though it no longer means anything once an
item is done). ` + "`outcome:cancelled`" + ` is how work is abandoned without
deleting it.

## The rules

1. An ID appears in exactly one file. Never copy an item — move it.
2. Every ID is below ` + "`next_id`" + `. Never reuse or renumber an ID.
3. ` + "`board.md`" + ` holds only open boxes; ` + "`done.md`" + ` only closed ones.
4. Every open item declares a ` + "`stage:`" + ` that is one of the board's declared
   stages.
5. Every item on a ` + "`needs_reason`" + ` stage has a ` + "`reason:`" + ` field; a stage not
   listed there never requires one.
6. Every item in ` + "`done.md`" + ` has ` + "`done:`" + ` and ` + "`outcome:`" + `, under the month
   heading its date names.
7. Dates are real calendar dates, tags have no spaces, no value contains ` + "`|`" + `.
8. Every ` + "`detail:`" + ` path is ` + "`details/<that item's ID>.md`" + ` and exists.
9. Every non-` + "`_`" + ` file in ` + "`details/`" + ` is referenced by exactly one item, with
   matching ` + "`id`" + ` and ` + "`title`" + `.
10. A stage named in ` + "`wip.<slug>`" + `, ` + "`tickler_stages`" + ` or ` + "`needs_reason`" + ` is
    always a member of the board's declared ` + "`stages`" + `.
`
}

// ---------------------------------------------------------------------------
// Version 1 (initV1): unexported, test-only. The public Init above always
// creates version 2 (T-0241); this is the prior, unmodified implementation,
// kept reachable solely from this package's own tests, which still need to
// construct version-1 fixtures to test version-1 behavior the CLI and
// library keep supporting on directories that already exist (sample-data/,
// and any directory not yet migrated).

// initV1 creates a version-1 micro-manager directory: backlog.md,
// working.NN.md and done.md, exactly as Init used to before T-0241.
func initV1(path string, req InitRequest, today Date) (*Store, TxResult, error) {
	project, err := validatedProjectName(req.Project)
	if err != nil {
		return nil, TxResult{}, err
	}

	wip := req.Wip
	if wip == 0 {
		wip = 1
	}
	if wip < 1 {
		return nil, TxResult{}, fmt.Errorf(
			"%w: a directory needs at least one working file", ErrInvalidArgument)
	}
	width := req.SlotWidth
	if width == 0 {
		width = 2
	}
	if width < 1 {
		return nil, TxResult{}, fmt.Errorf("%w: slot width must be at least 1", ErrInvalidArgument)
	}
	// Uniform width is I10. A width too narrow for the highest slot would force
	// working.9.md next to working.10.md, which names nine slots and one lie.
	if n := len(strconv.Itoa(wip)); n > width {
		return nil, TxResult{}, fmt.Errorf(
			"%w: %d slots need at least %d digits, but slot width is %d",
			ErrInvalidArgument, wip, n, width)
	}

	g, err := resolvedIDGrammar(req.IDPrefix, req.IDWidth)
	if err != nil {
		return nil, TxResult{}, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, TxResult{}, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}

	files := map[string]string{
		"backlog.md": renderInitBacklog(project, today, g),
		"done.md":    renderInitDone(today, 1),
		templateName: initTemplate,
	}
	if !req.NoStructure {
		files["structure.md"] = renderInitStructure(project, today)
	}
	var order []string
	for i := 1; i <= wip; i++ {
		name := workingFileName(i, width)
		files[name] = initWorking
		order = append(order, name)
	}
	order = append([]string{"backlog.md", "done.md"}, order...)
	order = append(order, templateName)
	if !req.NoStructure {
		order = append(order, "structure.md")
	}

	// Nothing may be overwritten. An existing file here is a directory that is
	// already something else, and merging into it would mean choosing which
	// layout wins.
	for _, name := range order {
		if _, err := os.Stat(filepath.Join(abs, name)); err == nil {
			return nil, TxResult{}, fmt.Errorf(
				"%w: %s already exists in %s", ErrAlreadyExists, name, path)
		}
	}

	// Pre-commit validation, on the same terms as every mutation: parse what is
	// about to be written and refuse to create a directory this package's own
	// checker would reject.
	if vs := validateInitV1(abs, files, order); len(vs) > 0 {
		return nil, TxResult{}, &InvariantError{Violations: vs}
	}

	var ws writeSet
	var res TxResult
	res.DryRun = req.DryRun
	for _, name := range order {
		ws.Add(filepath.Join(abs, name), []byte(files[name]), stamp{missing: true})
		res.Changes = append(res.Changes, Change{Kind: ChangeCreated, File: name})
	}
	res.Files = ws.Paths()

	if req.DryRun {
		return nil, res, nil
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, res, fmt.Errorf("%w: creating %s: %v", ErrIO, path, err)
	}
	if err := ws.Commit(); err != nil {
		return nil, res, err
	}
	s, err := Open(abs)
	if err != nil {
		return nil, res, err
	}
	return s, res, nil
}

// validateInitV1 runs the real validator over the version-1 files initV1 is
// about to write.
func validateInitV1(path string, files map[string]string, order []string) []Violation {
	m := &dirModel{path: path, details: map[string]*detailFile{}, stamps: map[string]stamp{}}
	b, vs := parseBacklog("backlog.md", []byte(files["backlog.md"]))
	m.backlog = b
	m.parseVs = append(m.parseVs, vs...)
	g := b.grammar
	d, vs := parseDoneG("done.md", []byte(files["done.md"]), g)
	m.done = d
	m.parseVs = append(m.parseVs, vs...)
	for _, name := range order {
		if _, _, ok := isWorkingFileName(name); !ok {
			continue
		}
		m.entries = append(m.entries, name)
		w, vs := parseWorkingG(name, []byte(files[name]), g)
		m.working = append(m.working, w)
		m.parseVs = append(m.parseVs, vs...)
	}
	// The template is exempt from I9 by name, so it is deliberately not added to
	// m.details - doing so would report the file the format requires as an orphan.
	_, _, _, wvs := discoverWorkingFiles(m.entries, ".")
	m.parseVs = append(m.parseVs, wvs...)
	return m.validate()
}

func renderInitBacklog(project string, today Date, g IDGrammar) string {
	fm := "---\ndoc: backlog\nversion: 1\nproject: " + project +
		"\nnext_id: " + string(g.NewID(1))
	// Keys are written only when the value is not the default, so an init that
	// declares nothing stays byte-identical to spec version 1 (§3.3.2 rule 6).
	// Key order follows the §5.1 schema: next_id, then the two optional keys.
	if g.Prefix != "T" {
		fm += "\nid_prefix: " + g.Prefix
	}
	if g.Width != 4 {
		fm += "\nid_width: " + strconv.Itoa(g.Width)
	}
	fm += "\nupdated: " + today.String()
	return fm + `
---

# Backlog

Everything not started. See [structure.md](structure.md) for the line format.

` + "`## Ready`" + ` is ordered — the top of the list is what gets picked next.

## Ready

## Blocked

## Someday
`
}

// renderInitDone is done.md, shared by version 1 and version 2 - the two
// differ only in the declared version, everything else about a fresh,
// month-group-free done.md is identical either way.
func renderInitDone(today Date, version int) string {
	return "---\ndoc: done\nversion: " + strconv.Itoa(version) + "\nupdated: " + today.String() + `
---

# Done

Closed items, newest month first, newest item first within a month. Cancelled
work stays here too, with ` + "`outcome:cancelled`" + ` — it is recorded, never deleted.
`
}

// initWorking is an idle slot.
//
// It deliberately carries no "nothing in progress" prose: an operation that
// fills the frontmatter does not rewrite a person's text, so a sentence like
// that would still be sitting there claiming the slot was free.
const initWorking = `---
doc: working
version: 1
status: idle
id: null
title: null
prio: null
tags: null
detail: null
created: null
started: null
---

# Working

## Task

## Plan

## Notes

## Blockers
`

// initTemplate is details/_template.md. A leading underscore marks it a
// template: exempt from I9, and never any item's detail file.
const initTemplate = `---
doc: detail
id: T-XXXX
title: Copy this file to details/<ID>.md and match id + title to the item line
---

# T-XXXX — Title goes here

## Context

Why this exists. What is broken, what is missing, what prompted it.

## Requirements

What has to be true when this is done. Concrete enough to check.

## Open questions

Things you do not know yet. Delete them as they resolve, or promote them to
requirements.

## References

Links, file paths, prior art, related items by ID.
`

func renderInitStructure(project string, today Date) string {
	return "---\ndoc: structure\nversion: 1\nupdated: " + today.String() + `
---

# ` + project + `

A todo directory in plain Markdown. Every file is readable in any editor and
parseable with a handful of regexes — a tool is faster than editing by hand, but
nothing here needs one.

## Files

| File | Holds |
|---|---|
| ` + "`backlog.md`" + ` | Everything not started. |
| ` + "`working.NN.md`" + ` | One in-progress item each, with its notes. |
| ` + "`done.md`" + ` | Everything finished or cancelled, newest first. |
| ` + "`details/T-NNNN.md`" + ` | Long-form description for one item. |

An item lives in **exactly one** of those places at a time. Moving it is
cut-and-paste, never a copy. Detail files are the exception: they never move, so
the long text survives every transition.

## The item line

` + "```" + `
- [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
` + "```" + `

- Box — a space for open, ` + "`x`" + ` for closed. Open lines only in
  ` + "`backlog.md`" + `, closed lines only in ` + "`done.md`" + `.
- ID — ` + "`T-`" + ` plus four digits. Permanent: never reused, never renumbered.
- Title — one line, and it must not contain ` + "`|`" + `.
- Fields — ` + "` | `" + ` separated ` + "`key:value`" + ` pairs. Order does not matter, and
  unknown keys are legal and must be preserved when an item moves.

| Key | Values |
|---|---|
| ` + "`prio`" + ` | ` + "`high`" + ` ` + "`med`" + ` ` + "`low`" + ` — absent means ` + "`med`" + ` |
| ` + "`tags`" + ` | comma separated, no spaces |
| ` + "`created`" + ` ` + "`started`" + ` ` + "`done`" + ` | ` + "`YYYY-MM-DD`" + ` |
| ` + "`outcome`" + ` | ` + "`shipped`" + ` ` + "`cancelled`" + ` ` + "`obsolete`" + ` — required in done.md |
| ` + "`blocked`" + ` | free text — required in ` + "`## Blocked`" + `, forbidden elsewhere |
| ` + "`detail`" + ` | ` + "`details/T-0042.md`" + ` — must match the item's own ID |

## backlog.md

Frontmatter carries ` + "`project`" + ` and ` + "`next_id`" + `, the ID to hand out next. Three
sections, all required even when empty:

` + "```" + `
## Ready      order is meaningful; the top is what you pick next
## Blocked    every item here needs a blocked: field
## Someday    not committed to
` + "```" + `

## working.NN.md

**The number of these files is the WIP limit.** Nothing declares it; each file
holds at most one item, so an item can only be started when a file is free.

The item lives in the frontmatter, not as an item line, and uses the same
lexical form for every value — ` + "`tags: infra,ci`" + `, not a YAML list. When
` + "`status: idle`" + `, every item field is ` + "`null`" + `.

Body sections ` + "`## Task`" + `, ` + "`## Plan`" + `, ` + "`## Notes`" + ` and ` + "`## Blockers`" + ` are always
present. ` + "`- [ ]`" + ` lines here are **subtasks, not items** — no IDs, and they are
discarded when the item leaves.

## done.md

Items grouped under ` + "`## YYYY-MM`" + ` headings, newest month first, newest item
first within a month. Every line closed, with ` + "`done:`" + ` and ` + "`outcome:`" + `.

## Operations by hand

**Add** — read ` + "`next_id`" + `, append the line to the bottom of ` + "`## Ready`" + ` with
today's ` + "`created:`" + `, increment ` + "`next_id`" + `.

**Start** — find an idle working file. *If every slot is occupied, stop* — that
is the WIP limit doing its job; do not add a file to make room. Copy the fields
into the frontmatter, set ` + "`status: working`" + ` and ` + "`started:`" + `, then remove the
backlog line. Write the working file first: a crash between the two duplicates
the item rather than losing it.

**Pause** — write the line back at the top of ` + "`## Ready`" + `, keeping ` + "`started:`" + `.
Move anything worth keeping out of ` + "`## Notes`" + ` into the detail file — it exists
nowhere else. Reset the slot to idle.

**Finish** — write the line at the top of the current month group in
` + "`done.md`" + ` as ` + "`- [x]`" + ` with ` + "`done:`" + ` and ` + "`outcome:`" + `, preserving every other
field. Works straight from the backlog too; ` + "`outcome:cancelled`" + ` is how work is
abandoned without deleting it.

## The rules

1. An ID appears in exactly one file. Never copy an item — move it.
2. Every ID is below ` + "`next_id`" + `. Never reuse or renumber an ID.
3. ` + "`backlog.md`" + ` holds only open boxes; ` + "`done.md`" + ` only closed ones.
4. A working file is ` + "`working`" + ` with ` + "`id`" + `, ` + "`title`" + ` and ` + "`started`" + ` set, or
   ` + "`idle`" + ` with every item field ` + "`null`" + `.
5. Every item under ` + "`## Blocked`" + ` has a ` + "`blocked:`" + ` field; no other one does.
6. Every item in ` + "`done.md`" + ` has ` + "`done:`" + ` and ` + "`outcome:`" + `, under the month
   heading its date names.
7. Dates are real calendar dates, tags have no spaces, no value contains ` + "`|`" + `.
8. Every ` + "`detail:`" + ` path is ` + "`details/<that item's ID>.md`" + ` and exists.
9. Every non-` + "`_`" + ` file in ` + "`details/`" + ` is referenced by exactly one item, with
   matching ` + "`id`" + ` and ` + "`title`" + `.
10. Working files share a digit width and are numbered 1..N with no gaps.
`
}
