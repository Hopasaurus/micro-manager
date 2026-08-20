package web

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// boardServer opens one fixture and returns the server plus its project id.
func boardServer(t *testing.T, fixture string) (*testServer, string) {
	t.Helper()
	ts := newTestServer(t, fixture)
	id, err := mm.ProjectID(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	return ts, id
}

// §5.5 fixes the columns and their DOM ORDER: someday, ready, blocked, working,
// then done. A suite reads them positionally.
func TestBoardColumnsAndOrder(t *testing.T) {
	ts, id := boardServer(t, "clean-multi-slot")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	want := []string{
		"board-column-someday", "board-column-ready", "board-column-blocked",
		"board-column-working", "board-column-done",
	}
	// The alternation is exact: every column's children repeat its testid as a
	// prefix, so a looser pattern matches board-column-ready-header too.
	columnRE := regexp.MustCompile(`data-testid="(board-column-(?:ready|blocked|someday|working|done))"`)
	var got []string
	for _, m := range columnRE.FindAllStringSubmatch(body, -1) {
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("columns are\n  %v\nwant\n  %v", got, want)
	}

	// Every column carries its header, title, count and body; backlog and done carry -add.
	for _, col := range want {
		suffixes := []string{"-header", "-title", "-count", "-body"}
		if col != "board-column-working" {
			suffixes = append(suffixes, "-add")
		}
		for _, suffix := range suffixes {
			if !hasTestid(body, col+suffix) {
				t.Errorf("%s is missing", col+suffix)
			}
		}
		if col == "board-column-working" && hasTestid(body, col+"-add") {
			t.Errorf("board-column-working-add MUST NOT exist (§4, D10)")
		}
	}

	if !hasTestid(body, "board-column-someday-toggle") {
		t.Errorf("board-column-someday-toggle is missing")
	}
}

// §5.5: the someday collapse is a client preference, sent on every htmx
// request. A refresh must render the column ALREADY collapsed (T-0150):
// collapsing it after the swap is what lets a full refresh expand the column
// for a frame and jump it back.
func TestSomedayCollapseRenderedFromHeader(t *testing.T) {
	ts, id := boardServer(t, "clean-multi-slot")
	target := "/p/" + id + "/board?fragment=1"

	// Without the header the column renders expanded, per the canonical §5.5.
	body := ts.get(target).Body
	if got := attrOf(t, testid(t, body, "board-column-someday"), "data-collapsed"); got != "false" {
		t.Errorf("someday data-collapsed = %q without the header, want false", got)
	}
	if got := somedayToggle(t, body); got != "&gt;" {
		t.Errorf("expanded toggle is %q, want &gt;", got)
	}

	// With the header the fragment is born collapsed, so a swap never expands it.
	body = ts.get(target, "X-Someday-Collapsed", "true").Body
	if got := attrOf(t, testid(t, body, "board-column-someday"), "data-collapsed"); got != "true" {
		t.Errorf("someday data-collapsed = %q with the header, want true", got)
	}
	if got := somedayToggle(t, body); got != "v" {
		t.Errorf("collapsed toggle is %q, want v", got)
	}

	// The collapse is the someday column's alone: a header must not leak
	// onto the other columns.
	if got := attrOf(t, testid(t, body, "board-column-ready"), "data-collapsed"); got != "" {
		t.Errorf("ready column carries data-collapsed=%q", got)
	}
}

// somedayToggle extracts the toggle's visible text from a board fragment.
func somedayToggle(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`<button[^>]*data-testid="board-column-someday-toggle"[^>]*>([^<]*)</button>`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no board-column-someday-toggle button in\n%s", body)
	}
	return m[1]
}

// T-0162: the column add links open the new-item panel through the same htmx
// swap the card title uses, so the + button stops being the board's last
// full-page load. The href stays for no-JS, and the hx-get carries the same
// ?stage= the href does, which seeds the new-item form's stage selector.
func TestColumnAddLinksSwapThePanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	ready := testid(t, body, "board-column-ready-add")
	href := attrOf(t, ready, "href")
	if href != "/p/"+id+"/new?stage=ready" {
		t.Errorf("ready add href = %q, want the stage-seeded panel URL", href)
	}
	for _, want := range []string{
		`hx-get="/p/` + id + `/new?stage=ready"`, // matches the href
		`hx-target="#item-panel-root"`,
		`hx-swap="innerHTML"`,
		`hx-push-url="true"`,
	} {
		if !strings.Contains(ready, want) {
			t.Errorf("the ready add link is missing %s: %s", want, ready)
		}
	}

	// Every column's add link seeds its own stage; the someday one proves
	// the hx-get follows the href rather than being hardcoded to ready.
	someday := testid(t, body, "board-column-someday-add")
	if got := attrOf(t, someday, "hx-get"); got != "/p/"+id+"/new?stage=someday" {
		t.Errorf("someday add hx-get = %q", got)
	}

	// The working column has no add link (its header carries no +).
	if hasTestid(body, "board-column-working-add") {
		t.Error("the working column has an add link")
	}

	// The swap contract: an htmx GET of the add link's URL returns the panel
	// fragment alone — the board stays, the app shell is not re-rendered.
	frag := ts.get("/p/"+id+"/new?section=ready", "HX-Request", "true").
		expectStatus(http.StatusOK).Body
	if !strings.Contains(frag, `data-testid="item-panel"`) {
		t.Errorf("the htmx add-link GET does not return the panel:\n%s", frag)
	}
	if strings.Contains(frag, `data-testid="app-header"`) {
		t.Error("the htmx add-link GET returned the whole shell, not the panel fragment")
	}
	// The fragment carries the seeded section into the form's selector.
	if !strings.Contains(frag, `<option value="ready" selected>`) {
		t.Error("the new-item form is not seeded with section=ready")
	}
}

