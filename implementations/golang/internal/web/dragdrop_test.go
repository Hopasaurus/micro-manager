package web

import (
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"micromanager/mm"
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
	t.Run("slot to slot", func(t *testing.T) {
		ts, id := boardServer(t, "clean-multi-slot")

		// clean-multi-slot has an item in slot 1. Starting it again is the
		// server-side shape of dragging it to another slot.
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
			url.Values{"slot": {"2"}})
		if r.Status == http.StatusOK {
			t.Error("the server allowed a slot-to-slot move; slots are interchangeable (§7.2)")
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
	// client.
	if lines := strings.Count(js, "\n"); lines > 700 {
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
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/move",
		url.Values{"section": {"someday"}, "position": {"1"}})
	r.expectStatus(http.StatusOK)

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.List(mm.Filter{Section: mm.SectionSomeday})
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
