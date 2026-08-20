package mm

import "fmt"

// PauseRequest returns a working item to the backlog (spec-tools.md §5.1.9).
type PauseRequest struct {
	// Section defaults to Ready. Pausing into Blocked needs a reason, because
	// I5 ties the field to the section. Version 1 only.
	Section Section
	Blocked string

	// Stage is the version-2 destination (§5.1.1); defaults to "ready".
	Stage Stage

	// End appends instead of inserting at the top.
	//
	// THE DEFAULT IS THE TOP, which is the opposite of --add. A paused item is
	// usually the next thing you will pick up, not the last; the CLI's --top
	// switch says so explicitly and changes nothing.
	End bool

	// DiscardNotes throws away the slot's ## Notes instead of preserving them.
	//
	// The default is to preserve, because those notes exist NOWHERE ELSE: the
	// moment an item leaves its slot is the last moment they exist at all
	// (spec-tools.md §5.1.9). Discarding has to be asked for.
	DiscardNotes bool

	DryRun bool
}

// Pause moves an item from a working slot back to the backlog.
//
// Fields are preserved, started: included - it records when the work began, not
// when it was last picked up, and a paused item that comes back has not started
// over. Subtasks in ## Plan are discarded, which is what they are for.
func (s *Store) Pause(id ID, req PauseRequest, today Date) (Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if t.model.isV2() {
		return s.pauseV2(t, id, req, today)
	}
	if it.State != StateWorking {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s is %s, not in a working slot; there is nothing to pause",
			ErrConflict, id, it.State)
	}
	w := t.slotOf(id)
	if w == nil {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s claims to be working but is in no slot", ErrConflict, id)
	}

	sec := req.Section
	if sec == "" {
		sec = SectionReady
	}
	if _, err := ParseSection(string(sec)); err != nil {
		return zero, TxResult{}, err
	}
	if sec == SectionBlocked && req.Blocked == "" {
		return zero, TxResult{}, fmt.Errorf(
			"%w: pausing %s into Blocked needs a reason (--blocked)", ErrInvalidArgument, id)
	}
	if sec != SectionBlocked && req.Blocked != "" {
		return zero, TxResult{}, fmt.Errorf(
			"%w: a blocked: reason only belongs in Blocked, not %s", ErrInvalidArgument, sec)
	}

	b, be, err := t.backlog()
	if err != nil {
		return zero, TxResult{}, err
	}
	if b.Section(sec) == nil {
		return zero, TxResult{}, fmt.Errorf("%w: backlog.md has no ## %s section", ErrNotFound, sec)
	}

	e := t.working(w)
	notes := sectionBody(e, w.FM, "Notes")
	if notes != "" && !req.DiscardNotes {
		if err := t.preserveNotes(it, notes, today); err != nil {
			return zero, TxResult{}, err
		}
	}

	it.Blocked = req.Blocked
	it.Slot = 0
	index := 0
	if req.End {
		index = len(sectionItems(b, sec))
	}
	b.InsertItem(be, sec, index, it) // sets State, Section and Source
	touchUpdated(be, today)

	resetSlot(e, w.FM)
	touchUpdated(e, today)
	w.Item = nil

	// Backlog first, then the slot: the copy is written before the original is
	// cleared, so a crash between them duplicates the item - which I1 reports -
	// rather than losing it (spec-tools.md §7 rule 4).
	t.stage("backlog.md", w.Name)
	t.record(Change{Kind: ChangeMoved, ID: id, File: "backlog.md",
		After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// resetSlot returns a working file to status: idle.
//
// Every item field goes to null, and every key the item BROUGHT WITH IT is
// removed - an unregistered field left behind would be inherited by the next
// item to occupy the file, which is worse than losing it. The four body sections
// are emptied: subtasks are scratch, and the rest belonged to the item that just
// left.
func resetSlot(e *fileEdit, fm *Frontmatter) {
	e.SetFM("status", "idle")
	for _, key := range workingItemKeys {
		e.SetFM(key, "null")
	}
	for _, key := range fm.Keys() {
		if isWorkingItemField(key) && !isRegisteredWorkingField(key) {
			e.DeleteFM(key)
		}
	}
	for _, heading := range []string{"Task", "Plan", "Notes", "Blockers"} {
		clearSection(e, fm, heading)
	}
}

// preserveNotes appends a slot's ## Notes to the item's detail file, creating
// that file when the item has none.
//
// Creating it is not an optional nicety: the notes have no other home, and
// spec-tools.md §5.1.9 forbids discarding them silently.
func (t *tx) preserveNotes(it *Item, notes string, today Date) error {
	if it.Detail == "" {
		path := it.DetailPath()
		if _, exists := t.model.details[path]; exists {
			// An orphan already sits where this item's detail file belongs.
			// Adopting it could contradict I9 in ways the user did not ask for,
			// and overwriting it would destroy whatever it holds.
			return fmt.Errorf(
				"%w: %s has notes to preserve but %s already exists with no owner; "+
					"attach it to the item first, or pause with DiscardNotes",
				ErrConflict, it.ID, path)
		}
		content := renderDetailFile(it, "## Notes\n\n"+notes+"\n", today)
		it.Detail = path
		t.stageRaw(path, []byte(content))
		t.record(Change{Kind: ChangeCreated, ID: it.ID, File: path})
		// Validation reads this map, so the new file has to be in it or I8
		// reports the detail: field it just created as dangling.
		t.model.details[path] = mustParseDetail(path, content)
		return nil
	}

	df, ok := t.model.details[it.Detail]
	if !ok {
		return fmt.Errorf("%w: %s references %s, which does not exist",
			ErrNotFound, it.ID, it.Detail)
	}
	e := t.detailEdit(df)
	appendUnderHeading(e, df.FM, "Notes", notes)
	if df.FM.Has("updated") {
		e.SetFM("updated", today.String())
	}
	if e.Dirty() {
		t.stageRaw(df.Name, e.Bytes())
		t.record(Change{Kind: ChangeUpdated, ID: it.ID, File: df.Name})
		t.model.details[df.Name] = mustParseDetail(df.Name, string(e.Bytes()))
	}
	return nil
}
