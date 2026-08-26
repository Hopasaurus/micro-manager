package mm

import (
	"fmt"
	"strconv"
	"strings"
)

// board.md: the version-2 replacement for backlog.md + working.NN.md
// (spec-file-format.md §5.1). Every item carries an explicit `stage:`
// field; there are no `## ` sections and no working files — the whole
// board is one flat, order-significant list of item lines.
//
// This file is additive: it introduces a new parser/model alongside the
// version-1 ones in parsefile.go/working.go, which are untouched and keep
// serving a version-1 directory exactly as before. dirModel.board is set
// instead of dirModel.backlog/working when board.md is found (store.go).

// boardFile is a parsed board.md.
type boardFile struct {
	Name  string
	FM    *Frontmatter
	Lines []string
	Items []*Item // in file order

	grammar  IDGrammar
	stageCfg StageConfig
	warnings []Violation

	edit *fileEdit
}

// StageItems returns the items currently on a stage, in file order.
func (b *boardFile) StageItems(stage Stage) []*Item {
	var out []*Item
	for _, it := range b.Items {
		if it.Stage == stage {
			out = append(out, it)
		}
	}
	return out
}

// parseBoard reads board.md. The ID grammar and stage configuration are
// read from its own frontmatter first (mirroring §3.3.2 rule 4 for
// backlog.md), then used to interpret every item line.
func parseBoard(name string, data []byte) (*boardFile, []Violation) {
	lines := splitLines(data)
	vs := markerViolations(name, lines)
	fm, body, hvs := readHeader(name, lines)
	vs = append(vs, hvs...)
	g, gvs, gwarns := ParseIDGrammar(fm)
	vs = append(vs, gvs...)
	if fm.Has("board") {
		if bv := fm.Get("board"); !validSlug(bv) {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: name, Line: fm.Line("board")},
				Message: "board must be a slug [a-z][a-z0-9-]{0,15}: " + bv,
			})
		}
	}

	cfg, cvs := parseStageConfig(name, fm)
	vs = append(vs, cvs...)

	b := &boardFile{Name: name, FM: fm, Lines: lines, grammar: g, stageCfg: cfg, warnings: gwarns}

	for i := body; i < len(lines); i++ {
		line := lines[i]
		lineNo := i + 1

		if _, ok := headingName(line); ok {
			// board.md does not use section headings (§5.1.6): any "## " line
			// is ordinary non-item content, unlike backlog.md where the
			// heading vocabulary is closed.
			continue
		}
		if !looksLikeItemLine(line) {
			continue
		}
		it, err := parseItemLineG(name, lineNo, line, g)
		if err != nil {
			vs = append(vs, violationFrom(invFormat, name, lineNo, err))
			continue
		}
		if it.Stage == "" {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s has no stage: field", it.ID),
			})
		}
		it.State = StateBoard
		it.Pos = len(b.Items) + 1
		b.Items = append(b.Items, it)
	}
	return b, vs
}

