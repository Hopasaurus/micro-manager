package mm

import "fmt"

// SetAudit enables or disables audit.md (spec-file-format.md §5.1.8,
// T-0253) by writing board.md's `audit: true`/`false` key. Version 2 only —
// the key it edits does not exist in a version-1 directory.
func (s *Store) SetAudit(enabled bool, dryRun bool) (Directory, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Directory
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	if !t.model.isV2() {
		return zero, TxResult{}, fmt.Errorf(
			"%w: audit.md is a version-2 concept (this directory is version 1)", ErrInvalidArgument)
	}
	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}

	want := "false"
	if enabled {
		want = "true"
	}
	if b.stageCfg.AuditEnabled == enabled {
		// A no-op writes nothing: no line, no mtime bump, no diff.
		res, err := t.commit(dryRun)
		if err != nil {
			return zero, res, err
		}
		return t.model.directory(), res, nil
	}
	before := ""
	if e.fm.Has("audit") {
		before = e.fm.Get("audit")
	}
	e.SetFM("audit", want)
	b.stageCfg.AuditEnabled = enabled
	t.record(Change{Kind: ChangeUpdated, File: "board.md", Before: before, After: want})

	t.stage("board.md")
	res, err := t.commit(dryRun)
	if err != nil {
		return zero, res, err
	}
	return t.model.directory(), res, nil
}
