package api

import (
	"github.com/labstack/echo/v5"
)

// Detail files (spec-tools.md §5.1.4, spec-gui.md §4.2).

// getDetail serves GET /api/v1/projects/:projectId/detail/:itemId.
func (s *Server) getDetail(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := itemID(c)
	if err != nil {
		return err
	}
	d, err := store.Detail(id)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{
		"path":  d.Path,
		"id":    string(d.ID),
		"title": d.Title,
		"body":  d.Body,
	})
}

// putDetailRequest is the body of PUT /api/v1/projects/:projectId/detail/:itemId.
type putDetailRequest struct {
	Body   string `json:"body"`
	DryRun bool   `json:"dryRun"`
}

// putDetail serves PUT /api/v1/projects/:projectId/detail/:itemId: replace the
// detail file's body. The id and title frontmatter are NOT body fields — they
// must match the item, and the library maintains that (I9).
func (s *Server) putDetail(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := itemID(c)
	if err != nil {
		return err
	}
	var req putDetailRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	res, err := store.SetDetailBody(id, req.Body, req.DryRun || dryRun(c), s.today())
	if err != nil {
		return err
	}
	return ok(c, map[string]any{
		"changes": toJSONChanges(res),
		"dryRun":  res.DryRun,
	})
}
