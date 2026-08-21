package mm

import "fmt"

// T-0236: Store.Add/Start/Pause/Finish/Move/Update/Remove now refuse against
// a version-1 directory with VersionMismatch (spec-tools.md §5.3.4's
// longevity policy) - see refuseIfV1 in migrate.go and the guard at the top
// of each public method in op_*.go.
//
// The unguarded xV1 (or, for Update/Remove, which never had a separate v1/v2
// split, the shared xInternal) siblings those methods now delegate to still
// exist and are unchanged in behavior - they are exactly the code that ran
// inline before the guard was added. The helpers below are the test-only way
// to reach them directly: every test in this package that specifically
// exercises v1 mutation logic (as opposed to merely using a v1 fixture
// incidentally to set up a scenario about something else) calls one of these
// instead of the public Store method, so that logic - which --check and
// --migrate both still depend on - keeps being exercised even though the
// public API no longer reaches it for a v1 directory.
//
// Each wrapper replicates ONLY the not-found lookup the public method did
// before its version dispatch; every other behavior (including every error
// case a test asserts on) is the same xV1/xInternal function the public
// method calls, unchanged.

func testAddV1(s *Store, req AddRequest, today Date) (Item, TxResult, error) {
	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	return s.addV1(t, req, today)
}

func testStartV1(s *Store, id ID, req StartRequest, today Date) (Item, TxResult, error) {
	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	return s.startV1(t, it, req, today)
}

func testPauseV1(s *Store, id ID, req PauseRequest, today Date) (Item, TxResult, error) {
	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	return s.pauseV1(t, it, req, today)
}

func testFinishV1(s *Store, id ID, req FinishRequest, today Date) (Item, TxResult, error) {
	// Mirrors Store.Finish's own pre-dispatch validation exactly (outcome and
	// date parsing, then the already-done check) - none of that lives in
	// finishV1 itself, so skipping it here would let a bad outcome or date
	// reach the write instead of being refused before it.
	outcome := req.Outcome
	if outcome == OutcomeNone {
		outcome = OutcomeShipped
	}
	if _, err := ParseOutcome(string(outcome)); err != nil {
		return Item{}, TxResult{}, err
	}
	when := req.Done
	if when.IsZero() {
		when = today
	}
	if !when.Valid() {
		return Item{}, TxResult{}, fmt.Errorf(
			"%w: done:%04d-%02d-%02d is not a real date", ErrInvalidArgument,
			when.Year, when.Month, when.Day)
	}

	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.State == StateDone {
		return Item{}, TxResult{}, fmt.Errorf(
			"%w: %s was already closed on %s; edit it instead of finishing it twice",
			ErrConflict, id, it.Done)
	}
	return s.finishV1(t, it, req, outcome, when, today)
}

func testMoveV1(s *Store, id ID, req MoveRequest, today Date) (Item, TxResult, error) {
	// Mirrors Store.Move's own pre-begin validation exactly (the selector
	// count and the before/after self-reference checks) - none of that lives
	// in moveV1 itself.
	if n := req.selectors(); n > 1 {
		return Item{}, TxResult{}, fmt.Errorf(
			"%w: give one destination: --position, --top, --end, --before or --after",
			ErrInvalidArgument)
	} else if n == 0 && req.Section == "" && req.Stage == "" {
		return Item{}, TxResult{}, fmt.Errorf(
			"%w: --move needs a destination: --position, --top, --end, --before, --after, --section or --stage",
			ErrInvalidArgument)
	}
	if req.Before != "" && req.Before == id {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s cannot move before itself", ErrInvalidArgument, id)
	}
	if req.After != "" && req.After == id {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s cannot move after itself", ErrInvalidArgument, id)
	}

	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	return s.moveV1(t, it, req, today)
}

func testUpdateV1(s *Store, id ID, req UpdateRequest, today Date) (Item, TxResult, error) {
	t, err := s.begin()
	if err != nil {
		return Item{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Item{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	return s.updateInternal(t, it, req, today)
}

func testRemoveV1(s *Store, id ID, req RemoveRequest, today Date) (Removal, TxResult, error) {
	t, err := s.begin()
	if err != nil {
		return Removal{}, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return Removal{}, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	return s.removeInternal(t, it, req, today)
}
