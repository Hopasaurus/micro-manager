// Package api is the JSON half of the GUI service: the /api/v1 routes of
// spec-gui.md §4.2.
//
// It is a SEPARATE handler set from the HTML views on purpose
// (project/architecture.md §4.7). The API is not the UI's data source — the UI
// renders server-side — and keeping the two sets of handlers apart is what makes
// that boundary physical rather than conventional. Both call the same mm
// functions, so behavior cannot diverge; nothing in this package renders a
// template, and nothing in the view handlers writes a JSON envelope.
//
// Errors are not mapped here either. A handler returns the library error and
// the web server's single error handler (web.errorHandler) turns it into the
// §4.3 JSON envelope, because the path prefix /api/ is exactly what that
// handler keys on. One mapping table, one place, both front ends.
package api

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Service is what the API layer needs from the web server: project resolution,
// discovery, the two list files, and the merged configuration. It exists so
// that this package never reaches into internal/web's unexported state; the web
// Server implements it with thin delegating methods.
type Service interface {
	ResolveProject(id string) (*mm.Store, error)
	ProjectDirectory(id string) (mm.Directory, bool)
	DiscoveryRaw() (mm.DiscoveryResult, mm.Timestamp)
	RescanRaw() (mm.DiscoveryResult, mm.Timestamp)
	Lists() (recent, favorites *mm.ProjectList, err error)
	Config() mm.Config
	ConfigHome() string
	SystemConfig() *mm.ConfigFile
	Now() time.Time

	// SystemConfigReload re-reads the system config file into the merged view
	// and applies runtime effects (the tickler service interval, which changes
	// without a restart). The config PUT replaces the file; the running
	// service must follow it.
	SystemConfigReload()

	// Subscribe opens a per-project event stream. The returned function MUST be
	// called when the stream closes, or the project's poller runs forever.
	Subscribe(projectID string) (<-chan Event, func())

	// Done is closed when the service is told to stop. A long-lived handler
	// (the event stream) selects on it so it ends promptly instead of holding
	// its connection open through the server's graceful-shutdown window
	// (T-0151).
	Done() <-chan struct{}
}

// Config carries what the API handlers need beyond the service: nothing else is
// permitted to reach here from the web layer.
type Config struct {
	Service Service
	Logger  *slog.Logger
}

// Server is the JSON API. It is stateless apart from the service it wraps.
type Server struct {
	svc Service
	log *slog.Logger
}

// New builds the API over a service.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Server{svc: cfg.Service, log: cfg.Logger}
}

// Register adds every §4.2 API route to the group. The group is expected to be
// the /api/v1 group of the web server's echo instance.
//
// GET /api/v1/health is deliberately not here: it already exists on the web
// server, and registering it twice would make the route table lie about which
// handler answers it.
func (s *Server) Register(g *echo.Group) {
	// Discovery (spec-tools.md §6.1).
	g.GET("/projects", s.listProjects) // --find; ?refresh=true rescans
	g.POST("/scan", s.scan)            // re-run discovery
	g.GET("/scan", s.lastScan)         // last result, with scannedAt
	g.POST("/projects", s.initProject) // --init

	// A project (spec-gui.md §3.1).
	g.GET("/projects/:projectId", s.projectSummary)
	g.GET("/projects/:projectId/fingerprint", s.fingerprint)

	// Items (§4.2, spec-tools.md §5.1).
	g.GET("/projects/:projectId/items", s.listItems)             // --list
	g.POST("/projects/:projectId/items", s.addItem)              // --add
	g.GET("/projects/:projectId/items/:itemId", s.showItem)      // --show
	g.PATCH("/projects/:projectId/items/:itemId", s.editItem)    // --edit
	g.DELETE("/projects/:projectId/items/:itemId", s.removeItem) // --remove
	for _, op := range []string{"move", "start", "pause", "finish", "block", "unblock", "note"} {
		g.POST("/projects/:projectId/items/:itemId/"+op, s.operation(op))
	}

	// Detail files (spec-tools.md §5.1.4).
	g.GET("/projects/:projectId/detail/:itemId", s.getDetail)
	g.PUT("/projects/:projectId/detail/:itemId", s.putDetail)

	// Reports and validation.
	g.GET("/projects/:projectId/report", s.report) // --report
	g.GET("/projects/:projectId/check", s.check)   // --check
	g.GET("/projects/:projectId/wip", s.getWip)
	g.PUT("/projects/:projectId/wip", s.putWip)

	// Config and theme, at both scopes (spec-gui.md §8, §9).
	g.GET("/projects/:projectId/config", s.getProjectConfig)
	g.PUT("/projects/:projectId/config", s.putProjectConfig)
	g.GET("/projects/:projectId/theme", s.getProjectTheme)
	g.PUT("/projects/:projectId/theme", s.putProjectTheme)
	g.DELETE("/projects/:projectId/theme", s.deleteProjectTheme)
	g.GET("/config", s.getSystemConfig)
	g.PUT("/config", s.putSystemConfig)
	g.GET("/theme", s.getSystemTheme)
	g.PUT("/theme", s.putSystemTheme)
	g.DELETE("/theme", s.deleteSystemTheme)
	g.GET("/themes", s.themes)
	g.POST("/themes/import", s.importTheme)
	g.GET("/themes/:themeId/export", s.exportTheme)

	// The two list files (§10).
	g.GET("/recent", s.getRecent)
	g.PUT("/recent", s.putRecent)
	g.GET("/favorites", s.getFavorites)
	g.PUT("/favorites", s.putFavorites)

	// The event stream (§2.3) — OPTIONAL in §4.2 but implemented, because
	// freshness is part of the design (project/architecture.md §4.5).
	g.GET("/events", s.events)
}

// today returns the date the service believes it is, from the injectable clock
// so a test can fix it. This is the date granularity the format demands
// (spec-file-format.md §3.3): a timezone shift must not move a stored date.
func (s *Server) today() mm.Date {
	ts := mm.NewTimestamp(s.svc.Now())
	d, err := mm.ParseDate(ts.String()[:10])
	if err != nil {
		return mm.Date{}
	}
	return d
}

// dryRun reports whether a request asks for a dry run, from the JSON body or
// the query string, mirroring --dry-run (spec-tools.md §3.4).
func dryRun(c *echo.Context) bool {
	q := c.QueryParam("dryRun")
	if q == "true" {
		return true
	}
	return false
}
