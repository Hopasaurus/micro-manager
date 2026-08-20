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

	// StageCounts holds a version-2 directory's per-stage counts, one entry
	// for every declared stage (zero counts included, so a front end can
	// render every column without guessing which stages exist). Empty for
	// version 1, where Ready/Blocked/Someday above carry the equivalent.
	StageCounts map[Stage]int

	// Next is the top of the ready-equivalent list (## Ready, or a version-2
	// board's "ready" stage when one is declared), nil when it is empty or
	// there is none. It is the same item Next() returns, carried here so a
	// status screen needs one call.
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
	if m.isV2() {
		out.StageCounts = map[Stage]int{}
		for _, st := range m.board.stageCfg.Stages {
			out.StageCounts[st] = 0
		}
	}
	for _, it := range m.items() {
		switch it.State {
		case StateBacklog:
			switch it.Section {
			case SectionReady:
				out.Ready++
			case SectionBlocked:
				out.Blocked++
			case SectionSomeday:
				out.Someday++
			}
		case StateBoard:
			out.StageCounts[it.Stage]++
		case StateDone:
			out.Done++
		}
	}

	ready := readyEquivalent(m)
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

// readyEquivalent returns the ready-equivalent items in file order: a
// version-1 directory's ## Ready section, or a version-2 directory's "ready"
// stage when one is declared. Nil when the section/stage is empty or, for a
// custom version-2 stage set with no "ready" slug, does not exist at all -
// inventing an equivalent for a directory that declares none is not this
// function's call to make.
func readyEquivalent(m *dirModel) []*Item {
	switch {
	case m.board != nil:
		if !m.board.stageCfg.IsStage("ready") {
			return nil
		}
		return m.board.StageItems("ready")
	case m.backlog != nil:
		if sec := m.backlog.Section(SectionReady); sec != nil {
			return sec.Items
		}
	}
	return nil
}

// Next returns the top of the ready-equivalent list - the thing to start
// next (spec-tools.md §5.2).
//
// ErrNotFound when it is empty, which is what lets the CLI exit non-zero and
// a script stop rather than start something arbitrary.
func (s *Store) Next() (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return Item{}, err
	}
	ready := readyEquivalent(m)
	if len(ready) == 0 {
		return Item{}, fmt.Errorf("%w: ## Ready is empty", ErrNotFound)
	}
	return *ready[0], nil
}
