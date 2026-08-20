package mm

import "fmt"

// The tickler, from spec-tools.md §5.3.3 and §6.1.
//
// A Someday item may carry tickler: SCHEDULE. A tick run — the CLI's --tick, a
// UI service's tickler.interval goroutine — asks each scheduled item whether it
// is due, and fires the ones that are:
//
//   - one-shot (a bare date): the item MOVES to Ready, its tickler: is dropped
//     and tickled:<today> is stamped. The schedule is consumed by firing.
//   - recurring (weekday/monthday shapes): the item is a prototype. It stays in
//     Someday with tickled:<today> stamped, and a NEW Ready item is spawned
//     carrying the title, prio and tags — and nothing else. No detail: line is
//     copied, because I9 claims every detail file exactly once (the no-detail-
//     copy rule); a spawned task gets a fresh ID from next_id (I2).
//
// Every fire is its own single-file transaction in backlog.md, and the fires
// of one run never abort one another: a failing item is reported per-item and
// the run continues (spec-tools.md §5.3.3 rule: "a failing item never aborts
// the run").

// Tickler is one scheduled item, as Store.Ticklers reports it.
type Tickler struct {
	ID       ID
	Schedule string // the expression, verbatim
	Dest     Stage  // version 2 only: where a fire would land (§5.1.4)
	Last     Date   // tickled:, zero when never fired
	Next     Date   // Schedule.next(last ?? created), zero when the schedule is spent
	Due      bool   // the run's own due test at the listing's now: due on or before it
}

// FiredTickler reports one item a tick run fired.
type FiredTickler struct {
	ID      ID
	Kind    FireKind
	Dest    Stage  // version 2 only: the stage the fire landed on (§5.1.4)
	Tickled Date   // the date stamped as tickled:
	Spawned ID     // the spawned item, for Kind == FireSpawn
	Before  string // the prototype's line before the fire
	After   string // the prototype's line after the fire
}

// FireKind says what one fire did to the item.
type FireKind string

const (
	FireMove  FireKind = "move"  // one-shot: the item moved to Ready
	FireSpawn FireKind = "spawn" // recurring: a new Ready item was created
)

// TickError is one item's failure during a tick run; the run itself continues.
type TickError struct {
	ID    ID
	Error error
}

// TickResult is what one tick run did, dry run or real.
type TickResult struct {
	Fired []FiredTickler
	// Errors are per-item: a malformed schedule or a failing transaction never
	// aborts the run, it is reported here and the rest of the run proceeds.
	Errors []TickError
}

// Ticklers lists every scheduled item: version 1's ## Someday, or version
// 2's items on any stage (spec-tools.md §6.1).
//
// now is where the caller judges "due". Next is Schedule.next(tickled ??
// created), the same anchor the due test uses, and Due is Schedule.Due itself
// — the run's test, not an approximation — so the listing and the run agree
// about what has already happened, including the backdated one-shot whose
// Next is zero but which is due right now (§5.3.3: "overdue, fire now").
// Items whose schedule does not parse are I7 violations that --check reports;
// a read-only listing skips them rather than inventing an error channel.
func (s *Store) Ticklers(now Date) ([]Tickler, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return nil, err
	}
	var items []*Item
	switch {
	case m.board != nil:
		// Any stage may be a tickler_stages source (§5.1.4); a listing does
		// not enforce placement, it reports what is on disk.
		items = m.board.Items
	case m.backlog != nil:
		if sec := m.backlog.Section(SectionSomeday); sec != nil {
			items = sec.Items
		}
	}

	var out []Tickler
	for _, it := range items {
		if it.Tickler == "" {
			continue
		}
		sch, err := ParseSchedule(it.Tickler)
		if err != nil {
			continue // an I7 shape violation; --check reports it
		}
		after := it.Tickled
		if after.IsZero() {
			after = it.Created
		}
		tk := Tickler{
			ID:       it.ID,
			Schedule: it.Tickler,
			Last:     it.Tickled,
			Next:     sch.Next(after),
			Due:      sch.Due(now, it.Tickled, it.Created),
		}
		if m.board != nil {
			dest, ok := m.board.stageCfg.TicklerDestOf(it.Stage)
			if it.TicklerDest != "" {
				dest, ok = it.TicklerDest, true
			}
			if ok {
				tk.Dest = dest
			}
		}
		out = append(out, tk)
	}
	return out, nil
}

// Tick runs the tickler once (spec-tools.md §5.3.3): every due item fires,
// each fire in its own transaction. dryRun validates and reports without
// writing anything, and the two runs are guaranteed to agree — the due test
// is the same, and dry-run fires are not stamped, so a real run a minute
// later fires the same set.
//
// A tick is date-granular like every operation: now is the caller's today,
// and an item fires at most once per day, which the tickled: stamp enforces.
// Two runners on overlapping schedules are safe: the stamp is written in the
// same transaction as the fire, so the second runner re-reads a directory
// that no longer owes the item, or loses a begin/commit race to rule 5 of
// spec-tools.md §5.3.3 (ErrConcurrent) and reports the item per-fire.
func (s *Store) Tick(now Date, dryRun bool) (TickResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return TickResult{}, err
	}
	if m.isV2() {
		return s.tickLoopV2(now, dryRun)
	}
	return s.tickLoopV1(now, dryRun)
}

