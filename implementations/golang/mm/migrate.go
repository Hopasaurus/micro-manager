package mm

import (
	"fmt"
	"maps"
	"path/filepath"
	"strconv"
	"strings"
)

// The versioned, chained migration mechanism (spec-tools.md §5.3.4).
//
// This supersedes version 1 of that document's description of --migrate as a
// grab-bag of small, unversioned historical fixups (op_migrate.go, T-0044,
// which stays exactly as it was: it repairs shapes older than any version
// number this format has ever declared, and is orthogonal to the version
// chain). This file is what the current spec calls --migrate: a directory's
// declared version determines where it enters a registered, ordered list of
// steps, and migration walks the chain to the target version.
//
// Named MigrateVersion rather than Migrate to keep both call sites addressable
// without a collision; the spec's pseudocode names the concept, not a Go
// identifier ("parameter passing style is not [normative]", §6).

// MigrationStep is one registered link in the chain: (from, to) plus the
// transform it runs. An implementation adds a step the day a new incompatible
// format version ships; it never replaces or renumbers an existing one
// (spec-tools.md §5.3.4).
type MigrationStep struct {
	From, To int
}

// migrationChain is the registered chain, in order. Exactly one link exists
// today - this document's own 1->2 transition (spec-file-format.md Appendix
// C) - so the multi-step case (walking two links in one run, each its own
// transaction per §5.3.4) is exercised only up to N=1 for now; a second entry
// will need the loop below to prove itself against a real two-hop directory.
var migrationChain = []MigrationStep{
	{From: 1, To: 2},
}

// latestVersion is the highest version this build can migrate to.
func latestVersion() int {
	v := 0
	for _, s := range migrationChain {
		if s.To > v {
			v = s.To
		}
	}
	return v
}

// stepFrom finds the registered step whose From matches, or nil.
func stepFrom(from int) *MigrationStep {
	for i := range migrationChain {
		if migrationChain[i].From == from {
			return &migrationChain[i]
		}
	}
	return nil
}

// MigrationResult reports one step's outcome (spec-tools.md §6.1).
type MigrationResult struct {
	From, To int
	Changes  []Change
	Warnings []string
}

// MigrateVersionRequest configures a --migrate run.
type MigrateVersionRequest struct {
	// To targets a specific version instead of the latest this build
	// implements. Zero means "latest" (spec-tools.md §5.3.4).
	To int

	DryRun bool
}

// MigrateVersion brings a directory to the current (or a named) format
// version by applying every step in the chain between its current version
// and the target, each step its own transaction (spec-tools.md §5.3.4).
//
// The git-uncommitted-changes warning §5.3.4 also asks for is NOT produced
// here: the library does not run programs (git included — see
// TestLibraryImports), so a front end wrapping this in a --migrate command
// owns that check, the same way internal/cli owns launching $EDITOR. This is
// a gap against the spec text until that wrapping lands (tracked as
// follow-up work alongside the CLI/GUI/TUI surface, T-0231's M4 remainder).
func (s *Store) MigrateVersion(req MigrateVersionRequest, today Date) ([]MigrationResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return nil, err
	}
	vs := m.validate()
	if markers := conflictMarkerFindings(vs); len(markers) > 0 {
		return nil, &InvariantError{Violations: markers}
	}

	var from int
	switch {
	case m.board != nil:
		from = 2
	case m.backlog != nil:
		from = 1
	default:
		return nil, fmt.Errorf("%w: this directory has neither board.md nor backlog.md; nothing to migrate",
			ErrNotFound)
	}

	latest := latestVersion()
	to := req.To
	if to == 0 {
		to = latest
	}
	if to > latest {
		return nil, fmt.Errorf("%w: version %d is newer than this build supports (latest %d)",
			ErrInvalidArgument, to, latest)
	}
	if to < from {
		return nil, fmt.Errorf("%w: --to %d is older than this directory's current version %d",
			ErrInvalidArgument, to, from)
	}
	if to == from {
		return nil, fmt.Errorf("%w: this directory is already at version %d", ErrConflict, from)
	}

	var results []MigrationResult
	for cur := from; cur < to; {
		step := stepFrom(cur)
		if step == nil {
			return results, fmt.Errorf("%w: no migration step is registered from version %d",
				ErrInvalidArgument, cur)
		}
		res, err := s.runMigrationStep(*step, req.DryRun, today)
		if err != nil {
			return results, err
		}
		results = append(results, res)
		cur = step.To
	}
	return results, nil
}

