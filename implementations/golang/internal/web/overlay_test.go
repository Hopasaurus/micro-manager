package web

// The overlay lifecycle (T-0078, T-0083, T-0085).
//
// Three user-visible failures shared one root: nothing ever dismissed the
// transient overlays, and one overlay could not open at all.
//
//   - The item panel swaps into #item-panel-root (the container in the app
//     shell). The card link targets it by id; the panel fragment is served by
//     the same route that renders the full panel page.
//   - ANY successful mutation returns board-swap, which out-of-band dismisses
//     both overlays: the board already reflects the new state, and a panel or
//     dialog left open over it would show values that no longer exist.
//   - Error responses use the error template and leave the overlay open, so
//     the user can correct the form that failed.
//
// The dismissal is out-of-band because the mutation response's in-band target
// is the board: htmx swaps the board with the response body and applies the
// hx-swap-oob elements on top.

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// panelDismiss is the OOB element that removes the item panel.
const panelDismiss = `<div hx-swap-oob="outerHTML:[data-testid='item-panel']"></div>`

// dialogDismiss is the OOB element that clears every open dialog. The string
// matches the EMPTY dismissal element exactly: a dialog fragment legitimately
// carries the same attribute value when it swaps ITSELF into dialog-root.
const dialogDismiss = `<div hx-swap-oob="innerHTML:[data-testid='dialog-root']"></div>`

// T-0083: a card must open the item panel. The swap target is the container
// in the shell; the panel fragment is served by the item route when htmx
// asks for one.
func TestPanelOpensFromCard(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
	if !strings.Contains(body, `id="item-panel-root"`) {
		t.Fatal("the board page has no #item-panel-root container for the panel")
	}
	if !strings.Contains(body, `hx-target="#item-panel-root"`) {
		t.Fatal("item cards do not target #item-panel-root")
	}

	// The fragment an htmx click receives is the panel, not the whole page.
	frag := ts.get("/p/"+id+"/item/T-0001", "HX-Request", "true").
		expectStatus(http.StatusOK).Body
	if !hasTestid(frag, "item-panel") {
		t.Fatalf("the htmx item route did not return the panel fragment:\n%s", frag)
	}
	if hasTestid(frag, "board") {
		t.Error("the htmx item route returned the board; the panel must swap in alone")
	}
	if !hasTestid(frag, "item-form") {
		t.Error("the panel fragment lacks the edit form")
	}
}

// T-0078: saving a NEW item dismisses the panel — the board swap carries the
// dismissal out of band.
func TestNewItemSaveDismissesPanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	// The new panel page renders the panel over the board.
	page := ts.get("/p/" + id + "/new").expectStatus(http.StatusOK).Body
	if !hasTestid(page, "item-panel") {
		t.Fatalf("the new page has no panel:\n%s", page)
	}

	r := ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {"A fresh item"}, "section": {"ready"}},
		"HX-Request", "true")
	r.expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, panelDismiss) {
		t.Errorf("the add response does not dismiss the panel:\n%s", r.Body)
	}
	if !strings.Contains(r.Body, dialogDismiss) {
		t.Error("the add response does not clear the dialog root")
	}
}

// Saving an EDIT dismisses the panel too: the board shows the saved item.
func TestEditSaveDismissesPanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPatch, "/p/"+id+"/items/T-0001",
		url.Values{"title": {"Renamed"}}, "HX-Request", "true")
	r.expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, panelDismiss) {
		t.Errorf("the edit response does not dismiss the panel:\n%s", r.Body)
	}
}

// T-0080: "save and add another" — the saved item lands on the board, but
// instead of dismissing the panel, the response swaps a FRESH empty form into
// #item-panel-root. The dismissals must NOT be present: that is the whole
// point of the button.
func TestAddAnotherKeepsFreshPanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {"First of many"}, "section": {"ready"},
			"addAnother": {"1"}},
		"HX-Request", "true")
	r.expectStatus(http.StatusOK)

	if !hasTestid(r.Body, "board") {
		t.Errorf("the add-another response lacks the refreshed board:\n%s", r.Body)
	}
	if !strings.Contains(r.Body, `<div hx-swap-oob="innerHTML:#item-panel-root">`) {
		t.Errorf("the add-another response does not re-open the panel:\n%s", r.Body)
	}
	// The old panel must be REMOVED wherever it lives (app-main on a direct
	// load, #item-panel-root on an htmx flow) — one panel in the DOM, ever.
	if !strings.Contains(r.Body, panelDismiss) {
		t.Errorf("the add-another response does not dismiss the old panel:\n%s", r.Body)
	}
	if !strings.Contains(r.Body, dialogDismiss) {
		t.Error("the add-another response does not clear the dialog root")
	}

	// The re-opened form is the EMPTY new-item form, not the saved item's.
	// The board's cards legitimately carry data-item-id, so assert on the
	// panel element alone.
	panel := regexp.MustCompile(`(?s)<aside data-testid="item-panel".*?</aside>`).FindString(r.Body)
	if panel == "" {
		t.Fatalf("no item panel in the response:\n%s", r.Body)
	}
	if !strings.Contains(panel, `data-new="true"`) {
		t.Errorf("the re-opened panel is not a fresh new-item form:\n%s", panel)
	}
	if strings.Contains(panel, `data-item-id=`) {
		t.Error("the re-opened panel is bound to an item; it must be blank")
	}
	if !strings.Contains(panel, `hx-post="/p/`+id+`/items"`) {
		t.Error("the re-opened form does not POST to the add route")
	}
	if !strings.Contains(panel, `name="section" value="Ready"`) {
		t.Error("the re-opened form lost the section of the item just added")
	}
	if strings.Contains(panel, "First of many") {
		t.Error("the re-opened form kept the previous title; it must be empty")
	}
}

