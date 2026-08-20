package mm

import (
	"fmt"
	"strings"
)

// Version-2 (board.md) operation logic: --add, --start, --pause, --finish,
// --move against a stage-based board (spec-tools.md §5.1.2, §5.1.7-§5.1.10,
// generalized). Kept in one file, separate from the version-1 op_*.go files
// they are dispatched from, so the whole version-2 operation surface for
// this milestone reads in one place.
//
// Each Store method (Add, Start, Pause, Finish, Move) branches to its *V2
// counterpart here immediately after t.begin(), when t.model.isV2(). The
// version-1 path below that branch is untouched.

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func (s *Store) addV2(t *tx, req AddRequest, today Date) (Item, TxResult, error) {
	var zero Item
	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}
	cfg := b.stageCfg

	it, err := buildNewItemV2(req, cfg, today)
	if err != nil {
		return zero, TxResult{}, err
	}

	g := t.model.grammar()
	next, err := allocNextV2(e, b, g)
	if err != nil {
		return zero, TxResult{}, err
	}
	it.ID = next

	index := len(b.StageItems(it.Stage)) // bottom by default
	if req.Top {
		index = 0
	}
	b.InsertItem(e, it.Stage, index, it)
	touchUpdated(e, today)

	t.record(Change{Kind: ChangeCreated, ID: it.ID, File: "board.md",
		After: RenderItemLine(it)})

	if req.DetailBody != "" {
		it.Detail = it.DetailPath()
		e.ReplaceItem(it)
		body := renderDetailFile(it, req.DetailBody, today)
		t.stageRaw(it.Detail, []byte(body))
		t.record(Change{Kind: ChangeCreated, ID: it.ID, File: it.Detail})
		t.model.details[it.Detail] = mustParseDetail(it.Detail, body)
	}

	t.stage("board.md")
	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// buildNewItemV2 validates a request against a directory's declared stages
// and turns it into an item, ID aside.
func buildNewItemV2(req AddRequest, cfg StageConfig, today Date) (*Item, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: an item needs a title", ErrInvalidArgument)
	}
	if strings.Contains(title, "|") {
		return nil, fmt.Errorf("%w: a title may not contain %q", ErrInvalidArgument, "|")
	}

	stage := req.Stage
	if stage == "" {
		if req.Reason != "" {
			// A supplied reason implies the directory's needs_reason stage,
			// mirroring how version 1 lets --blocked imply --section
			// blocked (spec-tools.md §5.1.2). With more than one
			// needs_reason stage declared, the first is used — a caller
			// that cares picks --stage explicitly.
			if len(cfg.NeedsReason) > 0 {
				stage = cfg.NeedsReason[0]
			}
		}
		if stage == "" {
			stage = "ready"
		}
	}
	if _, err := ParseStage(string(stage), cfg.Stages); err != nil {
		return nil, err
	}
	if cfg.StageNeedsReason(stage) && req.Reason == "" {
		return nil, fmt.Errorf("%w: an item on stage %q needs a reason: (needs_reason)",
			ErrInvalidArgument, stage)
	}
	if strings.Contains(req.Reason, "|") {
		return nil, fmt.Errorf("%w: a reason: may not contain %q", ErrInvalidArgument, "|")
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
	for _, ref := range req.Refs {
		if !validSlug(ref.Slug) || !validGenericID(string(ref.ID)) {
			return nil, fmt.Errorf("%w: malformed ref %q", ErrInvalidArgument, ref.String())
		}
	}

	created := req.Created
	if created.IsZero() {
		created = today
	}
	if req.Tickler != "" {
		if _, err := ParseSchedule(req.Tickler); err != nil {
			return nil, err
		}
		if _, ok := cfg.TicklerDestOf(stage); !ok {
			return nil, fmt.Errorf(
				"%w: a tickler: schedule only belongs on a tickler_stages source; %q is not one",
				ErrInvalidArgument, stage)
		}
	}
	if req.TicklerDest != "" {
		if req.Tickler == "" {
			return nil, fmt.Errorf("%w: tickler_dest: without tickler: has nothing to route",
				ErrInvalidArgument)
		}
		if _, err := ParseStage(string(req.TicklerDest), cfg.Stages); err != nil {
			return nil, err
		}
	}

	return &Item{
		Title:       title,
		State:       StateBoard,
		Stage:       stage,
		Prio:        req.Prio,
		Tags:        req.Tags,
		Refs:        req.Refs,
		Reason:      req.Reason,
		Tickler:     req.Tickler,
		TicklerDest: req.TicklerDest,
		Created:     created,
		Extra:       req.Extra,
	}, nil
}

