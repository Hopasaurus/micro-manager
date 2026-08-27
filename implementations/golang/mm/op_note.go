package mm

import "fmt"

// NoteRequest appends a note to an item (spec-tools.md §5.2, `--note`).
type NoteRequest struct {
	Text string

	// Undated writes the text as given. The default stamps the date, because
	// notes accumulate over days and an undated pile of them cannot be read
	// back as a history.
	Undated bool

	DryRun bool
}

// Note appends a dated entry to an item's notes.
//
// WHERE the note lands depends on where the item is, and that is the whole
// design: a working item's notes belong in its slot, next to the work, where
// --pause and --finish already know to preserve them. An item that is not in a
// slot has no such place, so the note goes to its detail file — created if
// necessary, since the alternative is refusing to record something the user
// has already typed.
//
// This is the highest-frequency write in daily use, which is why it is one
// operation rather than "edit the file yourself".
func (s *Store) Note(id ID, req NoteRequest, today Date) (Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	if req.Text == "" {
		return zero, TxResult{}, fmt.Errorf("%w: a note needs some text", ErrInvalidArgument)
	}

	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}

	text := req.Text
	if !req.Undated {
		text = "**" + today.String() + "** — " + text
	}

	if it.State == StateWorking {
		w := t.slotOf(id)
		if w == nil {
			return zero, TxResult{}, fmt.Errorf(
				"%w: %s claims to be working but is in no slot", ErrConflict, id)
		}
		e := t.working(w)
		appendUnderHeading(e, w.FM, "Notes", text)
		touchUpdated(e, today)
		t.stage(w.Name)
		t.record(Change{Kind: ChangeUpdated, ID: id, File: w.Name, After: text})
	} else {
		// Captured before preserveNotes, which may attach a detail: field
		// this item did not carry before (T-0253: writeItemLine below stages
		// the write but records no Change of its own - the audit log's only
		// way to see that field actually changed is a Before/After taken
		// around both calls, the same shape updateInternal uses for --edit).
		before := RenderItemLine(it)

		// preserveNotes is the same path --pause and --finish use, so a note
		// written before a pause and one written by the pause end up in the same
		// place, in the same shape.
		if err := t.preserveNotes(it, text, today); err != nil {
			return zero, TxResult{}, err
		}
		// preserveNotes may have attached a detail file, which changes the line.
		if it.Detail != "" {
			file, err := t.writeItemLine(it, today)
			if err != nil {
				return zero, TxResult{}, err
			}
			t.record(Change{Kind: ChangeUpdated, ID: id, File: file,
				Before: before, After: RenderItemLine(it)})
		}
	}

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}
