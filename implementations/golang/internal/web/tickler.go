package web

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The tickler in the UI (spec-gui.md §2.4, §4.2, §5.5, §5.6).
//
// Three pieces, all wired here:
//
//   - the someday-card badge: the next fire, computed server-side with
//     Schedule.next(today) on every board render (§5.5);
//   - the Wake-up group: the controls, the server-side composition of the
//     tickler: value from them, and the reverse parse that pre-fills them
//     from an item's schedule (§5.6, §4.2);
//   - the service goroutine: Store.tick per held board on tickler.interval
//     (§2.4).
//
// The library owns the grammar. This file never re-implements it: the badge
// reads Schedule.shape accessors, the pre-fill reads them too, composition
// hands the library a value it validates on write, and the service loop calls
// Store.Tick directly.

// ---------------------------------------------------------------------------
// The someday-card badge (§5.5)
// ---------------------------------------------------------------------------

// ticklerBadge is the next-fire badge of a someday card carrying tickler:.
//
// text is what the badge reads; next is the data-next date, empty when the
// schedule is spent or already due (Next returned null). The text rules are
// the spec's: a recurring schedule shows the weekday name of the next fire
// plus its time; a one-shot shows its date, with the time appended; a null
// next fire reads "due" — the next tick fires it.
func ticklerBadge(it mm.Item, today mm.Date) (text, next string) {
	sched, err := mm.ParseSchedule(it.Tickler)
	if err != nil {
		// I7 shape-only validation means a someday item can carry a schedule
		// the checker accepts but this parser cannot (the checker checks the
		// expression's SHAPE, never its meaning; spec-file-format.md §7). The
		// badge shows the expression and no date rather than refusing to
		// render the board — --check and the check view are where a bad
		// schedule is reported.
		return "next " + it.Tickler, ""
	}
	fire := sched.Next(today)
	if fire.IsZero() {
		return "due", ""
	}
	hour, minute, hasTime := sched.Time()
	timePart := ""
	if hasTime {
		timePart = fmt.Sprintf(" %02d:%02d", hour, minute)
	}
	if sched.IsOneShot() {
		return "next " + fire.String() + timePart, fire.String()
	}
	return "next " + weekdayName(fire.Weekday()) + timePart, fire.String()
}

// weekdayName is the three-letter name of a day, capitalised as the badge
// shows it. Date.Weekday is the time package's Sunday-first numbering, and the
// badge text is presentation — both stay in this package.
func weekdayName(wd time.Weekday) string {
	switch wd {
	case time.Sunday:
		return "Sun"
	case time.Monday:
		return "Mon"
	case time.Tuesday:
		return "Tue"
	case time.Wednesday:
		return "Wed"
	case time.Thursday:
		return "Thu"
	case time.Friday:
		return "Fri"
	case time.Saturday:
		return "Sat"
	}
	return ""
}

// ---------------------------------------------------------------------------
// The Wake-up group (§5.6)
// ---------------------------------------------------------------------------

// ticklerGroupData is the view model of the Wake-up partial. The controls are
// pre-filled from the item's parsed schedule (Present) or left at kind never
// with everything empty (not Present).
type ticklerGroupData struct {
	Present bool // the item carries a tickler:
	Kind    string
	Date    string // one-time
	Weekday string // weekly, mon..sun
	Ordinal string // weekly, first..fourth,last, "" = every
	Month   string // monthly, 1..31 or "last"
	Time    string // @HH:MM without the @, "" = no time

	// Dest and DestOptions are version 2's tickler-dest (§5.1.4, §5.6): the
	// select's current value (the item's own tickler_dest, or the source
	// stage's default when it carries none) and the full list of declared
	// stages to route to. Both stay empty for a version-1 directory, which
	// has no such field.
	Dest        string
	DestOptions []stageOption
}

// prefillTickler parses an item's tickler back into the controls (§5.6
// pre-fill), through the library's shape accessors — the grammar lives in one
// place. An unparseable schedule pre-fills as nothing; the check view reports
// it.
func prefillTickler(it mm.Item) ticklerGroupData {
	if it.Tickler == "" {
		return ticklerGroupData{Kind: "never"}
	}
	sched, err := mm.ParseSchedule(it.Tickler)
	if err != nil {
		return ticklerGroupData{Kind: "never"}
	}
	g := ticklerGroupData{Present: true}
	hour, minute, hasTime := sched.Time()
	if hasTime {
		g.Time = fmt.Sprintf("%02d:%02d", hour, minute)
	}
	switch {
	case sched.IsOneShot():
		g.Kind = "one-time"
		g.Date = sched.FireDate().String()
		return g
	}
	if wd, ok := sched.Weekday(); ok {
		g.Kind = "weekly"
		g.Weekday = wd.String()
		g.Ordinal, _ = sched.Ordinal()
		return g
	}
	g.Kind = "monthly"
	if day, ok := sched.Monthday(); ok {
		if day == 0 {
			g.Month = "last"
		} else {
			g.Month = fmt.Sprintf("%d", day)
		}
	}
	return g
}

