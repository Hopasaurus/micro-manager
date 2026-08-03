package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// Config and theme files (spec-gui.md §8, §9), at both scopes.
//
// These are the shared files two front ends rewrite, so the round-trip rule of
// §9.4 governs every write here: unknown keys survive because they were never
// decoded away — the document written is the document read, with the caller's
// changes applied to it. The library's ConfigFile and Theme are built exactly
// for that, and these handlers are thin translates over them.

// rawDocument is a parsed JSON document as an object, the shape both GET
// endpoints return and both PUT endpoints accept.
type rawDocument map[string]any

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

// configPathFor is the config file of a scope.
func (s *Server) configPathFor(scope mm.ConfigScope) (string, error) {
	home := s.svc.ConfigHome()
	if home == "" {
		return "", fmt.Errorf("%w: no configuration home directory", mm.ErrIO)
	}
	paths := mm.NewSystemPaths(home)
	if scope == mm.ScopeProject {
		return "", fmt.Errorf("%w: a project config needs a project", mm.ErrInvalidArgument)
	}
	return paths.Config, nil
}

// getSystemConfig serves GET /api/v1/config. A missing file is the empty
// object, per §9.1.
func (s *Server) getSystemConfig(c *echo.Context) error {
	path, err := s.configPathFor(mm.ScopeSystem)
	if err != nil {
		return err
	}
	f, err := mm.LoadConfigFile(path, mm.ScopeSystem)
	if err != nil {
		return err
	}
	doc, err := documentOf(f)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"config": doc})
}

// putSystemConfig serves PUT /api/v1/config: replace the system config file.
func (s *Server) putSystemConfig(c *echo.Context) error {
	return s.putConfig(c, mm.ScopeSystem)
}

// getProjectConfig serves GET /api/v1/projects/:projectId/config.
func (s *Server) getProjectConfig(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}
	f, err := mm.LoadConfigFile(mm.ProjectConfigPath(d.Path), mm.ScopeProject)
	if err != nil {
		return err
	}
	doc, err := documentOf(f)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"config": doc})
}

// putProjectConfig serves PUT /api/v1/projects/:projectId/config.
func (s *Server) putProjectConfig(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}
	return s.putConfigAt(c, mm.ProjectConfigPath(d.Path), mm.ScopeProject)
}

// putConfig handles a config PUT at the system path.
func (s *Server) putConfig(c *echo.Context, scope mm.ConfigScope) error {
	path, err := s.configPathFor(scope)
	if err != nil {
		return err
	}
	return s.putConfigAt(c, path, scope)
}

// putConfigAt writes a config document.
//
// System-scoped keys in a project file are refused; the whole document is
// parsed into a ConfigFile first so a bad key surfaces before anything is
// written. dryRun computes and reports without writing.
func (s *Server) putConfigAt(c *echo.Context, path string, scope mm.ConfigScope) error {
	doc, dry, err := s.documentBody(c)
	if err != nil {
		return err
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	f, err := mm.ParseConfigFile(path, scope, data)
	if err != nil {
		return err
	}
	if warnings := configWarnings(f, scope); len(warnings) > 0 {
		return fmt.Errorf("%w: %s", mm.ErrInvalidArgument, warnings[0].String())
	}
	if err := f.Save(dry); err != nil {
		return err
	}
	out, err := documentOf(f)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"config": out, "dryRun": dry})
}

// configWarnings surfaces the warnings a merged view would report, as a refusal
// on write: a project config that sets a system-scoped key is wrong even when
// the merge would ignore it (§9.3).
func configWarnings(f *mm.ConfigFile, scope mm.ConfigScope) []mm.ConfigWarning {
	var out []mm.ConfigWarning
	if scope == mm.ScopeProject {
		for _, key := range systemOnlyKeys() {
			if f.Get(key) != nil {
				out = append(out, mm.ConfigWarning{File: f.Path, Key: key,
					Message: "system-scoped key in a project config is ignored; refusing to write it"})
			}
		}
	}
	return out
}

func systemOnlyKeys() []string {
	return []string{
		"ui.recentCount", "ui.favoritesCount", "ui.recentMaxStored",
		"server", "server.bind", "server.port", "server.allowRemote", "server.socket",
	}
}