// The add-another response must ACTUALLY have added the item: the toast names
// the new ID and the board carries its card.
func TestAddAnotherSavesTheItem(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {"First of many"}, "section": {"ready"},
			"addAnother": {"1"}},
		"HX-Request", "true")
	r.expectStatus(http.StatusOK)

	newID := regexp.MustCompile(`T-\d{4} added`).FindString(r.Body)
	newID = strings.TrimSuffix(newID, " added")
	if newID == "" {
		t.Fatalf("the toast does not name the added item:\n%s", r.Body)
	}
	if !strings.Contains(r.Body, `data-item-id="`+newID+`"`) {
		t.Errorf("the board in the response lacks the new item's card (%s)", newID)
	}

	// The board fragment route confirms it persists, and the fresh panel is
	// still empty for the next one.
	board := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body
	if !strings.Contains(board, `data-item-id="`+newID+`"`) {
		t.Errorf("the item was not persisted to the board:\n%s", board)
	}
}

// The new-item form's detail textarea must survive the add: the detail is
// what makes an add useful, and an add-another workflow especially so.
func TestAddCarriesDetail(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {"Detailed"}, "section": {"ready"},
			"detail": {"The durable description."}},
		"HX-Request", "true")
	r.expectStatus(http.StatusOK)

	newID := strings.TrimSuffix(regexp.MustCompile(`T-\d{4} added`).FindString(r.Body), " added")
	if newID == "" {
		t.Fatalf("the toast does not name the added item:\n%s", r.Body)
	}

	frag := ts.get("/p/"+id+"/item/"+newID, "HX-Request", "true").
		expectStatus(http.StatusOK).Body
	if !strings.Contains(frag, "The durable description.") {
		t.Errorf("the added item lost its detail:\n%s", frag)
	}
}

// T-0085: confirming the remove dialog dismisses the dialog (and any open
// panel) — the response swaps the board and clears dialog-root out of band.
func TestRemoveConfirmationDismissesDialogs(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	// The dialog is fetched before the operation runs.
	dialog := ts.get("/p/" + id + "/dialog/confirm-remove?item=T-0001").
		expectStatus(http.StatusOK).Body
	if !hasTestid(dialog, "dialog-confirm-remove") {
		t.Fatalf("the confirmation did not render:\n%s", dialog)
	}

	r := ts.form(http.MethodDelete, "/p/"+id+"/items/T-0001?force=true", nil,
		"HX-Request", "true")
	r.expectStatus(http.StatusOK)
	if !strings.Contains(r.Body, dialogDismiss) {
		t.Errorf("the removal response does not clear the dialog root:\n%s", r.Body)
	}
	if !strings.Contains(r.Body, panelDismiss) {
		t.Error("the removal response does not dismiss the panel")
	}
}

// A blocked operation leaves the dialog OPEN — its 409 response is the
// wip-limit dialog itself, and the dismissal OOBs must not be part of it.
func TestBlockedMutationKeepsTheDialog(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	// clean-full's single slot is occupied: starting T-0002 is refused with
	// the remedy dialog.
	r := ts.form(http.MethodPost, "/p/"+id+"/items/T-0002/start", nil,
		"HX-Request", "true")
	r.expectStatus(http.StatusConflict)
	if !hasTestid(r.Body, "dialog-wip-limit") {
		t.Fatalf("the refusal is not the wip-limit dialog:\n%s", r.Body)
	}
	if strings.Contains(r.Body, panelDismiss) || strings.Contains(r.Body, dialogDismiss) {
		t.Error("the refusal response dismisses the overlays; the dialog must stay for its remedies")
	}

	// The dialog travels INSIDE an OOB carrier (T-0082): htmx's OOB innerHTML
	// swap inserts the carrier's CHILDREN, so a carrier that IS the dialog
	// would leave its h2/p/ul in dialog-root with no .mm-dialog wrapper — no
	// role, no aria-modal, no focus trap. The carrier must be a plain div
	// wrapping the dialog, and the dialog itself must not carry the OOB.
	if !strings.Contains(r.Body, `<div hx-swap-oob="innerHTML:[data-testid='dialog-root']">`) {
		t.Fatalf("the wip-limit dialog has no OOB carrier:\n%s", r.Body)
	}
	if m := regexp.MustCompile(`data-testid="dialog-wip-limit"[^>]*hx-swap-oob`).FindString(r.Body); m != "" {
		t.Errorf("the OOB attribute sits ON the dialog, not on a carrier: %s", m)
	}
}

// An invalid form keeps the panel open so the user can correct it: the error
// response is a toast, never the dismissal-carrying board swap.
func TestInvalidFormKeepsThePanel(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/items",
		url.Values{"title": {""}, "section": {"ready"}}, "HX-Request", "true")
	r.expectStatus(http.StatusBadRequest)
	if strings.Contains(r.Body, panelDismiss) || strings.Contains(r.Body, dialogDismiss) {
		t.Error("the error response carries the overlay dismissal; the form must stay open")
	}
}
