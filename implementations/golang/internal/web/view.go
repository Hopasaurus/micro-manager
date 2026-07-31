package web

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// The view models the templates render from (spec-gui.md §5).
//
// Every data-* attribute in §5.1 is computed HERE, in Go, and passed to a
// template. None of it is derived in JavaScript: state the client invents is
// state the server cannot be held to, and the external suite reads these
// attributes as the truth about what the service did.

// view is what every template receives: the shell's data, plus the page's own.
type view struct {
	App  appData
	Data any
}

// appData fills the app shell of §5.2, present on every route.
type appData struct {
	Title string

	// Busy is the quiescence signal an external suite waits on (§5.1). The
	// server always renders false: a rendered response IS the view fully
	// reflecting server state. mm.js sets it true while a request is in flight.
	Busy bool

	// TestMode sets data-test-mode="true" (§4.4).
	TestMode bool

	Theme   themeData
	Project *projectData // nil when no project is open
	Status  *statusData  // nil when no project is open

	// Toast is what a mutation raised, rendered into toast-region. It lives on
	// the shell rather than on a page's data because BOTH renderings need it:
	// an htmx swap sends it out of band, and a plain form post gets a whole page
	// that must still say what happened.
	Toast *toastData

	Nav        string // which nav item is current: board | report | check | settings
	Query      string // the search box's value
	Favorites  []entryData
	Recent     []entryData
	NeedsRoots bool
}

// navLink is one entry in app-nav.
type navLink struct {
	Key     string
	Label   string
	Href    string
	Current bool
}

// projectData is the open project, as the app root's attributes need it.
type projectData struct {
	ID   string
	Name string
	Path string
}

// statusData fills app-status: status-wip, status-counts, status-check (§5.2).
type statusData struct {
	WipUsed  int
	WipLimit int
	Ready    int
	Blocked  int
	Someday  int
	Done     int

	// Violations is the count status-check carries as data-violations, so a
	// suite can assert cleanliness without opening the check view.
	Violations int
	Checked    bool
}

// entryData is one row of the recent or favorites list (§10).
type entryData struct {
	ProjectID string
	Name      string
	Path      string
	Missing   bool
	Favorite  bool
}

// themeData is the resolved theme, ready for the app root (§8.4, §8.7).
type themeData struct {
	Name       string
	Source     mm.ThemeSource
	Appearance string
	Brand      map[string]string

	// Properties and PropertiesDark are the custom properties of Appendix B.
	Properties     []mm.CSSProperty
	PropertiesDark []mm.CSSProperty
}

// Style renders the custom properties as a stylesheet for the app root.
//
// They are INLINED in the served HTML rather than fetched. That is what makes
// §8.7's "no flash of the previous theme when switching projects" true by
// construction rather than by timing: the new values are in the document that
// carries the new view.
func (t themeData) Style() template.CSS {
	var b strings.Builder
	b.WriteString(`[data-testid="app"] {`)
	for _, p := range t.Properties {
		fmt.Fprintf(&b, "%s:%s;", p.Name, cssValue(p.Value))
	}
	b.WriteString("}")

	// auto follows prefers-color-scheme, so the dark set is a media query rather
	// than something a script swaps (§8.2).
	if t.Appearance == mm.AppearanceAuto && len(t.PropertiesDark) > 0 {
		b.WriteString(`@media (prefers-color-scheme: dark){[data-testid="app"] {`)
		for _, p := range t.PropertiesDark {
			fmt.Fprintf(&b, "%s:%s;", p.Name, cssValue(p.Value))
		}
		b.WriteString("}}")
	}
	if t.Appearance == mm.AppearanceDark && len(t.PropertiesDark) > 0 {
		b.WriteString(`[data-testid="app"] {`)
		for _, p := range t.PropertiesDark {
			fmt.Fprintf(&b, "%s:%s;", p.Name, cssValue(p.Value))
		}
		b.WriteString("}")
	}
	return template.CSS(b.String())
}

// cssValue strips what could close the style element or the declaration.
//
// Theme values are user data - a theme file is editable and importable - and
// template.CSS bypasses escaping, so this is the gate. A value that tries to
// carry markup is dropped rather than sanitised into something half-meant.
func cssValue(v string) string {
	if strings.ContainsAny(v, "<>{};") {
		return ""
	}
	return v
}

