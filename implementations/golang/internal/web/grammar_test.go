package web

// T-0117 — the GUI honours a directory's declared ID grammar end to end
// (spec-file-format.md §3.3.2). The web package interpolates rendered IDs
// everywhere, so most of it adapts to any shape automatically; what must be
// audited is where an ID argument is PARSED (routes, the API item paths) and
// where a fixture or example assumed the default T-/4 form.
//
// These tests serve testdata/clean-id-grammar — the T-0116 corpus fixture
// declaring id_prefix: X / id_width: 3 — and drive the board, the panel, every
// mutation, search, the JSON API and SSE with X-003-style IDs. The default
// corpus (clean-full) is asserted byte-unchanged by the same read battery.

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Hopasaurus/micro-manager/mm"
)

// grammarServer opens the non-default fixture and returns the server plus its
// project id, exactly as boardServer does for the default corpus.
func grammarServer(t *testing.T, fixture string) (*testServer, string) {
	t.Helper()
	ts := newTestServer(t, fixture)
	id, err := mm.ProjectID(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	return ts, id
}

// TestGrammarBoardRendersOwnIDs: the board renders X-003-style cards with the
// ID verbatim in every place the DOM contract interpolates it.
func TestGrammarBoardRendersOwnIDs(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	for _, want := range []string{"item-X-001", "item-X-002", "item-X-003", "item-X-004"} {
		if !hasTestid(body, want) {
			t.Errorf("the board is missing %q", want)
		}
		card := testid(t, body, want)
		if got := attrOf(t, card, "data-item-id"); got != strings.TrimPrefix(want, "item-") {
			t.Errorf("%s data-item-id = %q", want, got)
		}
	}

	// The id span and the action family interpolate the same way.
	if !hasTestid(body, "item-X-001-id") {
		t.Error("item-X-001-id is missing")
	}
	for _, op := range []string{"start", "finish", "block", "unblock", "move", "note", "edit", "remove"} {
		if !hasTestid(body, "item-X-001-action-"+op) {
			t.Errorf("item-X-001-action-%s is missing", op)
		}
	}

	// The card's own id text is the verbatim ID, not a re-rendered default.
	if !strings.Contains(body, ">X-001<") {
		t.Error("the card does not show the verbatim ID X-001")
	}
}

// TestGrammarPanelAndDialogs: an item panel and every dialog open by X-ID.
func TestGrammarPanelAndDialogs(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")

	panel := ts.get("/p/" + id + "/item/X-001").expectStatus(http.StatusOK).Body
	if !hasTestid(panel, "item-panel") {
		t.Fatal("the panel did not render")
	}
	p := testid(t, panel, "item-panel")
	if got := attrOf(t, p, "data-item-id"); got != "X-001" {
		t.Errorf("item-panel data-item-id = %q, want X-001", got)
	}
	// The panel's form posts to the item's own route.
	if !strings.Contains(panel, "/p/"+id+"/items/X-001") {
		t.Error("the panel does not target the X-001 route")
	}

	// A working item's panel reads its slot; clean-id-grammar has both slots
	// idle, so the add panel is the second panel shape.
	add := ts.get("/p/" + id + "/new").expectStatus(http.StatusOK).Body
	if !hasTestid(add, "item-panel") {
		t.Error("the add panel did not render")
	}

	for _, tc := range []struct{ name, item string }{
		{"confirm-remove", "X-003"},
		{"block", "X-002"},
		{"finish", "X-001"},
	} {
		body := ts.get("/p/" + id + "/dialog/" + tc.name + "?item=" + tc.item).expectStatus(http.StatusOK).Body
		if !hasTestid(body, "dialog-"+tc.name) {
			t.Errorf("dialog-%s did not render", tc.name)
		}
		// The dialog form targets the item's own route.
		if !strings.Contains(body, "/p/"+id+"/items/"+tc.item) {
			t.Errorf("dialog-%s does not target %s", tc.name, tc.item)
		}
	}

	// A foreign-shape ID is refused with the directory's grammar in the error:
	// the parse runs against X-###, so T-0001 is not merely "not found", it is
	// not even a valid id here.
	res := ts.get("/p/" + id + "/item/T-0001").expectStatus(http.StatusBadRequest)
	if !strings.Contains(res.Body, "X-###") {
		t.Errorf("the error should name the declared grammar: %s", res.Body)
	}
}

// TestGrammarMutations: add/edit/start/finish/note/remove/move round trip on
// X-IDs through the HTML routes, which is what a person using the GUI drives.
func TestGrammarMutations(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")

	// Add: next_id is X-005, so the first new item is X-005.
	r := ts.form(http.MethodPost, "/p/"+id+"/items", url.Values{
		"title": {"Grammar add"},
		"stage": {"ready"},
		"prio":  {"med"},
	}, "HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-005 added") {
		t.Errorf("the add toast should name X-005: %s", r.Body)
	}
	if !hasTestid(r.Body, "item-X-005") {
		t.Error("the re-rendered board is missing item-X-005")
	}

	// Edit: PATCH the title.
	r = ts.form(http.MethodPatch, "/p/"+id+"/items/X-001", url.Values{
		"title": {"Grammar edited"},
	}).expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-001 saved") {
		t.Errorf("the edit toast should name X-001: %s", r.Body)
	}
	store := openCopy(t, ts)
	if it, err := store.Get("X-001"); err != nil || it.Title != "Grammar edited" {
		t.Errorf("X-001 title = %q, %v; want Grammar edited", it.Title, err)
	}

	// Edit with a detail body attaches details/X-001.md.
	r = ts.form(http.MethodPatch, "/p/"+id+"/items/X-001", url.Values{
		"title":  {"Grammar edited"},
		"detail": {"Body for X-001."},
	}).expectStatus(http.StatusOK)
	if d, err := store.Detail("X-001"); err != nil || !strings.Contains(d.Body, "Body for X-001") {
		t.Errorf("detail after save = %q, %v", d.Body, err)
	}

	// Note.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-002/note", url.Values{
		"text": {"grammar note"},
	}, "HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "noted on X-002") {
		t.Errorf("the note toast should name X-002: %s", r.Body)
	}

	// Move repositions a board item; the reference must still be on the
	// destination stage, so this happens before X-001 leaves Ready.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-002/move", url.Values{
		"stage": {"ready"},
		"after": {"X-001"},
	}, "HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-002 moved") {
		t.Errorf("the move toast should name X-002: %s", r.Body)
	}
	it, err := store.Get("X-002")
	if err != nil || it.Stage != "ready" {
		t.Errorf("after move, X-002 = %s, %v; want ready", it.Stage, err)
	}

	// Block needs a reason, and names the item in its toast (§7.2). X-003
	// starts in Someday, so it is the one that can be blocked.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-003/block", url.Values{
		"reason": {"waiting again"},
	}, "HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-003 blocked") {
		t.Errorf("the block toast should name X-003: %s", r.Body)
	}
	it, err = store.Get("X-003")
	if err != nil || it.Stage != "blocked" || it.Reason != "waiting again" {
		t.Errorf("after block, X-003 = %s %q, %v", it.Stage, it.Reason, err)
	}

	// Unblock returns it to Ready on top.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-003/unblock", url.Values{},
		"HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-003 unblocked") {
		t.Errorf("the unblock toast should name X-003: %s", r.Body)
	}

	// Start: both working slots (wip.working: 2) were idle, so X-001 moves
	// onto stage:working.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-001/start", url.Values{},
		"HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-001 started") {
		t.Errorf("the start toast should name X-001: %s", r.Body)
	}
	it, err = store.Get("X-001")
	if err != nil || it.Stage != "working" {
		t.Errorf("after start, X-001 = %s, %v; want working", it.Stage, err)
	}

	// Finish with an outcome.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/X-001/finish", url.Values{
		"outcome": {"shipped"},
	}, "HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-001 finished") {
		t.Errorf("the finish toast should name X-001: %s", r.Body)
	}
	it, err = store.Get("X-001")
	if err != nil || it.State != mm.StateDone {
		t.Errorf("after finish, X-001 = %s, %v", it.State, err)
	}

	// Remove is behind the force guard; X-004 (done, no detail) is fair game.
	r = ts.form(http.MethodDelete, "/p/"+id+"/items/X-004?force=true", url.Values{},
		"HX-Request", "true").expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, "X-004 removed") {
		t.Errorf("the remove toast should name X-004: %s", r.Body)
	}
	if _, err := store.Get("X-004"); err == nil {
		t.Error("X-004 should be gone after remove")
	}

	// The directory is still valid after everything above.
	store = openCopy(t, ts)
	if vs, _ := store.Validate(); len(vs) != 0 {
		t.Errorf("violations after the grammar mutation battery:\n%s", violationList(vs))
	}
}

