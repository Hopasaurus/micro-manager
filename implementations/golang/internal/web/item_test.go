package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// form posts an urlencoded body, which is what every form and every htmx
// mutation in this UI sends.
func (ts *testServer) form(method, target string, values url.Values, headers ...string) *response {
	ts.t.Helper()
	headers = append(headers, "Content-Type", "application/x-www-form-urlencoded")
	return ts.do(method, target, strings.NewReader(values.Encode()), headers...)
}

// §5.6 fixes the panel's testids. It renders OVER the board, so aria-modal is
// false: the board stays reachable behind it.
func TestItemPanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/item/T-0001").expectStatus(http.StatusOK).Body

	required := []string{
		"item-panel", "item-panel-title", "item-form",
		"item-field-title", "item-field-prio", "item-field-tags",
		"item-field-reason", "item-field-detail",
		"item-save", "item-cancel", "item-actions",
		"item-notes", "item-plan", "item-meta",
		// T-0242: the resize handle, x- prefixed since resizing is a client
		// affordance §5.1's closed testid list does not name.
		"x-item-panel-resize-handle",
	}
	for _, testid := range required {
		if !hasTestid(body, testid) {
			t.Errorf("the panel is missing data-testid=%q", testid)
		}
	}

	panel := testid(t, body, "item-panel")
	if got := attrOf(t, panel, "aria-modal"); got != "false" {
		t.Errorf("aria-modal = %q, want false: the board stays in the DOM", got)
	}
	if got := attrOf(t, panel, "data-item-id"); got != "T-0001" {
		t.Errorf("data-item-id = %q", got)
	}
	if attrOf(t, panel, "data-revision") == "" {
		t.Error("item-panel carries no baseline revision")
	}
	fresh := testid(t, body, "x-item-freshness")
	if got := attrOf(t, fresh, "hx-trigger"); got != "sse:item from:body" {
		t.Errorf("freshness trigger = %q", got)
	}

	meta := testid(t, body, "item-meta")
	if attrOf(t, meta, "data-created") == "" {
		t.Error("item-meta carries no data-created")
	}
}

func TestItemFreshnessIsTargetedAndReportsChanges(t *testing.T) {
	ts, projectID := boardServer(t, "clean-full")
	panel := ts.get("/p/" + projectID + "/item/T-0001").Body
	revision := attrOf(t, testid(t, panel, "item-panel"), "data-revision")
	path := "/p/" + projectID + "/item/T-0001/freshness?revision=" + revision
	unchanged := ts.get(path).expectStatus(http.StatusOK).Body
	if got := attrOf(t, testid(t, unchanged, "x-item-freshness"), "data-state"); got != "unchanged" {
		t.Fatalf("initial freshness state = %q", got)
	}

	store, err := ts.registry.resolve(projectID)
	if err != nil {
		t.Fatal(err)
	}
	today, _ := mm.ParseDate("2026-09-05")
	if _, err := store.SetDetailBody("T-0001", "changed elsewhere\n", false, today); err != nil {
		t.Fatal(err)
	}
	changed := ts.get(path).expectStatus(http.StatusOK).Body
	if got := attrOf(t, testid(t, changed, "x-item-freshness"), "data-state"); got != "changed" {
		t.Fatalf("changed freshness state = %q", got)
	}
	if !hasTestid(changed, "x-item-freshness-reload") {
		t.Error("changed item has no explicit reload")
	}
}

func TestItemFreshnessReportsRemoval(t *testing.T) {
	ts, projectID := boardServer(t, "clean-v2-full")
	panel := ts.get("/p/" + projectID + "/item/T-0001").Body
	revision := attrOf(t, testid(t, panel, "item-panel"), "data-revision")
	store, err := ts.registry.resolve(projectID)
	if err != nil {
		t.Fatal(err)
	}
	today, _ := mm.ParseDate("2026-09-05")
	if _, _, err := store.Remove("T-0001", mm.RemoveRequest{Force: true, WithDetail: true}, today); err != nil {
		t.Fatal(err)
	}
	body := ts.get("/p/" + projectID + "/item/T-0001/freshness?revision=" + revision).expectStatus(http.StatusOK).Body
	if got := attrOf(t, testid(t, body, "x-item-freshness"), "data-state"); got != "missing" {
		t.Fatalf("removed freshness state = %q", got)
	}
}

