package web

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Home and the projects list (spec-gui.md §5.3, §5.4, §10).
//
// These are the two views that make the service navigable: everything else is
// reached from a project, and until now a project could only be reached by
// typing its id into the URL.

// homeData is the Home view model.
type homeData struct {
	Favorites  []projectCard
	Recent     []projectCard
	NeedsRoots bool
	StartDir   string
}

// projectsData is the /projects view model.
type projectsData struct {
	Groups    []projectGroup
	Count     int
	Partial   bool
	Skipped   int
	ScannedAt string
	Roots     []rootStatus
}

type projectGroup struct {
	N       int
	Root    string
	Missing bool
	Cards   []projectCard
}

// projectCard is §5.4's card, used by Home and by /projects alike - one shape,
// one template, wherever a project is listed.
type projectCard struct {
	ProjectID string
	Name      string
	Path      string
	WipUsed   int
	WipLimit  int
	Missing   bool
	Favorite  bool
}

// home serves /.
func (s *Server) home(c *echo.Context) error {
	v := s.newView(c, "micro-manager", nil)

	recent, favorites, err := s.registry.lists()
	if err != nil {
		return err
	}

	data := homeData{
		NeedsRoots: s.registry.needsRoots(),
		StartDir:   s.opts.StartDir,
	}
	favIDs := map[string]bool{}
	for _, e := range favorites.Entries {
		favIDs[e.ProjectID] = true
	}
	for _, e := range favorites.Display(s.opts.Config.UI.FavoritesCount) {
		data.Favorites = append(data.Favorites, s.cardFor(e, true))
	}
	for _, e := range recent.Display(s.opts.Config.UI.RecentCount) {
		data.Recent = append(data.Recent, s.cardFor(e, favIDs[e.ProjectID]))
	}

	v.Data = data
	return s.render(c, http.StatusOK, "home", "home", v)
}

// projects serves /projects: everything discovery finds, grouped by scan root.
func (s *Server) projects(c *echo.Context) error {
	v := s.newView(c, "Projects", nil)

	view := s.registry.discovery()
	if c.Request().URL.Query().Get("refresh") == "true" {
		view = s.registry.rescan()
	}

	_, favorites, err := s.registry.lists()
	if err != nil {
		return err
	}
	favIDs := map[string]bool{}
	for _, e := range favorites.Entries {
		favIDs[e.ProjectID] = true
	}

	data := projectsData{
		Partial:   view.Partial,
		Skipped:   view.Skipped,
		ScannedAt: view.ScannedAt.String(),
		Roots:     view.Roots,
	}
	for i, g := range view.Groups {
		group := projectGroup{N: i, Root: g.Root, Missing: g.Missing}
		for _, d := range g.Directories {
			group.Cards = append(group.Cards, projectCard{
				ProjectID: d.ProjectID,
				Name:      directoryName(d),
				Path:      d.Path,
				WipUsed:   d.WipUsed,
				WipLimit:  d.WipLimit,
				Missing:   !isDir(d.Path),
				Favorite:  favIDs[d.ProjectID],
			})
			data.Count++
		}
		data.Groups = append(data.Groups, group)
	}

	v.Data = data
	return s.render(c, http.StatusOK, "projects", "projects", v)
}

// rescan forces a re-walk. §9.5 requires this on demand, because the
// fingerprint poll detects change WITHIN a project and never a new one
// appearing.
func (s *Server) rescan(c *echo.Context) error {
	s.registry.rescan()
	return s.projects(c)
}

// favoriteToggle is project-card-favorite-toggle (§10).
//
// Adding and removing are one route because the control is one button whose
// aria-pressed says which it will do; splitting them would let a client and the
// server disagree about the current state.
func (s *Server) favoriteToggle(c *echo.Context) error {
	id := c.Param("projectId")
	d, known := s.registry.directory(id)
	if !known {
		return fmt.Errorf("%w: no project %s", mm.ErrNotFound, id)
	}
	if s.opts.ConfigHome == "" {
		return fmt.Errorf("%w: no configuration directory to write favorites to", mm.ErrIO)
	}

	paths := mm.NewSystemPaths(s.opts.ConfigHome)
	err := mm.UpdateProjectList(paths.Favorites, mm.ListFavorites, func(l *mm.ProjectList) error {
		if _, ok := l.Find(id); ok {
			l.Remove(id)
			return nil
		}
		l.Add(mm.EntryFor(d, ""))
		return nil
	})
	if err != nil {
		return err
	}

	// Re-render whichever view asked, so the toggle and the lists stay in step.
	if strings.Contains(c.Request().Header.Get("HX-Current-URL"), "/projects") {
		return s.projects(c)
	}
	return s.home(c)
}

// cardFor builds a card from a list entry.
//
// The WIP counts come from the directory when it is open; an entry on an
// unmounted drive keeps its name and path and reports missing, because §10 rule
// 5 renders it rather than dropping it.
func (s *Server) cardFor(e mm.ListEntry, favorite bool) projectCard {
	card := projectCard{
		ProjectID: e.ProjectID,
		Name:      e.Display(),
		Path:      e.Path,
		Missing:   e.Missing(),
		Favorite:  favorite,
	}
	if d, known := s.registry.directory(e.ProjectID); known {
		card.WipUsed, card.WipLimit = d.WipUsed, d.WipLimit
	}
	return card
}

// directoryName is the project's own name, falling back to the directory it
// sits in. A card the user cannot identify is worse than one with an ugly name.
func directoryName(d mm.Directory) string {
	if d.Project != "" {
		return d.Project
	}
	if d.Path != "" {
		return filepath.Base(filepath.Dir(d.Path))
	}
	return d.ProjectID
}
