package web

// T-0144 — the done column's header count reports what is really done, not the
// capped number of cards.
//
// T-0145 — a way to see every done item: board-column-done-show-all, a link to
// the addressable ?done=all state, present only while the limit hides some.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// doneHeavyServer is a server over a fixture with more done items than the
// default ui.board.doneLimit (20) renders.
func doneHeavyServer(t *testing.T) (*testServer, string) {
	t.Helper()
	dir := bigDoneFixture(t, t.TempDir(), 12, 25) // 25 done, 12 ready
	ts := serverOver(t, dir, nil)
	id := projectIDOf(t, ts, dir)
	return ts, id
}

// elementText returns the inline text of the element with the given testid,
// up to the first closing angle bracket of its content — enough for the count
// span and the show-all link.
func elementText(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`data-testid="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)<`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no element %q with inline text", id)
	}
	return m[1]
}

// T-0144: the done column header must show the FULL done count, as "shown of
// total", and the column must carry both the rendered count and the total.
func TestDoneColumnCountIsTheFullCount(t *testing.T) {
	ts, id := doneHeavyServer(t)

	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	col := testid(t, body, "board-column-done")
	if got := attrOf(t, col, "data-count"); got != "20" {
		t.Errorf("data-count = %q, want 20 (cards rendered under the default limit)", got)
	}
	if got := attrOf(t, col, "data-total"); got != "25" {
		t.Errorf("data-total = %q, want 25 (the full done count)", got)
	}

	// The header reads "20 of 25", not "20" — that is the bug T-0144 filed.
	if got := elementText(t, body, "board-column-done-count"); got != "20 of 25" {
		t.Errorf("board-column-done-count = %q, want \"20 of 25\"", got)
	}

	// Every other column reports plain numbers: the "of" form is the done
	// column's way of saying something is hidden.
	for _, key := range []string{"someday", "ready", "blocked", "working"} {
		if c := elementText(t, body, "board-column-"+key+"-count"); strings.Contains(c, "of") {
			t.Errorf("%s-count = %q, the \"of\" form must be done-only", key, c)
		}
	}
}

// The bare count when nothing is hidden: "3", never "3 of 3".
func TestDoneColumnCountWhenUntruncated(t *testing.T) {
	ts, id := boardServer(t, "clean-full") // 3 done items, under the limit
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	col := testid(t, body, "board-column-done")
	if got := attrOf(t, col, "data-total"); got != "3" {
		t.Errorf("data-total = %q, want 3", got)
	}
	if got := elementText(t, body, "board-column-done-count"); got != "3" {
		t.Errorf("board-column-done-count = %q, want the bare \"3\"", got)
	}
	if hasTestid(body, "board-column-done-show-all") {
		t.Error("show-all present when nothing is hidden")
	}
}

// T-0145: the affordance exists only while items are hidden, and it links to
// the addressable state that shows them all.
func TestShowAllDoneItems(t *testing.T) {
	ts, id := doneHeavyServer(t)

	truncated := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
	link := testid(t, truncated, "board-column-done-show-all")
	if !strings.HasPrefix(link, "<a") {
		t.Fatalf("board-column-done-show-all is not a link: %s", link)
	}
	href := attrOf(t, link, "href")
	if href != "/p/"+id+"/board?done=all" {
		t.Errorf("href = %q, want the addressable ?done=all state", href)
	}
	if got := elementText(t, truncated, "board-column-done-show-all"); got != "Show all 25" {
		t.Errorf("link text = %q, want it to name the full count", got)
	}
}

// ?done=all renders every done item and drops the affordance: there is nothing
// left to show, and the header reads the bare count.
func TestDoneAllRendersEverything(t *testing.T) {
	ts, id := doneHeavyServer(t)
	body := ts.get("/p/" + id + "/board?done=all").expectStatus(http.StatusOK).Body

	col := testid(t, body, "board-column-done")
	if got := attrOf(t, col, "data-count"); got != "25" {
		t.Errorf("data-count = %q, want 25 under done=all", got)
	}
	if got := attrOf(t, col, "data-total"); got != "25" {
		t.Errorf("data-total = %q, want 25", got)
	}
	if got := countCards(columnBody(t, body, "board-column-done")); got != 25 {
		t.Errorf("done column rendered %d cards, want 25", got)
	}
	if got := elementText(t, body, "board-column-done-count"); got != "25" {
		t.Errorf("board-column-done-count = %q, want the bare \"25\"", got)
	}
	if hasTestid(body, "board-column-done-show-all") {
		t.Error("show-all present under done=all, where nothing is hidden")
	}
}

// §4.1 rule 2: loading the URI directly produces the same state as navigating
// to it. The board STATE must be stable across loads — the recent list is
// allowed to move (the first full load records the open, §10 rule 1), so the
// board section is compared, not the whole page.
func TestDoneAllIsAddressable(t *testing.T) {
	ts, id := doneHeavyServer(t)

	first := ts.get("/p/" + id + "/board?done=all").expectStatus(http.StatusOK).Body
	second := ts.get("/p/" + id + "/board?done=all").expectStatus(http.StatusOK).Body

	for _, body := range []string{first, second} {
		if got := attrOf(t, testid(t, body, "board-column-done"), "data-count"); got != "25" {
			t.Errorf("a direct load does not render the full done column: data-count=%q", got)
		}
	}
	board := func(body string) string {
		i := strings.Index(body, `data-testid="board"`)
		return body[i:]
	}
	if board(first) != board(second) {
		t.Error("the board state differs between two loads of ?done=all")
	}
}

// The SSE backstop re-fetches the board every poll (partials/board.html's
// hx-get on [data-testid=board]); it must carry done=all forward, or a poll
// would silently collapse the column back to the limit a beat later.
func TestBoardRefreshPreservesDoneAll(t *testing.T) {
	ts, id := doneHeavyServer(t)

	board := ts.get("/p/" + id + "/board?done=all").expectStatus(http.StatusOK).Body
	section := testid(t, board, "board")
	if !strings.Contains(section, `hx-get="/p/`+id+`/board?fragment=1&done=all"`) {
		t.Errorf("board hx-get does not preserve done=all:\n%s", section)
	}

	plain := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
	if strings.Contains(testid(t, plain, "board"), "done=all") {
		t.Error("the plain board's hx-get mentions done=all")
	}
}

// doneLimit=0 already means "show everything" (the config's escape hatch), and
// must behave exactly like done=all: full column, bare count, no affordance.
func TestDoneAllWithUncappedLimit(t *testing.T) {
	dir := bigDoneFixture(t, t.TempDir(), 12, 25)
	ts := serverOver(t, dir, func(o *Options) { o.Config.UI.Board.DoneLimit = 0 })
	id := projectIDOf(t, ts, dir)

	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
	if got := attrOf(t, testid(t, body, "board-column-done"), "data-count"); got != "25" {
		t.Errorf("data-count = %q, want 25 with doneLimit=0", got)
	}
	if hasTestid(body, "board-column-done-show-all") {
		t.Error("show-all present when doneLimit=0 shows everything")
	}
}

// The default board render is unchanged: the refresh-cost measurement that
// pins the cap (refreshcost_test.go) keeps its contract — 12 ready + limit
// cards.
func TestDoneColumnCardCountStillCapped(t *testing.T) {
	ts, id := doneHeavyServer(t)
	body := ts.get("/p/" + id + "/board?fragment=1").expectStatus(http.StatusOK).Body
	want := 12 + mm.DefaultConfig().UI.Board.DoneLimit
	if got := countCards(body); got != want {
		t.Errorf("rendered %d cards, want %d (12 ready + DoneLimit)", got, want)
	}
}
