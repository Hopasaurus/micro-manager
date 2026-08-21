package web

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hopasaurus/micro-manager/mm"
)

// T-0173 — the tickler in the web UI (spec-gui.md §2.4, §4.2, §5.5, §5.6).
//
// Four contracts, tested separately:
//
//   - the someday-card badge: data-tickler on the article, the next-fire badge
//     with data-next, computed server-side (§5.5);
//   - the Wake-up group: the controls, hidden/visible by context, pre-filled
//     from the item's schedule (§5.6);
//   - the composition: add and edit fold the controls into the tickler: field
//     in the same transaction (§4.2);
//   - the service: tickHeld runs Store.tick over every held board and logs
//     what fired (§2.4).

func pinnedServer(t *testing.T, fixture string) (*testServer, string) {
	t.Helper()
	ts, id := boardServer(t, fixture)
	// The badge and the service share the registry's clock; pin it for the
	// same reason the audit does — a test that drifts with the calendar is
	// not a test.
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	return ts, id
}

// badgeText reads a tickler badge's inner text, which sits AFTER the opening
// tag that testid returns.
func badgeText(body, id string) string {
	re := regexp.MustCompile(`data-testid="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]+)`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// itemIDByTitle looks up an item's ID after an API add, so a test does not
// hard-code the fixture's next_id.
func itemIDByTitle(t *testing.T, ts *testServer, id, title string) mm.ID {
	t.Helper()
	store, err := ts.registry.resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(mm.Filter{State: mm.StateAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Title == title {
			return it.ID
		}
	}
	t.Fatalf("no item titled %q", title)
	return ""
}

// clean-full's someday item T-0005 carries tickler:2026-09-01.
func TestSomedayCardBadge(t *testing.T) {
	ts, id := pinnedServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	card := testid(t, body, "item-T-0005")
	if got := attrOf(t, card, "data-tickler"); got != "2026-09-01" {
		t.Errorf("data-tickler = %q, want 2026-09-01", got)
	}
	badge := testid(t, body, "item-T-0005-tickler")
	if got := attrOf(t, badge, "data-next"); got != "2026-09-01" {
		t.Errorf("data-next = %q, want 2026-09-01", got)
	}
	if got := badgeText(body, "item-T-0005-tickler"); got != "next 2026-09-01" {
		t.Errorf("badge text = %q, want it to read the next fire", got)
	}

	// A ready item carries no tickler and no badge.
	if got := attrOf(t, testid(t, body, "item-T-0001"), "data-tickler"); got != "" {
		t.Errorf("a ready card carries data-tickler=%q", got)
	}
	if hasTestid(body, "item-T-0001-tickler") {
		t.Error("a ready card renders a tickler badge")
	}
}

// A schedule that is already due — Next returns null — reads "due" and has no
// data-next: the next tick fires it.
func TestTicklerBadgeDue(t *testing.T) {
	ts, id := pinnedServer(t, "clean-v2-full")
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Overdue"}, "stage": {"someday"}, "tickler-kind": {"one-time"},
		"tickler-date": {"2026-07-01"},
	}).expectStatus(http.StatusOK)
	overdue := itemIDByTitle(t, ts, id, "Overdue")

	body := ts.get("/p/" + id + "/board").Body
	badge := testid(t, body, "item-"+string(overdue)+"-tickler")
	if got := attrOf(t, badge, "data-next"); got != "" {
		t.Errorf("data-next = %q, want absent for a due schedule", got)
	}
	if got := badgeText(body, "item-"+string(overdue)+"-tickler"); got != "due" {
		t.Errorf("badge = %q, want it to read \"due\"", got)
	}
}

// The Wake-up group in the new panel: always present, hidden until the section
// selector is Someday.
func TestWakeUpGroupNewPanel(t *testing.T) {
	ts, id := pinnedServer(t, "clean-full")

	// Default: Ready, group hidden.
	ready := ts.get("/p/" + id + "/new").expectStatus(http.StatusOK).Body
	group := testid(t, ready, "item-tickler")
	if !strings.Contains(group, "hidden") {
		t.Error("the group must be hidden in a Ready new-item form")
	}
	if got := attrOf(t, group, "data-present"); got != "false" {
		t.Errorf("a new form has nothing scheduled: data-present = %q", got)
	}

	// ?stage=someday: visible, kind never, controls empty.
	someday := ts.get("/p/" + id + "/new?stage=someday").expectStatus(http.StatusOK).Body
	group = testid(t, someday, "item-tickler")
	if strings.Contains(group, "hidden") {
		t.Error("the group must be visible in a Someday new-item form")
	}
	for _, tid := range []string{"tickler-kind", "tickler-date", "tickler-weekday",
		"tickler-ordinal", "tickler-monthday", "tickler-time"} {
		if !hasTestid(someday, tid) {
			t.Errorf("the Wake-up group is missing %s", tid)
		}
	}
	if !strings.Contains(someday, `<option value="never" selected>`) {
		t.Error("kind must default to never in a new form")
	}

	// The stage selector itself is new-panel only.
	if !hasTestid(someday, "item-field-stage") {
		t.Error("the new panel is missing its stage selector")
	}
	item := ts.get("/p/" + id + "/item/T-0002").Body
	if hasTestid(item, "item-field-stage") {
		t.Error("the item panel must not carry the new-panel stage selector")
	}
}

// The Wake-up group in the item panel: rendered for a someday item, pre-filled
// from its schedule; absent for every other state.
func TestWakeUpGroupItemPanel(t *testing.T) {
	ts, id := pinnedServer(t, "clean-full")

	// T-0005 is a someday item with tickler:2026-09-01: one-time, date filled.
	body := ts.get("/p/" + id + "/item/T-0005").expectStatus(http.StatusOK).Body
	group := testid(t, body, "item-tickler")
	if got := attrOf(t, group, "data-present"); got != "true" {
		t.Errorf("data-present = %q, want true for a scheduled item", got)
	}
	if !strings.Contains(body, `<option value="one-time" selected>`) {
		t.Error("kind should pre-fill to one-time for a bare date")
	}
	date := testid(t, body, "tickler-date")
	if got := attrOf(t, date, "value"); got != "2026-09-01" {
		t.Errorf("tickler-date = %q, want 2026-09-01", got)
	}

	// A ready item's panel has no Wake-up group: there is no schedule to set.
	if hasTestid(ts.get("/p/"+id+"/item/T-0002").Body, "item-tickler") {
		t.Error("a ready item renders the Wake-up group")
	}
}

// The other shapes pre-fill back into their controls (§5.6 pre-fill).
func TestWakeUpGroupPrefillShapes(t *testing.T) {
	ts, id := pinnedServer(t, "clean-v2-full")

	add := func(title, tickler string) {
		ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
			"title": {title}, "stage": {"someday"}, "tickler-kind": {"one-time"},
			"tickler-date": {"2026-09-01"},
		})
		// Rewrite the field server-side: the library's line splice is what a
		// hand edit would do, and the panel must parse any of them.
		store, _ := ts.registry.resolve(id)
		items, _ := store.List(mm.Filter{State: mm.StateAll})
		var it mm.Item
		for _, i := range items {
			if i.Title == title {
				it = i
			}
		}
		store.Update(it.ID, mm.UpdateRequest{Set: []mm.Field{{Key: "tickler", Value: tickler}}}, mm.Date{Year: 2026, Month: 7, Day: 30})
	}

	add("Weekly", "first-mon@08:00")
	body := ts.get("/p/" + id + "/item/" + string(itemIDByTitle(t, ts, id, "Weekly"))).Body
	if !strings.Contains(body, `<option value="weekly" selected>`) {
		t.Error("kind should pre-fill to weekly")
	}
	if !strings.Contains(body, `<option value="first" selected>`) {
		t.Error("the ordinal should pre-fill to first")
	}
	if !strings.Contains(body, `<option value="mon" selected>`) {
		t.Error("the weekday should pre-fill to mon")
	}
	if got := attrOf(t, testid(t, body, "tickler-time"), "value"); got != "08:00" {
		t.Errorf("tickler-time = %q, want 08:00", got)
	}

	add("Monthly", "last@07:30")
	body = ts.get("/p/" + id + "/item/" + string(itemIDByTitle(t, ts, id, "Monthly"))).Body
	if !strings.Contains(body, `<option value="monthly" selected>`) {
		t.Error("kind should pre-fill to monthly")
	}
	if got := attrOf(t, testid(t, body, "tickler-monthday"), "value"); got != "last" {
		t.Errorf("tickler-monthday = %q, want last", got)
	}
	if got := attrOf(t, testid(t, body, "tickler-time"), "value"); got != "07:30" {
		t.Errorf("tickler-time = %q, want 07:30", got)
	}
}