func TestStaleItemEditReturnsComparisonWithoutOverwriting(t *testing.T) {
	ts, projectID := boardServer(t, "clean-v2-full")
	panel := ts.get("/p/" + projectID + "/item/T-0001").Body
	revision := attrOf(t, testid(t, panel, "item-panel"), "data-revision")
	store, err := ts.registry.resolve(projectID)
	if err != nil {
		t.Fatal(err)
	}
	today, _ := mm.ParseDate("2026-09-05")
	if _, err := store.SetDetailBody("T-0001", "remote detail", false, today); err != nil {
		t.Fatal(err)
	}

	r := ts.form(http.MethodPatch, "/p/"+projectID+"/items/T-0001", url.Values{
		"expectedRevision": {revision}, "title": {"my title"}, "detail": {"my detail"},
		"base-title": {"Fix the deploy script"}, "base-detail": {"base detail"},
	}, "HX-Request", "true")
	if r.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", r.Status, r.Body)
	}
	for _, id := range []string{"x-item-edit-conflict", "x-item-edit-comparison", "x-item-edit-conflict-reload", "x-item-edit-conflict-keep"} {
		if !hasTestid(r.Body, id) {
			t.Errorf("conflict response missing %s", id)
		}
	}
	for _, value := range []string{"base detail", "my detail", "remote detail"} {
		if !strings.Contains(r.Body, value) {
			t.Errorf("comparison missing %q", value)
		}
	}
	if strings.Contains(strings.ToLower(r.Body), "overwrite") {
		t.Error("conflict offers blind overwrite")
	}
	current, err := store.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if current.Title == "my title" {
		t.Error("stale local title was written")
	}
}

func TestCurrentItemEditCommitsFieldsAndDetailTogether(t *testing.T) {
	ts, projectID := boardServer(t, "clean-v2-full")
	panel := ts.get("/p/" + projectID + "/item/T-0001").Body
	revision := attrOf(t, testid(t, panel, "item-panel"), "data-revision")
	r := ts.form(http.MethodPatch, "/p/"+projectID+"/items/T-0001", url.Values{
		"expectedRevision": {revision}, "title": {"saved together"}, "detail": {"new body"},
	})
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Status, r.Body)
	}
	store, _ := ts.registry.resolve(projectID)
	snapshot, err := store.ItemSnapshot("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Item.Title != "saved together" || snapshot.Detail == nil || snapshot.Detail.Title != "saved together" || !strings.Contains(snapshot.Detail.Body, "new body") {
		t.Fatalf("snapshot after save = %+v", snapshot)
	}
}

// §5.6: item-action-remove MUST carry data-guarded="true" and MUST NOT be
// satisfiable by a single click.
func TestRemoveIsGuarded(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	body := ts.get("/p/" + id + "/item/T-0001").Body
	remove := testid(t, body, "item-action-remove")
	if got := attrOf(t, remove, "data-guarded"); got != "true" {
		t.Errorf("item-action-remove carries data-guarded=%q", got)
	}
	if !strings.Contains(remove, "dialog/confirm-remove") {
		t.Errorf("remove does not open the confirmation dialog: %s", remove)
	}

	// And the route itself refuses without force, so the guard is not something
	// a client can forget its way past.
	r := ts.form(http.MethodDelete, "/p/"+id+"/items/T-0001", nil)
	if r.Status != http.StatusPreconditionFailed {
		t.Errorf("DELETE without force returned %d, want 412", r.Status)
	}

	// The item is still there.
	if ts.get("/p/"+id+"/item/T-0001").Status != http.StatusOK {
		t.Error("the item was removed by an unforced delete")
	}
}

