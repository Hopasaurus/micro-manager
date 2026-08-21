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
func (s *Store) Update(id ID, req UpdateRequest, today Date) (Item, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Item
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
	return s.updateInternal(t, it, req, today)
}

// updateInternal is Update's shared logic. Unlike Add/Start/Pause/Finish/
// Move, Update never had a separate v1/v2 code path to split, so there is
// nothing to extract beyond factoring the VersionMismatch guard (§5.3.4,
// T-0236) out of it. Deliberately NOT itself guarded, so a test can call it
// directly against a v1 fixture to keep exercising v1 mutation correctness
// - which --check and --migrate still depend on - even though the public
// Update no longer reaches this code for a v1 directory.
func (s *Store) updateInternal(t *tx, it *Item, req UpdateRequest, today Date) (Item, TxResult, error) {
	var zero Item
	id := it.ID
	before := RenderItemLine(it)
	oldTitle := it.Title

	if err := applyUpdate(it, req, t.model); err != nil {
		return zero, TxResult{}, err
	}

	// Write the item back to whichever file holds it.
	file, err := t.writeItemLine(it, today)
	if err != nil {
		return zero, TxResult{}, err
	}
	t.record(Change{Kind: ChangeUpdated, ID: id, File: file,
		Before: before, After: RenderItemLine(it)})

	// A retitled item drags its detail file with it, or I9 breaks. This is the
	// sharpest coupling in the operation set and the easiest to forget, because
	// the item write succeeds perfectly well on its own.
	if it.Title != oldTitle && it.Detail != "" {
		if err := t.syncDetailTitle(it, today); err != nil {
			return zero, TxResult{}, err
		}
	}

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// applyUpdate mutates an item per the request, validating as it goes. m is
// the transaction's model, needed only for setAnyField's tickler placement
// check (§5.1.4 requires knowing the directory's declared tickler_stages,
// not just the item).
func applyUpdate(it *Item, req UpdateRequest, m *dirModel) error {
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
			return fmt.Errorf("%w: a reason may not contain %q", ErrInvalidArgument, "|")
		}
		if it.State == StateBoard {
			// Version 2's reason: is valid on any stage (spec-file-format.md
			// §5.1.5) - no section to agree with, and nothing here can know
			// whether the item's current stage requires one, since that is a
			// directory-level declaration (needs_reason) applyUpdate is not
			// given. A request to clear a required reason still fails, just
			// later: the transaction's own I5 check catches it at commit.
			it.Reason = *req.Blocked
		} else {
			// I5 ties the field to the section, and --edit cannot move an
			// item, so the two must already agree.
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
	}

	if req.Created != nil {
		it.Created = *req.Created
	}
	if req.Started != nil {
		it.Started = *req.Started
	}

	for _, f := range req.Set {
		if err := setAnyField(it, f.Key, f.Value, m); err != nil {
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

// setAnyField writes a field by name, whether or not this implementation
// knows it. A registered key is validated; an unknown one is stored
// verbatim. m is the transaction's model - only "tickler" reads it, for the
// directory's declared tickler_stages.
func setAnyField(it *Item, key, value string, m *dirModel) error {
	if key == "" {
		return fmt.Errorf("%w: a field needs a key", ErrInvalidArgument)
	}
	if strings.ContainsAny(value, "|") {
		return fmt.Errorf("%w: field %s may not contain %q", ErrInvalidArgument, key, "|")
	}
	switch key {
	case "stage":
		// A version-2 item's stage: is not a plain field: leaving it (I7),
		// entering a WIP-capped one, leaving a tickler_stages source, and
		// entering working (started: must be stamped) are all --move's
		// job. Duplicating that logic here so --set could do it too would
		// be two implementations of one operation to keep in sync; refusing
		// and naming the right one is the honest alternative to a silent,
		// partial move. A version-1 item has no stage: concept at all, so
		// nothing here applies to it - stored as an unregistered field,
		// same as any other key version 1 does not recognise.
		if it.State == StateBoard {
			return fmt.Errorf(
				"%w: stage: is not settable with --set; use --move --stage %s", ErrInvalidArgument, value)
		}
		setExtra(it, key, value)
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
	case "refs":
		refs, err := ParseRefs(value)
		if err != nil {
			return err
		}
		it.Refs = refs
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
	case "reason":
		// Version 2's field (§5.1.5). Routed to its own Item field for the
		// same reason "blocked" is above it: leaving it in Extra alongside an
		// already-registered it.Reason would render reason: twice on the
		// same line the moment the field already carries a value.
		it.Reason = value
	case "tickler_dest":
		it.TicklerDest = Stage(value)
	case "tickler":
		if _, err := ParseSchedule(value); err != nil {
			return err
		}
		if it.State == StateBoard {
			// §5.1.4: valid only where the item's current stage is a
			// tickler_stages source - version 1's fixed "Someday only"
			// (below), generalized to whatever the directory declares.
			if _, ok := m.board.stageCfg.TicklerDestOf(it.Stage); !ok {
				return fmt.Errorf(
					"%w: a tickler: schedule only belongs on a tickler_stages source; stage %q is not one",
					ErrConflict, it.Stage)
			}
		} else if it.Section != SectionSomeday {
			return fmt.Errorf("%w: a tickler: schedule only belongs in Someday", ErrConflict)
		}
		it.Tickler = value
	case "tickled":
		d, err := ParseDate(value)
		if err != nil {
			return err
		}
		it.Tickled = d
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
	case "stage":
		// Every version-2 item requires one (I7); clearing it is not a
		// smaller version of a move, it is an invalid item. Same reasoning
		// as setAnyField's refusal, the other direction.
		if it.State == StateBoard {
			return fmt.Errorf("%w: stage: cannot be removed; every item needs one", ErrInvalidArgument)
		}
		removeExtra(it, key)
	case "prio":
		it.Prio = PrioNone
	case "tags":
		it.Tags = nil
	case "refs":
		it.Refs = nil
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
	case "reason":
		// Unlike v1's blocked:, v2's reason: is not dropped automatically on
		// leaving a needs_reason stage (research decision 18) - but an
		// explicit --unset still has to be allowed to remove it, or there
		// would be no way to clear a stale one. Whether the item's current
		// stage still requires it is a directory-level question this
		// function is not given the StageConfig to answer; the transaction's
		// own I5 check catches a now-missing required reason at commit.
		it.Reason = ""
	case "tickler_dest":
		it.TicklerDest = ""
	case "tickler":
		it.Tickler = ""
	case "tickled":
		it.Tickled = Date{}
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
