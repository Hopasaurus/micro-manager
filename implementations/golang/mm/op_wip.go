package mm

import (
	"fmt"
	"path/filepath"
)

// SetWipLimit changes the WIP limit by creating or deleting working files
// (spec-tools.md §5.2, spec-file-format.md §5.2.1).
//
// THE LIMIT IS THE FILE COUNT. There is no setting to change, which is what
// makes raising the limit a deliberate act: the file has to be created, so it
// shows up in a diff and somebody has to mean it.
//
// Shrinking is the interesting direction. Slots must stay numbered 1..N with no
// gaps (I10), so only the highest-numbered files can go, and each of them must
// be idle. Renumbering an occupied slot to close a gap is not an option: the
// item would move files for a reason that has nothing to do with the item.
func (s *Store) SetWipLimit(n int, dryRun bool) (Directory, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Directory
	if n < 1 {
		return zero, TxResult{}, fmt.Errorf(
			"%w: a directory needs at least one working file", ErrInvalidArgument)
	}

	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	current := len(t.model.working)
	if current == 0 {
		return zero, TxResult{}, fmt.Errorf(
			"%w: this directory has no working.NN.md file to count from", ErrNotFound)
	}

	width := t.model.working[0].Width
	for _, w := range t.model.working[1:] {
		if w.Width != width {
			// Mixed widths are already an I10 violation; picking one and writing
			// more files at it would bury the problem under new files.
			return zero, TxResult{}, fmt.Errorf(
				"%w: working files mix digit widths; fix that before changing the limit",
				ErrInvariantViolation)
		}
	}

	switch {
	case n == current:
		// A no-op writes nothing at all: no file, no mtime bump, no diff.
		res, err := t.commit(dryRun)
		if err != nil {
			return zero, res, err
		}
		return t.model.directory(), res, nil

	case n > current:
		if digits := len(fmt.Sprint(n)); digits > width {
			return zero, TxResult{}, fmt.Errorf(
				"%w: %d slots need at least %d digits, but this directory numbers its "+
					"slots with %d; every working file must use the same width (I10)",
				ErrInvalidArgument, n, digits, width)
		}
		for i := current + 1; i <= n; i++ {
			name := workingFileName(i, width)
			t.stageRaw(name, []byte(initWorking))
			t.record(Change{Kind: ChangeCreated, File: name})
		}

	default: // n < current: delete from the top down
		var occupied []*workingFile
		for _, w := range t.model.working {
			if w.Number > n && !w.Idle() {
				occupied = append(occupied, w)
			}
		}
		if len(occupied) > 0 {
			var b []byte
			for _, w := range occupied {
				b = append(b, fmt.Sprintf("\n  slot %0*d  %s  %s",
					w.Width, w.Number, w.Item.ID, w.Item.Title)...)
			}
			return zero, TxResult{}, fmt.Errorf(
				"%w: lowering the limit to %d would delete a slot that is in use:%s\n"+
					"finish or pause it first; an occupied slot is never renumbered",
				ErrConflict, n, b)
		}
		for _, w := range t.model.working {
			if w.Number > n {
				t.ws.Delete(filepath.Join(s.path, w.Name))
				t.record(Change{Kind: ChangeDeleted, File: w.Name})
			}
		}
	}

	// The deleted files must leave the model before validation runs, or I10
	// would be checked against a file set that is about to change.
	kept := make([]*workingFile, 0, n)
	for _, w := range t.model.working {
		if w.Number <= n {
			kept = append(kept, w)
		}
	}
	for i := current + 1; i <= n; i++ {
		nw, _ := parseWorking(workingFileName(i, width), []byte(initWorking))
		kept = append(kept, nw)
	}
	t.model.working = kept
	t.model.entries = nil
	for _, w := range kept {
		t.model.entries = append(t.model.entries, w.Name)
	}

	res, err := t.commit(dryRun)
	if err != nil {
		return zero, res, err
	}
	return t.model.directory(), res, nil
}