// §5.1: the board carries data-wip-used and data-wip-limit; working cards carry
// data-slot.
func TestBoardAndWorkingAttributes(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").Body

	board := testid(t, body, "board")
	if attrOf(t, board, "data-wip-limit") == "" || attrOf(t, board, "data-wip-used") == "" {
		t.Errorf("the board carries no WIP counts: %s", board)
	}
	if got := attrOf(t, board, "data-state"); got != "ready" {
		t.Errorf("data-state = %q", got)
	}

	col := testid(t, body, "board-column-working")
	if got := attrOf(t, col, "role"); got != "list" {
		t.Errorf("working column role is %q, want list (§11 rule 3)", got)
	}

	card := testid(t, body, "item-T-0003")
	if got := attrOf(t, card, "data-slot"); got != "01" {
		t.Errorf("card data-slot = %q, want zero-padded form", got)
	}
}

// §5.5 fixes every attribute on a card, and they must be identical in every
// column because one template renders all of them.
func TestItemCardAttributes(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").Body

	// The fixture's first ready item.
	card := testid(t, body, "item-T-0001")
	for _, attr := range []string{
		"data-item-id", "data-item-state", "data-section", "data-prio",
		"data-tags", "data-blocked", "data-has-detail", "data-position",
	} {
		if attrOf(t, card, attr) == "" && attr != "data-tags" {
			t.Errorf("item-T-0001 carries no %s: %s", attr, card)
		}
	}
	if got := attrOf(t, card, "role"); got != "listitem" {
		t.Errorf("a card's role is %q, want listitem", got)
	}
	if got := attrOf(t, card, "draggable"); got != "true" {
		t.Errorf("a card is not draggable: %s", card)
	}
	if got := attrOf(t, card, "tabindex"); got != "0" {
		t.Errorf("a card is not keyboard reachable: %s", card)
	}

	for _, suffix := range []string{"-title", "-id", "-prio", "-tags", "-detail-indicator", "-menu"} {
		if !hasTestid(body, "item-T-0001"+suffix) {
			t.Errorf("item-T-0001%s is missing", suffix)
		}
	}
}

// data-position is 1-based within its column and is how a test asserts ordering
// without reading text (§5.5).
func TestItemPositionsAreOneBasedPerColumn(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board").Body

	ready := columnBody(t, body, "board-column-ready")
	positions := regexp.MustCompile(`data-position="(\d+)"`).FindAllStringSubmatch(ready, -1)
	if len(positions) < 2 {
		t.Fatalf("the ready column has %d cards; the fixture should have more", len(positions))
	}
	for i, m := range positions {
		if m[1] != itoa(i+1) {
			t.Errorf("card %d carries data-position=%q, want %d", i, m[1], i+1)
		}
	}
}

// §5.5: the menu contains one entry per legal operation, and illegal ones are
// PRESENT and disabled with data-reason naming the error code - a test asserts
// why an action is unavailable rather than finding nothing at all.
func TestItemActionsAreAlwaysPresent(t *testing.T) {
	// clean-multi-slot has a free slot, so start is legal here. The fixture with
	// every slot busy is the subject of the next test.
	ts, id := boardServer(t, "clean-multi-slot")
	body := ts.get("/p/" + id + "/board").Body

	ops := []string{"start", "pause", "finish", "block", "unblock", "move", "note", "edit", "remove"}
	for _, op := range ops {
		if !hasTestid(body, "item-T-0001-action-"+op) {
			t.Errorf("item-T-0001-action-%s is missing", op)
		}
	}

	// A backlog item cannot be paused, and the button says why.
	pause := testid(t, body, "item-T-0001-action-pause")
	if !strings.Contains(pause, "disabled") {
		t.Errorf("pause is enabled for a backlog item: %s", pause)
	}
	if got := attrOf(t, pause, "data-reason"); got != codePreconditionFailed {
		t.Errorf("pause carries data-reason=%q, want %s", got, codePreconditionFailed)
	}

	// And it can be started, because the fixture has a free slot.
	start := testid(t, body, "item-T-0001-action-start")
	if strings.Contains(start, "disabled") {
		t.Errorf("start is disabled with a free slot: %s", start)
	}
}

