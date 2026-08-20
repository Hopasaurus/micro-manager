package mm

import (
	"errors"
	"strings"
	"testing"
)

// The tickler, generalized to a stage-based board (spec-tools.md §5.3.3,
// §5.1.4). Version 1 hardcoded ## Someday as the only eligible section and
// ## Ready as every fire's destination; version 2 generalizes both to
// whatever tickler_stages declares, with an item's own tickler_dest able to
// override its stage's default.

const tickV2Board = `---
doc: board
version: 2
project: Tick V2
next_id: T-0006
updated: 2026-07-29
stages: someday,ready,review
wip.ready: 3
tickler_stages: someday->ready
---

# Board

- [ ] [T-0001] One-shot to default | stage:someday | created:2026-07-20 | tickler:2026-08-01
- [ ] [T-0002] One-shot to override | stage:someday | created:2026-07-20 | tickler:2026-08-01 | tickler_dest:review
- [ ] [T-0003] Recurring standup | stage:someday | created:2026-07-27 | tickler:mon@09:00 | tags:ritual
- [ ] [T-0004] Already in ready | stage:ready | created:2026-07-20
`

const tickV2Done = `---
doc: done
version: 2
---

# Done
`

func tickV2Dir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newV2Dir(t, map[string]string{
		"board.md": tickV2Board,
		"done.md":  tickV2Done,
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

func TestTickV2OneShotUsesStageDefault(t *testing.T) {
	_, s := tickV2Dir(t)

	res, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}

	var t1, t2 *FiredTickler
	for i := range res.Fired {
		switch res.Fired[i].ID {
		case "T-0001":
			t1 = &res.Fired[i]
		case "T-0002":
			t2 = &res.Fired[i]
		}
	}
	if t1 == nil || t1.Kind != FireMove || t1.Dest != "ready" {
		t.Errorf("T-0001 fired = %+v, want a move to ready", t1)
	}
	if t2 == nil || t2.Kind != FireMove || t2.Dest != "review" {
		t.Errorf("T-0002 fired = %+v, want a move to review (its own tickler_dest)", t2)
	}

	got, err := s.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stage != "ready" || got.Tickler != "" || got.TicklerDest != "" {
		t.Errorf("T-0001 = stage:%s tickler:%q tickler_dest:%q, want ready/empty/empty",
			got.Stage, got.Tickler, got.TicklerDest)
	}

	got2, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if got2.Stage != "review" || got2.Tickler != "" || got2.TicklerDest != "" {
		t.Errorf("T-0002 = stage:%s tickler:%q tickler_dest:%q, want review/empty/empty",
			got2.Stage, got2.Tickler, got2.TicklerDest)
	}
}

func TestTickV2RecurringSpawnsOnStageDefaultWithNoCopiedExtras(t *testing.T) {
	_, s := tickV2Dir(t)
	// Give the prototype a reason (harmless here, but must not travel) and an
	// extra field, the same no-copy assertions op_tick_test.go makes for v1.
	if _, _, err := s.Update("T-0003", UpdateRequest{
		Set: []Field{{Key: "owner", Value: "dana"}},
	}, today); err != nil {
		t.Fatal(err)
	}

	res, err := s.Tick(Date{2026, 8, 3}, false) // the next Monday
	if err != nil {
		t.Fatal(err)
	}
	var fired *FiredTickler
	for i := range res.Fired {
		if res.Fired[i].ID == "T-0003" {
			fired = &res.Fired[i]
		}
	}
	if fired == nil || fired.Kind != FireSpawn || fired.Spawned == "" || fired.Dest != "ready" {
		t.Fatalf("T-0003 fired = %+v, want a spawn onto ready", fired)
	}

	proto, err := s.Get("T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if proto.Stage != "someday" || proto.Tickler == "" {
		t.Errorf("the prototype must stay on someday with its schedule intact, got stage:%s tickler:%q",
			proto.Stage, proto.Tickler)
	}
	if proto.Tickled != (Date{2026, 8, 3}) {
		t.Errorf("prototype Tickled = %s, want 2026-08-03", proto.Tickled)
	}

	spawn, err := s.Get(fired.Spawned)
	if err != nil {
		t.Fatal(err)
	}
	if spawn.Stage != "ready" {
		t.Errorf("spawn stage = %s, want ready", spawn.Stage)
	}
	if spawn.Title != "Recurring standup" || len(spawn.Tags) != 1 || spawn.Tags[0] != "ritual" {
		t.Errorf("spawn = %+v, want title/tags copied from the prototype", spawn)
	}
	if len(spawn.Extra) != 0 {
		t.Errorf("spawn Extra = %+v, want nothing copied (the no-detail-copy rule, I9)", spawn.Extra)
	}
	if spawn.Reason != "" {
		t.Errorf("spawn Reason = %q, want empty: a reason is not copied to a spawn", spawn.Reason)
	}
}

func TestTicklersV2ListingReportsDest(t *testing.T) {
	_, s := tickV2Dir(t)
	tks, err := s.Ticklers(Date{2026, 8, 3})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[ID]Tickler{}
	for _, tk := range tks {
		byID[tk.ID] = tk
	}
	if got := byID["T-0001"]; got.Dest != "ready" || !got.Due {
		t.Errorf("T-0001 = %+v, want dest:ready due:true", got)
	}
	if got := byID["T-0002"]; got.Dest != "review" || !got.Due {
		t.Errorf("T-0002 = %+v, want dest:review (its own override)", got)
	}
	if got := byID["T-0003"]; got.Dest != "ready" {
		t.Errorf("T-0003 = %+v, want dest:ready", got)
	}
}

// A WIP-capped destination is not an invariant this format checks (§10.4:
// a limit can be violated by hand-editing same as anything else), so
// nothing but the fire itself refuses to overfill one.
func TestTickV2RefusesToOverfillAWipCappedDestination(t *testing.T) {
	const board = `---
doc: board
version: 2
project: Tick Cap
next_id: T-0004
updated: 2026-07-29
stages: someday,ready
wip.ready: 1
tickler_stages: someday->ready
---

# Board

- [ ] [T-0001] Already in ready | stage:ready | created:2026-07-20
- [ ] [T-0002] Wants in too | stage:someday | created:2026-07-20 | tickler:2026-08-01
`
	dir := newV2Dir(t, map[string]string{"board.md": board, "done.md": tickV2Done})
	s := mustOpen(t, dir)

	res, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 0 || len(res.Errors) != 1 {
		t.Fatalf("fired = %+v errors = %+v, want zero fires and one capacity error", res.Fired, res.Errors)
	}
	if !errors.Is(res.Errors[0].Error, ErrWipLimitReached) {
		t.Errorf("error = %v, want ErrWipLimitReached", res.Errors[0].Error)
	}

	// Nothing was written: the item is still scheduled, still on someday.
	it, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if it.Stage != "someday" || it.Tickler == "" || !it.Tickled.IsZero() {
		t.Errorf("T-0002 = stage:%s tickler:%q tickled:%s, want untouched", it.Stage, it.Tickler, it.Tickled)
	}
}

func TestTickV2DryRunMatchesRealRun(t *testing.T) {
	dir, s := tickV2Dir(t)
	dry, err := s.Tick(Date{2026, 8, 3}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Fired) != 3 {
		t.Fatalf("dry fired = %+v, want 3", dry.Fired)
	}
	before := readMigFile(t, dir, "board.md")

	real, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(real.Fired) != len(dry.Fired) {
		t.Errorf("real fired %d, dry fired %d, want equal", len(real.Fired), len(dry.Fired))
	}
	if got := readMigFile(t, dir, "board.md"); got == before {
		t.Error("board.md should have changed after the real run")
	}
}

func TestTickV2MalformedTicklerDestNeverAbortsTheRun(t *testing.T) {
	const board = `---
doc: board
version: 2
project: Bad Dest
next_id: T-0003
updated: 2026-07-29
stages: someday,ready
tickler_stages: someday->ready
---

# Board

- [ ] [T-0001] Points nowhere declared | stage:someday | created:2026-07-20 | tickler:2026-08-01 | tickler_dest:nope
- [ ] [T-0002] Fine | stage:someday | created:2026-07-20 | tickler:2026-08-01
`
	dir := newV2Dir(t, map[string]string{"board.md": board, "done.md": tickV2Done})
	s := mustOpen(t, dir)

	res, err := s.Tick(Date{2026, 8, 3}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fired) != 1 || res.Fired[0].ID != "T-0002" {
		t.Fatalf("fired = %+v, want just T-0002", res.Fired)
	}
	if len(res.Errors) != 1 || res.Errors[0].ID != "T-0001" {
		t.Fatalf("errors = %+v, want T-0001's undeclared tickler_dest reported", res.Errors)
	}
	if !strings.Contains(res.Errors[0].Error.Error(), "nope") {
		t.Errorf("error = %v, want it to name the bad destination", res.Errors[0].Error)
	}
}
