package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Dialogs (spec-gui.md §5.10).
//
// Two of them exist because §7.2 requires an operation to PROMPT before it runs:
// blocking needs a reason, finishing needs an outcome. Cancelling either aborts
// without issuing a request, which is why the prompt is a dialog the client
// fetches rather than a field it sends optimistically.

// dialog serves /p/:projectId/dialog/:name.
func (s *Server) dialog(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	name := c.Param("name")
	fragment := "dialog-" + name
	switch name {
	case "confirm-remove", "block", "finish":
	default:
		return fmt.Errorf("%w: no dialog %q", mm.ErrNotFound, name)
	}

	itemID := c.Request().URL.Query().Get("item")
	if _, err := parseID(itemID, store); err != nil {
		return err
	}
	it, err := store.Get(mm.ID(itemID))
	if err != nil {
		return err
	}

	v := s.newView(c, "", store)
	v.Data = map[string]any{
		"ItemID": itemID,
		// confirm-remove offers to delete the detail file along with the item,
		// so it has to know whether there is one to offer (spec-tools.md
		// §5.1.6). Empty for an item with no detail file, and the dialog then
		// says nothing about detail files at all.
		"Detail": it.Detail,
		// dialog-block (T-0250): the reason input is pre-populated from the
		// item's existing reason, if it has one - blank for a fresh item, so
		// the placeholder/required behavior is unaffected either way.
		"Reason": it.Reason,
		// Position (T-0250): the drop index a drag computed, carried through
		// as a hidden field so the dialog's own submission lands the item
		// where it was actually dropped instead of Move's own default
		// (append at the end). Absent when the dialog was opened from the
		// item menu rather than a drag, in which case that default is
		// exactly right - there is no drop position to honour.
		"Position": c.Request().URL.Query().Get("position"),
	}

	// A dialog is always a fragment: it is swapped into dialog-root over
	// whatever view is already there.
	return s.renderFragmentAlways(c, http.StatusOK, "item", fragment, v)
}

// renderFragmentAlways renders a fragment whether or not the request came from
// htmx, for responses that have no whole-page form - a dialog is not a page.
func (s *Server) renderFragmentAlways(c *echo.Context, status int, page, fragment string, data any) error {
	var buf bytes.Buffer
	if err := s.renderer.renderFragment(&buf, page, fragment, data); err != nil {
		return err
	}
	return c.HTMLBlob(status, buf.Bytes())
}