// Every enabled entry in the card menu must DO something when clicked.
//
// mm.js returns early for the panel's operations — "the panel owns these" — so
// each is driven by its own hx-get. Without it the early return left the entry
// inert: Edit and Move did nothing at all, no request, no panel, no error
// (T-0105). Note was worse than inert: it posted an empty body and the server
// answered "a note needs some text" (T-0106), because a note's text is typed in
// item-notes, which lives in the panel (§7.2).
func TestCardMenuPanelOpsOpenThePanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	for _, op := range []string{"edit", "move", "note"} {
		entry := testid(t, body, "item-T-0001-action-"+op)
		if got := attrOf(t, entry, "hx-get"); got != "/p/"+id+"/item/T-0001" {
			t.Errorf("%s entry hx-get = %q, want the item panel route", op, got)
		}
		if got := attrOf(t, entry, "hx-target"); got != "#item-panel-root" {
			t.Errorf("%s entry hx-target = %q, want #item-panel-root", op, got)
		}
	}

	// And the route those entries point at actually serves the panel, with the
	// notes field the note entry exists to reach.
	panel := ts.get("/p/" + id + "/item/T-0001").expectStatus(http.StatusOK).Body
	if !hasTestid(panel, "item-panel") {
		t.Error("the route the menu entries open does not render the item panel")
	}
	if !hasTestid(panel, "item-notes") {
		t.Error("the panel has no item-notes for the note entry to land in")
	}

	// A disabled entry stays inert by being disabled, not by lacking wiring:
	// move is refused for a working item, and the reason is on the element.
	working := testid(t, body, "item-T-0003-action-move")
	if !strings.Contains(working, "disabled") {
		t.Errorf("move on a working item is not disabled: %s", working)
	}
}

// T-0147: the card menu (and the panel, which shares the actions list) offers
// "Move to top" and "Move to bottom" - sugar for --move --top/--end
// (spec-tools.md §5.1.7). Reordering never changes section.
func TestMoveTopAndBottomActions(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	// Ready items (clean-v2-full: T-0001, T-0002): both actions present and
	// enabled - a same-position move is a legal no-op, as on the CLI.
	for _, op := range []string{"move-top", "move-end"} {
		if entry := testid(t, body, "item-T-0001-action-"+op); strings.Contains(entry, "disabled") {
			t.Errorf("move on a ready item is disabled: %s", entry)
		}
	}
	// Unlike version 1 (where a working item has no position to move within -
	// each slot is its own file), version 2's board is one flat ordered list:
	// a working item has a position on its own stage's run like any other,
	// so move-top/move-end stay enabled there too (board.go's actionsForV2).
	for _, op := range []string{"move-top", "move-end"} {
		entry := testid(t, body, "item-T-0003-action-"+op)
		if strings.Contains(entry, "disabled") {
			t.Errorf("%s on a working item should be enabled on version 2: %s", op, entry)
		}
	}

	// Move to top: T-0002 lands before T-0001 in Ready.
	ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move-top", url.Values{}).expectStatus(http.StatusOK)
	if got := strings.Join(readyOrder(t, ts.get("/p/"+id+"/board").Body), ","); got != "T-0002,T-0001" {
		t.Errorf("after move-top the ready order is %s, want T-0002,T-0001", got)
	}

	// Move to bottom: T-0002 goes back to the end of Ready.
	ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move-end", url.Values{}).expectStatus(http.StatusOK)
	if got := strings.Join(readyOrder(t, ts.get("/p/"+id+"/board").Body), ","); got != "T-0001,T-0002" {
		t.Errorf("after move-end the ready order is %s, want T-0001,T-0002", got)
	}
}

