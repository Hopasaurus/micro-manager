package mm

import (
	"fmt"
	"strings"
)

// UpdateRequest modifies an item's fields in place (spec-tools.md §5.1.5).
//
// It does NOT move the item: position is --move's job, state is
// --start/--pause/--finish. An edit that changed where an item lives would make
// the operation set ambiguous and the audit trail unreadable.
//
// Every field is a pointer or a slice so that "leave alone" is distinguishable
// from "set to empty". Without that, an UpdateRequest with an unset Prio would
// be indistinguishable from one asking to clear it.
type UpdateRequest struct {
	Title   *string
	Prio    *Prio
	Blocked *string
	Created *Date
	Started *Date

	// Tags replaces the whole list. AddTags and RemoveTags adjust it
	// incrementally, which is what --tag and --untag do; the two forms are
	// mutually exclusive.
	Tags       []string
	SetTags    bool // distinguishes "replace with empty" from "leave alone"
	AddTags    []string
	RemoveTags []string

	// Set and Unset reach ANY key, registered or not. A tool must be able to
	// write a field it does not understand (spec-file-format.md §9) - refusing
	// would make this implementation the ceiling on what the format can carry.
	Set   []Field
	Unset []string

	DryRun bool
}

// Update modifies an item wherever it lives.
func (s *Store) Update(id ID, req UpdateRequest, today Date) (Item, txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
	t, err := s.begin()
	if err != nil {
		return zero, txResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, txResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	before := RenderItemLine(it)
	oldTitle := it.Title

	if err := applyUpdate(it, req); err != nil {
		return zero, txResult{}, err
	}

	// Write the item back to whichever file holds it.
	file, err := t.writeItemLine(it, today)
	if err != nil {
		return zero, txResult{}, err
	}
	t.record(Change{Kind: ChangeUpdated, ID: id, File: file,
		Before: before, After: RenderItemLine(it)})

	// A retitled item drags its detail file with it, or I9 breaks. This is the
	// sharpest coupling in the operation set and the easiest to forget, because
	// the item write succeeds perfectly well on its own.
	if it.Title != oldTitle && it.Detail != "" {
		if err := t.syncDetailTitle(it, today); err != nil {
			return zero, txResult{}, err
		}
	}

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// applyUpdate mutates an item per the request, validating as it goes.
func applyUpdate(it *Item, req UpdateRequest) error {
	if req.SetTags && (len(req.AddTags) > 0 || len(req.RemoveTags) > 0) {
		return fmt.Errorf("%w: replacing the tag list and adjusting it are exclusive",
			ErrInvalidArgument)
	}

	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			return fmt.Errorf("%w: an item needs a title", ErrInvalidArgument)
		}
		if strings.Contains(title, "|") {
			return fmt.Errorf("%w: a title may not contain %q", ErrInvalidArgument, "|")
		}
		it.Title = title
	}

	if req.Prio != nil {
		if *req.Prio != PrioNone {
			if _, err := ParsePrio(string(*req.Prio)); err != nil {
				return err
			}
		}
		it.Prio = *req.Prio
	}

	if req.SetTags {
		if err := checkTags(req.Tags); err != nil {
			return err
		}
		it.Tags = dedupeTags(req.Tags)
	}
	if len(req.AddTags) > 0 {
		if err := checkTags(req.AddTags); err != nil {
			return err
		}
		it.Tags = dedupeTags(append(it.Tags, req.AddTags...))
	}
	if len(req.RemoveTags) > 0 {
		it.Tags = removeTags(it.Tags, req.RemoveTags)
	}

	if req.Blocked != nil {
		if strings.Contains(*req.Blocked, "|") {
			return fmt.Errorf("%w: a blocked: reason may not contain %q", ErrInvalidArgument, "|")
		}
		// I5 ties the field to the section, and --edit cannot move an item, so
		// the two must already agree.
		if *req.Blocked == "" && it.Section == SectionBlocked {
			return fmt.Errorf(
				"%w: %s is under Blocked, so it needs a reason; unblock it with --move or --unblock",
				ErrConflict, it.ID)
		}
		if *req.Blocked != "" && it.State == StateBacklog && it.Section != SectionBlocked {
			return fmt.Errorf(
				"%w: a blocked: reason only belongs in Blocked; move %s there first",
				ErrConflict, it.ID)
		}
		it.Blocked = *req.Blocked
	}

	if req.Created != nil {
		it.Created = *req.Created
	}
	if req.Started != nil {
		it.Started = *req.Started
	}

	for _, f := range req.Set {
		if err := setAnyField(it, f.Key, f.Value); err != nil {
			return err
		}
	}
	for _, key := range req.Unset {
		if err := unsetAnyField(it, key); err != nil {
			return err
		}
	}
	return nil
}

