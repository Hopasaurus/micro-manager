package web

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// The /about view (spec-gui.md §4.1): version info. It renders with no project
// open, like /settings, because it is a statement about the service, not about
// a project.

type aboutData struct {
	Service    string
	Version    string
	SpecUI     string
	SpecTools  string
	SpecFormat string
}

func (s *Server) about(c *echo.Context) error {
	v := s.newView(c, "About", nil)
	v.App.Nav = "about"
	v.Data = aboutData{
		Service:    "micro-manager",
		Version:    Version,
		SpecUI:     SpecUIVersion,
		SpecTools:  SpecToolsVersion,
		SpecFormat: SpecFormatVersion,
	}
	return s.render(c, http.StatusOK, "about", "content", v)
}