// composeTickler reads the Wake-up group's controls out of a form and composes
// the tickler: value the library will validate (spec-gui.md §4.2: "the server
// composes, the library validates on write").
//
// present=false means the form carried no tickler controls at all — the group
// was not rendered (a non-someday item's panel, a new item in Ready) — and the
// field must be left exactly as it is. present=true with an empty schedule
// means kind never: remove any existing tickler.
func composeTickler(c *echo.Context) (schedule string, present bool, err error) {
	kind, ok := formValue(c, "tickler-kind")
	if !ok {
		return "", false, nil
	}
	present = true
	timePart := ""
	if v, ok := formValue(c, "tickler-time"); ok && v != "" {
		timePart = "@" + v
	}
	switch kind {
	case "never":
		return "", true, nil
	case "one-time":
		date, ok := formValue(c, "tickler-date")
		if !ok || date == "" {
			return "", true, fmt.Errorf("%w: one-time needs tickler-date", mm.ErrInvalidArgument)
		}
		return date + timePart, true, nil
	case "weekly":
		weekday, ok := formValue(c, "tickler-weekday")
		if !ok || weekday == "" {
			return "", true, fmt.Errorf("%w: weekly needs tickler-weekday", mm.ErrInvalidArgument)
		}
		ordinal := ""
		if v, ok := formValue(c, "tickler-ordinal"); ok && v != "" {
			ordinal = v + "-"
		}
		return ordinal + weekday + timePart, true, nil
	case "monthly":
		month, ok := formValue(c, "tickler-monthday")
		if !ok || month == "" {
			return "", true, fmt.Errorf("%w: monthly needs tickler-monthday", mm.ErrInvalidArgument)
		}
		if month != "last" {
			if n, err := parseMonthday(month); err != nil {
				return "", true, err
			} else {
				// The format zero-pads to two digits: 05@08:00, never 5@08:00
				// (spec-file-format.md §3.3).
				month = fmt.Sprintf("%02d", n)
			}
		}
		return month + timePart, true, nil
	default:
		return "", true, fmt.Errorf("%w: tickler-kind must be one of never, one-time, weekly, monthly, got %q",
			mm.ErrInvalidArgument, kind)
	}
}

// emptyTicklerFor is the new-item panel's Wake-up group before anything has
// been typed: kind never, with version 2's destination options and stage's
// own tickler_stages default pre-selected when it has one (§5.1.4) — nothing
// to compose until the user picks a kind, but the select still needs
// something to show.
func emptyTicklerFor(dir mm.Directory, stage string) ticklerGroupData {
	g := ticklerGroupData{Kind: "never"}
	if dir.Version == 2 {
		g.DestOptions = stageOptionsFor(dir)
		if dest, ok := dir.StageCfg.TicklerDestOf(mm.Stage(stage)); ok {
			g.Dest = string(dest)
		}
	}
	return g
}

// composeTicklerDest reads the Wake-up group's tickler-dest override (version
// 2 only, §5.1.4, §5.6). present reports whether the form carried the control
// at all — a version-1 form and a non-eligible item's form never render it,
// same as composeTickler's own present. override reports whether the
// submitted value differs from def, the caller's default destination for the
// item's current (or, on the new panel, chosen) stage: composing only the
// difference is what keeps a board that never overrides a destination from
// writing any tickler_dest: at all.
func composeTicklerDest(c *echo.Context, def mm.Stage) (dest mm.Stage, override, present bool) {
	v, ok := formValue(c, "tickler-dest")
	if !ok {
		return "", false, false
	}
	dest = mm.Stage(v)
	return dest, dest != "" && dest != def, true
}

// parseMonthday reads a monthly tickler-monthday control value.
func parseMonthday(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 1 || n > 31 {
		return 0, fmt.Errorf("%w: tickler-monthday must be 1-31 or \"last\", got %q",
			mm.ErrInvalidArgument, s)
	}
	return n, nil
}

// applyTickler folds a composed tickler into an add or update request. It is
// what §4.2 means by "setting or removing the field in the same transaction as
// the rest of the form": present means the group was on the form, and the
// library's I7 placement rule (Someday only) is its check to make, not a copy
// of the rule here.
func applyTicklerAdd(req *mm.AddRequest, schedule string, present bool) {
	if !present {
		return
	}
	req.Tickler = schedule
}

func applyTicklerUpdate(req *mm.UpdateRequest, schedule string, present bool) {
	if !present {
		return
	}
	if schedule == "" {
		req.Unset = append(req.Unset, "tickler")
		return
	}
	req.Set = append(req.Set, mm.Field{Key: "tickler", Value: schedule})
}

// ---------------------------------------------------------------------------
// The service goroutine (§2.4)
// ---------------------------------------------------------------------------

