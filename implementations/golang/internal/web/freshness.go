package web

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// The SSE-driven refresh routes (project/architecture.md §4.5).
//
// Events are notifications, not content: when one arrives, the client
// re-fetches the affected region over its NORMAL route. These two routes are
// those fetches. They are internal — not in the spec's route table, and not
// addressable views — so they always render the fragment, exactly as a dialog
// does.

// status serves GET /p/:projectId/status: the status bar alone, for the
// sse:status trigger and the every-30s backstop on the footer.
//
// The fragment is the SAME app-status template the layout renders in-page, so
// the swapped-in footer cannot differ from the first-render one (§4.3).
func (s *Server) status(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Status", store)
	return s.renderFragmentAlways(c, http.StatusOK, "board", "app-status", v)
}

// shell serves GET /p/:projectId/shell: the whole app element, for the
// sse:theme trigger.
//
// The theme's resolved tokens are inlined INSIDE the app element (§8.7), so a
// shell swap is what re-themes every open tab when a theme file changes — no
// fetch of a stylesheet, no flash of the previous theme. The fragment is the
// same app template the layout renders, so it cannot drift.
func (s *Server) shell(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Board", store)
	v.App.Nav = "board"
	data, err := s.buildBoard(c, store)
	if err != nil {
		return err
	}
	v.Data = data
	return s.renderFragmentAlways(c, http.StatusOK, "board", "app", v)
}
