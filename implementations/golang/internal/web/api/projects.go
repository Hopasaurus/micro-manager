package api

import (
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// Projects and discovery (spec-tools.md §6.1, spec-gui.md §5.3).

// jsonDiscovery is the shared shape of GET /projects and GET·POST /scan:
// what discovery found, when it ran, and whether it was truncated. The
// truncation flag is on the object because a truncated scan is
// indistinguishable from a missing project without it.
type jsonDiscovery struct {
	Directories []jsonDirectory `json:"directories"`
	Partial     bool            `json:"partial"`
	Skipped     int             `json:"skipped"`
	ScannedAt   string          `json:"scannedAt"`
}

func toJSONDiscovery(result mm.DiscoveryResult, scannedAt mm.Timestamp) jsonDiscovery {
	dirs := make([]jsonDirectory, 0, len(result.Directories))
	for _, d := range result.Directories {
		dirs = append(dirs, toJSONDirectory(d))
	}
	return jsonDiscovery{
		Directories: dirs,
		Partial:     result.Partial,
		Skipped:     result.Skipped,
		ScannedAt:   scannedAt.String(),
	}
}

// listProjects serves GET /api/v1/projects (--find). ?refresh=true forces a
// rescan; otherwise the cached walk is returned.
func (s *Server) listProjects(c *echo.Context) error {
	var result mm.DiscoveryResult
	var scannedAt mm.Timestamp
	if c.QueryParam("refresh") == "true" {
		result, scannedAt = s.svc.RescanRaw()
	} else {
		result, scannedAt = s.svc.DiscoveryRaw()
	}
	return ok(c, toJSONDiscovery(result, scannedAt))
}

// scan serves POST /api/v1/scan: re-run discovery and return what it found.
func (s *Server) scan(c *echo.Context) error {
	result, scannedAt := s.svc.RescanRaw()
	return ok(c, toJSONDiscovery(result, scannedAt))
}

// lastScan serves GET /api/v1/scan: the last DiscoveryResult, including
// scannedAt and partial. The first request of the process runs the walk.
func (s *Server) lastScan(c *echo.Context) error {
	result, scannedAt := s.svc.DiscoveryRaw()
	return ok(c, toJSONDiscovery(result, scannedAt))
}

// initRequest is the body of POST /api/v1/projects (--init).
type initRequest struct {
	Path        string `json:"path"`
	Project     string `json:"project"`
	Wip         int    `json:"wip"`
	SlotWidth   int    `json:"slotWidth"`
	NoStructure bool   `json:"noStructure"`
	DryRun      bool   `json:"dryRun"`
}

// initProject serves POST /api/v1/projects. The path is required — the service
// never guesses where a new directory should go.
func (s *Server) initProject(c *echo.Context) error {
	var req initRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Path == "" {
		return mm.ErrInvalidArgument
	}

	store, res, err := mm.Init(req.Path, mm.InitRequest{
		Project:     req.Project,
		Wip:         req.Wip,
		SlotWidth:   req.SlotWidth,
		NoStructure: req.NoStructure,
		DryRun:      req.DryRun,
	}, s.today())
	if err != nil {
		return err
	}

	out := map[string]any{
		"changes": toJSONChanges(res),
		"dryRun":  req.DryRun,
	}
	if store != nil {
		d, err := store.Directory()
		if err != nil {
			return err
		}
		out["directory"] = toJSONDirectory(d)
	}
	if req.DryRun {
		return ok(c, out)
	}
	return created(c, out)
}

// projectSummary serves GET /api/v1/projects/:projectId.
func (s *Server) projectSummary(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}

	st, err := store.Status()
	if err != nil {
		return err
	}
	out := toJSONDirectory(d)
	out.Ready = st.Ready
	out.Blocked = st.Blocked
	out.Someday = st.Someday
	out.Done = st.Done
	return ok(c, out)
}

// fingerprint serves GET /api/v1/projects/:projectId/fingerprint (§2.3).
//
// A client polls this at a bounded interval and re-reads when it changes; it is
// the polling-only freshness path, which the spec requires to work even when no
// event stream is running.
func (s *Server) fingerprint(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	fp, err := store.Fingerprint()
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"fingerprint": fp.String()})
}

// listItems serves GET /api/v1/projects/:projectId/items (--list).
//
// The query parameters mirror the board's: state, section, prio, tag, q
// (spec-gui.md §4.1). A free-text query goes through the library's search so
// the API, the board and the CLI's --search all agree about what matches.
func (s *Server) listItems(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}

	q := c.QueryParam("q")
	if q != "" {
		hits, err := store.Search(mm.SearchRequest{Query: q})
		if err != nil {
			return err
		}
		return ok(c, map[string]any{"items": toJSONItems(mm.SearchItems(hits))})
	}

	state := mm.State(c.QueryParam("state"))
	if state == "" {
		state = mm.StateAll
	}
	filter := mm.Filter{State: state}
	if section, ok := parseSection(c.QueryParam("section")); ok {
		filter.Section = section
	}
	if prio, err := mm.ParsePrio(c.QueryParam("prio")); err == nil && c.QueryParam("prio") != "" {
		filter.Prio = prio
	}
	if tag := c.QueryParam("tag"); tag != "" {
		filter.Tag = tag
	}

	items, err := store.List(filter)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"items": toJSONItems(items)})
}

// parseSection is the lowercase query-parameter form of §5.1 ("ready") mapped
// to the library's heading form ("Ready").
func parseSection(key string) (mm.Section, bool) {
	for _, s := range mm.Sections() {
		if strings.EqualFold(string(s), key) {
			return s, true
		}
	}
	return "", false
}