// readyOrder lists a column's cards in render order, which is the section's
// order: data-position is computed per column, so reading the cards in DOM
// order is reading the file order.
func readyOrder(t *testing.T, body string) []string {
	t.Helper()
	col := columnBody(t, body, "board-column-ready")
	re := regexp.MustCompile(`data-testid="item-(T-\d{4})"`)
	var ids []string
	for _, m := range re.FindAllStringSubmatch(col, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

// spec-tools.md §5.1.6: a remove MUST either delete the item's detail file in
// the same transaction or report the orphan it left; "silently leaving an
// invalid directory is not conforming". The route did neither - it discarded
// the Removal - so removing an item through the context menu left the project
// failing I9 with nothing on screen to say so (T-0107).
func TestRemoveDeletesTheDetailFile(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	detail := filepath.Join(ts.Dirs[0], "details", "T-0001.md")

	if _, err := os.Stat(detail); err != nil {
		t.Fatalf("fixture has no detail file to remove: %v", err)
	}

	res := ts.form(http.MethodDelete, "/p/"+id+"/items/T-0001?force=true&withDetail=true", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("remove returned %d, want 200", res.Status)
	}

	if _, err := os.Stat(detail); !os.IsNotExist(err) {
		t.Errorf("details/T-0001.md survived a remove that asked for it to go (err=%v)", err)
	}

	// The whole point: the directory is still valid afterwards.
	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if v, err := store.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	} else if len(v) != 0 {
		t.Errorf("removing with the detail file left violations: %v", v)
	}
}

// The other branch of §5.1.6: the file may be left, but then the orphan MUST be
// reported. The toast is the only place a GUI user can learn it.
func TestRemoveWithoutDetailReportsTheOrphan(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	detail := filepath.Join(ts.Dirs[0], "details", "T-0001.md")

	res := ts.form(http.MethodDelete, "/p/"+id+"/items/T-0001?force=true", nil)
	if res.Status != http.StatusOK {
		t.Fatalf("remove returned %d, want 200", res.Status)
	}

	if _, err := os.Stat(detail); err != nil {
		t.Errorf("the detail file was deleted without being asked for: %v", err)
	}

	toast := testid(t, res.Body, "toast-1")
	if got := attrOf(t, toast, "data-severity"); got != "warning" {
		t.Errorf("orphan toast severity = %q, want warning", got)
	}
	if !strings.Contains(res.Body, "details/T-0001.md is now an orphan") {
		t.Errorf("the toast does not name the orphan it left: %s", toast)
	}
}

// The dialog offers the choice, and only when there is something to choose.
func TestRemoveDialogOffersTheDetailFile(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	// T-0001 has a detail file: the box is offered, and pre-checked, because
	// deleting is the only branch that leaves the directory valid.
	withDetail := ts.get("/p/" + id + "/dialog/confirm-remove?item=T-0001").expectStatus(http.StatusOK).Body
	if !hasTestid(withDetail, "x-dialog-confirm-remove-with-detail") {
		t.Fatal("the dialog does not offer to delete the detail file")
	}
	box := testid(t, withDetail, "x-dialog-confirm-remove-with-detail")
	if !strings.Contains(box, "checked") {
		t.Errorf("the detail-file box is not checked by default: %s", box)
	}
	if !strings.Contains(withDetail, "details/T-0001.md") {
		t.Error("the dialog does not name the file it would delete")
	}

	// T-0002 has none: the dialog says nothing about detail files.
	none := ts.get("/p/" + id + "/dialog/confirm-remove?item=T-0002").expectStatus(http.StatusOK).Body
	if hasTestid(none, "x-dialog-confirm-remove-with-detail") {
		t.Error("the dialog offers to delete a detail file that does not exist")
	}
}

// /p/:id/new is the same panel in the same position with an empty form (§4.1).
func TestNewItemPanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/new?stage=someday").expectStatus(http.StatusOK).Body

	panel := testid(t, body, "item-panel")
	if got := attrOf(t, panel, "data-new"); got != "true" {
		t.Errorf("data-new = %q", got)
	}
	if !hasTestid(body, "item-form") || !hasTestid(body, "item-field-title") {
		t.Error("the add panel is missing the form")
	}
	// An empty form has no actions to offer: there is no item yet.
	if hasTestid(body, "item-actions") {
		t.Error("the add panel offers item actions")
	}
	// The stage selector (§5.6, item-field-stage) carries the column it was
	// opened from as the selected option.
	if !strings.Contains(body, `data-testid="item-field-stage" name="stage"`) {
		t.Error("the add panel is missing its stage selector")
	}
	if !strings.Contains(body, `<option value="someday" selected>`) {
		t.Error("the add panel did not carry the column it was opened from")
	}
}

