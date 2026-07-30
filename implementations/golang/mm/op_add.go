package mm

import (
	"fmt"
	"strings"
)

// AddRequest describes a new backlog item (spec-tools.md §5.1.2).
type AddRequest struct {
	Title string

	// Top inserts at the top of the section. The DEFAULT IS THE BOTTOM: a new
	// item is not automatically more important than everything already queued,
	// and ## Ready order is the user's own prioritisation.
	Top bool

	Section Section // defaults to Ready
	Prio    Prio
	Tags    []string
	Blocked string
	Created Date // defaults to today; settable for backfilling

	// DetailBody, when non-empty, creates details/<ID>.md with this body.
	// Template copying and title synchronisation are T-0016's job.
	DetailBody string

	// Extra carries unregistered fields through, so a caller that knows about a
	// field this implementation does not can still set it.
	Extra []Field

	DryRun bool
}

// Add creates a backlog item, allocating its ID from next_id.
func (s *Store) Add(req AddRequest, today Date) (Item, txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	t, err := s.begin()
	if err != nil {
		return zero, txResult{}, err
	}
	b, e, err := t.backlog()
	if err != nil {
		return zero, txResult{}, err
	}

	it, err := buildNewItem(req, today)
	if err != nil {
		return zero, txResult{}, err
	}

	// Allocate from next_id. The counter only ever moves forward: an ID is never
	// reused, so a deleted item's number stays retired (I2).
	next := ID(b.FM.Get("next_id"))
	if !next.Valid() {
		return zero, txResult{}, fmt.Errorf(
			"%w: backlog.md has no usable next_id (found %q)", ErrInvalidArgument, b.FM.Get("next_id"))
	}
	n := next.Num()
	if n > 9999 {
		return zero, txResult{}, fmt.Errorf(
			"%w: next_id is exhausted at T-9999; the four-digit width caps a directory at 9999 items",
			ErrConflict)
	}
	it.ID = next

	sec := it.Section
	index := len(sectionItems(b, sec)) // bottom by default
	if req.Top {
		index = 0
	}
	b.InsertItem(e, sec, index, it)
	e.SetFM("next_id", string(NewID(n+1)))
	touchUpdated(e, today)

	t.record(Change{Kind: ChangeCreated, ID: it.ID, File: "backlog.md",
		After: RenderItemLine(it)})

	if req.DetailBody != "" {
		it.Detail = it.DetailPath()
		e.ReplaceItem(it)
		body := renderDetailFile(it, req.DetailBody, today)
		t.stageRaw(it.Detail, []byte(body))
		t.record(Change{Kind: ChangeCreated, ID: it.ID, File: it.Detail})
		// The new file has to be visible to validation, or I8 would report it
		// as missing and block its own creation.
		t.model.details[it.Detail] = mustParseDetail(it.Detail, body)
	}

	t.stage("backlog.md")
	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// buildNewItem validates a request and turns it into an item, ID aside.
func buildNewItem(req AddRequest, today Date) (*Item, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: an item needs a title", ErrInvalidArgument)
	}
	// A pipe in a title is only sometimes detectable on the way back in
	// (spec-file-format.md §10.2), so the writer is what has to refuse it.
	if strings.Contains(title, "|") {
		return nil, fmt.Errorf("%w: a title may not contain %q", ErrInvalidArgument, "|")
	}

	sec := req.Section
	if sec == "" {
		if req.Blocked != "" {
			sec = SectionBlocked // supplying a reason implies the section
		} else {
			sec = SectionReady
		}
	}
	switch sec {
	case SectionReady, SectionBlocked, SectionSomeday:
	default:
		return nil, fmt.Errorf("%w: unknown section %q", ErrInvalidArgument, sec)
	}
	if sec == SectionBlocked && req.Blocked == "" {
		return nil, fmt.Errorf("%w: an item in Blocked needs a blocked: reason", ErrInvalidArgument)
	}
	if sec != SectionBlocked && req.Blocked != "" {
		return nil, fmt.Errorf("%w: a blocked: reason only belongs in Blocked", ErrInvalidArgument)
	}
	if req.Prio != PrioNone {
		if _, err := ParsePrio(string(req.Prio)); err != nil {
			return nil, err
		}
	}
	for _, tag := range req.Tags {
		if !validTag(tag) {
			return nil, fmt.Errorf("%w: malformed tag %q", ErrInvalidArgument, tag)
		}
	}
	if strings.ContainsAny(req.Blocked, "|") {
		return nil, fmt.Errorf("%w: a blocked: reason may not contain %q", ErrInvalidArgument, "|")
	}

	created := req.Created
	if created.IsZero() {
		created = today
	}
	return &Item{
		Title:   title,
		State:   StateBacklog,
		Section: sec,
		Prio:    req.Prio,
		Tags:    req.Tags,
		Blocked: req.Blocked,
		Created: created,
		Extra:   req.Extra,
	}, nil
}

// sectionItems returns a section's items, or nil when the section is absent.
func sectionItems(b *backlogFile, sec Section) []*Item {
	if s := b.Section(sec); s != nil {
		return s.Items
	}
	return nil
}

// touchUpdated refreshes the updated: date, but only in a file already being
// written. Bumping it in an untouched file would produce a diff for nothing.
func touchUpdated(e *fileEdit, today Date) {
	if e.Dirty() && e.fm != nil && e.fm.Has("updated") {
		e.SetFM("updated", today.String())
	}
}

// renderDetailFile produces a detail file body with the frontmatter I9 requires:
// id and title matching the item exactly.
func renderDetailFile(it *Item, body string, today Date) string {
	fm := NewFrontmatter()
	fm.Set("doc", "detail")
	fm.Set("id", string(it.ID))
	fm.Set("title", it.Title)
	fm.Set("updated", today.String())
	out := fm.Render() + "\n# " + string(it.ID) + " — " + it.Title + "\n"
	if body != "" {
		out += "\n" + strings.TrimRight(body, "\n") + "\n"
	}
	return out
}

func mustParseDetail(name, body string) *detailFile {
	lines := splitLines([]byte(body))
	fm, _, _ := readHeader(name, lines)
	return &detailFile{Name: name, FM: fm, Lines: lines}
}