// tickler owns the tickler service's goroutine so the interval can change at
// runtime (T-0207): a settings save or a config PUT applies without a
// restart. apply starts the loop when turned on, stops it when turned off,
// restarts it with a new duration when changed, and never leaks a stopped
// loop — a change waits for the previous loop to exit before starting the
// next. The loop itself keeps the §2.4 semantics: one pass per interval over
// held boards only, stopping with the service's context.
//
// The controller is deliberately dumb about configuration — it is handed an
// interval and reports whether the effective state changed; the caller
// decides what to log. The interval comes from the system config key
// tickler.interval; a nil/absent key never reaches apply.
type tickler struct {
	ctx  context.Context
	pass func() // one tick pass over every held board

	applyMu  sync.Mutex // serializes interval changes
	mu       sync.Mutex
	stop     chan struct{} // nil while off
	interval time.Duration
	wg       sync.WaitGroup
}

func newTickler(ctx context.Context, pass func()) *tickler {
	return &tickler{ctx: ctx, pass: pass}
}

// apply switches the running loop to interval; 0 is off. It reports whether
// the effective state changed, so the caller logs a transition instead of
// chattering on every config reload that did not touch the tickler.
func (t *tickler) apply(interval time.Duration) bool {
	t.applyMu.Lock()
	defer t.applyMu.Unlock()

	t.mu.Lock()
	changed := t.interval != interval
	stop := t.stop
	t.mu.Unlock()
	if !changed {
		return false
	}
	if stop != nil {
		close(stop)
		t.mu.Lock()
		t.stop = nil
		t.mu.Unlock()
		t.wg.Wait()
	}
	t.mu.Lock()
	t.interval = interval
	t.mu.Unlock()
	if interval <= 0 {
		return true
	}
	stop = make(chan struct{})
	t.mu.Lock()
	t.stop = stop
	t.mu.Unlock()
	t.wg.Add(1)
	go t.run(interval, stop)
	return true
}

// run is one loop lifetime: a pass per interval until the service stops or
// the loop is replaced. It is deliberately dumb — no backoff, no retry, no
// catch-up pass. tick is date-granular and idempotent across overlapping
// runners (§2.4), so a missed run is caught up by the next one and a UI
// service and a CLI cron can tick one board at once. Retrying here would just
// re-raise the same per-item errors a minute later.
func (t *tickler) run(interval time.Duration, stop <-chan struct{}) {
	defer t.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			t.pass()
		}
	}
}

// tickHeld runs one tick pass over every board the service holds.
//
// Logging is the §2.4 audit trail: "The service MUST log what fired — item,
// kind, spawned ID". Each fire is one Info line with the fields a log
// consumer can act on; per-item failures are Warn lines. A board-level failure
// (an unreadable directory) is logged and the pass continues to the next
// board — one broken board must not starve the others. Before the run, the
// board's scheduled set is listed (logScheduled) so the log answers "why
// didn't it move" — the fires alone only show the half that did.
func (s *Server) tickHeld() {
	today, err := mm.ParseDate(mm.NewTimestamp(s.registry.now()).String()[:10])
	if err != nil {
		s.log.Warn("tickler", "error", err)
		return
	}
	for _, store := range s.registry.heldStores() {
		s.logScheduled(store, today)
		res, err := store.Tick(today, false)
		if err != nil {
			s.log.Warn("tickler", "directory", store.Path(), "error", err)
			continue
		}
		for _, f := range res.Fired {
			s.log.Info("tickler fired",
				"item", string(f.ID), "kind", string(f.Kind),
				"spawned", string(f.Spawned), "tickled", f.Tickled.String())
		}
		for _, e := range res.Errors {
			s.log.Warn("tickler", "item", string(e.ID), "error", e.Error.Error())
		}
	}
}

// logScheduled emits the scheduled half of the §2.4 audit trail: one Info
// line per scheduled someday item — its schedule, when it last fired, when it
// will next fire, and whether the run's own due test says it is due now — and
// one line saying nothing is scheduled, so a board the service holds but
// never acts on is visible in the log instead of looking like a silent skip.
//
// The listing comes from store.Ticklers, the library's read-only view of the
// same due test the run uses, so the log and the run agree by construction:
// an item logged with next=none but due=true is the backdated one-shot that
// the very next Tick fires (spec-tools.md §5.3.3's "overdue, fire now").
func (s *Server) logScheduled(store *mm.Store, today mm.Date) {
	scheduled, err := store.Ticklers(today)
	if err != nil {
		s.log.Warn("tickler", "directory", store.Path(), "error", err)
		return
	}
	if len(scheduled) == 0 {
		s.log.Info("tickler scheduled", "directory", store.Path(), "count", 0)
		return
	}
	for _, t := range scheduled {
		last, next := t.Last.String(), t.Next.String()
		if t.Last.IsZero() {
			last = "never"
		}
		if t.Next.IsZero() {
			next = "none"
		}
		s.log.Info("tickler scheduled",
			"directory", store.Path(),
			"item", string(t.ID),
			"schedule", t.Schedule,
			"tickled", last,
			"next", next,
			"due", t.Due)
	}
}

// heldStores returns every open store, deterministically ordered, for
// background passes that act on what the service holds (§2.4).
func (r *registry) heldStores() []*mm.Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*mm.Store, 0, len(r.stores))
	ids := make([]string, 0, len(r.stores))
	for id := range r.stores {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		out = append(out, r.stores[id])
	}
	return out
}
