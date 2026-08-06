package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helpers: add a scheduled someday item the way the GUI would, via the API.
func addScheduled(t *testing.T, s *Store, title, schedule string, created Date) Item {
	t.Helper()
	it, _, err := s.Add(AddRequest{
		Title:   title,
		Section: SectionSomeday,
		Tickler: schedule,
		Created: created,
	}, today)
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
	return it
}

func somedayItems(t *testing.T, s *Store) []Item {
	t.Helper()
	items, err := s.List(Filter{Section: SectionSomeday})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func readyItems(t *testing.T, s *Store) []Item {
	t.Helper()
	items, err := s.List(Filter{Section: SectionReady})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// The core fire: a one-shot moves to Ready, drops its tickler and stamps
// tickled: today. The schedule is consumed by firing.
func TestTickOneShotMovesToReady(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	it := addScheduled(t, s, "Garden", "2026-08-01", Date{2026, 7, 20})

	res, err := s.Tick(Date{2026, 8, 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || len(res.Errors) != 0 {
		t.Fatalf("fired = %+v errors = %+v, want exactly one fire", res.Fired, res.Errors)
	}
	f := res.Fired[0]
	if f.ID != it.ID || f.Kind != FireMove {
		t.Errorf("fired = %+v, want a move of %s", f, it.ID)
	}
	if !strings.Contains(f.After, "tickled:2026-08-01") {
		t.Errorf("after = %q, want a tickled stamp", f.After)
	}
	if strings.Contains(f.After, "tickler:") {
		t.Errorf("after = %q, want the tickler dropped", f.After)
	}

	// Where it sits now: Ready, no tickler, tickled stamped.
	got, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateBacklog || got.Section != SectionReady {
		t.Errorf("item now %s/%s, want Ready", got.State, got.Section)
	}
	if got.Tickler != "" {
		t.Errorf("Tickler = %q, want dropped", got.Tickler)
	}
	if got.Tickled != (Date{2026, 8, 1}) {
		t.Errorf("Tickled = %s, want 2026-08-01", got.Tickled)
	}
	remaining := somedayItems(t, s)
	if len(remaining) != 1 || remaining[0].ID == it.ID {
		t.Errorf("Someday after the fire = %v, want only the fixture's own item", idsOfValues(remaining))
	}
}

// A recurring item is a prototype: it stays in Someday with tickled stamped,
// and a fresh Ready item is spawned. The spawn copies title/prio/tags ONLY -
// no detail, no refs, no extras (the no-detail-copy rule, I9).
func TestTickRecurringSpawnsReadyItem(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	proto, _, err := s.Add(AddRequest{
		Title:   "Standup notes",
		Section: SectionSomeday,
		Tickler: "mon@09:00",
		Tags:    []string{"ritual"},
		Created: Date{2026, 7, 27},
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	// The detail file must exist for I9, so it is written before the update
	// that points at it - the commit validates against a fresh load.
	detailsDir := filepath.Join(s.Path(), "details")
	if err := os.MkdirAll(detailsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(detailsDir, string(proto.ID)+".md"), []byte(
		"---\ndoc: detail\nid: "+string(proto.ID)+"\ntitle: Standup notes\nupdated: 2026-07-29\n---\n\n# "+string(proto.ID)+" — Standup notes\n\nLong form.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Give the prototype a detail file, a ref and an extra: none may travel.
	_, _, err = s.Update(proto.ID, UpdateRequest{
		Set: []Field{
			{"detail", proto.DetailPath()},
			{"owner", "dana"},
			{"refs", "ops:T-001"},
		},
	}, today)
	if err != nil {
		t.Fatal(err)
	}

	res, err := s.Tick(Date{2026, 8, 3}, false) // the next Monday
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 {
		t.Fatalf("fired = %+v, want one", res.Fired)
	}
	f := res.Fired[0]
	if f.Kind != FireSpawn || f.Spawned == "" {
		t.Fatalf("fired = %+v, want a spawn", f)
	}

	// The prototype stays in Someday, stamped.
	p, err := s.Get(proto.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Section != SectionSomeday || p.Tickler != "mon@09:00" {
		t.Errorf("prototype = %+v, want still scheduled in Someday", p)
	}
	if p.Tickled != (Date{2026, 8, 3}) {
		t.Errorf("prototype Tickled = %s, want 2026-08-03", p.Tickled)
	}

	// The spawn is a fresh item in Ready with the prototype's title/prio/tags.
	spawn, err := s.Get(f.Spawned)
	if err != nil {
		t.Fatal(err)
	}
	if spawn.State != StateBacklog || spawn.Section != SectionReady {
		t.Errorf("spawn = %s/%s, want Ready", spawn.State, spawn.Section)
	}
	if spawn.Title != "Standup notes" || spawn.Prio != p.Prio {
		t.Errorf("spawn title/prio = %q/%s, want the prototype's", spawn.Title, spawn.Prio)
	}
	if !sameStrings(spawn.Tags, p.Tags) {
		t.Errorf("spawn tags = %v, want %v", spawn.Tags, p.Tags)
	}
	if spawn.Detail != "" {
		t.Errorf("spawn detail = %q, want none (I9's one-claim rule)", spawn.Detail)
	}
	if len(spawn.Refs) != 0 || len(spawn.Extra) != 0 {
		t.Errorf("spawn refs/extra = %v/%v, want none", spawn.Refs, spawn.Extra)
	}
	if spawn.Created != (Date{2026, 8, 3}) {
		t.Errorf("spawn Created = %s, want the fire day", spawn.Created)
	}
	if spawn.Tickler != "" {
		t.Errorf("spawn Tickler = %q, want none", spawn.Tickler)
	}

	// The prototype's detail file is still claimed by the prototype alone (I9).
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("validation after a spawn: %v", vs)
	}
}

// The at-most-once-per-day guard: a second run over the same directory fires
// nothing, because tickled was written in the same transaction as the fire.
func TestTickIsIdempotentWithinADay(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	addScheduled(t, s, "Watering", "mon@08:00", Date{2026, 7, 27})
	addScheduled(t, s, "Prune", "2026-08-03", Date{2026, 7, 20})

	res, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil || len(res.Fired) != 2 {
		t.Fatalf("first run: %d fired, err %v; want 2", len(res.Fired), err)
	}
	res, err = s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(res.Errors) != 0 {
		t.Errorf("second run fired %d (errors %d), want none", len(res.Fired), len(res.Errors))
	}
}

// Two runners on overlapping schedules are safe: the stamp is transactional, so
// the second runner re-reads a directory that no longer owes the item.
func TestTickConcurrentRunnersStayConsistent(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	addScheduled(t, s, "Watering", "mon@08:00", Date{2026, 7, 27})

	res, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	// A stale reader that began before the write loses the begin/commit race
	// and is refused by rule 5 (ErrConcurrent); a fresh reader sees nothing
	// due. Either way nothing double-fires: replay the directory from disk.
	again := mustOpen(t, s.Path())
	res, err = again.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 {
		t.Errorf("reopened store fired %d, want 0", len(res.Fired))
	}
}

// Dry run and real run agree: the same fires, and a real run a minute later
// fires the same set because dry-run fires are not stamped.
func TestTickDryRunMatchesRealRun(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	addScheduled(t, s, "Watering", "mon@08:00", Date{2026, 7, 27})
	addScheduled(t, s, "Prune", "2026-08-03", Date{2026, 7, 20})
	// Not due yet.
	addScheduled(t, s, "Weed", "2026-09-01", Date{2026, 7, 20})

	dry, err := s.Tick(Date{2026, 8, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Fired) != 2 {
		t.Fatalf("dry run fired %d, want 2", len(dry.Fired))
	}

	real, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(real.Fired) != 2 {
		t.Fatalf("real run fired %d, want the same 2 (dry-run must not stamp)", len(real.Fired))
	}
	for i := range real.Fired {
		if real.Fired[i].ID != dry.Fired[i].ID || real.Fired[i].Kind != dry.Fired[i].Kind {
			t.Errorf("real[%d] = %+v, dry[%d] = %+v", i, real.Fired[i], i, dry.Fired[i])
		}
	}
}

// A hand-edited malformed schedule never aborts a tick run: the line that
// carries it fails to parse, so the item simply is not in the model - the run
// fires everything else, and --check reports the shape violation. What stays
// in TickResult.Errors is the transaction-level failure of one item's fire
// (a lost begin/commit race, an invariant the fire itself would break); that
// too is per-item and never fatal.
func TestTickMalformedScheduleNeverAbortsTheRun(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	prune := addScheduled(t, s, "Prune", "2026-08-03", Date{2026, 7, 20})
	addScheduled(t, s, "Watering", "mon@08:00", Date{2026, 7, 27})

	// Corrupt the Watering schedule by editing the file directly - the API
	// refuses to write a malformed expression, which is the point.
	dir := s.Path()
	blob, err := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	src := strings.Replace(string(blob), "| tickler:mon@08:00", "| tickler:mon@25:00", 1)
	if src == string(blob) {
		t.Fatal("fixture: no tickler line to corrupt")
	}
	if err := writeFileAtomic(filepath.Join(dir, "backlog.md"), []byte(src)); err != nil {
		t.Fatal(err)
	}

	s2 := mustOpen(t, dir)
	res, err := s2.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || res.Fired[0].ID != prune.ID {
		t.Errorf("fired = %+v, want the healthy item to fire anyway", res.Fired)
	}
	// The corrupted line is a parse failure, not a per-item tick error: the
	// run reported no errors because it never saw the item. The shape
	// violation is --check's report, and it must be there.
	if len(res.Errors) != 0 {
		t.Errorf("errors = %+v, want none from a parse-level corruption", res.Errors)
	}
	vs, err := s2.Validate()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range vs {
		if strings.Contains(v.Message, "tickler") && strings.Contains(v.Message, "SCHEDULE") {
			found = true
		}
	}
	if !found {
		t.Errorf("validation after the corruption = %v, want a SCHEDULE shape finding", vs)
	}
}

// Ticklers lists every scheduled someday item with its next fire, computed
// from the same anchor the due test uses: next(tickled ?? created).
func TestTicklersListing(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	one := addScheduled(t, s, "Prune", "2026-09-01", Date{2026, 7, 20})
	rec := addScheduled(t, s, "Watering", "mon@08:00", Date{2026, 7, 27})

	tl, err := s.Ticklers(Date{2026, 8, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 2 {
		t.Fatalf("ticklers = %+v, want 2", tl)
	}
	byID := map[ID]Tickler{}
	for _, t := range tl {
		byID[t.ID] = t
	}
	if byID[one.ID].Next != (Date{2026, 9, 1}) {
		t.Errorf("one-shot next = %s, want 2026-09-01", byID[one.ID].Next)
	}
	// next(tickled ?? created): the Monday schedule was created on a Monday,
	// so the first fire is the following Monday, 2026-08-03 - the listing's
	// "next" is the anchored one, which is what the due test compares to now.
	if byID[rec.ID].Next != (Date{2026, 8, 3}) {
		t.Errorf("recurring next = %s, want 2026-08-03", byID[rec.ID].Next)
	}

	// After a run on 2026-08-03, the recurring item has stamped its fire and
	// its next moves on; the one-shot is untouched until its own date.
	if _, err := s.Tick(Date{2026, 8, 3}, false); err != nil {
		t.Fatal(err)
	}
	tl, err = s.Ticklers(Date{2026, 8, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 2 {
		t.Fatalf("ticklers after a run = %+v, want both scheduled items", tl)
	}
	for _, tick := range tl {
		if tick.ID == rec.ID {
			if tick.Last != (Date{2026, 8, 3}) || tick.Next != (Date{2026, 8, 10}) {
				t.Errorf("recurring after fire = last %s next %s, want 2026-08-03/2026-08-10",
					tick.Last, tick.Next)
			}
		}
	}
}

// A missed run catches up: the recurring prototype's last fire is more than a
// week old, so the next run fires again - and the spawn is the one missed.
func TestTickMissedRunCatchesUp(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	addScheduled(t, s, "Standup", "mon@09:00", Date{2026, 7, 27})

	if _, err := s.Tick(Date{2026, 8, 3}, false); err != nil {
		t.Fatal(err)
	}
	// A runner that misses the 10th, 17th and 24th: on the 31st it owes three
	// Mondays. The first is next(2026-08-03) = 2026-08-10 <= now.
	res, err := s.Tick(Date{2026, 8, 31}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 {
		t.Fatalf("fired = %+v, want the catch-up fire", res.Fired)
	}
	// The run fires one per day, per the due test - the other two Mondays are
	// not owed until their own runs.
	res, err = s.Tick(Date{2026, 8, 31}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 {
		t.Errorf("second catch-up run fired %d, want 0 (one fire per run)", len(res.Fired))
	}
}

// Move drops tickler when leaving Someday (I7 would reject it elsewhere) but
// keeps tickled, which is history rather than a placement claim.
func TestMoveLeavingSomedayDropsTickler(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	it := addScheduled(t, s, "Garden", "2026-08-01", Date{2026, 7, 20})
	// Fire it so tickled is set, then move it out.
	if _, err := s.Tick(Date{2026, 8, 1}, false); err != nil {
		t.Fatal(err)
	}

	moved, _, err := s.Move(it.ID, MoveRequest{Section: SectionBlocked, Blocked: "waiting"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Tickler != "" {
		t.Errorf("Tickler = %q, want dropped on leaving Someday", moved.Tickler)
	}
	if moved.Tickled != (Date{2026, 8, 1}) {
		t.Errorf("Tickled = %s, want kept as history", moved.Tickled)
	}

	// Moving INTO Someday does not create a schedule; I7 allows the empty case.
	back, _, err := s.Move(moved.ID, MoveRequest{Section: SectionSomeday}, today)
	if err != nil {
		t.Fatal(err)
	}
	if back.Tickler != "" || back.Tickled != (Date{2026, 8, 1}) {
		t.Errorf("after the round trip: tickler %q tickled %s", back.Tickler, back.Tickled)
	}
}

// I7 placement: an item carrying tickler outside Someday is a violation, and a
// scheduled item without created is one too. The validator, not just the API,
// must see both - this is what a hand edit trips over.
func TestTicklerPlacementValidation(t *testing.T) {
	t.Run("tickler in Ready", func(t *testing.T) {
		src := strings.Replace(dirBacklog,
			"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
			"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29 | tickler:mon@08:00", 1)
		s := mustOpen(t, newDir(t, map[string]string{"backlog.md": src}))
		vs, err := s.Validate()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range vs {
			if v.Invariant == "I7" && strings.Contains(v.Message, "Someday") {
				found = true
			}
		}
		if !found {
			t.Errorf("validation = %v, want an I7 placement finding", vs)
		}
	})

	t.Run("tickler in a working slot", func(t *testing.T) {
		src := strings.Replace(busySlot, "started: 2026-07-30",
			"started: 2026-07-30\ntickler: mon@08:00", 1)
		s := mustOpen(t, newDir(t, map[string]string{"working.01.md": src}))
		vs, err := s.Validate()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range vs {
			if v.Invariant == "I7" && strings.Contains(v.Message, "Someday") {
				found = true
			}
		}
		if !found {
			t.Errorf("validation = %v, want an I7 placement finding in the slot", vs)
		}
	})

	t.Run("scheduled item without created", func(t *testing.T) {
		src := strings.Replace(dirBacklog,
			"- [ ] [T-0003] Maybe | prio:low | created:2026-07-29",
			"- [ ] [T-0003] Maybe | prio:low | tickler:2026-09-01", 1)
		s := mustOpen(t, newDir(t, map[string]string{"backlog.md": src}))
		vs, err := s.Validate()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range vs {
			if v.Invariant == "I7" && strings.Contains(v.Message, "created") {
				found = true
			}
		}
		if !found {
			t.Errorf("validation = %v, want an I7 created finding", vs)
		}
	})

	t.Run("malformed schedule shape", func(t *testing.T) {
		src := strings.Replace(dirBacklog,
			"- [ ] [T-0003] Maybe | prio:low | created:2026-07-29",
			"- [ ] [T-0003] Maybe | prio:low | created:2026-07-29 | tickler:mon@25:00", 1)
		s := mustOpen(t, newDir(t, map[string]string{"backlog.md": src}))
		vs, err := s.Validate()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range vs {
			if strings.Contains(v.Message, "tickler") && strings.Contains(v.Message, "SCHEDULE") {
				found = true
			}
		}
		if !found {
			t.Errorf("validation = %v, want a shape finding", vs)
		}
	})
}

// The API refuses the shapes the format forbids before they reach the disk.
func TestTicklerAPIValidation(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))

	if _, _, err := s.Add(AddRequest{
		Title: "Nope", Section: SectionReady, Tickler: "mon@08:00",
	}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("tickler outside Someday: want ErrInvalidArgument, got %v", err)
	}
	if _, _, err := s.Add(AddRequest{
		Title: "Nope", Section: SectionSomeday, Tickler: "not a schedule",
	}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("malformed tickler: want ErrInvalidArgument, got %v", err)
	}

	// A scheduled item added to Someday always carries created (Add defaults
	// it), so the I7 pair is satisfied by construction.
	it := addScheduled(t, s, "Fine", "mon@08:00", Date{2026, 7, 27})
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("a correctly scheduled item validates dirty: %v", vs)
	}

	// setAnyField refuses a tickler on a non-Someday item.
	if _, _, err := s.Update(it.ID, UpdateRequest{
		Set: []Field{{"tickler", "fri@08:00"}},
	}, today); err != nil {
		t.Fatalf("update (still in Someday): %v", err)
	}
	// Move it out first - the move itself drops the schedule - then try again.
	moved, _, err := s.Move(it.ID, MoveRequest{Section: SectionReady}, today)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Update(moved.ID, UpdateRequest{
		Set: []Field{{"tickler", "fri@08:00"}},
	}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("setAnyField tickler outside Someday: want ErrConflict, got %v", err)
	}
}

// Finishing a scheduled item retires its prototype: the done.md line cannot
// carry tickler: (I7 makes Someday its only home), so it drops there too, and
// tickled: stays as history. The user's way to cancel a schedule must work.
func TestFinishDropsTickler(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	it := addScheduled(t, s, "Garden", "mon@08:00", Date{2026, 7, 27})
	if _, err := s.Tick(Date{2026, 8, 3}, false); err != nil {
		t.Fatal(err)
	}

	finished, _, err := s.Finish(it.ID, FinishRequest{Note: "retired the prototype"}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if finished.Tickler != "" {
		t.Errorf("Tickler = %q, want dropped on finish", finished.Tickler)
	}
	if finished.Tickled != (Date{2026, 8, 3}) {
		t.Errorf("Tickled = %s, want kept as history", finished.Tickled)
	}
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("validation after finishing a scheduled item: %v", vs)
	}
}

// A spawned item allocates from next_id (I2): the counter moves up and the
// spawn's ID is the pre-increment value.
func TestTickSpawnBumpsNextID(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	addScheduled(t, s, "Standup", "mon@09:00", Date{2026, 7, 27})

	if _, err := s.Tick(Date{2026, 8, 3}, false); err != nil {
		t.Fatal(err)
	}
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != "T-0013" {
		t.Errorf("NextID = %s, want T-0013 (spawn took T-0012)", d.NextID)
	}
	items := readyItems(t, s)
	if len(items) != 3 || items[2].ID != "T-0012" {
		t.Errorf("ready = %v, want T-0012 appended last", idsOfValues(items))
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
