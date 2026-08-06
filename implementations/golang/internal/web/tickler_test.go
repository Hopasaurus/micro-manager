package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
	ts, id := pinnedServer(t, "clean-full")
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Overdue"}, "section": {"someday"}, "tickler-kind": {"one-time"},
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

	// ?section=someday: visible, kind never, controls empty.
	someday := ts.get("/p/" + id + "/new?section=someday").expectStatus(http.StatusOK).Body
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

	// The section selector itself is new-panel only.
	if !hasTestid(someday, "item-field-section") {
		t.Error("the new panel is missing its section selector")
	}
	item := ts.get("/p/" + id + "/item/T-0002").Body
	if hasTestid(item, "item-field-section") {
		t.Error("the item panel must not carry the new-panel section selector")
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
	ts, id := pinnedServer(t, "clean-full")

	add := func(title, tickler string) {
		ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
			"title": {title}, "section": {"someday"}, "tickler-kind": {"one-time"},
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
	ts, id := pinnedServer(t, "clean-full")

	// weekly + time, on add.
	res := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Weekly thing"}, "section": {"someday"},
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
		"title": {"Broken"}, "section": {"someday"}, "tickler-kind": {"one-time"},
	})
	if bad.Status != http.StatusBadRequest {
		t.Errorf("a one-time without a date must be 400, got %d", bad.Status)
	}

	// The library refuses a tickler outside Someday (I7), as 400.
	conflict := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Wrong place"}, "section": {"ready"}, "tickler-kind": {"one-time"},
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
	ts, id := boardServer(t, "clean-full")
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }

	// A backdated one-shot (fires: overdue) and a recurring prototype whose
	// first fire is anchored in the past (fires: spawns).
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Backdated"}, "section": {"someday"}, "tickler-kind": {"one-time"},
		"tickler-date": {"2026-07-01"},
	}).expectStatus(http.StatusOK)
	ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Old recurring"}, "section": {"someday"}, "tickler-kind": {"monthly"},
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
	var moved, spawned bool
	for _, it := range items {
		switch it.Title {
		case "Backdated":
			moved = it.State == mm.StateBacklog && it.Section == mm.SectionReady && it.Tickler == "" && it.Tickled == (mm.Date{Year: 2026, Month: 7, Day: 30})
		case "Old recurring":
			spawned = it.Tickled == (mm.Date{Year: 2026, Month: 7, Day: 30}) && it.Tickler != ""
		}
	}
	if !moved {
		t.Error("the backdated one-shot did not fire into Ready")
	}
	if !spawned {
		t.Error("the recurring prototype was not stamped tickled")
	}
	// The spawn itself is a fresh Ready item with its own ID.
	var spawnCount int
	for _, it := range items {
		if it.Title == "Old recurring" && it.State == mm.StateBacklog && it.Section == mm.SectionReady {
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
