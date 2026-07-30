package mm

import "fmt"

// MoveRequest repositions a backlog item (spec-tools.md §5.1.7).
//
// Exactly one destination selector must be given: Position, Top, End, Before or
// After. Section moves between sections and may be combined with a selector, in
// which case the selector is interpreted in the DESTINATION section.
type MoveRequest struct {
	Section  Section // "" keeps the current section
	Position int     // 1-based within the destination section; 0 means unset
	Top      bool
	End      bool
	Before   ID
	After    ID

	// Blocked supplies a reason when moving into ## Blocked. I5 requires one,
	// and an item that has none yet cannot be moved there without it.
	Blocked string

	DryRun bool
}

// selectors counts how many destination selectors were given.
func (r MoveRequest) selectors() int {
	n := 0
	for _, set := range []bool{r.Position > 0, r.Top, r.End, r.Before != "", r.After != ""} {
		if set {
			n++
		}
	}
	return n
}

// Move repositions an item within backlog.md.
//
// ONLY BACKLOG ITEMS MOVE. Ordering is meaningless in done.md beyond its month
// grouping, and working slots are interchangeable - so moving an item between
// slots is --start --slot, not this (spec-file-format.md §5.2.1).
func (s *Store) Move(id ID, req MoveRequest, today Date) (Item, txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	if n := req.selectors(); n > 1 {
		return zero, txResult{}, fmt.Errorf(
			"%w: give one destination: --position, --top, --end, --before or --after",
			ErrInvalidArgument)
	} else if n == 0 && req.Section == "" {
		return zero, txResult{}, fmt.Errorf(
			"%w: --move needs a destination: --position, --top, --end, --before, --after or --section",
			ErrInvalidArgument)
	}
	if req.Before != "" && req.Before == id {
		return zero, txResult{}, fmt.Errorf("%w: %s cannot move before itself", ErrInvalidArgument, id)
	}
	if req.After != "" && req.After == id {
		return zero, txResult{}, fmt.Errorf("%w: %s cannot move after itself", ErrInvalidArgument, id)
	}

	t, err := s.begin()
	if err != nil {
		return zero, txResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, txResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.State != StateBacklog {
		return zero, txResult{}, fmt.Errorf(
			"%w: %s is %s, and only backlog items can be moved; use --start, --pause or --finish",
			ErrConflict, id, it.State)
	}

	b, e, err := t.backlog()
	if err != nil {
		return zero, txResult{}, err
	}

	from := it.Section
	to := from
	if req.Section != "" {
		if _, err := ParseSection(string(req.Section)); err != nil {
			return zero, txResult{}, err
		}
		to = req.Section
	}
	if b.Section(to) == nil {
		return zero, txResult{}, fmt.Errorf("%w: backlog.md has no ## %s section", ErrNotFound, to)
	}

	// I5 ties the blocked: field to the section, so a section change has to
	// carry it. Moving in needs a reason; moving out drops the one there was.
	if to == SectionBlocked && it.Blocked == "" {
		if req.Blocked == "" {
			return zero, txResult{}, fmt.Errorf(
				"%w: moving %s into Blocked needs a reason (--blocked)", ErrInvalidArgument, id)
		}
		it.Blocked = req.Blocked
	}
	if to != SectionBlocked {
		if req.Blocked != "" {
			return zero, txResult{}, fmt.Errorf(
				"%w: a blocked: reason only belongs in Blocked, not %s", ErrInvalidArgument, to)
		}
		it.Blocked = ""
	}

	before := RenderItemLine(it)

	// Remove first, then resolve the index against what remains. Resolving
	// against the pre-removal list would be off by one for every downward move
	// within a section.
	b.RemoveItem(e, it)
	dest := b.Section(to)

	index, err := resolveIndex(req, dest.Items, from == to)
	if err != nil {
		return zero, txResult{}, err
	}

	b.InsertItem(e, to, index, it)
	touchUpdated(e, today)
	t.stage("backlog.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: "backlog.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// resolveIndex turns a destination selector into a 0-based insertion index.
//
// items is the destination section AFTER the moved item was removed. sameSection
// lets a within-section move report --position against the length the user
// actually sees, which still includes the item being moved.
func resolveIndex(req MoveRequest, items []*Item, sameSection bool) (int, error) {
	switch {
	case req.Top:
		return 0, nil

	case req.End:
		return len(items), nil

	case req.Position > 0:
		// The user counts the section as it looks on screen. For a same-section
		// move that still includes the item being moved, so N may name any of
		// its current positions; for a cross-section move it may also name one
		// past the end, to append. Both come to len(items)+1 - the moved item is
		// already out of `items` in the first case.
		//
		// Out of range is an ERROR, not a clamp: silently landing somewhere near
		// what was asked for is worse than refusing (spec-tools.md §5.1.7).
		if req.Position > len(items)+1 {
			shown := len(items)
			if sameSection {
				shown++
			}
			return 0, fmt.Errorf(
				"%w: position %d is past the end of the section, which holds %d item(s)",
				ErrInvalidArgument, req.Position, shown)
		}
		return req.Position - 1, nil

	case req.Before != "":
		i, ok := indexOf(items, req.Before)
		if !ok {
			return 0, fmt.Errorf(
				"%w: %s is not in the destination section", ErrInvalidArgument, req.Before)
		}
		return i, nil

	case req.After != "":
		i, ok := indexOf(items, req.After)
		if !ok {
			return 0, fmt.Errorf(
				"%w: %s is not in the destination section", ErrInvalidArgument, req.After)
		}
		return i + 1, nil
	}

	// Section change with no selector: append. --add defaults to the bottom for
	// the same reason - arriving somewhere new does not make an item urgent.
	//
	// spec-tools.md §5.1.7 says "exactly one destination selector is required"
	// but also describes --section as usable on its own; this resolves that in
	// favour of the reading that does not make `--move ID --section someday`
	// a usage error.
	return len(items), nil
}

func indexOf(items []*Item, id ID) (int, bool) {
	for i, it := range items {
		if it.ID == id {
			return i, true
		}
	}
	return 0, false
}