// allocNextV2 is allocNext for board.md: same counter discipline (§7 I2),
// different frontmatter home.
func allocNextV2(e *fileEdit, b *boardFile, g IDGrammar) (ID, error) {
	next := ID(b.FM.Get("next_id"))
	if !g.ValidID(string(next)) {
		return "", fmt.Errorf(
			"%w: board.md has no usable next_id (found %q)", ErrInvalidArgument, b.FM.Get("next_id"))
	}
	n := g.Num(string(next))
	if n >= g.Cap() {
		return "", fmt.Errorf(
			"%w: next_id is exhausted at %s; the %d-digit width caps a directory at %d items",
			ErrConflict, g.NewID(g.Cap()), g.Width, g.Cap())
	}
	e.SetFM("next_id", string(g.NewID(n+1)))
	return next, nil
}

// ---------------------------------------------------------------------------
// Start
// ---------------------------------------------------------------------------

func (s *Store) startV2(t *tx, id ID, today Date) (Item, TxResult, error) {
	var zero Item
	it := t.model.find(id)
	if it.Stage == "working" {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s is already on stage working; pause or finish it instead", ErrConflict, id)
	}

	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}
	if limit, ok := b.stageCfg.WipLimits["working"]; ok {
		if used := len(b.StageItems("working")); used >= limit {
			return zero, TxResult{}, &StageWipLimitError{Stage: "working", Limit: limit,
				Occupants: b.StageItems("working")}
		}
	}

	before := RenderItemLine(it)
	b.RemoveItem(e, it)
	it.Started = today
	b.InsertItem(e, "working", len(b.StageItems("working")), it)
	touchUpdated(e, today)

	t.stage("board.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: "board.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(false)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// ---------------------------------------------------------------------------
// Pause
// ---------------------------------------------------------------------------

func (s *Store) pauseV2(t *tx, id ID, req PauseRequest, today Date) (Item, TxResult, error) {
	var zero Item
	it := t.model.find(id)
	if it.Stage != "working" {
		return zero, TxResult{}, fmt.Errorf(
			"%w: %s is on stage %q, not working; there is nothing to pause", ErrConflict, id, it.Stage)
	}

	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}
	dest := req.Stage
	if dest == "" {
		dest = "ready"
	}
	if _, err := ParseStage(string(dest), b.stageCfg.Stages); err != nil {
		return zero, TxResult{}, err
	}
	if b.stageCfg.StageNeedsReason(dest) && it.Reason == "" {
		return zero, TxResult{}, fmt.Errorf(
			"%w: pausing %s onto stage %q needs a reason: (needs_reason)", ErrInvalidArgument, id, dest)
	}

	before := RenderItemLine(it)
	b.RemoveItem(e, it)
	index := 0
	if req.End {
		index = len(b.StageItems(dest))
	}
	b.InsertItem(e, dest, index, it)
	touchUpdated(e, today)

	t.stage("board.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: "board.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// ---------------------------------------------------------------------------
// Finish
// ---------------------------------------------------------------------------

func (s *Store) finishV2(t *tx, id ID, req FinishRequest, outcome Outcome, when Date, today Date) (Item, TxResult, error) {
	var zero Item
	it := t.model.find(id)

	d, de, err := t.done()
	if err != nil {
		return zero, TxResult{}, err
	}
	b, be, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}

	notes := req.Note
	if notes != "" {
		if err := t.preserveNotes(it, notes, today); err != nil {
			return zero, TxResult{}, err
		}
	}

	before := RenderItemLine(it)
	b.RemoveItem(be, it)
	touchUpdated(be, today)

	it.Done = when
	it.Outcome = outcome
	it.Stage = ""
	// tickler: only lives on a board stage (§5.1.4); done.md is not one, so
	// finishing a scheduled item drops the schedule the same way version 1
	// drops it on entering done.md.
	it.Tickler = ""
	it.TicklerDest = ""

	d.InsertItem(de, when.Month7(), it)
	touchUpdated(de, today)

	t.stage("done.md", "board.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: "done.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// ---------------------------------------------------------------------------
// Move
// ---------------------------------------------------------------------------

func (s *Store) moveV2(t *tx, id ID, req MoveRequest, today Date) (Item, TxResult, error) {
	var zero Item
	it := t.model.find(id)

	b, e, err := t.board()
	if err != nil {
		return zero, TxResult{}, err
	}

	from := it.Stage
	to := from
	if req.Stage != "" {
		if _, err := ParseStage(string(req.Stage), b.stageCfg.Stages); err != nil {
			return zero, TxResult{}, err
		}
		to = req.Stage
	}

	if to != from {
		if limit, ok := b.stageCfg.WipLimits[to]; ok {
			used := len(b.StageItems(to))
			if used >= limit {
				return zero, TxResult{}, &StageWipLimitError{Stage: to, Limit: limit,
					Occupants: b.StageItems(to)}
			}
		}
	}

	// working requires started: (I7, folded in from version 1's I4) the same
	// way a needs_reason stage requires reason: below — a structural
	// requirement of the destination, not a --start-only ceremony. --move is
	// a plainer path than --start, but it must still land on a valid stage.
	if to == "working" && it.Started.IsZero() {
		it.Started = today
	}

	// §5.1.5: reason required entering a needs_reason stage; NOT dropped on
	// exit (unlike version 1's blocked:, which I5 forbade outside Blocked).
	if b.stageCfg.StageNeedsReason(to) && it.Reason == "" {
		if req.Reason == "" {
			return zero, TxResult{}, fmt.Errorf(
				"%w: moving %s onto stage %q needs a reason: (needs_reason)", ErrInvalidArgument, id, to)
		}
		it.Reason = req.Reason
	} else if req.Reason != "" {
		it.Reason = req.Reason
	}

	// §5.1.4: tickler:/tickler_dest: only survive on a tickler_stages source.
	if _, ok := b.stageCfg.TicklerDestOf(to); !ok {
		it.Tickler = ""
		it.TicklerDest = ""
	}

	before := RenderItemLine(it)
	b.RemoveItem(e, it)
	dest := b.StageItems(to)
	index, err := resolveIndexV2(req, dest, from == to)
	if err != nil {
		return zero, TxResult{}, err
	}
	b.InsertItem(e, to, index, it)
	touchUpdated(e, today)

	t.stage("board.md")
	t.record(Change{Kind: ChangeMoved, ID: id, File: "board.md",
		Before: before, After: RenderItemLine(it)})

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return *it, res, nil
}

// resolveIndexV2 is resolveIndex generalized to a stage's item run instead
// of a backlog section's.
func resolveIndexV2(req MoveRequest, items []*Item, sameStage bool) (int, error) {
	switch {
	case req.Top:
		return 0, nil
	case req.End:
		return len(items), nil
	case req.Position > 0:
		if req.Position > len(items)+1 {
			shown := len(items)
			if sameStage {
				shown++
			}
			return 0, fmt.Errorf(
				"%w: position %d is past the end of the stage, which holds %d item(s)",
				ErrInvalidArgument, req.Position, shown)
		}
		return req.Position - 1, nil
	case req.Before != "":
		i, ok := indexOf(items, req.Before)
		if !ok {
			return 0, fmt.Errorf("%w: %s is not on the destination stage", ErrInvalidArgument, req.Before)
		}
		return i, nil
	case req.After != "":
		i, ok := indexOf(items, req.After)
		if !ok {
			return 0, fmt.Errorf("%w: %s is not on the destination stage", ErrInvalidArgument, req.After)
		}
		return i + 1, nil
	}
	return len(items), nil
}

// StageWipLimitError reports a full stage, the version-2 analogue of
// WipLimitError (which names a working-file slot set that no longer exists).
type StageWipLimitError struct {
	Stage     Stage
	Limit     int
	Occupants []*Item
}

func (e *StageWipLimitError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "wip limit reached on stage %s (%d/%d)", e.Stage, e.Limit, e.Limit)
	for _, it := range e.Occupants {
		fmt.Fprintf(&b, "\n  %s  %s", it.ID, it.Title)
	}
	fmt.Fprintf(&b, "\nfinish one, move one off %s, or raise the limit with --wip", e.Stage)
	return b.String()
}

func (e *StageWipLimitError) Unwrap() error { return ErrWipLimitReached }