// documentOf renders a ConfigFile back to an object for the response.
func documentOf(f *mm.ConfigFile) (rawDocument, error) {
	data, err := f.Bytes()
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

// documentBody reads a request body once and interprets it as a document. The
// body may be the bare document ({ "theme": {...} }) or the wrapped form
// ({ "config": { ... }, "dryRun": true }); the wrapped form is the
// documented API and the bare form is forgiven, because a PUT that says "this
// file is now this" reads most naturally with the file as the whole body.
func (s *Server) documentBody(c *echo.Context) (rawDocument, bool, error) {
	body, err := readBody(c)
	if err != nil {
		return nil, false, err
	}
	var wrapped struct {
		Config rawDocument `json:"config"`
		Theme  rawDocument `json:"theme"`
		DryRun bool        `json:"dryRun"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &wrapped); err != nil {
			return nil, false, err
		}
	}
	doc := wrapped.Config
	if doc == nil {
		doc = wrapped.Theme
	}
	if doc == nil {
		var bare rawDocument
		if len(body) > 0 {
			if err := json.Unmarshal(body, &bare); err != nil {
				return nil, false, err
			}
		}
		doc = bare
	}
	if doc == nil {
		doc = rawDocument{}
	}
	return doc, wrapped.DryRun || dryRun(c), nil
}

// readBody drains the request body once; a body read twice is a body read
// zero times.
func readBody(c *echo.Context) ([]byte, error) {
	return io.ReadAll(c.Request().Body)
}

// ---------------------------------------------------------------------------
// Theme
// ---------------------------------------------------------------------------

// getSystemTheme serves GET /api/v1/theme.
func (s *Server) getSystemTheme(c *echo.Context) error {
	path, err := s.themePathFor(mm.ScopeSystem)
	if err != nil {
		return err
	}
	return s.getThemeFile(c, path)
}

// getProjectTheme serves GET /api/v1/projects/:projectId/theme.
func (s *Server) getProjectTheme(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}
	return s.getThemeFile(c, mm.ProjectThemePath(d.Path))
}

// getThemeFile reads one theme file. Absence is NotFound: there is nothing to
// read, and the client branches on the code rather than on an empty object.
func (s *Server) getThemeFile(c *echo.Context, path string) error {
	t, err := mm.LoadTheme(path)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("%w: no theme file at %s", mm.ErrNotFound, path)
	}
	doc, err := themeDocument(t)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"theme": doc})
}

// putSystemTheme serves PUT /api/v1/theme.
func (s *Server) putSystemTheme(c *echo.Context) error {
	path, err := s.themePathFor(mm.ScopeSystem)
	if err != nil {
		return err
	}
	return s.putThemeFile(c, path)
}

// putProjectTheme serves PUT /api/v1/projects/:projectId/theme.
func (s *Server) putProjectTheme(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}
	return s.putThemeFile(c, mm.ProjectThemePath(d.Path))
}

// deleteSystemTheme serves DELETE /api/v1/theme.
func (s *Server) deleteSystemTheme(c *echo.Context) error {
	path, err := s.themePathFor(mm.ScopeSystem)
	if err != nil {
		return err
	}
	return s.deleteThemeFile(c, path)
}

// deleteProjectTheme serves DELETE /api/v1/projects/:projectId/theme.
func (s *Server) deleteProjectTheme(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	d, err := store.Directory()
	if err != nil {
		return err
	}
	return s.deleteThemeFile(c, mm.ProjectThemePath(d.Path))
}

// themePathFor is the theme file of a scope.
func (s *Server) themePathFor(scope mm.ConfigScope) (string, error) {
	home := s.svc.ConfigHome()
	if home == "" {
		return "", fmt.Errorf("%w: no configuration home directory", mm.ErrIO)
	}
	paths := mm.NewSystemPaths(home)
	return paths.Theme, nil
}

// putThemeFile writes a theme document at path.
//
// §8.8 rule 2: every colour value is validated, and a bad token refuses the
// whole write — a rejected theme changes nothing. The theme is parsed from the
// request body, so a schemaVersion this implementation does not implement is
// rejected by name (rule 1), and unknown keys survive (rule 4).
func (s *Server) putThemeFile(c *echo.Context, path string) error {
	doc, dry, err := s.documentBody(c)
	if err != nil {
		return err
	}
	if len(doc) == 0 {
		return fmt.Errorf("%w: a theme document is required", mm.ErrInvalidArgument)
	}

	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	t, err := mm.ParseTheme(path, data)
	if err != nil {
		return err
	}
	if warnings := t.Validate(); len(warnings) > 0 {
		return fmt.Errorf("%w: %s", mm.ErrInvalidArgument, warnings[0].String())
	}
	if err := t.Save(path, dry); err != nil {
		return err
	}
	out, err := themeDocument(t)
	if err != nil {
		return err
	}
	return ok(c, map[string]any{"theme": out, "dryRun": dry})
}

// deleteThemeFile removes a theme file. Deleting what is not there is not an
// error — a script and a concurrent edit may race, and the outcome is the same.
//
// dryRun reports what the call would remove and removes nothing. It is not
// optional politeness: §4.2 requires EVERY mutating endpoint to accept it, and
// a delete is the one where getting it wrong is unrecoverable — a client asking
// to preview the change would otherwise lose the file to the preview.
func (s *Server) deleteThemeFile(c *echo.Context, path string) error {
	dry := dryRun(c)
	deleted := isFile(path)

	if !dry {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w: %s: %v", mm.ErrIO, path, err)
		}
	}
	return ok(c, map[string]any{
		"path":    path,
		"deleted": deleted,
		"dryRun":  dry,
	})
}

// themeDocument renders a Theme back to an object for the response.
func themeDocument(t *mm.Theme) (rawDocument, error) {
	data, err := t.Bytes()
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

// ---------------------------------------------------------------------------
// Theme library, import and export (§8.8)
// ---------------------------------------------------------------------------

// jsonThemeSummary is one entry of the theme library.
type jsonThemeSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Author     string `json:"author"`
	Appearance string `json:"appearance"`
	Source     string `json:"source"` // "builtin" | "system"
	Path       string `json:"path,omitempty"`
}

// themes serves GET /api/v1/themes: the built-ins plus everything in the theme
// library directory.
func (s *Server) themes(c *echo.Context) error {
	// Described FROM the themes rather than beside them, so the listing cannot
	// claim an id, name or appearance the exported document does not carry.
	// The built-in registry is the single source: the default plus the lite
	// theme (T-0137). "sample-one-dark" is NOT offered — it is the spec's
	// EXAMPLE theme document (§8.6), not a theme this library has, and asking
	// for it served the built-in under a name nothing defines.
	out := []jsonThemeSummary{}
	for _, bt := range mm.BuiltinThemes() {
		out = append(out, jsonThemeSummary{
			ID: bt.ID, Name: bt.Name, Author: "Builtin", Appearance: bt.Appearance, Source: "builtin",
		})
	}
	if home := s.svc.ConfigHome(); home != "" {
		sp := mm.NewSystemPaths(home)
		if entries, err := os.ReadDir(sp.Themes); err == nil {
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				id := strings.TrimSuffix(e.Name(), ".json")
				if mm.BuiltinLibraryTheme(id) != nil {
					// A file shadowing a built-in id overrides it; it is not a
					// second theme to list.
					continue
				}
				path := filepath.Join(sp.Themes, e.Name())
				if t, err := mm.LoadTheme(path); err == nil && t != nil {
					out = append(out, jsonThemeSummary{
						ID: id, Name: t.Name, Author: t.Author,
						Appearance: t.Appearance, Source: "system", Path: path,
					})
				}
			}
		}
	}
	return ok(c, map[string]any{"themes": out})
}

// importThemeRequest is the body of POST /api/v1/themes/import. The destination
// is explicit with no default (§8.8 rule 5's "explicit parameter").
type importThemeRequest struct {
	Theme       rawDocument `json:"theme"`
	Destination string      `json:"destination"` // "library" | "system" | "project"
	ProjectID   string      `json:"projectId"`   // required when destination is "project"
	Overwrite   bool        `json:"overwrite"`
	DryRun      bool        `json:"dryRun"`
}

// importTheme serves POST /api/v1/themes/import.
//
// A theme with a bad token or an unsupported schemaVersion is rejected whole —
// nothing is written (rules 1–3). On an id collision with an existing library
// theme, overwrite must be true or the import refuses with Conflict, which the
// client resolves by asking the user and retrying.
func (s *Server) importTheme(c *echo.Context) error {
	body, err := readBody(c)
	if err != nil {
		return err
	}
	var req importThemeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return err
	}
	// Accept the bare theme document as the body too: §8.8 says import "accepts
	// that file" — the exported .mm-theme.json, posted verbatim — and that the
	// destination is an explicit parameter. A bare body carries no envelope to
	// put that parameter in, so it comes from the query string instead.
	//
	// This must never swallow the ENVELOPE. A body carrying destination but no
	// theme member is a caller who forgot the theme, not a caller sending a
	// bare document — and since a theme with no recognised keys parses and
	// validates clean, accepting it here wrote the envelope itself over the
	// user's theme.json. Falling through to the error below is the only safe
	// reading of that request.
	if req.Theme == nil {
		var doc rawDocument
		if json.Unmarshal(body, &doc) == nil && isBareThemeDocument(doc) {
			req.Theme = doc
		}
	}
	if req.Theme == nil {
		return fmt.Errorf("%w: a theme document is required", mm.ErrInvalidArgument)
	}

	// The query is the fallback for every envelope field, so posting the file
	// verbatim to ?destination=library&overwrite=true is a complete request.
	// The body wins where it said anything.
	if req.Destination == "" {
		req.Destination = c.QueryParam("destination")
	}
	if req.ProjectID == "" {
		req.ProjectID = c.QueryParam("projectId")
	}
	if !req.Overwrite {
		req.Overwrite = c.QueryParam("overwrite") == "true"
	}
	if req.Destination == "" {
		return fmt.Errorf("%w: destination is required: library, system or project", mm.ErrInvalidArgument)
	}
	data, err := json.Marshal(req.Theme)
	if err != nil {
		return err
	}
	t, err := mm.ParseTheme("import.json", data)
	if err != nil {
		return err
	}
	if warnings := t.Validate(); len(warnings) > 0 {
		return fmt.Errorf("%w: %s", mm.ErrInvalidArgument, warnings[0].String())
	}

	var dest string
	switch req.Destination {
	case "library":
		if t.ID == "" {
			return fmt.Errorf("%w: importing to the library needs a theme id", mm.ErrInvalidArgument)
		}
		if !mm.ValidThemeID(t.ID) {
			return fmt.Errorf("%w: %q is not a valid theme id", mm.ErrInvalidArgument, t.ID)
		}
		home := s.svc.ConfigHome()
		if home == "" {
			return fmt.Errorf("%w: no configuration home directory", mm.ErrIO)
		}
		sp := mm.NewSystemPaths(home)
		dest = mm.ThemeLibraryPath(sp, t.ID)
		if isFile(dest) && !req.Overwrite {
			return fmt.Errorf("%w: theme %q already exists; pass overwrite to replace it",
				mm.ErrConflict, t.ID)
		}
	case "system":
		path, err := s.themePathFor(mm.ScopeSystem)
		if err != nil {
			return err
		}
		dest = path
	case "project":
		if req.ProjectID == "" {
			return fmt.Errorf("%w: importing to a project needs projectId", mm.ErrInvalidArgument)
		}
		store, err := s.svc.ResolveProject(req.ProjectID)
		if err != nil {
			return err
		}
		d, err := store.Directory()
		if err != nil {
			return err
		}
		dest = mm.ProjectThemePath(d.Path)
	default:
		return fmt.Errorf("%w: %q is not a destination: library, system or project",
			mm.ErrInvalidArgument, req.Destination)
	}

	dry := req.DryRun || dryRun(c)
	if err := t.Save(dest, dry); err != nil {
		return err
	}
	return ok(c, map[string]any{
		"themeId":     t.ID,
		"destination": req.Destination,
		"path":        dest,
		"dryRun":      dry,
	})
}

// isBareThemeDocument reports whether a decoded body is a theme in its own
// right rather than an import envelope.
//
// The test is the envelope's OWN field names: a document carrying any of them
// is the wrapper, however incomplete, and the theme it should have carried is
// missing. Every field of importThemeRequest is listed, so adding one there
// without adding it here is the way this check goes stale.
func isBareThemeDocument(doc rawDocument) bool {
	if len(doc) == 0 {
		return false
	}
	for _, envelopeKey := range []string{"theme", "destination", "projectId", "overwrite", "dryRun"} {
		if _, ok := doc[envelopeKey]; ok {
			return false
		}
	}
	return true
}

// exportTheme serves GET /api/v1/themes/:themeId/export: one self-contained
// theme file as an attachment (§8.8).
func (s *Server) exportTheme(c *echo.Context) error {
	themeID := c.Param("themeId")
	t := s.libraryTheme(themeID)
	if t == nil {
		return fmt.Errorf("%w: no theme %q in the library", mm.ErrNotFound, themeID)
	}
	data, err := t.Bytes()
	if err != nil {
		return err
	}
	filename := themeID + ".mm-theme.json"
	c.Response().Header().Set("Content-Type", "application/json")
	c.Response().Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", filename))
	return c.Blob(http.StatusOK, "application/json", data)
}

// libraryTheme resolves a themeId against the built-ins and the library
// directory. The built-in registry is checked for every id, so the lite theme
// (T-0137) exports like any library entry even though no file backs it.
func (s *Server) libraryTheme(themeID string) *mm.Theme {
	if bt := mm.BuiltinLibraryTheme(themeID); bt != nil {
		return bt
	}
	if home := s.svc.ConfigHome(); home != "" && mm.ValidThemeID(themeID) {
		sp := mm.NewSystemPaths(home)
		if t, err := mm.LoadTheme(mm.ThemeLibraryPath(sp, themeID)); err == nil && t != nil {
			return t
		}
	}
	return nil
}

func isFile(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