// tickLoopV1 is Tick's version-1 body: every ## Someday item fires into
// ## Ready. s.mu is already held by Tick.
func (s *Store) tickLoopV1(now Date, dryRun bool) (TickResult, error) {
	var res TickResult
	// done is the run's own ledger: every item the run has fired or failed,
	// which is what keeps a dry run (nothing is written, so nothing on disk
	// changes) and a run with persistent failures from looping forever.
	done := map[ID]bool{}

	for {
		t, err := s.begin()
		if err != nil {
			return res, err
		}
		b, e, err := t.backlog()
		if err != nil {
			return res, err
		}
		sec := b.Section(SectionSomeday)
		if sec == nil {
			return res, nil // nothing can be scheduled
		}

		// Pick the first still-undone item that is due. Order is file order,
		// which is deterministic: two runs over the same directory fire the
		// same sequence, dry run and real run alike.
		var due *Item
		for _, it := range sec.Items {
			if done[it.ID] || it.Tickler == "" {
				continue
			}
			sch, perr := ParseSchedule(it.Tickler)
			if perr != nil {
				// Hand-edited garbage is an I7 shape violation, but a tick run
				// is not --check: the item must not wedge the run, so it is
				// reported per-item and the run moves on.
				res.Errors = append(res.Errors, TickError{ID: it.ID, Error: perr})
				done[it.ID] = true
				continue
			}
			if sch.Due(now, it.Tickled, it.Created) {
				due = it
				break
			}
		}
		if due == nil {
			return res, nil
		}

		fired, ferr := fireOne(t, b, e, due, now, dryRun)
		if ferr != nil {
			res.Errors = append(res.Errors, TickError{ID: due.ID, Error: ferr})
		} else {
			res.Fired = append(res.Fired, fired)
		}
		done[due.ID] = true
	}
}

// fireOne performs one item's fire inside its own transaction.
//
// One-shot: move to Ready, drop the schedule, stamp tickled:today. Recurring:
// stamp the prototype (it stays in Someday) and spawn a fresh Ready item whose
// fields are title, prio and tags only — no detail:, no refs, no unregistered
// extras, no created-from-the-prototype (the spawn is created today). The
// no-detail-copy rule is what keeps I9's "every detail file is claimed by
// exactly one item" true for the spawned item.
func fireOne(t *tx, b *backlogFile, e *fileEdit, it *Item, now Date, dryRun bool) (FiredTickler, error) {
	sch, err := ParseSchedule(it.Tickler)
	if err != nil {
		return FiredTickler{}, err // unreachable: Tick only fires valid schedules
	}
	fired := FiredTickler{ID: it.ID, Tickled: now}

	if sch.IsOneShot() {
		if b.Section(SectionReady) == nil {
			return fired, fmt.Errorf("%w: backlog.md has no ## Ready section to move %s into",
				ErrNotFound, it.ID)
		}
		before := RenderItemLine(it)
		it.Tickler = ""
		it.Tickled = now
		b.RemoveItem(e, it)
		dest := b.Section(SectionReady)
		b.InsertItem(e, SectionReady, len(dest.Items), it)
		t.record(Change{Kind: ChangeMoved, ID: it.ID, File: "backlog.md",
			Before: before, After: RenderItemLine(it)})
		fired.Kind = FireMove
		fired.Before = before
		fired.After = RenderItemLine(it)
	} else {
		if b.Section(SectionReady) == nil {
			return fired, fmt.Errorf("%w: backlog.md has no ## Ready section to spawn %s's task into",
				ErrNotFound, it.ID)
		}
		before := RenderItemLine(it)
		it.Tickled = now
		e.ReplaceItem(it)
		t.record(Change{Kind: ChangeUpdated, ID: it.ID, File: "backlog.md",
			Before: before, After: RenderItemLine(it)})
		fired.Kind = FireSpawn
		fired.Before = before
		fired.After = RenderItemLine(it)

		g := t.model.grammar()
		nid, err := allocNext(e, b, g)
		if err != nil {
			return fired, err
		}
		spawn := &Item{
			ID:      nid,
			Title:   it.Title,
			State:   StateBacklog,
			Section: SectionReady,
			Prio:    it.Prio,
			Tags:    append([]string(nil), it.Tags...),
			Created: now,
		}
		dest := b.Section(SectionReady)
		b.InsertItem(e, SectionReady, len(dest.Items), spawn)
		t.record(Change{Kind: ChangeCreated, ID: nid, File: "backlog.md",
			After: RenderItemLine(spawn)})
		fired.Spawned = nid
	}

	touchUpdated(e, now)
	t.stage("backlog.md")
	if _, err := t.commit(dryRun); err != nil {
		return fired, err
	}
	return fired, nil
}