// Composition (§4.2): add and edit fold the controls into the tickler: field
// in the same transaction, and kind never removes it.
func TestWakeUpComposition(t *testing.T) {
	ts, id := pinnedServer(t, "clean-v2-full")

	// weekly + time, on add.
	res := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Weekly thing"}, "stage": {"someday"},
		"tickler-kind": {"weekly"}, "tickler-weekday": {"mon"}, "tickler-time": {"08:00"},
	}).expectStatus(http.StatusOK)
	if !strings.Contains(res.Body, "Weekly thing") {
		t.Fatalf("add failed:\n%s", res.Body)
	}
	store, _ := ts.registry.resolve(id)
	items, _ := store.List(mm.Filter{State: mm.StateAll})
	var weekly *mm.Item
	for _, i := range items {
		if i.Title == "Weekly thing" {
			weekly = &i
		}
	}
	if weekly == nil || weekly.Tickler != "mon@08:00" {
		t.Fatalf("add did not compose tickler:mon@08:00, got %+v", weekly)
	}

	// monthly on a later edit, monthday zero-padded.
	ts.form(http.MethodPatch, "/p/"+id+"/items/"+string(weekly.ID), url.Values{
		"tickler-kind": {"monthly"}, "tickler-monthday": {"5"},
	}).expectStatus(http.StatusOK)
	it, _ := store.Get(weekly.ID)
	if it.Tickler != "05" {
		t.Errorf("edit did not compose tickler:05, got %q", it.Tickler)
	}

	// The sentinel last — the value a number input cannot hold (T-0197) —
	// composes through unchanged, and the control itself is a text input
	// whose pattern admits it.
	ts.form(http.MethodPatch, "/p/"+id+"/items/"+string(weekly.ID), url.Values{
		"tickler-kind": {"monthly"}, "tickler-monthday": {"last"},
	}).expectStatus(http.StatusOK)
	it, _ = store.Get(weekly.ID)
	if it.Tickler != "last" {
		t.Errorf("monthday last did not compose, got %q", it.Tickler)
	}
	panel := ts.get("/p/" + id + "/item/" + string(weekly.ID)).Body
	md := testid(t, panel, "tickler-monthday")
	if !strings.Contains(md, `type="text"`) || !strings.Contains(md, `pattern="(0?[1-9]|[12][0-9]|3[01]|last)"`) {
		t.Errorf("tickler-monthday must be a text input whose pattern admits last, got %s", md)
	}

	// kind never removes the field.
	ts.form(http.MethodPatch, "/p/"+id+"/items/"+string(weekly.ID), url.Values{
		"tickler-kind": {"never"},
	}).expectStatus(http.StatusOK)
	it, _ = store.Get(weekly.ID)
	if it.Tickler != "" {
		t.Errorf("kind never should remove the tickler, still %q", it.Tickler)
	}

	// An edit with no tickler controls at all (a non-someday item's form)
	// leaves the field alone.
	ts.form(http.MethodPatch, "/p/"+id+"/items/T-0002", url.Values{"title": {"Second thing"}}).
		expectStatus(http.StatusOK)
	it, _ = store.Get(mm.ID("T-0002"))
	if it.Tickler != "" {
		t.Errorf("a ticklerless edit changed the tickler to %q", it.Tickler)
	}

	// A missing required control is refused by the server.
	bad := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Broken"}, "stage": {"someday"}, "tickler-kind": {"one-time"},
	})
	if bad.Status != http.StatusBadRequest {
		t.Errorf("a one-time without a date must be 400, got %d", bad.Status)
	}

	// The library refuses a tickler outside Someday (I7), as 400.
	conflict := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Wrong place"}, "stage": {"ready"}, "tickler-kind": {"one-time"},
		"tickler-date": {"2026-09-01"},
	})
	if conflict.Status != http.StatusBadRequest {
		t.Errorf("a tickler on a ready item must be refused, got %d", conflict.Status)
	}
}

