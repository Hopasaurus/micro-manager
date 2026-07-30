package mm

import "fmt"

// Status and Next (spec-tools.md §5.2).
//
// Both are derivable from list() and directory(), which is exactly why they are
// here: every front end would otherwise derive them slightly differently, and
// "what should I do next" would answer two things in two windows.

// Status is one screen: what is in each slot, WIP n/N, counts by section, and
// the oldest untouched Ready item (spec-tools.md §5.2).
type Status struct {
	Directory Directory

	Ready   int
	Blocked int
	Someday int
	Done    int

	// Next is the top of ## Ready, nil when the section is empty. It is the same
	// item Next() returns, carried here so a status screen needs one call.
	Next *Item

	// OldestReady is the oldest Ready item that has never been started - the one
	// quietly aging at the bottom of the list. Nil when ## Ready is empty.
	//
	// "Untouched" means no started: field. An item that was started and paused
	// has been looked at; one that has sat since it was written has not, and
	// that is the one worth surfacing.
	OldestReady *Item
}

// WipUsed and WipLimit are the pair a status line prints as n/N.
func (s Status) WipUsed() int  { return s.Directory.WipUsed }
func (s Status) WipLimit() int { return s.Directory.WipLimit }

// Backlog is every open item, whatever section it sits in.
func (s Status) Backlog() int { return s.Ready + s.Blocked + s.Someday }

// Status summarises the directory in one read.
func (s *Store) Status() (Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return Status{}, err
	}

	out := Status{Directory: m.directory()}
	var ready []*Item
	for _, it := range m.items() {
		switch it.State {
		case StateBacklog:
			switch it.Section {
			case SectionReady:
				out.Ready++
				ready = append(ready, it)
			case SectionBlocked:
				out.Blocked++
			case SectionSomeday:
				out.Someday++
			}
		case StateDone:
			out.Done++
		}
	}

	if len(ready) > 0 {
		next := *ready[0]
		out.Next = &next
	}
	if oldest := oldestUntouched(ready); oldest != nil {
		copied := *oldest
		out.OldestReady = &copied
	}
	return out, nil
}

// oldestUntouched picks the never-started Ready item with the earliest created:
// date. An item with no created: field cannot be compared, so it only wins when
// nothing else qualifies - it is aging too, just unmeasurably.
func oldestUntouched(ready []*Item) *Item {
	var best, undated *Item
	for _, it := range ready {
		if !it.Started.IsZero() {
			continue
		}
		if it.Created.IsZero() {
			if undated == nil {
				undated = it
			}
			continue
		}
		if best == nil || it.Created.Before(best.Created) {
			best = it
		}
	}
	if best != nil {
		return best
	}
	if undated != nil {
		return undated
	}
	// Everything in Ready has been started and paused. The top of the list is
	// still the answer to "what has been waiting longest for attention".
	if len(ready) > 0 {
		return ready[0]
	}
	return nil
}

// Next returns the top of ## Ready - the thing to start next (spec-tools.md
// §5.2).
//
// ErrNotFound when the section is empty, which is what lets the CLI exit
// non-zero and a script stop rather than start something arbitrary.
func (s *Store) Next() (Item, error) {
	items, err := s.List(Filter{Section: SectionReady, Limit: 1})
	if err != nil {
		return Item{}, err
	}
	if len(items) == 0 {
		return Item{}, fmt.Errorf("%w: ## Ready is empty", ErrNotFound)
	}
	return items[0], nil
}
