package mm

import "fmt"

// FinishRequest closes an item (spec-tools.md §5.1.10).
type FinishRequest struct {
	// Outcome defaults to shipped. cancelled and obsolete are how work is
	// abandoned: the format keeps closed work in done.md precisely so that
	// giving up leaves a record instead of a gap.
	Outcome Outcome

	// Done defaults to today. It decides which ## YYYY-MM group the item lands
	// in, so backdating a closure files it under the month it actually happened.
	Done Date

	// Note appends a closing note to the detail file, creating it if needed.
	Note string

	// DiscardNotes throws away a slot's ## Notes rather than preserving them.
	// This is the LAST moment they exist, so the default is to keep them.
	DiscardNotes bool

	DryRun bool
}

// Finish moves an item into done.md.
//
// It works from a working slot AND from the backlog directly: closing something
// that was never started is normal, and --outcome cancelled from the backlog is
// the supported way to abandon work without deleting it.
func (s *Store) Finish(id ID, req FinishRequest, today Date) (Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	outcome := req.Outcome
	if outcome == OutcomeNone {
		outcome = OutcomeShipped
	}
	if _, err := ParseOutcome(string(outcome)); err != nil {
		return zero, TxResult{}, err
	}
	when := req.Done
	if when.IsZero() {
		when = today
	}
	if !when.Valid() {
		return zero, TxResult{}, fmt.Errorf(
			"%w: done:%04d-%02d-%02d is not a real date", ErrInvalidArgument,
			when.Year, when.Month, when.Day)
	}

	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.State == StateDone {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s was already closed on %s; edit it instead of finishing it twice",
			ErrConflict, id, it.Done)
	}

	d, de, err := t.done()
	if err != nil {
		return zero, TxResult{}, err
	}

	// Collect what would otherwise be lost before anything is cleared.
	var w *workingFile
	var we *fileEdit
	notes := ""
	if it.State == StateWorking {
		if w = t.slotOf(id); w == nil {
			return zero, TxResult{}, fmt.Errorf(
				"%w: %s claims to be working but is in no slot", ErrConflict, id)
		}
		we = t.working(w)
		if !req.DiscardNotes {
			notes = sectionBody(we, w.FM, "Notes")
		}
	}
	if req.Note != "" {
		if notes != "" {
			notes += "\n\n"
		}
		notes += req.Note
	}
	if notes != "" {
		if err := t.preserveNotes(it, notes, today); err != nil {
			return zero, TxResult{}, err
		}
	}

	before := RenderItemLine(it)
	origin := "backlog.md"

	switch it.State {
	case StateBacklog:
		b, be, err := t.backlog()
		if err != nil {
			return zero, TxResult{}, err
		}
		b.RemoveItem(be, it)
		touchUpdated(be, today)
	case StateWorking:
		origin = w.Name
	}

	it.Done = when
	it.Outcome = outcome
	it.Slot = 0
	it.Section = SectionNone

	// InsertItem sets State to done, which is what flips the box to [x]: the box
	// is a function of the file an item lives in, not a field of its own.
	d.InsertItem(de, when.Month7(), it)
	touchUpdated(de, today)

	if w != nil {
		resetSlot(we, w.FM)
		touchUpdated(we, today)
		w.Item = nil
	}

	// done.md first, then the file the item came from: the copy exists before
	// the original goes away (spec-tools.md §7 rule 4).
	t.stage("done.md", origin)
	t.record(Change{Kind: ChangeMoved, ID: id, File: "done.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}
