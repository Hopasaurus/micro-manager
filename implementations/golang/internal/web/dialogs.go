package web

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
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
	if _, err := mm.ParseID(itemID); err != nil {
		return err
	}
	it, err := store.Get(mm.ID(itemID))
	if err != nil {
		return err
	}

	v := s.newView(c, "", store)
	v.Data = map[string]any{
		"ItemID":    itemID,
		"IsWorking": it.State == mm.StateWorking,
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
