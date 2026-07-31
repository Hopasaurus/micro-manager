package web

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// The route table (spec-gui.md §4).
//
// Routes are contract. An implementation MUST serve exactly the paths of §4,
// MUST NOT require a different prefix, and MUST NOT add a route that shadows one
// of them. This file is the single place that is true or false, which is what
// lets the audit test of T-0070 check it against Appendix C.
//
// Only the routes that have handlers are registered. A route registered with a
// "not implemented" handler would be indistinguishable, to a conformance suite,
// from one that is implemented and broken.
func (s *Server) routes() {
	api := s.echo.Group("/api/v1")
	api.GET("/health", s.health)

	// View routes (§4.1). /p/:projectId redirects to the board with a 302.
	s.echo.GET("/p/:projectId", s.boardRedirect)
	s.echo.GET("/p/:projectId/board", s.board)
	s.echo.GET("/p/:projectId/item/:itemId", s.itemPanel)
	s.echo.GET("/p/:projectId/new", s.newItemPanel)
	s.echo.GET("/p/:projectId/dialog/:name", s.dialog)

	// The mutating operations of §6.1. Each is one library call; the response is
	// the board plus whatever the change invalidated.
	s.echo.POST("/p/:projectId/items", s.addItem)
	s.echo.PATCH("/p/:projectId/items/:itemId", s.editItem)
	s.echo.DELETE("/p/:projectId/items/:itemId", s.removeItem)
	for _, op := range []string{"start", "pause", "finish", "block", "unblock", "move", "note"} {
		s.echo.POST("/p/:projectId/items/:itemId/"+op, s.operation(op))
	}

	s.echo.StaticFS("/static", staticFS())
}

// operation adapts one operation name to a handler, so the seven routes above
// are seven registrations of one code path rather than seven near-copies.
func (s *Server) operation(op string) echo.HandlerFunc {
	return func(c *echo.Context) error { return s.operate(c, op) }
}

// healthResponse is deliberately small: §4.2 calls this liveness, with no auth,
// and anything more would be a second place the version can disagree with
// /about.
type healthResponse struct {
	OK      bool   `json:"ok"`
	Service string `json:"service"`
	Version string `json:"version"`
}

func (s *Server) health(c *echo.Context) error {
	return c.JSON(http.StatusOK, healthResponse{
		OK:      true,
		Service: "micro-manager",
		Version: Version,
	})
}