// setAnyField writes a field by name, whether or not this implementation knows
// it. A registered key is validated; an unknown one is stored verbatim.
func setAnyField(it *Item, key, value string) error {
	if key == "" {
		return fmt.Errorf("%w: a field needs a key", ErrInvalidArgument)
	}
	if strings.ContainsAny(value, "|") {
		return fmt.Errorf("%w: field %s may not contain %q", ErrInvalidArgument, key, "|")
	}
	switch key {
	case "prio":
		p, err := ParsePrio(value)
		if err != nil {
			return err
		}
		it.Prio = p
	case "tags":
		tags, err := ParseTags(value)
		if err != nil {
			return err
		}
		it.Tags = tags
	case "created", "started", "done":
		d, err := ParseDate(value)
		if err != nil {
			return err
		}
		switch key {
		case "created":
			it.Created = d
		case "started":
			it.Started = d
		case "done":
			it.Done = d
		}
	case "outcome":
		o, err := ParseOutcome(value)
		if err != nil {
			return err
		}
		it.Outcome = o
	case "blocked":
		it.Blocked = value
	case "detail":
		// The path is not free-form: I8 requires details/<this item's ID>.md.
		if value != it.DetailPath() {
			return fmt.Errorf("%w: detail must be %s, not %s",
				ErrInvalidArgument, it.DetailPath(), value)
		}
		it.Detail = value
	default:
		setExtra(it, key, value)
	}
	return nil
}

// unsetAnyField clears a field, refusing where the format requires one.
func unsetAnyField(it *Item, key string) error {
	switch key {
	case "prio":
		it.Prio = PrioNone
	case "tags":
		it.Tags = nil
	case "created":
		it.Created = Date{}
	case "started":
		it.Started = Date{}
	case "detail":
		it.Detail = ""
	case "blocked":
		if it.Section == SectionBlocked {
			return fmt.Errorf("%w: %s is under Blocked, so blocked: cannot be removed",
				ErrConflict, it.ID)
		}
		it.Blocked = ""
	case "done", "outcome":
		// I6 requires both on every item in done.md.
		if it.State == StateDone {
			return fmt.Errorf("%w: %s is closed, so %s cannot be removed",
				ErrConflict, it.ID, key)
		}
		if key == "done" {
			it.Done = Date{}
		} else {
			it.Outcome = OutcomeNone
		}
	case "id", "title":
		return fmt.Errorf("%w: %s cannot be removed from an item", ErrInvalidArgument, key)
	default:
		removeExtra(it, key)
	}
	return nil
}

// setExtra sets an unregistered field, in place when it already exists so that
// the on-disk field order is preserved.
func setExtra(it *Item, key, value string) {
	for i := range it.Extra {
		if it.Extra[i].Key == key {
			it.Extra[i].Value = value
			return
		}
	}
	it.Extra = append(it.Extra, Field{Key: key, Value: value})
}

func removeExtra(it *Item, key string) {
	for i := range it.Extra {
		if it.Extra[i].Key == key {
			it.Extra = append(it.Extra[:i], it.Extra[i+1:]...)
			return
		}
	}
}

func checkTags(tags []string) error {
	for _, t := range tags {
		if !validTag(t) {
			return fmt.Errorf("%w: malformed tag %q", ErrInvalidArgument, t)
		}
	}
	return nil
}

// dedupeTags removes repeats while keeping first-seen order, so --tag applied
// twice does not produce "infra,infra".
func dedupeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func removeTags(tags, drop []string) []string {
	gone := make(map[string]bool, len(drop))
	for _, d := range drop {
		gone[d] = true
	}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		if !gone[t] {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// slotOf finds the working file holding an item.
func (t *tx) slotOf(id ID) *workingFile {
	for _, w := range t.model.working {
		if w.Item != nil && w.Item.ID == id {
			return w
		}
	}
	return nil
}

// syncDetailTitle rewrites a detail file's frontmatter title to match its item.
//
// Called whenever a title changes, in the SAME transaction as the item write.
// Doing it afterwards, or not at all, leaves I9 broken - and the failure is
// silent at the point of the edit, because writing the item line succeeded.
func (t *tx) syncDetailTitle(it *Item, today Date) error {
	df, ok := t.model.details[it.Detail]
	if !ok {
		// I8 will report the missing file; nothing to sync.
		return nil
	}
	e, seen := t.dirty[it.Detail]
	if !seen {
		e = newFileEdit(df.Name, df.Lines, df.FM)
		t.dirty[it.Detail] = e
	}
	e.SetFM("title", it.Title)
	if df.FM.Has("updated") {
		e.SetFM("updated", today.String())
	}
	if e.Dirty() {
		t.stageRaw(it.Detail, e.Bytes())
		t.record(Change{Kind: ChangeUpdated, ID: it.ID, File: it.Detail})
		// Validation re-reads the details map, so it must see the new bytes.
		t.model.details[it.Detail] = mustParseDetail(df.Name, string(e.Bytes()))
	}
	return nil
}
