package api

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The recent and favorites lists (spec-gui.md §10).
//
// GET·PUT pairs over the whole file: a script reads the list, modifies it, and
// writes it back. Both files are written atomically and tolerate concurrent
// writers because the library's WriteProjectList is the same write path the UI
// uses.

// listPathFor is the file of one list kind.
func (s *Server) listPathFor(kind mm.ListKind) (string, error) {
	home := s.svc.ConfigHome()
	if home == "" {
		return "", fmt.Errorf("%w: no configuration home directory", mm.ErrIO)
	}
	paths := mm.NewSystemPaths(home)
	switch kind {
	case mm.ListFavorites:
		return paths.Favorites, nil
	default:
		return paths.Recent, nil
	}
}

// getRecent serves GET /api/v1/recent.
func (s *Server) getRecent(c *echo.Context) error {
	return s.getList(c, mm.ListRecent)
}

// getFavorites serves GET /api/v1/favorites.
func (s *Server) getFavorites(c *echo.Context) error {
	return s.getList(c, mm.ListFavorites)
}

// getList reads one list file. A missing file is an empty list, not an error:
// neither list exists until something is opened (§10).
func (s *Server) getList(c *echo.Context, kind mm.ListKind) error {
	path, err := s.listPathFor(kind)
	if err != nil {
		return err
	}
	l, err := mm.LoadProjectList(path, kind)
	if err != nil {
		return err
	}
	doc, err := listDocument(l)
	if err != nil {
		return err
	}
	return ok(c, doc)
}

// putRecent serves PUT /api/v1/recent.
func (s *Server) putRecent(c *echo.Context) error {
	return s.putList(c, mm.ListRecent)
}

// putFavorites serves PUT /api/v1/favorites.
func (s *Server) putFavorites(c *echo.Context) error {
	return s.putList(c, mm.ListFavorites)
}

// putList writes a whole list document, atomically. The document is parsed
// first so a client cannot write a file the service could not load; entries
// without a path are dropped by the parse, and unknown keys survive (§9.4).
func (s *Server) putList(c *echo.Context, kind mm.ListKind) error {
	path, err := s.listPathFor(kind)
	if err != nil {
		return err
	}
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return err
	}
	if err := mm.WriteProjectList(path, kind, body); err != nil {
		return err
	}
	l, err := mm.LoadProjectList(path, kind)
	if err != nil {
		return err
	}
	doc, err := listDocument(l)
	if err != nil {
		return err
	}
	return ok(c, doc)
}

// listDocument renders a list file back to an object for the response.
func listDocument(l *mm.ProjectList) (rawDocument, error) {
	data, err := l.Bytes()
	if err != nil {
		return nil, err
	}
	var doc rawDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = rawDocument{}
	}
	return doc, nil
}