// Every operation of §6.1 that changes a file, end to end.
func TestMutations(t *testing.T) {
	cases := []struct {
		name    string
		fixture string // defaults to clean-full
		item    string
		run     func(ts *testServer, id string) *response
		assert  func(t *testing.T, store *mm.Store)
	}{
		{
			// clean-v2-full's working stage has room (wip.working: 2, T-0003
			// the only occupant) - starting at the limit is its own test
			// (boardv2_test.go's TestBoardV2StartDisabledOnlyAtItsOwnStageCap).
			name: "start",
			item: "T-0001",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0001/start", nil)
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0001")
				if err != nil {
					t.Fatal(err)
				}
				if it.Stage != "working" {
					t.Errorf("stage = %s, want working", it.Stage)
				}
			},
		},
		{
			name: "block prompts for a reason and refuses without one",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/block", nil)
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.Stage == "blocked" {
					t.Error("the item was blocked with no reason")
				}
			},
		},
		{
			name: "block with a reason",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/block",
					url.Values{"reason": {"waiting on the vendor"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.Stage != "blocked" || it.Reason == "" {
					t.Errorf("stage = %s, reason = %q", it.Stage, it.Reason)
				}
			},
		},
		{
			name: "finish defaults to shipped",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/finish", nil)
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.State != mm.StateDone || it.Outcome != mm.OutcomeShipped {
					t.Errorf("state = %s, outcome = %s", it.State, it.Outcome)
				}
			},
		},
		{
			name: "finish with an outcome",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/finish",
					url.Values{"outcome": {"cancelled"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.Outcome != mm.OutcomeCancelled {
					t.Errorf("outcome = %s", it.Outcome)
				}
			},
		},
		{
			name: "note",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/note",
					url.Values{"text": {"the cache key includes the build id"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.Detail == "" {
					t.Error("the note did not create a detail file for a backlog item")
				}
			},
		},
		{
			name: "move by position",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
					url.Values{"position": {"1"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				items, err := store.List(mm.Filter{Stage: "ready"})
				if err != nil {
					t.Fatal(err)
				}
				if len(items) == 0 || items[0].ID != "T-0002" {
					t.Errorf("the item did not move to the top: %v", items)
				}
			},
		},
		{
			name: "edit",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPatch, "/p/"+id+"/items/T-0002",
					url.Values{"title": {"A new title"}, "prio": {"high"}, "tags": {"infra,ci"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				it, err := store.Get("T-0002")
				if err != nil {
					t.Fatal(err)
				}
				if it.Title != "A new title" || it.Prio != mm.PrioHigh || len(it.Tags) != 2 {
					t.Errorf("item = %+v", it)
				}
			},
		},
		{
			name: "add",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodPost, "/p/"+id+"/items",
					url.Values{"title": {"Something new"}, "prio": {"low"}, "stage": {"someday"}})
			},
			assert: func(t *testing.T, store *mm.Store) {
				items, err := store.List(mm.Filter{Stage: "someday"})
				if err != nil {
					t.Fatal(err)
				}
				var found bool
				for _, it := range items {
					if it.Title == "Something new" {
						found = true
					}
				}
				if !found {
					t.Error("the added item is not in the someday section")
				}
			},
		},
		{
			name: "remove with force",
			run: func(ts *testServer, id string) *response {
				return ts.form(http.MethodDelete, "/p/"+id+"/items/T-0002?force=true", nil)
			},
			assert: func(t *testing.T, store *mm.Store) {
				if _, err := store.Get("T-0002"); err == nil {
					t.Error("the item is still there")
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := c.fixture
			if fixture == "" {
				fixture = "clean-v2-full"
			}
			ts, id := boardServer(t, fixture)
			c.run(ts, id)

			store, err := mm.Open(ts.Dirs[0])
			if err != nil {
				t.Fatal(err)
			}
			c.assert(t, store)

			// Whatever happened, the directory is still valid: the library
			// validates before every commit, and this proves the UI cannot
			// route around it.
			vs, err := store.Validate()
			if err != nil {
				t.Fatal(err)
			}
			if len(vs) != 0 {
				t.Errorf("the operation left the directory invalid: %v", vs)
			}
		})
	}
}

// A mutation returns the board plus what the change invalidated: the status bar
// and a toast, swapped out of band (§5.5, §5.10).
func TestMutationResponseCarriesTheCollateral(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
		url.Values{"position": {"1"}}, "HX-Request", "true")
	r.expectStatus(http.StatusOK)

	if !hasTestid(r.Body, "board") {
		t.Error("the response does not carry the board")
	}
	if !strings.Contains(r.Body, `data-testid="app-status"`) || !strings.Contains(r.Body, "hx-swap-oob") {
		t.Error("the response does not swap the status bar out of band")
	}
	if !hasTestid(r.Body, "toast-1") {
		t.Error("the response raises no toast")
	}
	toast := testid(t, r.Body, "toast-1")
	if got := attrOf(t, toast, "data-severity"); got != "success" {
		t.Errorf("toast severity = %q", got)
	}
}

