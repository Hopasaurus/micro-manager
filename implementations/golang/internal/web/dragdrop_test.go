package web

import (
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Drag and drop is a browser behaviour, so what a Go test can hold is the half
// that matters most: the SERVER refusing every transition §7.2 calls illegal,
// and the client script carrying the attributes §7.3 fixes.
//
// The gesture itself is exercised in a real browser; §12's external suite is
// what covers it for every implementation.

// §7.2: a drop onto an illegal target must be refused. The client marks it
// data-drop-allowed="false" and issues no request, but the server is the
// authority and has to refuse it too (§2.1).
func TestIllegalTransitionsAreRefused(t *testing.T) {
	t.Run("working to working", func(t *testing.T) {
		ts, id := boardServer(t, "clean-multi-slot")

		// clean-multi-slot has an item in slot 1. Starting it again or moving it to
		// working is the server-side shape of an intra-working drag.
		store, err := mm.Open(ts.Dirs[0])
		if err != nil {
			t.Fatal(err)
		}
		dir, err := store.Directory()
		if err != nil {
			t.Fatal(err)
		}
		var working mm.ID
		for _, slot := range dir.Slots {
			if slot.Item != nil {
				working = slot.Item.ID
			}
		}
		if working == "" {
			t.Skip("the fixture has no item in a slot")
		}

		r := ts.form(http.MethodPost, "/p/"+id+"/items/"+string(working)+"/start",
			url.Values{})
		if r.Status == http.StatusOK {
			t.Error("the server allowed starting an already working item; working->working is illegal (§7.2)")
		}

		r2 := ts.form(http.MethodPost, "/p/"+id+"/items/"+string(working)+"/move",
			url.Values{"section": {"working"}})
		if r2.Status == http.StatusOK {
			t.Error("the server allowed moving a working item; working->working is illegal (§7.2)")
		}
	})

	t.Run("out of done", func(t *testing.T) {
		ts, id := boardServer(t, "clean-full")

		store, err := mm.Open(ts.Dirs[0])
		if err != nil {
			t.Fatal(err)
		}
		items, err := store.List(mm.Filter{State: mm.StateDone})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 0 {
			t.Skip("the fixture has nothing done")
		}
		done := string(items[0].ID)

		// Reopening is not a specified operation in v1 (§7.2), so every route
		// out of done must refuse.
		for _, op := range []string{"start", "pause", "move"} {
			r := ts.form(http.MethodPost, "/p/"+id+"/items/"+done+"/"+op,
				url.Values{"section": {"ready"}, "position": {"1"}})
			if r.Status == http.StatusOK {
				t.Errorf("%s on a done item was allowed", op)
			}
		}
	})
}

// The board's own actions say the same thing: a done item offers no move, and
// the reason names the code the server would return.
func TestDoneItemsOfferNoMove(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/board?state=done").Body

	ids := regexp.MustCompile(`data-testid="item-(T-\d{4})"`).FindAllStringSubmatch(body, -1)
	if len(ids) == 0 {
		t.Skip("no done items on the board")
	}
	item := ids[0][1]

	for _, op := range []string{"start", "pause", "move", "block"} {
		button := testid(t, body, "item-"+item+"-action-"+op)
		if !strings.Contains(button, "disabled") {
			t.Errorf("%s is offered on a done item: %s", op, button)
		}
		if attrOf(t, button, "data-reason") == "" {
			t.Errorf("%s carries no data-reason", op)
		}
	}
}

// Every column's DROP ZONE is as tall as the tallest column (T-0100).
//
// The board laid columns out with align-items: flex-start, so each stopped at
// its own last card. The empty space beside a short column belonged to the
// board, not to any column, and mm.js resolves a drop with
// closest('.mm-column__body') — so aiming there hit nothing and the drop was
// silently discarded. A column with one card was measurably harder to drop into
// than a full one.
//
// Two rules carry this and BOTH are required: stretch makes the column tall,
// flex:1 makes the BODY (the droppable element) fill it rather than ending at
// its last card.
//
// Source-shape assertion, like the ones below: the geometry itself needs a
// browser. Measured there when this shipped — someday with one card had a 77px
// body and a point 400px down hit nothing; afterwards the body was 415px, equal
// to ready's, and the same point resolved to someday. T-0111 tracks closing
// this gap properly.
func TestColumnDropZonesShareTheTallestHeight(t *testing.T) {
	css, err := os.ReadFile("static/mm.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(css)

	// Anchored to a line start: the collapsed-column rule below contains
	// ".mm-column__body {" as a substring, and matching that one instead reads
	// display:none and reports the opposite of the truth.
	rule := func(selector string) string {
		start := strings.Index(sheet, "\n"+selector+" {")
		if start < 0 {
			t.Fatalf("mm.css has no %s rule", selector)
		}
		block := sheet[start:]
		return block[:strings.Index(block, "}")]
	}

	board := rule(".mm-board")
	if strings.Contains(board, "align-items: flex-start") {
		t.Error(".mm-board uses align-items: flex-start, so a short column's drop " +
			"zone stops at its last card and the space below it is not droppable (T-0100)")
	}
	if !strings.Contains(board, "align-items: stretch") {
		t.Error(".mm-board does not stretch its columns to a shared height (T-0100)")
	}

	body := rule(".mm-column__body")
	if !strings.Contains(body, "flex: 1") {
		t.Error(".mm-column__body has no flex: 1, so the body ends at its last card " +
			"and the column's extra height is not part of the drop zone (T-0100)")
	}
}

// The placeholder must be OUT of the column's flow, so the cards are always
// measured at their natural positions.
//
// The first version of this guard (T-0110) asserted that the dragover handler
// clears the in-flow placeholder before measuring: an in-flow placeholder sits
// between cards, every card below it drops one placeholder-height, and
// measuring around it fed that displacement back into the next index — the
// index pinned wherever it first landed and dragging DOWN was a no-op. T-0149
// removed the placeholder from the flow entirely, which makes that ordering
// moot: an absolutely-positioned placeholder displaces nothing, so the natural
// positions ARE the measured positions and the T-0110 feedback loop cannot
// recur. It also fixed the worse half of the same bug — an in-flow placeholder
// shifted the card under the pointer during the last dragover, moving the
// browser's drop target away and losing the release entirely (T-0141).
//
// Two source-shape assertions in the style of the tests around here: the
// gesture itself needs a browser, but a re-introduction of an in-flow
// placeholder is exactly what would silently bring both bugs back.
func TestPlaceholderIsOutOfTheColumnFlow(t *testing.T) {
	css, err := os.ReadFile("static/mm.css")
	if err != nil {
		t.Fatal(err)
	}
	sheet := string(css)

	start := strings.Index(sheet, ".mm-drop-placeholder {")
	if start < 0 {
		t.Fatal("mm.css has no .mm-drop-placeholder rule")
	}
	rule := sheet[start:]
	rule = rule[:strings.Index(rule, "}")]

	if !strings.Contains(rule, "position: absolute") {
		t.Error("the drop placeholder is in the column's flow: inserting it " +
			"shifts the card under the pointer and the release is lost (T-0149)")
	}
	if !strings.Contains(rule, "pointer-events: none") {
		t.Error("the drop placeholder can be a drag/drop target; it is removed " +
			"and re-inserted on every dragover, so a release over it targets a " +
			"detached element (T-0149)")
	}

	script, err := os.ReadFile("static/mm.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(script)

	if !strings.Contains(js, "placeholder.style.top") {
		t.Error("mm.js does not position the placeholder with style.top; it must " +
			"be positioned absolutely, out of flow (T-0149)")
	}
	if strings.Contains(js, "insertBefore(placeholder") {
		t.Error("mm.js inserts the placeholder into the column's flow; it must " +
			"be positioned absolutely, out of flow (T-0149)")
	}
}

// A collapsed column is still a drop target (§7.1, T-0152).
//
// mm.css hides a collapsed column's body outright, so the element mm.js used
// to resolve a drop against — closest('.mm-column__body') — is not in the
// layout and a pointer over the column hits the header instead. Verified in a
// browser when this shipped: over a collapsed Someday, elementFromPoint
// returned board-column-someday-header, whose closest('.mm-column__body') is
// null; the drop was accepted only once the lookup fell back to the column.
//
// A source-shape guard in the style of the ones around here. The behaviour
// itself is covered by jstest/mm.test.js, which runs the real script against a
// real DOM — but that suite SKIPS where node or jsdom is absent, and this is
// the cheap check that survives there.
func TestCollapsedColumnsResolveADrop(t *testing.T) {
	script, err := os.ReadFile("static/mm.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(script)

	if !strings.Contains(js, `.mm-column[data-collapsed="true"]`) {
		t.Error("mm.js never resolves a hit inside a collapsed column, so a drop " +
			"on a collapsed Someday hits no column body and is discarded (T-0152)")
	}
	for _, handler := range []string{"'dragover'", "'drop'"} {
		i := strings.Index(js, "addEventListener("+handler)
		if i < 0 {
			t.Fatalf("mm.js has no %s handler", handler)
		}
		body := js[i:]
		if end := strings.Index(body, "\n  });"); end > 0 {
			body = body[:end]
		}
		if strings.Contains(body, "closest('.mm-column__body')") {
			t.Errorf("the %s handler resolves the column body directly; a collapsed "+
				"column has none in the layout and its drops are lost (T-0152)", handler)
		}
	}
}

// §7.3 fixes the attributes a drag maintains, and §7.4 requires move mode to
// maintain the SAME ones so one set of assertions covers both input paths.
//
// This asserts the client script still carries them. It is a crude check and it
// is deliberately crude: it catches a rename or a deletion, which is what would
// silently break the external suite, without pretending to run a gesture.
func TestDragAttributesArePresentInTheClient(t *testing.T) {
	script, err := os.ReadFile("static/mm.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(script)

	for _, attr := range []string{
		"data-dragging", "data-drag-source", "data-drop-target",
		"data-drop-allowed", "data-drop-index", "data-drop-reason",
		"drop-placeholder", "data-move-mode", "data-busy",
	} {
		if !strings.Contains(js, attr) {
			t.Errorf("mm.js no longer maintains %s (§7.3, §7.4)", attr)
		}
	}

	// §7.4: the key bindings are normative.
	for _, key := range []string{"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight", "Enter", "Escape"} {
		if !strings.Contains(js, key) {
			t.Errorf("mm.js handles no %s key (§7.4)", key)
		}
	}

	// The one permitted duplication is the transition table, and it must name
	// the same error codes the server returns.
	if !strings.Contains(js, "Conflict") {
		t.Error("the client transition table names no error code")
	}

	// §4.6 of the architecture: keep this file small. If it grows past a few
	// hundred lines, something belongs on the server that drifted onto the
	// client. T-0150 added the someday-collapse request header - a few lines of
	// plumbing the server could not do itself - and T-0139 moved the polling
	// backstop from the templates' triggers into mm.js (the SSE-down interval),
	// which took the ceiling to 780. T-0152 added the collapsed-column drop
	// resolver — a hit test, which only the client can do — for 795. T-0173
	// added the Wake-up group's visibility toggles (kind select chooses the
	// shape; the new panel's section selector reveals the group) — a response
	// to a select that must be instant, which only the client can do — for 900.
	// T-0242 added the item panel's resize drag (pointer math and the clamp,
	// which the server has no way to compute before the client's own viewport
	// width is known) — for 960.
	if lines := strings.Count(js, "\n"); lines > 960 {
		t.Errorf("mm.js is %d lines; something has drifted onto the client", lines)
	}
}

// Every operation a drag can produce is reachable without one (§6.2), which is
// required three times over: for keyboard users, for the TUI, and because a
// suite that can only express intent through synthesized pointer gestures is
// brittle.
func TestEveryDragHasANonDragEquivalent(t *testing.T) {
	ts, id := boardServer(t, "clean-multi-slot")
	body := ts.get("/p/" + id + "/board").Body

	// The drag targets of §7.2, and the route each maps to.
	for _, op := range []string{"start", "pause", "finish", "block", "unblock", "move"} {
		if !hasTestid(body, "item-T-0001-action-"+op) {
			t.Errorf("no non-drag equivalent for %s on the card menu", op)
		}
	}

	panel := ts.get("/p/" + id + "/item/T-0001").Body
	for _, op := range []string{"start", "pause", "finish", "block", "unblock", "move"} {
		if !hasTestid(panel, "item-action-"+op) {
			t.Errorf("no non-drag equivalent for %s in the item panel", op)
		}
	}
}

// A drop into a backlog column names the column and the index, and the server
// puts the item exactly there.
func TestDropPositionIsHonoured(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
		url.Values{"stage": {"someday"}, "position": {"1"}})
	r.expectStatus(http.StatusOK)

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(mm.Filter{Stage: "someday"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 || items[0].ID != "T-0002" {
		t.Errorf("the item is not at the top of someday: %v", items)
	}

	// And the re-rendered board carries the new positions, which is what a
	// rejected drag would otherwise have to restore by hand (§7.1).
	body := ts.get("/p/" + id + "/board").Body
	someday := columnBody(t, body, "board-column-someday")
	first := regexp.MustCompile(`data-testid="item-(T-\d{4})"[^>]*data-position="1"`).FindStringSubmatch(someday)
	if first == nil || first[1] != "T-0002" {
		t.Errorf("data-position was not recomputed: %v", first)
	}
}

// §7.2 / D12: pausing a working item into Blocked MUST prompt for a reason
// and post it with section=blocked.
func TestWorkingToBlockedPromptsAndPauses(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")

	// clean-v2-full has T-0003 on stage:working.
	dialogBody := ts.get("/p/" + id + "/dialog/block?item=T-0003").expectStatus(http.StatusOK).Body
	if !hasTestid(dialogBody, "dialog-block") {
		t.Errorf("dialog-block not rendered for working item: %s", dialogBody)
	}
	if !strings.Contains(dialogBody, `action="/p/`+id+`/items/T-0003/pause"`) &&
		!strings.Contains(dialogBody, `hx-post="/p/`+id+`/items/T-0003/pause"`) {
		t.Errorf("dialog-block form does not post to /pause for working item: %s", dialogBody)
	}

	// Unlike version 1's Pause (which accepts a NEW reason at pause time),
	// pauseV2 itself only checks whether the item ALREADY carries one for a
	// needs_reason destination (research decision 18). dialog-block is one
	// dialog asking for the reason and pausing in a single submission
	// though, so the web handler sets the reason (via Update) before pausing
	// when both a stage and a reason arrive together - matching the
	// dialog's own single form (T-0245; previously refused with the dialog
	// stuck open on a fresh item, found live).
	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0003/pause",
		url.Values{"stage": {"blocked"}, "reason": {"waiting on ops"}})
	r.expectStatus(http.StatusOK)

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	it, err := store.Get("T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if it.Stage != "blocked" || it.Reason != "waiting on ops" {
		t.Errorf("item not paused into blocked with reason: %+v", it)
	}
}