// TestGrammarSearch: the board's ?q= filter and the API search both match
// X-IDs through the library's search, so the board and the CLI agree.
func TestGrammarSearch(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")

	body := ts.get("/p/" + id + "/board?q=Queued").expectStatus(http.StatusOK).Body
	if !hasTestid(body, "item-X-001") {
		t.Error("search for Queued did not return X-001")
	}
	if hasTestid(body, "item-X-003") {
		t.Error("search for Queued leaked X-003")
	}

	var env struct {
		OK     bool `json:"ok"`
		Result struct {
			Items []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"items"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + id + "/items?q=Queued").expectStatus(200).json(&env)
	if len(env.Result.Items) != 1 || env.Result.Items[0].ID != "X-001" {
		t.Errorf("API search = %+v, want exactly X-001", env.Result.Items)
	}
}

// TestGrammarStatus: the status bar reports the directory with its own ID
// shape — wip and counts are grammar-independent, and nothing re-renders a
// default prefix.
func TestGrammarStatus(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")
	body := ts.get("/p/" + id + "/status").expectStatus(http.StatusOK).Body

	wip := testid(t, body, "status-wip")
	if got := attrOf(t, wip, "data-wip-used"); got != "0" {
		t.Errorf("data-wip-used = %q, want 0", got)
	}
	if got := attrOf(t, wip, "data-wip-limit"); got != "2" {
		t.Errorf("data-wip-limit = %q, want 2", got)
	}
	if strings.Contains(body, "T-00") {
		t.Error("the status bar renders a default-shape ID")
	}
}

// TestGrammarAPI: the JSON API's item paths and nextId speak X-IDs end to end.
func TestGrammarAPI(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")

	// Project summary reports next_id in the declared grammar.
	var sum struct {
		OK     bool `json:"ok"`
		Result struct {
			NextID  string `json:"nextId"`
			WipUsed int    `json:"wipUsed"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + id).expectStatus(200).json(&sum)
	if sum.Result.NextID != "X-005" {
		t.Errorf("nextId = %q, want X-005", sum.Result.NextID)
	}

	// List.
	var list struct {
		OK     bool `json:"ok"`
		Result struct {
			Items []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"items"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + id + "/items").expectStatus(200).json(&list)
	if len(list.Result.Items) != 4 {
		t.Fatalf("list returned %d items, want 4", len(list.Result.Items))
	}
	if list.Result.Items[0].ID != "X-001" {
		t.Errorf("first item = %q, want X-001", list.Result.Items[0].ID)
	}

	// Show.
	var show struct {
		OK     bool `json:"ok"`
		Result struct {
			Item struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"item"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + id + "/items/X-003").expectStatus(200).json(&show)
	if show.Result.Item.ID != "X-003" || show.Result.Item.Title != "Maybe" {
		t.Errorf("show X-003 = %+v", show.Result.Item)
	}

	// Add via the API: the next allocation is X-005.
	var added struct {
		OK     bool `json:"ok"`
		Result struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"result"`
	}
	ts.post("/api/v1/projects/"+id+"/items",
		`{"title":"API grammar add"}`, apiHeaders()...).expectStatus(201).json(&added)
	if added.Result.Item.ID != "X-005" {
		t.Errorf("API add = %q, want X-005", added.Result.Item.ID)
	}

	// Move with before/after naming X-IDs, while X-001 is still in Ready (a
	// move reference must be on the destination stage).
	ts.post("/api/v1/projects/"+id+"/items/X-002/move",
		`{"stage":"ready","after":"X-001"}`, apiHeaders()...).expectStatus(200)

	// Edit, note, start, finish through the API, each addressing X-IDs.
	ts.post("/api/v1/projects/"+id+"/items/X-001/start", `{}`, apiHeaders()...).expectStatus(200)
	ts.post("/api/v1/projects/"+id+"/items/X-001/finish",
		`{"outcome":"shipped"}`, apiHeaders()...).expectStatus(200)
	ts.post("/api/v1/projects/"+id+"/items/X-002/note",
		`{"note":"api grammar note"}`, apiHeaders()...).expectStatus(200)
	ts.do(http.MethodPatch, "/api/v1/projects/"+id+"/items/X-002",
		strings.NewReader(`{"title":"API grammar edit"}`), apiHeaders()...).expectStatus(200)

	// Remove with the mandatory force guard.
	ts.do(http.MethodDelete, "/api/v1/projects/"+id+"/items/X-004?force=true",
		strings.NewReader(""), apiHeaders()...).expectStatus(200)

	// A foreign-shape ID is rejected by the parse, not silently misread.
	res := ts.get("/api/v1/projects/" + id + "/items/T-0001").expectStatus(400)
	if !strings.Contains(res.Body, "X-###") {
		t.Errorf("the API error should name the declared grammar: %s", res.Body)
	}

	// The directory is still valid after the API battery.
	store := openCopy(t, ts)
	if vs, _ := store.Validate(); len(vs) != 0 {
		t.Errorf("violations after the API grammar battery:\n%s", violationList(vs))
	}
}

// TestGrammarDetailRoundTrip: the detail endpoints address X-IDs, and a
// detail attached through the panel form is readable and replaceable by ID.
func TestGrammarDetailRoundTrip(t *testing.T) {
	ts, id := grammarServer(t, "clean-v2-id-grammar")

	// Attach a detail through the HTML edit form (item.go attaches when the
	// form carries a body and the item has none).
	ts.form(http.MethodPatch, "/p/"+id+"/items/X-001", url.Values{
		"title":  {"Queued"},
		"detail": {"First body."},
	}).expectStatus(http.StatusOK)

	var got struct {
		OK     bool `json:"ok"`
		Result struct {
			Path  string `json:"path"`
			ID    string `json:"id"`
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + id + "/detail/X-001").expectStatus(200).json(&got)
	if got.Result.ID != "X-001" || !strings.Contains(got.Result.Body, "First body") {
		t.Errorf("detail = %+v", got.Result)
	}
	if !strings.HasSuffix(got.Result.Path, "details/X-001.md") {
		t.Errorf("detail path = %q, want .../details/X-001.md", got.Result.Path)
	}

	// Replace the body by ID.
	ts.do(http.MethodPut, "/api/v1/projects/"+id+"/detail/X-001",
		strings.NewReader(`{"body":"Second body."}`), apiHeaders()...).expectStatus(200)
	ts.get("/api/v1/projects/" + id + "/detail/X-001").expectStatus(200).json(&got)
	if !strings.Contains(got.Result.Body, "Second body") {
		t.Errorf("detail after PUT = %q", got.Result.Body)
	}
}

// TestGrammarSSE: a mutation on the non-default fixture reaches the event
// stream exactly as on the default corpus — the stream carries names, not
// content, so the grammar never appears in it and cannot break it.
func TestGrammarSSE(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-v2-id-grammar")
	id := projectIDOf(t, ts, ts.Dirs[0])

	res := connectSSE(t, ts, id)
	time.Sleep(120 * time.Millisecond)

	ts.post("/api/v1/projects/"+id+"/items",
		`{"title":"SSE grammar probe"}`, apiHeaders()...).expectStatus(201)

	events := nextEvents(t, res, "board", "status", "check")
	for _, name := range events {
		switch name {
		case "board", "status", "check":
		default:
			t.Fatalf("unexpected event name %q", name)
		}
	}
}

// TestGrammarAPIInit: a script can create a board in a non-default grammar
// through the API, exactly as the CLI's --init --prefix/--id-width does, and
// the created directory allocates in its declared shape.
func TestGrammarAPIInit(t *testing.T) {
	ts := newTestServer(t)
	path := filepath.Join(t.TempDir(), "micro-manager")

	var res struct {
		OK     bool `json:"ok"`
		Result struct {
			Directory struct {
				ProjectID string `json:"projectId"`
				NextID    string `json:"nextId"`
			} `json:"directory"`
		} `json:"result"`
	}
	ts.post("/api/v1/projects",
		`{"path":"`+path+`","project":"API Init","idPrefix":"MM","idWidth":3}`,
		apiHeaders()...).expectStatus(201).json(&res)
	if res.Result.Directory.NextID != "MM-001" {
		t.Errorf("init nextId = %q, want MM-001", res.Result.Directory.NextID)
	}

	// The created directory opens and allocates in the declared grammar.
	// --init (and this API route) create version-2 directories directly
	// (T-0241 - spec-tools.md §5.1.1 describes no other output), so no
	// migration is needed before adding through the library directly.
	store, err := mm.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	it, _, err := store.Add(mm.AddRequest{Title: "Init add"}, mm.Date{Year: 2026, Month: 1, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "MM-001" {
		t.Errorf("first add in an MM/3 init = %s, want MM-001", it.ID)
	}
}

// TestGrammarReadBatteryIsByteUnchanged: serving a directory — the board, the
// panels, the dialogs, search, status, check and report — never writes into
// it, for the non-default grammar and the default corpus alike. The harness
// COPIES fixtures, so the copies are compared against the fixture source
// rather than the fixtures themselves.
func TestGrammarReadBatteryIsByteUnchanged(t *testing.T) {
	for _, fixture := range []string{"clean-id-grammar", "clean-full"} {
		ts, id := grammarServer(t, fixture)
		before := snapshotDir(t, ts.Dirs[0])

		ts.get("/p/" + id + "/board").expectStatus(200)
		ts.get("/p/" + id + "/status").expectStatus(200)
		ts.get("/p/" + id + "/check").expectStatus(200)
		ts.get("/p/" + id + "/report?include-backlog=1").expectStatus(200)
		ts.get("/p/" + id + "/new").expectStatus(200)
		ts.get("/api/v1/projects/" + id).expectStatus(200)
		ts.get("/api/v1/projects/" + id + "/items").expectStatus(200)

		if fixture == "clean-id-grammar" {
			ts.get("/p/" + id + "/item/X-001").expectStatus(200)
			ts.get("/p/" + id + "/dialog/block?item=X-002").expectStatus(200)
			ts.get("/p/" + id + "/board?q=Queued").expectStatus(200)
		} else {
			ts.get("/p/" + id + "/item/T-0001").expectStatus(200)
			ts.get("/p/" + id + "/dialog/block?item=T-0002").expectStatus(200)
			ts.get("/p/" + id + "/board?q=deploy").expectStatus(200)
		}

		after := snapshotDir(t, ts.Dirs[0])
		for name, b := range before {
			if a, ok := after[name]; !ok || a != b {
				t.Errorf("%s: reads changed %s", fixture, name)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers

// openCopy opens the copied fixture directory as a store, for assertions on
// what a mutation actually wrote. The harness copies fixtures into
// <temp>/<name>/micro-manager, and Dirs holds those copied paths.
func openCopy(t *testing.T, ts *testServer) *mm.Store {
	t.Helper()
	s, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatalf("open %s: %v", ts.Dirs[0], err)
	}
	return s
}

// snapshotDir returns every regular file's bytes, keyed by relative path.
func snapshotDir(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// violationList renders violations one per line for failure messages.
func violationList(vs []mm.Violation) string {
	var lines []string
	for _, v := range vs {
		lines = append(lines, v.At.File+":"+itoa(v.At.Line)+" "+v.Message)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