// The service (§2.4): tickHeld fires every held board's due schedules on the
// registry's clock and logs what fired.
func TestTicklerServiceFiresHeldBoards(t *testing.T) {
	cfg := mm.DefaultConfig()
	cfg.Tickler.Interval = "1m"
	ts, id := boardServer(t, "clean-v2-full")
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }

	// A backdated one-shot (fires: overdue) and a recurring prototype whose
	// first fire is anchored in the past (fires: spawns).
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Backdated"}, "stage": {"someday"}, "tickler-kind": {"one-time"},
		"tickler-date": {"2026-07-01"},
	}).expectStatus(http.StatusOK)
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Old recurring"}, "stage": {"someday"}, "tickler-kind": {"monthly"},
		"tickler-monthday": {"15"},
	}).expectStatus(http.StatusOK)
	recurring := itemIDByTitle(t, ts, id, "Old recurring")

	// The add stamps created: today, which anchors a never-fired recurring
	// schedule's first fire — so the monthly is not due until next month. A
	// recurring that IS due needs an earlier created, exactly as a hand edit
	// would set it.
	store, _ := ts.registry.resolve(id)
	if _, _, err := store.Update(recurring, mm.UpdateRequest{
		Set: []mm.Field{{Key: "created", Value: "2026-07-01"}},
	}, mm.Date{Year: 2026, Month: 7, Day: 30}); err != nil {
		t.Fatal(err)
	}

	ts.Server.tickHeld()

	items, _ := store.List(mm.Filter{State: mm.StateAll})
	var moved bool
	for _, it := range items {
		if it.Title == "Backdated" {
			moved = it.State == mm.StateBoard && it.Stage == "ready" && it.Tickler == "" && it.Tickled == (mm.Date{Year: 2026, Month: 7, Day: 30})
		}
	}
	if !moved {
		t.Error("the backdated one-shot did not fire into Ready")
	}

	// The prototype itself, found by the ID recorded before the tick - the
	// spawn it produces shares its title, so a title-only match (over the
	// post-tick list) cannot tell prototype from spawn apart.
	proto, err := store.Get(recurring)
	if err != nil {
		t.Fatal(err)
	}
	if proto.Tickled != (mm.Date{Year: 2026, Month: 7, Day: 30}) || proto.Tickler == "" {
		t.Errorf("the recurring prototype was not stamped tickled: %+v", proto)
	}

	// The spawn itself is a fresh Ready item with its own ID, sharing the
	// prototype's title but none of its schedule.
	var spawnCount int
	for _, it := range items {
		if it.Title == "Old recurring" && it.ID != recurring &&
			it.State == mm.StateBoard && it.Stage == "ready" {
			spawnCount++
		}
	}
	if spawnCount != 1 {
		t.Errorf("the recurring fire should spawn one Ready item, found %d", spawnCount)
	}
}

