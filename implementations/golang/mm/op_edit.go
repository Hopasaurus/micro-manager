package mm

import "fmt"

// EditRequest atomically changes editable item fields and, when DetailBody is
// non-nil, its detail body. ExpectedRevision is the snapshot rendered to the
// editor and is checked before the transaction mutates its model.
type EditRequest struct {
	Update           UpdateRequest
	DetailBody       *string
	ExpectedRevision ItemRevision
}

// Edit applies an item-panel edit under an item-specific revision
// precondition. Item-line and detail writes share one transaction.
func (s *Store) Edit(id ID, req EditRequest, today Date) (Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	if req.ExpectedRevision == "" {
		return zero, TxResult{}, fmt.Errorf("%w: expected revision is required", ErrInvalidArgument)
	}
	snapshot, err := itemSnapshot(t.model, id, s.path)
	if err != nil {
		return zero, TxResult{}, fmt.Errorf("%w: cannot verify %s: %v", ErrConcurrent, id, err)
	}
	if snapshot.Revision != req.ExpectedRevision {
		return zero, TxResult{}, fmt.Errorf("%w: %s changed since it was read", ErrConcurrent, id)
	}
	if err := refuseIfV1(t.model); err != nil {
		return zero, TxResult{}, err
	}

	it := t.model.find(id)
	if err := stageUpdate(t, it, req.Update, today); err != nil {
		return zero, TxResult{}, err
	}
	if req.DetailBody != nil {
		if it.Detail == "" {
			if *req.DetailBody != "" {
				if err := stageAttachedDetail(s, t, it, *req.DetailBody, today); err != nil {
					return zero, TxResult{}, err
				}
			}
		} else {
			df, _, err := t.detailFor(id)
			if err != nil {
				return zero, TxResult{}, err
			}
			e := t.detailEdit(df)
			replaceBody(e, df.FM, *req.DetailBody)
			if df.FM.Has("updated") {
				e.SetFM("updated", today.String())
			}
			if e.Dirty() {
				t.stageRaw(df.Name, e.Bytes())
				t.record(Change{Kind: ChangeUpdated, ID: id, File: df.Name})
				t.model.details[df.Name] = mustParseDetail(df.Name, string(e.Bytes()))
			}
		}
	}
	res, err := t.commit(req.Update.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

func stageAttachedDetail(s *Store, t *tx, it *Item, body string, today Date) error {
	path := it.DetailPath()
	if _, exists := t.model.details[path]; exists {
		return fmt.Errorf("%w: %s already exists as an orphan", ErrConflict, path)
	}
	content := renderDetailFile(it, body, today)
	before := RenderItemLine(it)
	it.Detail = path
	file, err := t.writeItemLine(it, today)
	if err != nil {
		return err
	}
	t.record(Change{Kind: ChangeUpdated, ID: it.ID, File: file, Before: before, After: RenderItemLine(it)})
	t.stageRaw(path, []byte(content))
	t.record(Change{Kind: ChangeCreated, ID: it.ID, File: path})
	t.model.details[path] = mustParseDetail(path, content)
	return nil
}
