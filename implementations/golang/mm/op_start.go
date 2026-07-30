package mm

import "fmt"

// StartRequest moves a backlog item into a working slot (spec-tools.md §5.1.8).
type StartRequest struct {
	// Slot names a slot explicitly. Zero means the LOWEST-NUMBERED IDLE SLOT,
	// which is what the format recommends: slots are interchangeable, nothing
	// depends on which one an item occupies (spec-file-format.md §5.2.1), so
	// choosing between them is not the user's problem.
	Slot int

	DryRun bool
}

// Start moves an item from backlog.md into a working slot.
//
// The item's fields are COPIED, never converted: the format gives an item line
// and a slot's frontmatter the same lexical form for every value
// (spec-file-format.md §5.2.2), so tags:infra,ci stays exactly that, and an
// unregistered field survives the round trip through the slot.
//
// One field does not travel: blocked:. It is meaningful only under ## Blocked,
// where I5 requires it, and carrying it into a slot would put it back on the line
// at --pause time in ## Ready, which I5 forbids. The reason is not discarded -
// it is seeded into the slot's ## Blockers section, where it is still in front of
// whoever picks the work up.
func (s *Store) Start(id ID, req StartRequest, today Date) (Item, txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	t, err := s.begin()
	if err != nil {
		return zero, txResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, txResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	switch it.State {
	case StateBacklog:
	case StateWorking:
		return zero, txResult{}, fmt.Errorf(
			"%w: %s is already in slot %d; pause or finish it instead", ErrConflict, id, it.Slot)
	default:
		return zero, txResult{}, fmt.Errorf(
			"%w: %s is done, and done work does not go back into a slot", ErrConflict, id)
	}

	// §9 reserves these keys, so an item line carrying one as an unregistered
	// field cannot be represented in a slot's frontmatter. Refusing is the only
	// honest option: writing it would collide with the file's own key, and
	// dropping it would destroy the field this whole path exists to preserve.
	for _, f := range it.Extra {
		if reservedKey(f.Key) {
			return zero, txResult{}, fmt.Errorf(
				"%w: %s carries a reserved field %q, which a working file cannot hold; "+
					"rename or remove it first", ErrInvalidArgument, id, f.Key)
		}
	}

	w, err := t.pickSlot(req.Slot)
	if err != nil {
		return zero, txResult{}, err
	}
	b, be, err := t.backlog()
	if err != nil {
		return zero, txResult{}, err
	}

	before := RenderItemLine(it)
	blocked := it.Blocked

	// Remove from the backlog model FIRST: RemoveItem works off the item's
	// recorded line, which is about to be replaced by its position in the slot.
	b.RemoveItem(be, it)
	touchUpdated(be, today)

	it.State = StateWorking
	it.Section = SectionNone
	it.Slot = w.Number
	it.Started = today
	it.Blocked = ""

	e := t.working(w)
	for _, f := range workingFields(it) {
		e.SetFM(f.Key, f.Value)
	}
	seedTask(e, w.FM, it)
	if blocked != "" {
		appendUnderHeading(e, w.FM, "Blockers", blocked)
	}
	touchUpdated(e, today)
	it.Source = Location{File: w.Name, Line: w.FM.Line("id")}
	w.Item = it

	// ORDER IS THE DURABILITY STRATEGY (spec-tools.md §7 rule 4): the slot is
	// written before the backlog line is removed, so a crash between the two
	// leaves a duplicate ID - which I1 reports - instead of losing the item.
	t.stage(w.Name, "backlog.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: w.Name,
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// pickSlot resolves the destination slot.
//
// THE WIP LIMIT IS ENFORCED HERE AND NOWHERE ELSE, because the limit is the
// number of working files and this is the only code that puts an item into one.
// It must never create a file to make room: that would raise the limit silently,
// which is the single thing the limit exists to prevent (spec-tools.md §5.1.8).
func (t *tx) pickSlot(want int) (*workingFile, error) {
	if len(t.model.working) == 0 {
		return nil, fmt.Errorf(
			"%w: this directory has no working.NN.md file to start into", ErrNotFound)
	}

	if want > 0 {
		for _, w := range t.model.working {
			if w.Number != want {
				continue
			}
			if !w.Idle() {
				return nil, fmt.Errorf("%w: slot %0*d already holds %s (%s)",
					ErrPreconditionFailed, w.Width, w.Number, w.Item.ID, w.Item.Title)
			}
			return w, nil
		}
		return nil, fmt.Errorf("%w: there is no slot %d; this directory has %d",
			ErrPreconditionFailed, want, len(t.model.working))
	}

	for _, w := range t.model.working {
		if w.Idle() {
			return w, nil // the set is sorted by number, so this is the lowest
		}
	}
	return nil, &WipLimitError{Limit: len(t.model.working), Occupants: t.model.slots()}
}

// seedTask writes the title, and a link to the detail file when there is one,
// into the slot's ## Task section. Every other body section is left empty:
// ## Plan and ## Notes are the user's scratch space, not the tool's.
func seedTask(e *fileEdit, fm *Frontmatter, it *Item) {
	text := it.Title
	if it.Detail != "" {
		text += "\n\nSee [" + it.Detail + "](" + it.Detail + ")."
	}
	appendUnderHeading(e, fm, "Task", text)
}
