package mm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// ItemRevision identifies the exact item-line and detail-file state rendered
// by an item panel. It is a freshness token, not a transaction precondition.
type ItemRevision string

// String renders the revision for transport in HTML and URLs.
func (r ItemRevision) String() string { return string(r) }

// ItemSnapshot is one consistent read of an item, its optional detail, and the
// revision covering both. Detail is nil when the item has no detail reference.
type ItemSnapshot struct {
	Item     Item
	Detail   *Detail
	Revision ItemRevision
}

// ItemSnapshot returns an item-panel snapshot from one directory load.
func (s *Store) ItemSnapshot(id ID) (ItemSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return ItemSnapshot{}, err
	}
	return itemSnapshot(m, id, s.path)
}

func itemSnapshot(m *dirModel, id ID, directory string) (ItemSnapshot, error) {
	it := m.find(id)
	if it == nil {
		return ItemSnapshot{}, fmt.Errorf("%w: %s is not in %s", ErrNotFound, id, directory)
	}
	var detail *Detail
	var detailBytes string
	if it.Detail != "" {
		df, ok := m.details[it.Detail]
		if !ok {
			return ItemSnapshot{}, fmt.Errorf("%w: %s references %s, which does not exist", ErrNotFound, id, it.Detail)
		}
		detail = &Detail{Path: df.Name, ID: ID(df.FM.Get("id")), Title: df.FM.Get("title"), Body: bodyOf(df.Lines, df.FM)}
		detailBytes = joinLines(df.Lines)
	}
	h := sha256.New()
	h.Write([]byte(itemRevisionLine(m, it)))
	h.Write([]byte{0})
	if detail != nil {
		h.Write([]byte{1})
		h.Write([]byte(detailBytes))
	} else {
		h.Write([]byte{0})
	}
	return ItemSnapshot{Item: *it, Detail: detail, Revision: ItemRevision(hex.EncodeToString(h.Sum(nil)))}, nil
}

func itemRevisionLine(m *dirModel, it *Item) string {
	var lines []string
	switch it.Source.File {
	case "board.md":
		if m.board != nil {
			lines = m.board.Lines
		}
	case "backlog.md":
		if m.backlog != nil {
			lines = m.backlog.Lines
		}
	case "done.md":
		if m.done != nil {
			lines = m.done.Lines
		}
	}
	if it.Source.Line > 0 && it.Source.Line <= len(lines) {
		return lines[it.Source.Line-1]
	}
	return RenderItemLine(it)
}

// ItemRevision returns the current item-panel freshness token.
func (s *Store) ItemRevision(id ID) (ItemRevision, error) {
	snapshot, err := s.ItemSnapshot(id)
	return snapshot.Revision, err
}