// T-0146: saving an edit returns to the board — the address bar must follow
// the swap. Every mutation that dismisses the panel or a dialog replaces the
// transient URL (/p/:id/item/:itemId) with the board's, so a reload after
// saving shows the board rather than re-opening the panel. "Save and add
// another" keeps the panel open, so it must NOT replace the URL: the board
// would lie about what is on screen.
func TestMutationReturnsToTheBoardURL(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	// The reported bug: saving an edit leaves the URL on the item.
	r := ts.form(http.MethodPatch, "/p/"+id+"/items/T-0002",
		url.Values{"title": {"A new title"}}, "HX-Request", "true")
	r.expectStatus(http.StatusOK)
	r.expectHeader("HX-Replace-Url", "/p/"+id+"/board")

	// The panel's action buttons take the same path, because afterMutation is
	// the one place a dismissing mutation renders.
	r = ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
		url.Values{"position": {"1"}}, "HX-Request", "true")
	r.expectStatus(http.StatusOK)
	r.expectHeader("HX-Replace-Url", "/p/"+id+"/board")

	// ...but add-another keeps the panel open for the next title.
	r = ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {"Another"}, "addAnother": {"1"}}, "HX-Request", "true")
	r.expectStatus(http.StatusOK)
	r.expectNoHeader("HX-Replace-Url")
}

// §4.2: every mutating endpoint MUST accept dryRun and return the change set
// without writing.
func TestDryRunWritesNothing(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	before := fingerprintOfDir(t, ts.Dirs[0])
	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
		url.Values{"position": {"1"}, "dryRun": {"true"}})
	r.expectStatus(http.StatusOK)

	if got := fingerprintOfDir(t, ts.Dirs[0]); got != before {
		t.Error("a dry run wrote to the directory")
	}
	if !strings.Contains(r.Body, "data-dry-run=\"true\"") {
		t.Error("the response does not say it was a dry run")
	}
}