// parseStageConfig reads stages, stage_labels, wip.<slug>, tickler_stages
// and needs_reason from board.md frontmatter (§5.1.1-§5.1.5), applying each
// key's default when absent.
func parseStageConfig(name string, fm *Frontmatter) (StageConfig, []Violation) {
	var vs []Violation
	cfg := StageConfig{
		Labels:        map[Stage]string{},
		WipLimits:     map[Stage]int{},
		TicklerStages: map[Stage]Stage{},
	}

	if fm.Has("stages") {
		stages, err := parseStageList(fm.Get("stages"))
		if err != nil {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: name, Line: fm.Line("stages")},
				Message: "malformed stages: " + err.Error(),
			})
			cfg.Stages = DefaultStages()
		} else {
			cfg.Stages = stages
		}
	} else {
		cfg.Stages = DefaultStages()
	}

	member := func(key, s string) bool {
		for _, d := range cfg.Stages {
			if string(d) == s {
				return true
			}
		}
		vs = append(vs, Violation{
			Invariant: "I7", At: Location{File: name, Line: fm.Line(key)},
			Message: fmt.Sprintf("%s names stage %q, which is not in stages:", key, s),
		})
		return false
	}

	if fm.Has("stage_labels") {
		for _, pair := range splitTopLevel(fm.Get("stage_labels")) {
			slug, label, ok := strings.Cut(pair, ":")
			if !ok || !validSlug(slug) {
				vs = append(vs, Violation{
					Invariant: invFormat, At: Location{File: name, Line: fm.Line("stage_labels")},
					Message: "malformed stage_labels entry: " + pair,
				})
				continue
			}
			if member("stage_labels", slug) {
				cfg.Labels[Stage(slug)] = label
			}
		}
	}

	for _, key := range fm.Keys() {
		slug, ok := strings.CutPrefix(key, "wip.")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(fm.Get(key))
		if err != nil || n < 1 {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: name, Line: fm.Line(key)},
				Message: fmt.Sprintf("%s must be a positive integer: %s", key, fm.Get(key)),
			})
			continue
		}
		if member(key, slug) {
			cfg.WipLimits[Stage(slug)] = n
		}
	}

	if fm.Has("tickler_stages") {
		pairs, err := parseTicklerStages(fm.Get("tickler_stages"))
		if err != nil {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: name, Line: fm.Line("tickler_stages")},
				Message: "malformed tickler_stages: " + err.Error(),
			})
			cfg.TicklerStages = map[Stage]Stage{"someday": "ready"}
		} else {
			for source, dest := range pairs {
				sOK := member("tickler_stages", string(source))
				dOK := member("tickler_stages", string(dest))
				if sOK && dOK {
					cfg.TicklerStages[source] = dest
				}
			}
		}
	} else {
		cfg.TicklerStages = map[Stage]Stage{"someday": "ready"}
	}

	if fm.Has("needs_reason") {
		stages, err := parseStageList(fm.Get("needs_reason"))
		if err != nil {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: name, Line: fm.Line("needs_reason")},
				Message: "malformed needs_reason: " + err.Error(),
			})
			cfg.NeedsReason = []Stage{"blocked"}
		} else {
			for _, s := range stages {
				if member("needs_reason", string(s)) {
					cfg.NeedsReason = append(cfg.NeedsReason, s)
				}
			}
		}
	} else {
		cfg.NeedsReason = []Stage{"blocked"}
	}

	return cfg, vs
}