// At the WIP limit, start is disabled and says WipLimitReached - the same code
// the server would return, so the client's early disable and the server's
// refusal agree (§2.1).
func TestStartIsDisabledAtTheWipLimit(t *testing.T) {
	ts, id := boardServer(t, "clean-minimal")
	dir := ts.Dirs[0]

	store, err := mm.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d, err := store.Directory()
	if err != nil {
		t.Fatal(err)
	}
	// Fill every slot.
	items, err := store.List(mm.Filter{Section: mm.SectionReady})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < d.WipLimit {
		t.Skipf("the fixture has %d ready items for %d slots", len(items), d.WipLimit)
	}
	for i := 0; i < d.WipLimit; i++ {
		if _, _, err := store.Start(items[i].ID, mm.StartRequest{}, mm.Date{Year: 2026, Month: 7, Day: 30}); err != nil {
			t.Fatal(err)
		}
	}

	body := ts.get("/p/" + id + "/board").Body
	remaining, err := store.List(mm.Filter{Section: mm.SectionReady})
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) == 0 {
		t.Skip("no backlog item left to assert on")
	}
	start := testid(t, body, "item-"+string(remaining[0].ID)+"-action-start")
	if !strings.Contains(start, "disabled") {
		t.Errorf("start is enabled at the WIP limit: %s", start)
	}
	if got := attrOf(t, start, "data-reason"); got != codeWipLimitReached {
		t.Errorf("data-reason = %q, want %s", got, codeWipLimitReached)
	}
}

// §4.1: the board's query parameters are all optional and combinable, and
// loading a URI directly produces the same state as navigating to it.
func TestBoardFilters(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	base := "/p/" + id + "/board"

	all := ts.get(base).Body
	allCount := countCards(all)
	if allCount == 0 {
		t.Fatal("the unfiltered board is empty")
	}

	cases := []struct{ name, query string }{
		{"by section", "?section=ready"},
		{"by state", "?state=done"},
		{"by priority", "?prio=high"},
		{"by tag", "?tag=infra"},
		{"by free text", "?q=deploy"},
		{"combined", "?section=ready&prio=high"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			body := ts.get(base + c.query).expectStatus(http.StatusOK).Body
			if got := countCards(body); got > allCount {
				t.Errorf("%s returned more cards (%d) than the unfiltered board (%d)", c.query, got, allCount)
			}
			// The shell is intact whatever the filter.
			if !hasTestid(body, "board") || !hasTestid(body, "app-status") {
				t.Error("a filtered board lost part of the shell")
			}
		})
	}

	// A section filter really does narrow to that section.
	readyOnly := ts.get(base + "?section=ready").Body
	if strings.Contains(columnBody(t, readyOnly, "board-column-done"), "data-item-id") {
		t.Error("?section=ready left cards in the done column")
	}
}

// §4.1: /p/:projectId MUST redirect (302) to /p/:projectId/board.
func TestBoardRedirect(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.get("/p/" + id)
	if r.Status != http.StatusFound {
		t.Errorf("status = %d, want 302", r.Status)
	}
	if got := r.Header.Get("Location"); got != "/p/"+id+"/board" {
		t.Errorf("Location = %q", got)
	}

	// An unknown id is a 404 there too, not a redirect into nowhere.
	if got := ts.get("/p/ffffffffffff").Status; got != http.StatusNotFound {
		t.Errorf("an unknown project redirected with %d", got)
	}
}

// The board is a fragment for htmx and a whole page for a direct load, and the
// fragment is byte-identical inside the page (§4.1, architecture.md §4.3).
func TestBoardFragmentParity(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	page := ts.get("/p/" + id + "/board").Body
	fragment := ts.get("/p/"+id+"/board", "HX-Request", "true").Body

	if strings.Contains(fragment, "<!DOCTYPE") {
		t.Error("the htmx response is a whole document")
	}
	if !strings.Contains(page, strings.TrimSpace(fragment)) {
		t.Error("the board fragment is not byte-identical inside the page")
	}
}

// A directory with existing violations must still render: refusing to show a
// broken directory removes the tool exactly when it is needed (spec-tools.md §8).
func TestBoardRendersABrokenDirectory(t *testing.T) {
	for _, fixture := range []string{"broken-i9-title-drift", "broken-i5-blocked-without-reason", "broken-i3-closed-in-backlog"} {
		t.Run(fixture, func(t *testing.T) {
			ts, id := boardServer(t, fixture)
			body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
			if !hasTestid(body, "board") {
				t.Error("the board did not render")
			}
		})
	}
}

// columnBody returns the markup between a column's body element and the end of
// that column, which is enough to assert what is inside it.
func columnBody(t *testing.T, body, column string) string {
	t.Helper()
	start := strings.Index(body, `data-testid="`+column+`-body"`)
	if start < 0 {
		t.Fatalf("no %s-body in the document", column)
	}
	rest := body[start:]
	end := strings.Index(rest, "</section>")
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func countCards(body string) int {
	return len(regexp.MustCompile(`data-testid="item-T-\d{4}"`).FindAllString(body, -1))
}

func itoa(n int) string { return strconv.Itoa(n) }
