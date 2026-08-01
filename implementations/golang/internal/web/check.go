package web

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// The check view (spec-gui.md §5.8).
//
// It runs Store.Validate and nothing else - the same call every mutation runs
// before it commits, so the view and the tool's own guard cannot disagree
// about what is wrong. (The CLI's --check also surfaces non-fatal warnings
// through ValidateWithWarnings; surfacing them in the GUI is the T-0117
// grammar work.)

// checkData is the check view model.
type checkData struct {
	Violations []violationData
	Count      int
	Path       string
}

// violationData is one finding, in the shape §5.8 renders.
type violationData struct {
	N         int
	Invariant string
	File      string
	Line      int
	Message   string
}

// check serves /p/:projectId/check.
func (s *Server) check(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Check", store)
	v.App.Nav = "check"

	data, err := s.buildCheck(store)
	if err != nil {
		return err
	}
	v.Data = data

	return s.render(c, http.StatusOK, "check", "check", v)
}

// buildCheck validates the directory.
//
// A directory with violations is still a 200: the findings ARE the answer, not
// an error (spec-tools.md §5.1.12). An error here would mean the directory could
// not be read at all.
func (s *Server) buildCheck(store *mm.Store) (checkData, error) {
	violations, err := store.Validate()
	if err != nil {
		return checkData{}, err
	}

	data := checkData{Count: len(violations), Path: store.Path()}
	for i, v := range violations {
		data.Violations = append(data.Violations, violationData{
			N:         i,
			Invariant: v.Invariant,
			File:      v.At.File,
			Line:      v.At.Line,
			Message:   v.Message,
		})
	}
	return data, nil
}
