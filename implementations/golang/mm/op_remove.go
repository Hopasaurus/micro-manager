package mm

import (
	"fmt"
	"path/filepath"
)

// RemoveRequest deletes an item outright (spec-tools.md §5.1.6).
type RemoveRequest struct {
	// Force is REQUIRED. The guard is not ceremony: the format keeps cancelled
	// work in done.md with outcome:cancelled precisely so that abandoning
	// something leaves a record, so --remove is for a typo, a duplicate, or an
	// item added to the wrong directory - not for work you decided against.
	Force bool

	// WithDetail deletes the item's detail file in the same transaction.
	// Without it the file is left in place and reported as an orphan; what a
	// tool may NOT do is leave an invalid directory without saying so.
	WithDetail bool

	DryRun bool
}

// Removal reports what a remove did.
//
// DetailOrphan is the part a caller must not ignore: the file is still on disk,
// nothing references it, and the directory now fails I9 until somebody attaches
// or deletes it.
type Removal struct {
	Item          Item
	DetailDeleted string
	DetailOrphan  string
}

// Remove deletes an item's line.
//
// The ID is RETIRED, NOT RECYCLED: next_id is never decremented (I2). A future
// item with the same number would silently inherit this one's history in every
// log, commit message and detail file that ever named it.
func (s *Store) Remove(id ID, req RemoveRequest, today Date) (Removal, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Removal
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, TxResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if err := refuseIfV1(t.model); err != nil {
		return zero, TxResult{}, err
	}
	return s.removeInternal(t, it, req, today)
}

// removeInternal is Remove's shared logic. Unlike Add/Start/Pause/Finish/
// Move, Remove never had a separate v1/v2 code path to split, so there is
// nothing to extract beyond factoring the VersionMismatch guard (§5.3.4,
// T-0236) out of it. Deliberately NOT itself guarded, so a test can call it
// directly against a v1 fixture to keep exercising v1 mutation correctness
// - which --check and --migrate still depend on - even though the public
// Remove no longer reaches this code for a v1 directory.
func (s *Store) removeInternal(t *tx, it *Item, req RemoveRequest, today Date) (Removal, TxResult, error) {
	var zero Removal
	id := it.ID
	if !req.Force {
		return zero, TxResult{}, fmt.Errorf(
			"%w: removing %s deletes it with no record; pass Force if that is really what you want, "+
				"or close it with --finish %s --outcome cancelled to keep the history",
			ErrPreconditionFailed, id, id)
	}
	if it.State == StateWorking {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s is in a working slot; pause or finish it before removing it",
			ErrConflict, id)
	}

	before := RenderItemLine(it)
	detail := it.Detail
	out := Removal{Item: *it}

	switch it.State {
	case StateBacklog:
		b, e, err := t.backlog()
		if err != nil {
			return zero, TxResult{}, err
		}
		b.RemoveItem(e, it)
		touchUpdated(e, today)
		t.stage("backlog.md")
		t.record(Change{Kind: ChangeDeleted, ID: id, File: "backlog.md", Before: before})
	case StateBoard:
		b, e, err := t.board()
		if err != nil {
			return zero, TxResult{}, err
		}
		b.RemoveItem(e, it)
		touchUpdated(e, today)
		t.stage("board.md")
		t.record(Change{Kind: ChangeDeleted, ID: id, File: "board.md", Before: before})
	case StateDone:
		d, e, err := t.done()
		if err != nil {
			return zero, TxResult{}, err
		}
		d.RemoveItem(e, it)
		touchUpdated(e, today)
		t.stage("done.md")
		t.record(Change{Kind: ChangeDeleted, ID: id, File: "done.md", Before: before})
	default:
		return zero, TxResult{}, fmt.Errorf("%w: %s has no state", ErrConflict, id)
	}

	// next_id is deliberately untouched.

	if detail != "" {
		if _, exists := t.model.details[detail]; exists {
			if req.WithDetail {
				t.ws.Delete(filepath.Join(s.path, detail))
				t.record(Change{Kind: ChangeDeleted, ID: id, File: detail})
				// Validation must not see a file that is about to go away, or it
				// reports the orphan this deletion exists to prevent.
				delete(t.model.details, detail)
				out.DetailDeleted = detail
			} else {
				// The orphan is the documented default, so it is accepted rather
				// than blocking the write - but it is returned, and a caller that
				// says nothing about it is the one not conforming.
				t.baseline[violationKey(Violation{
					Invariant: "I9", At: Location{File: detail},
					Message: "orphan — no item references it",
				})] = struct{}{}
				out.DetailOrphan = detail
			}
		}
	}

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return out, res, nil
}