// The interval is parsed from the merged config; a value that is not a
// duration is a warning at load, and the service stays off.
func TestTicklerIntervalConfig(t *testing.T) {
	sys, err := mm.ParseConfigFile("/tmp/system.json", mm.ScopeSystem, []byte(`{"tickler": {"interval": "5m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, warnings := mm.MergeConfig(sys, nil)
	if len(warnings) != 0 {
		t.Fatalf("a valid duration must load without warnings: %v", warnings)
	}
	if cfg.Tickler.Interval != "5m" {
		t.Errorf("interval = %q, want 5m", cfg.Tickler.Interval)
	}

	bad, err := mm.ParseConfigFile("/tmp/system.json", mm.ScopeSystem, []byte(`{"tickler": {"interval": "soon"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, warnings = mm.MergeConfig(bad, nil)
	if cfg.Tickler.Interval != "" {
		t.Errorf("a bad duration must leave the service off, got %q", cfg.Tickler.Interval)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w.Key, "tickler.interval") {
			found = true
		}
	}
	if !found {
		t.Error("a bad duration must be reported as a config warning")
	}

	// A project config MUST NOT set it: system-scoped, reported by name.
	proj, err := mm.ParseConfigFile("/tmp/project.json", mm.ScopeProject, []byte(`{"tickler": {"interval": "1m"}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, warnings = mm.MergeConfig(nil, proj)
	if cfg.Tickler.Interval != "" {
		t.Errorf("a project config must not turn the service on, got %q", cfg.Tickler.Interval)
	}
	found = false
	for _, w := range warnings {
		if strings.Contains(w.Key, "tickler.interval") {
			found = true
		}
	}
	if !found {
		t.Error("a project config setting tickler must be reported as a warning")
	}
}

// recordsHandler keeps every record a logger emits, so a test can assert the
// §2.4 audit trail instead of eyeballing t.Log output.
type recordsHandler struct {
	mu     sync.Mutex
	events []logEvent
}

type logEvent struct {
	Level slog.Level
	Msg   string
	Attrs map[string]string
}

func (h *recordsHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordsHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	ev := logEvent{Level: r.Level, Msg: r.Message, Attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		ev.Attrs[a.Key] = a.Value.String()
		return true
	})
	h.events = append(h.events, ev)
	return nil
}

func (h *recordsHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordsHandler) WithGroup(string) slog.Handler      { return h }

// eventsOf returns the recorded events whose message is msg, in order.
func (h *recordsHandler) eventsOf(msg string) []logEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []logEvent
	for _, e := range h.events {
		if e.Msg == msg {
			out = append(out, e)
		}
	}
	return out
}

// The scheduled half of the audit trail (T-0205): every held board logs what
// is scheduled and when it will fire before the run, so the log answers "why
// didn't it move". The backdated one-shot is the sharp case: next=none because
// its date is behind its created, but due=true — the run's own test — and the
// line is followed immediately by the fire.
func TestTicklerLogsScheduled(t *testing.T) {
	rec := &recordsHandler{}
	ts, id := boardServer(t, "clean-v2-full")
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	ts.Server.log = slog.New(rec)

	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Backdated"}, "stage": {"someday"}, "tickler-kind": {"one-time"},
		"tickler-date": {"2026-07-01"},
	}).expectStatus(http.StatusOK)
	backdated := string(itemIDByTitle(t, ts, id, "Backdated"))

	ts.Server.tickHeld()

	scheduled := rec.eventsOf("tickler scheduled")
	if len(scheduled) != 2 {
		t.Fatalf("scheduled lines = %+v, want 2 (T-0005 and the backdated add)", scheduled)
	}
	if got := scheduled[0].Attrs; got["item"] != "T-0005" ||
		got["schedule"] != "2026-09-01" || got["tickled"] != "never" ||
		got["next"] != "2026-09-01" || got["due"] != "false" {
		t.Errorf("T-0005 line = %v, want the fixture's future one-shot", got)
	}
	if got := scheduled[1].Attrs; got["item"] != backdated ||
		got["tickled"] != "never" || got["next"] != "none" || got["due"] != "true" {
		t.Errorf("backdated line = %v, want next=none due=true (overdue, fires now)", got)
	}

	fired := rec.eventsOf("tickler fired")
	if len(fired) != 1 || fired[0].Attrs["item"] != backdated {
		t.Errorf("fired lines = %+v, want exactly the backdated item", fired)
	}
	for i, e := range rec.events {
		if e.Msg == "tickler fired" && i != 2 {
			t.Errorf("the fired line must come after both scheduled lines, got it at index %d of %+v", i, rec.events)
		}
	}
}

// A board with nothing scheduled logs the absence (count 0) instead of
// silence, so a held-but-inert board is distinguishable from a dead service.
func TestTicklerLogsNothingScheduled(t *testing.T) {
	rec := &recordsHandler{}
	ts, id := boardServer(t, "clean-minimal")
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	ts.Server.log = slog.New(rec)

	// The board GET is what holds the store, the way opening a board does.
	ts.get("/p/" + id + "/board").expectStatus(http.StatusOK)

	ts.Server.tickHeld()

	scheduled := rec.eventsOf("tickler scheduled")
	if len(scheduled) != 1 {
		t.Fatalf("scheduled lines = %+v, want exactly the count=0 line", scheduled)
	}
	if got := scheduled[0].Attrs["count"]; got != "0" {
		t.Errorf("count = %q, want 0", got)
	}
	if fired := rec.eventsOf("tickler fired"); len(fired) != 0 {
		t.Errorf("nothing is due on clean-minimal, but %d lines fired", len(fired))
	}
}

// T-0206 — the disabled tickler is not silent: startup logs a Warn naming the
// config key and both ways to enable the service, and the enabled path still
// logs the Info. Both go through the real Start, because the warning is part
// of the startup path, not a helper.
func TestTicklerStartupOffWarns(t *testing.T) {
	rec := &recordsHandler{}
	srv, err := New(Options{Bind: "127.0.0.1", Port: 0, Logger: slog.New(rec)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	waitForAddr(t, srv)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("shutdown returned %v", err)
	}

	off := rec.eventsOf("tickler service off")
	if len(off) != 1 {
		t.Fatalf("tickler service off lines = %+v, want exactly one at startup", off)
	}
	if got := off[0].Attrs; got["key"] != "tickler.interval" ||
		!strings.Contains(got["help"], "tickler.interval") ||
		!strings.Contains(got["help"], "mm --tick") {
		t.Errorf("the off warning must name the key and both enablement paths, got %v", got)
	}
	if on := rec.eventsOf("tickler service on"); len(on) != 0 {
		t.Errorf("a default-config service must not log tickler service on, got %+v", on)
	}
}

func TestTicklerStartupOnLogsInfo(t *testing.T) {
	cfg := mm.DefaultConfig()
	cfg.Tickler.Interval = "1m"
	rec := &recordsHandler{}
	srv, err := New(Options{Bind: "127.0.0.1", Port: 0, Config: cfg, Logger: slog.New(rec)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	waitForAddr(t, srv)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("shutdown returned %v", err)
	}

	on := rec.eventsOf("tickler service on")
	if len(on) != 1 {
		t.Fatalf("tickler service on lines = %+v, want exactly one", on)
	}
	if got := on[0].Attrs["interval"]; got != "1m0s" {
		t.Errorf("interval = %q, want 1m0s", got)
	}
	if off := rec.eventsOf("tickler service off"); len(off) != 0 {
		t.Errorf("an enabled service must not warn that it is off, got %+v", off)
	}
}

// T-0207 — the controller: apply starts the loop when turned on, stops it when
// turned off, restarts it with a new duration when changed, and reports
// whether anything changed so the caller can log transitions instead of
// chattering.
func TestTicklerControllerApply(t *testing.T) {
	var mu sync.Mutex
	passes := 0
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tk := newTickler(ctx, func() {
		mu.Lock()
		passes++
		mu.Unlock()
	})
	passCount := func() int {
		mu.Lock()
		defer mu.Unlock()
		return passes
	}

	// Nothing applied: off, and applying off reports no change.
	if tk.apply(0) {
		t.Error("applying off to a fresh controller must not report a change")
	}

	// On with a fast interval: the pass runs without anyone calling it.
	if !tk.apply(5 * time.Millisecond) {
		t.Fatal("turning on must report a change")
	}
	deadline := time.Now().Add(2 * time.Second)
	for passCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the loop did not run its pass: %d passes", passCount())
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Re-applying the same interval is a no-op: the same loop keeps running.
	if tk.apply(5 * time.Millisecond) {
		t.Error("re-applying the same interval must not report a change")
	}

	// Off stops the loop: the pass count freezes.
	if !tk.apply(0) {
		t.Fatal("turning off must report a change")
	}
	frozen := passCount()
	time.Sleep(30 * time.Millisecond)
	if got := passCount(); got != frozen {
		t.Errorf("the loop kept running after off: %d -> %d", frozen, got)
	}

	// Back on with a new interval runs again.
	if !tk.apply(10 * time.Millisecond) {
		t.Fatal("turning back on must report a change")
	}
	deadline = time.Now().Add(2 * time.Second)
	for passCount() <= frozen {
		if time.Now().After(deadline) {
			t.Fatalf("the restarted loop did not run: %d passes", passCount())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// T-0207 end to end, through the real startup path and the real config API:
// enabling the tickler from the UI's own write paths moves a due someday item
// without a restart, logs the transition, and disabling it stops the loop.
func TestTicklerSettingsAppliesLive(t *testing.T) {
	rec := &recordsHandler{}
	ts, id := func() (*testServer, string) {
		ts := newTestServerWith(t, func(o *Options) { o.Logger = slog.New(rec) }, "clean-v2-full")
		pid, err := mm.ProjectID(ts.Dirs[0])
		if err != nil {
			t.Fatal(err)
		}
		return ts, pid
	}()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ts.Server.Start(ctx) }()
	addr := waitForAddr(t, ts.Server)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("the service did not shut down")
		}
	}()
	base := "http://" + addr.String()

	// Open the board so the registry holds the store — the tickler ticks only
	// held boards (§2.4).
	if resp, err := http.Get(base + "/p/" + id + "/board"); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("open board: %v (status %v)", err, resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// A due someday item: a backdated one-shot, due the next tick.
	resp, err := http.PostForm(base+"/p/"+id+"/items", url.Values{
		"title": {"Due now"}, "stage": {"someday"},
		"tickler-kind": {"one-time"}, "tickler-date": {"2026-07-01"},
	})
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("add due item: %v (status %v)", err, resp.StatusCode)
	}
	resp.Body.Close()

	// Enable the tickler at a test-friendly interval via the config API, the
	// same path the settings control writes.
	put := func(body string) int {
		req, err := http.NewRequest(http.MethodPut, base+"/api/v1/config", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("config PUT: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := put(`{"tickler":{"interval":"10ms"}}`); got != http.StatusOK {
		t.Fatalf("enable tickler: status %d", got)
	}

	// The loop moves the due item to Ready without a restart.
	deadline := time.Now().Add(5 * time.Second)
	moved := false
	for !moved {
		if time.Now().After(deadline) {
			var list struct {
				Result struct {
					Items []struct {
						Title string `json:"title"`
					} `json:"items"`
				} `json:"result"`
			}
			if resp, err := http.Get(base + "/api/v1/projects/" + id + "/items?stage=someday"); err == nil {
				if resp.StatusCode != http.StatusOK {
					b, _ := io.ReadAll(resp.Body)
					t.Fatalf("someday listing status = %d body=%s", resp.StatusCode, b)
				}
				json.NewDecoder(resp.Body).Decode(&list)
				resp.Body.Close()
			}
			t.Fatalf("the due item never moved into Ready; somdays=%+v events=%+v", list.Result.Items, rec.events)
		}
		resp, err := http.Get(base + "/api/v1/projects/" + id + "/items?stage=ready")
		if err != nil {
			t.Fatalf("list ready: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("ready listing status = %d body=%s", resp.StatusCode, b)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var list struct {
			Result struct {
				Items []struct {
					ID    string `json:"id"`
					Title string `json:"title"`
				} `json:"items"`
			} `json:"result"`
		}
		if err := json.Unmarshal(b, &list); err != nil {
			t.Fatalf("decode items: %v", err)
		}
		for _, it := range list.Result.Items {
			if it.Title == "Due now" {
				moved = true
			}
		}
		if !moved {
			time.Sleep(20 * time.Millisecond)
		}
	}

	// The runtime transition logged on, the startup log said off.
	if on := rec.eventsOf("tickler service on"); len(on) != 1 {
		t.Errorf("tickler service on lines = %+v, want exactly the enable transition", on)
	}

	// Disable through the API: the loop stops and the transition logs off.
	if got := put(`{"tickler":{"interval":null}}`); got != http.StatusOK {
		t.Fatalf("disable tickler: status %d", got)
	}
	if off := rec.eventsOf("tickler service off"); len(off) != 2 {
		t.Errorf("tickler service off lines = %+v, want the startup and the disable", off)
	}
}