// §5.10 and §7.5: a WipLimitReached opens dialog-wip-limit. The dialog is
// reachable only as the error response to an actual --start attempt at the
// cap - there is no standalone GET route for it - so with version 1's
// mutations now refusing outright (T-0236), the v1 form of this scenario
// (occupants and the limit rendered from a working.NN.md slot set) can no
// longer be reached through the HTTP layer at all; a real v1 user hitting
// this would now get VersionMismatch instead, which is the intended
// behavior, not a regression to route around. This tests version 2 instead.
//
// Routing (409, the dialog-wip-limit fragment, the WipLimitReached code)
// already works for version 2, because StageWipLimitError.Unwrap() returns
// the same ErrWipLimitReached sentinel version 1's error does. The occupant
// list and limit numbers do NOT yet - envelopeFor (errors.go) only recovers
// *mm.WipLimitError, a different concrete type than version 2's
// *mm.StageWipLimitError, so those come back empty/zeroed. Tracked as its
// own follow-up rather than asserted here as if it worked; this only pins
// the part that is genuinely correct today.
func TestWipLimitOpensItsDialog(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	// working already holds T-0003; starting T-0001 fills the wip.working: 2
	// cap, so a third start (T-0002) hits it.
	ts.form(http.MethodPost, "/p/"+id+"/items/T-0001/start", nil, "HX-Request", "true").
		expectStatus(http.StatusOK)
	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/start", nil, "HX-Request", "true")
	if r.Status != http.StatusConflict {
		t.Fatalf("status = %d, want 409\nbody: %s", r.Status, r.Body)
	}
	if !hasTestid(r.Body, "dialog-wip-limit") {
		t.Fatalf("the response is not the WIP dialog:\n%s", r.Body)
	}
	dialog := testid(t, r.Body, "dialog-wip-limit")
	if got := attrOf(t, dialog, "data-code"); got != codeWipLimitReached {
		t.Errorf("data-code = %q", got)
	}
}

// The prompting dialogs of §7.2, fetched before the operation runs.
func TestDialogs(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	for _, c := range []struct{ name, testid string }{
		{"confirm-remove", "dialog-confirm-remove"},
		{"block", "dialog-block"},
		{"finish", "dialog-finish"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := ts.get("/p/" + id + "/dialog/" + c.name + "?item=T-0001").
				expectStatus(http.StatusOK).Body

			if !hasTestid(body, c.testid) {
				t.Fatalf("the dialog did not render:\n%s", body)
			}
			tag := testid(t, body, c.testid)
			if got := attrOf(t, tag, "role"); got != "dialog" {
				t.Errorf("role = %q", got)
			}
			if got := attrOf(t, tag, "aria-modal"); got != "true" {
				t.Errorf("aria-modal = %q, want true", got)
			}
			if !strings.Contains(body, "cancel") {
				t.Error("the dialog offers no way to cancel")
			}
		})
	}

	if got := ts.get("/p/" + id + "/dialog/nonsense?item=T-0001").Status; got != http.StatusNotFound {
		t.Errorf("an unknown dialog returned %d", got)
	}
}

// §5.10: dialog-conflict offers reload and MUST NOT offer a blind overwrite.
func TestConflictDialogOffersOnlyReload(t *testing.T) {
	ts := newTestServer(t)

	// The dialog is rendered directly: reaching it through a request would mean
	// provoking a real concurrent write, and what is under test is the markup.
	v := view{Data: map[string]any{"Code": codeConcurrent, "Message": "the file changed"}}
	var buf strings.Builder
	if err := ts.Server.renderer.renderFragment(&buf, "error", "dialog-conflict", v); err != nil {
		t.Fatal(err)
	}
	body := buf.String()

	if !hasTestid(body, "dialog-conflict-reload") {
		t.Error("the conflict dialog offers no reload")
	}
	for _, forbidden := range []string{"overwrite", "force", "Save anyway"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Errorf("the conflict dialog offers %q", forbidden)
		}
	}
}

func fingerprintOfDir(t *testing.T, dir string) mm.Fingerprint {
	t.Helper()
	store, err := mm.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return f
}