// runMigrationStep dispatches to one step's transform. s.mu is already held
// by MigrateVersion.
func (s *Store) runMigrationStep(step MigrationStep, dryRun bool, today Date) (MigrationResult, error) {
	switch step {
	case MigrationStep{From: 1, To: 2}:
		return s.migrateOneToTwo(dryRun, today)
	}
	return MigrationResult{}, fmt.Errorf("%w: step %d->%d is registered but has no transform",
		ErrInvalidArgument, step.From, step.To)
}

// migrateOneToTwo is the 1->2 step: backlog.md + working.NN.md fold into
// board.md (spec-file-format.md Appendix C).
func (s *Store) migrateOneToTwo(dryRun bool, today Date) (MigrationResult, error) {
	res := MigrationResult{From: 1, To: 2}

	m, err := s.load()
	if err != nil {
		return res, err
	}
	if m.backlog == nil {
		return res, fmt.Errorf("%w: backlog.md is missing; version 1 requires it", ErrNotFound)
	}
	res.Changes = append(res.Changes, Change{
		Kind: ChangeMoved, File: "board.md", Before: "backlog.md", After: "board.md",
	})

	// ---- board.md frontmatter: doc/version rewritten, everything else from
	// backlog.md carried through verbatim (§9: unknown keys are valid and
	// must not be dropped), plus wip.working - version 1's structural WIP
	// limit (the working-file count) made explicit, since version 2 has no
	// implicit equivalent and leaving it out would silently uncap the stage.
	boardFM := NewFrontmatter()
	boardFM.Set("doc", "board")
	boardFM.Set("version", "2")
	for _, k := range m.backlog.FM.Keys() {
		switch k {
		case "doc", "version":
			continue
		case "updated":
			boardFM.Set("updated", today.String())
		default:
			boardFM.Set(k, m.backlog.FM.Get(k))
		}
	}
	if !boardFM.Has("updated") {
		boardFM.Set("updated", today.String())
	}
	boardFM.Set("wip.working", strconv.Itoa(len(m.working)))

	// ---- items: someday, ready, blocked (DefaultStages order), then
	// working - each existing item copied, never converted in place, so a
	// bug here cannot corrupt the source the operation is reading from.
	sectionStage := map[Section]Stage{
		SectionReady: "ready", SectionBlocked: "blocked", SectionSomeday: "someday",
	}
	var boardItems []*Item
	addFromSection := func(sec Section) {
		span := m.backlog.Section(sec)
		if span == nil {
			return
		}
		for _, it := range span.Items {
			nit := *it
			nit.State = StateBoard
			nit.Stage = sectionStage[sec]
			nit.Section = SectionNone
			nit.Reason = nit.Blocked
			nit.Blocked = ""
			nit.Pos = 0
			res.Changes = append(res.Changes, Change{
				Kind: ChangeMoved, ID: nit.ID, File: "board.md",
				Before: RenderItemLine(it), After: RenderItemLine(&nit),
			})
			boardItems = append(boardItems, &nit)
		}
	}
	addFromSection(SectionSomeday)
	addFromSection(SectionReady)
	addFromSection(SectionBlocked)

	// working.NN.md items fold in as stage:working, in slot order
	// (discoverWorkingFiles already sorts m.working by number).
	newDetails := map[string]string{}     // path -> full file content, newly created
	detailEdits := map[string]*fileEdit{} // path -> editor, existing file being appended to
	for _, w := range m.working {
		if w.Item == nil {
			continue // an idle slot carries no item to fold in
		}
		it := w.Item
		nit := *it
		nit.State = StateBoard
		nit.Stage = "working"
		nit.Slot = 0
		nit.Reason = nit.Blocked // always "" in practice: a working item never carries blocked:
		nit.Blocked = ""
		nit.Pos = 0

		if err := foldWorkingBody(m, w, &nit, today, newDetails, detailEdits, &res); err != nil {
			return res, err
		}

		res.Changes = append(res.Changes, Change{
			Kind: ChangeMoved, ID: nit.ID, File: "board.md",
			Before: RenderItemLine(it), After: RenderItemLine(&nit),
		})
		boardItems = append(boardItems, &nit)
	}

	var body strings.Builder
	body.WriteString("\n# Board\n\n")
	for _, it := range boardItems {
		body.WriteString(RenderItemLine(it))
		body.WriteString("\n")
	}
	boardContent := boardFM.Render() + body.String()

	newBoard, bvs := parseBoard("board.md", []byte(boardContent))
	if len(bvs) > 0 {
		return res, &InvariantError{Violations: bvs}
	}

	// ---- done.md: version bumped in place, one line, nothing else changes.
	var newDone *doneFile
	var doneEdit *fileEdit
	if m.done != nil {
		doneEdit = m.done.Edit()
		doneEdit.SetFM("version", "2")
		touchUpdated(doneEdit, today)
		if doneEdit.Dirty() {
			res.Changes = append(res.Changes, Change{Kind: ChangeUpdated, File: "done.md"})
			var dvs []Violation
			newDone, dvs = parseDoneG("done.md", doneEdit.Bytes(), newBoard.grammar)
			if len(dvs) > 0 {
				return res, &InvariantError{Violations: dvs}
			}
		} else {
			newDone = m.done
		}
	}

	// ---- validate the resulting version-2 model before writing anything.
	finalDetails := maps.Clone(m.details)
	for path, content := range newDetails {
		finalDetails[path] = mustParseDetail(path, content)
	}
	for path, e := range detailEdits {
		finalDetails[path] = mustParseDetail(path, string(e.Bytes()))
	}
	target := &dirModel{
		path:    m.path,
		board:   newBoard,
		done:    newDone,
		details: finalDetails,
	}
	if vs := target.validate(); len(vs) > 0 {
		return res, &InvariantError{Violations: vs}
	}

	if dryRun {
		return res, nil
	}

	var ws writeSet
	boardPath := filepath.Join(s.path, "board.md")
	ws.Add(boardPath, []byte(boardContent), stampOf(boardPath))
	if doneEdit != nil && doneEdit.Dirty() {
		donePath := filepath.Join(s.path, "done.md")
		ws.Add(donePath, doneEdit.Bytes(), stampOf(donePath))
	}
	for path, content := range newDetails {
		p := filepath.Join(s.path, path)
		ws.Add(p, []byte(content), stampOf(p))
	}
	for path, e := range detailEdits {
		p := filepath.Join(s.path, path)
		ws.Add(p, e.Bytes(), stampOf(p))
	}
	ws.Delete(filepath.Join(s.path, "backlog.md"))
	for _, w := range m.working {
		ws.Delete(filepath.Join(s.path, w.Name))
		res.Changes = append(res.Changes, Change{Kind: ChangeDeleted, File: w.Name})
	}

	if err := ws.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// foldWorkingBody moves a working file's ## Task/## Plan/## Notes/##
// Blockers content into the item's detail file (Appendix C), creating one
// when it has none. Empty sections contribute nothing - an idle slot's
// boilerplate headings must not manufacture a detail file out of nothing.
func foldWorkingBody(m *dirModel, w *workingFile, nit *Item, today Date,
	newDetails map[string]string, detailEdits map[string]*fileEdit, res *MigrationResult) error {

	e := w.Edit()
	headings := []string{"Task", "Plan", "Notes", "Blockers"}
	sections := map[string]string{}
	any := false
	for _, h := range headings {
		if text := sectionBody(e, w.FM, h); text != "" {
			sections[h] = text
			any = true
		}
	}
	if !any {
		return nil
	}

	if nit.Detail == "" {
		path := nit.DetailPath()
		if _, exists := m.details[path]; exists {
			return fmt.Errorf(
				"%w: %s has working-file notes to fold in but %s already exists with no owner; "+
					"adopt or remove it before migrating",
				ErrConflict, nit.ID, path)
		}
		var parts []string
		for _, h := range headings {
			if text, ok := sections[h]; ok {
				parts = append(parts, "## "+h+"\n\n"+text)
			}
		}
		newDetails[path] = renderDetailFile(nit, strings.Join(parts, "\n\n"), today)
		nit.Detail = path
		res.Changes = append(res.Changes, Change{Kind: ChangeCreated, ID: nit.ID, File: path})
		return nil
	}

	df, ok := m.details[nit.Detail]
	if !ok {
		return fmt.Errorf("%w: %s references %s, which does not exist", ErrNotFound, nit.ID, nit.Detail)
	}
	de, ok := detailEdits[df.Name]
	if !ok {
		de = newFileEdit(df.Name, df.Lines, df.FM)
		detailEdits[df.Name] = de
	}
	for _, h := range headings {
		if text, ok := sections[h]; ok {
			appendUnderHeading(de, df.FM, h, text)
		}
	}
	if df.FM.Has("updated") {
		de.SetFM("updated", today.String())
	}
	res.Changes = append(res.Changes, Change{Kind: ChangeUpdated, ID: nit.ID, File: df.Name})
	return nil
}