// newView builds the shell for a request.
//
// A project is optional: Home, /projects and /settings render the shell with no
// project open, and the app root then carries no data-project-id.
func (s *Server) newView(c *echo.Context, title string, store *mm.Store) view {
	app := appData{
		Title:    title,
		TestMode: s.testMode(c),
		Query:    c.Request().URL.Query().Get("q"),
	}

	// An error response arrives here with no store, but the URI still names a
	// project. Resolving it keeps the project's own theme on an error page
	// (§8.7) and lets dialog-wip-limit link to that project's settings.
	if store == nil {
		if id := c.Param("projectId"); id != "" {
			if resolved, err := s.registry.resolve(id); err == nil {
				store = resolved
			}
		}
	}

	var projectDir string
	if store != nil {
		if d, err := store.Directory(); err == nil {
			app.Project = &projectData{ID: d.ProjectID, Name: d.Project, Path: d.Path}
			projectDir = d.Path
			app.Status = s.statusFor(store)
		}
	}
	app.Theme = s.themeFor(projectDir)
	app.Favorites, app.Recent = s.listsFor()
	app.NeedsRoots = s.registry.needsRoots()

	return view{App: app}
}

// testMode is MM_UI_TEST=1 or ?mm-test=1 (§4.4). It disables animations and
// auto-dismissing toasts and sets data-test-mode; it changes nothing else.
func (s *Server) testMode(c *echo.Context) bool {
	return s.opts.TestMode || c.Request().URL.Query().Get("mm-test") == "1"
}

// statusFor fills the status bar.
//
// Validation runs here because §5.2 requires status-check to carry the violation
// count on every route, so a suite can assert cleanliness without opening the
// check view. A directory small enough to be a todo list is cheap to validate.
func (s *Server) statusFor(store *mm.Store) *statusData {
	st, err := store.Status()
	if err != nil {
		return nil
	}
	out := &statusData{
		WipUsed:  st.WipUsed(),
		WipLimit: st.WipLimit(),
		Ready:    st.Ready,
		Blocked:  st.Blocked,
		Someday:  st.Someday,
		Done:     st.Done,
	}
	if vs, err := store.Validate(); err == nil {
		out.Violations = len(vs)
		out.Checked = true
	}
	return out
}

// themeFor resolves the theme for a project, or the system theme when none is
// open (§8.7).
//
// Resolution reads at most two small JSON files and is not cached: a theme edit
// must apply live to the current view (§8.7), and a cache would be one more
// thing to invalidate for no measurable gain on a local tool.
func (s *Server) themeFor(projectDir string) themeData {
	req := mm.ThemeRequest{
		ProjectDir:    projectDir,
		ConfigHome:    s.opts.ConfigHome,
		SystemThemeID: s.opts.Config.Theme.ID,
	}
	if projectDir != "" {
		if f, err := mm.LoadConfigFile(mm.ProjectConfigPath(projectDir), mm.ScopeProject); err == nil {
			cfg, _ := mm.MergeConfig(nil, f)
			req.ProjectThemeID = cfg.Theme.ID
		}
	}

	resolved, warnings, err := mm.ResolveTheme(req)
	if err != nil {
		s.log.Warn("theme", "error", err)
	}
	for _, w := range warnings {
		s.log.Warn("theme", "file", w.File, "token", w.Key, "message", w.Message)
	}

	name := resolved.Name
	if brand := resolved.Brand["name"]; brand != "" {
		// §8.6: brand.name defaults to the project's own name when absent, and
		// the two are separate on purpose - project is data, branding is
		// presentation.
		name = resolved.Name
	}
	return themeData{
		Name:           name,
		Source:         resolved.Source,
		Appearance:     resolved.Appearance,
		Brand:          resolved.Brand,
		Properties:     resolved.Properties(false),
		PropertiesDark: resolved.Properties(true),
	}
}

// listsFor loads the two lists for the project switcher, trimmed to the
// configured display counts (§9.2, §10 rules 2 and 3).
func (s *Server) listsFor() (favorites, recent []entryData) {
	recentList, favoritesList, err := s.registry.lists()
	if err != nil {
		s.log.Warn("lists", "error", err)
		return nil, nil
	}

	favIDs := map[string]bool{}
	for _, e := range favoritesList.Entries {
		favIDs[e.ProjectID] = true
	}
	for _, e := range favoritesList.Display(s.opts.Config.UI.FavoritesCount) {
		favorites = append(favorites, entryData{
			ProjectID: e.ProjectID, Name: e.Display(), Path: e.Path,
			Missing: e.Missing(), Favorite: true,
		})
	}
	for _, e := range recentList.Display(s.opts.Config.UI.RecentCount) {
		recent = append(recent, entryData{
			ProjectID: e.ProjectID, Name: e.Display(), Path: e.Path,
			Missing: e.Missing(), Favorite: favIDs[e.ProjectID],
		})
	}
	return favorites, recent
}

// notFound renders the not-found view with 404.
//
// §4.1 rule 4 is explicit: an unknown projectId MUST render the not-found view
// with HTTP 404, NEVER a redirect to /. A redirect would tell a bookmark that
// the project moved rather than that it is not here.
func (s *Server) notFound(c *echo.Context, message string) error {
	v := s.newView(c, "Not found", nil)
	v.Data = map[string]any{"Message": message, "Code": codeNotFound}
	return s.render(c, http.StatusNotFound, "error", "error", v)
}