// splitTopLevel splits a comma-separated list. Extracted for the one place
// (stage_labels) where each entry's own grammar (slug:Label) is handled by
// the caller, unlike parseStageList and parseTicklerStages which parse each
// entry themselves.
func splitTopLevel(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// parseStageList parses a comma-separated STAGE list (stages:, needs_reason:).
func parseStageList(s string) ([]Stage, error) {
	if s == "" {
		return nil, fmt.Errorf("empty list")
	}
	parts := strings.Split(s, ",")
	out := make([]Stage, 0, len(parts))
	for _, p := range parts {
		if !validSlug(p) {
			return nil, fmt.Errorf("%q is not a SLUG", p)
		}
		out = append(out, Stage(p))
	}
	return out, nil
}

// parseTicklerStages parses tickler_stages: SOURCE->DEST,SOURCE->DEST (§5.1.4).
// Once the key is present every entry MUST be a complete pair, and a SOURCE
// MUST NOT repeat.
func parseTicklerStages(s string) (map[Stage]Stage, error) {
	if s == "" {
		return nil, fmt.Errorf("empty list")
	}
	out := map[Stage]Stage{}
	for pair := range strings.SplitSeq(s, ",") {
		source, dest, ok := strings.Cut(pair, "->")
		if !ok || !validSlug(source) || !validSlug(dest) {
			return nil, fmt.Errorf("%q is not SOURCE->DEST", pair)
		}
		if _, dup := out[Stage(source)]; dup {
			return nil, fmt.Errorf("source %q repeats", source)
		}
		out[Stage(source)] = Stage(dest)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Editing
// ---------------------------------------------------------------------------

// Edit returns a line editor over board.md, with every item registered so
// its recorded position stays accurate across splices.
func (b *boardFile) Edit() *fileEdit {
	e := newFileEdit(b.Name, b.Lines, b.FM)
	for _, it := range b.Items {
		e.track(&it.Source.Line)
	}
	b.edit = e
	return e
}

// stageRunEnd returns the 1-based line just past the last item of a stage's
// run, or the end of the file when the stage currently has no items —
// board.md items are appended to the bottom of the file in that case,
// since there is no heading to anchor to (§5.1.6).
func boardInsertPos(e *fileEdit, items []*Item, index int) int {
	if index < 0 {
		index = 0
	}
	if index > len(items) {
		index = len(items)
	}
	if len(items) == 0 {
		at := len(e.lines)
		for at > 0 && strings.TrimSpace(e.lines[at-1]) == "" {
			at--
		}
		return at + 1
	}
	if index == len(items) {
		return items[index-1].Source.Line + 1
	}
	return items[index].Source.Line
}

// InsertItem inserts an item into board.md at a 0-based index within its
// stage's own run (§5.1.6: order within a stage is the file's own order;
// stages are not otherwise grouped in the file).
func (b *boardFile) InsertItem(e *fileEdit, stage Stage, index int, it *Item) {
	it.State = StateBoard
	it.Stage = stage

	items := b.StageItems(stage)
	at := boardInsertPos(e, items, index)
	e.InsertLine(at, RenderItemLine(it))
	it.Source = Location{File: b.Name, Line: at}
	e.track(&it.Source.Line)

	// b.Items must stay in FILE order, not insertion order: StageItems (and
	// so every stage's own positioning) is a filtered view over it, computed
	// fresh on every call. A transaction that inserts more than once - a
	// bulk add, say - would otherwise see stage 2's insert positioned
	// against stage 1's items in the wrong order, because a plain append
	// puts every newly inserted item after every item this transaction has
	// not yet touched, regardless of where its line actually landed.
	idx := len(b.Items)
	for i, x := range b.Items {
		if x.Source.Line > it.Source.Line {
			idx = i
			break
		}
	}
	b.Items = append(b.Items, nil)
	copy(b.Items[idx+1:], b.Items[idx:])
	b.Items[idx] = it
	renumber(b.StageItems(stage))
}

// RemoveItem deletes an item's line from board.md and drops it from the model.
func (b *boardFile) RemoveItem(e *fileEdit, it *Item) {
	e.RemoveItem(it)
	for i, x := range b.Items {
		if x == it {
			b.Items = append(b.Items[:i], b.Items[i+1:]...)
			break
		}
	}
	renumber(b.StageItems(it.Stage))
}

// regroupBoard rewrites board.md's item lines into contiguous,
// blank-line-separated blocks in stages: order — spec-file-format.md §5.1.6
// explicitly permits physically grouping items by stage ("a writer MAY still
// group items physically... for a human reading the raw file"); this makes
// that the standing layout, reapplied by every mutation (tx.stage), rather
// than an incidental side effect of wherever InsertItem happened to land a
// line.
//
// Content between the frontmatter and the first item — the board's own
// heading and any introductory prose — is preserved verbatim. Content
// interspersed AMONG or AFTER the items is not: this fully replaces
// everything from the first item's current line to the end of the file,
// which is the one place a regroup does not round-trip byte for byte.
// Nothing today writes such content there, and §5.1.6 requires only that a
// reader ignore non-item content, not that a writer preserve its position.
func regroupBoard(b *boardFile, e *fileEdit) {
	if len(b.Items) == 0 {
		return
	}
	firstLine := b.Items[0].Source.Line
	for _, it := range b.Items[1:] {
		if it.Source.Line < firstLine {
			firstLine = it.Source.Line
		}
	}

	// Preserve the heading/prose before the first item verbatim, dropping
	// one trailing run of blank lines so the regrouped section controls its
	// own single separating blank line.
	head := append([]string(nil), e.lines[:firstLine-1]...)
	for len(head) > 0 && head[len(head)-1] == "" {
		head = head[:len(head)-1]
	}

	lines := append(head, "")
	wrote := false
	written := map[*Item]bool{}
	emit := func(items []*Item) {
		if len(items) == 0 {
			return
		}
		if wrote {
			lines = append(lines, "")
		}
		for _, it := range items {
			it.Source.Line = len(lines) + 1
			lines = append(lines, RenderItemLine(it))
			written[it] = true
		}
		wrote = true
	}
	for _, stage := range b.stageCfg.Stages {
		emit(b.StageItems(stage))
	}
	// Defensive: an item on a stage outside stages: is a pre-existing
	// violation (I4) this must preserve, not silently delete.
	var stray []*Item
	for _, it := range b.Items {
		if !written[it] {
			stray = append(stray, it)
		}
	}
	emit(stray)

	lines = append(lines, "")
	e.lines = lines
}
