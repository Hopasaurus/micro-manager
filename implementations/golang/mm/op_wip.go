package mm

import (
	"fmt"
	"path/filepath"
	"strconv"
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
	if t.model.isV2() {
		return zero, TxResult{}, fmt.Errorf(
			"%w: this directory is version 2, which has no working.NN.md file count to set; "+
				"use --wip N --stage SLUG for a stage's own cap instead",
			ErrInvalidArgument)
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

// SetStageWipLimit sets or clears one stage's WIP cap (spec-file-format.md
// §5.1.3), version 2's per-stage generalization of SetWipLimit's file count.
//
// n == 0 clears the cap. There is no other way to spell "uncapped" in this
// key's own shape: absence, not a sentinel value, is what wip.<slug> being
// missing already means (§10.4), so writing "0" would be a second, competing
// way to say the same thing rather than a real limit of zero items.
func (s *Store) SetStageWipLimit(stage Stage, n int, dryRun bool) (Directory, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Directory
	if n < 0 {
		return zero, TxResult{}, fmt.Errorf("%w: a WIP limit cannot be negative", ErrInvalidArgument)
	}
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	if !t.model.isV2() {
		return zero, TxResult{}, fmt.Errorf(
			"%w: a per-stage WIP limit is a version-2 concept (this directory is version 1); "+
				"--wip N without --stage sets the version-1 slot count instead",
			ErrInvalidArgument)
	}
	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}
	if _, err := ParseStage(string(stage), b.stageCfg.Stages); err != nil {
		return zero, TxResult{}, err
	}
	key := "wip." + string(stage)

	before := b.FM.Get(key)

	if n == 0 {
		if !b.FM.Has(key) {
			// A no-op writes nothing: no line, no mtime bump, no diff.
			res, err := t.commit(dryRun)
			if err != nil {
				return zero, res, err
			}
			return t.model.directory(), res, nil
		}
		e.DeleteFM(key)
		delete(b.stageCfg.WipLimits, stage)
		t.record(Change{Kind: ChangeDeleted, File: "board.md", Before: key + ": " + before})
	} else {
		if used := len(b.StageItems(stage)); used > n {
			return zero, TxResult{}, fmt.Errorf(
				"%w: stage %q already holds %d item(s), above the new limit of %d",
				ErrConflict, stage, used, n)
		}
		if before == strconv.Itoa(n) {
			res, err := t.commit(dryRun)
			if err != nil {
				return zero, res, err
			}
			return t.model.directory(), res, nil
		}
		e.SetFM(key, strconv.Itoa(n))
		b.stageCfg.WipLimits[stage] = n
		kind := ChangeCreated
		if before != "" {
			kind = ChangeUpdated
		}
		t.record(Change{Kind: kind, File: "board.md", Before: before, After: strconv.Itoa(n)})
	}

	t.stage("board.md")
	res, err := t.commit(dryRun)
	if err != nil {
		return zero, res, err
	}
	return t.model.directory(), res, nil
}
