package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Settings views for system and project scopes (spec-gui.md §5.9, T-0063).

type settingsData struct {
	Scope     string // "system" or "project"
	ProjectID string
	Theme     themeSettingsData
	Lists     listsSettingsData
	Scan      scanSettingsData
	Wip       wipSettingsData
}

type themeSettingsData struct {
	Current string
	Source  string // "project", "system", "builtin"
	Themes  []themeOption
}

type themeOption struct {
	ID       string
	Name     string
	Source   string
	Selected bool
}

type listsSettingsData struct {
	RecentCount    int
	FavoritesCount int
}

type scanSettingsData struct {
	RootCount      int
	Partial        bool
	Roots          []rootSettingsData
	MaxDepth       int
	FollowSymlinks bool
	IncludeHidden  bool
	Excludes       string
	ScannedAt      string
}

type rootSettingsData struct {
	N       int
	Path    string
	Missing bool
}

type wipSettingsData struct {
	Limit int
}

// settingsSystem serves /settings (data-scope="system").
func (s *Server) settingsSystem(c *echo.Context) error {
	v := s.newView(c, "Settings", nil)
	v.App.Nav = "settings"

	data := s.buildSystemSettings()
	v.Data = data

	return s.render(c, http.StatusOK, "settings", "settings", v)
}

// settingsProject serves /p/:projectId/settings (data-scope="project").
func (s *Server) settingsProject(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Project Settings", store)
	v.App.Nav = "settings"

	data, err := s.buildProjectSettings(store)
	if err != nil {
		return err
	}
	v.Data = data

	return s.render(c, http.StatusOK, "settings", "settings", v)
}

// saveSettingsSystem handles POST /settings.
func (s *Server) saveSettingsSystem(c *echo.Context) error {
	if s.opts.ConfigHome == "" {
		return fmt.Errorf("%w: no configuration home directory", mm.ErrIO)
	}

	paths := mm.NewSystemPaths(s.opts.ConfigHome)
	if err := os.MkdirAll(paths.Dir, 0755); err != nil {
		return err
	}

	cfgFile, err := mm.LoadConfigFile(paths.Config, mm.ScopeSystem)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfgFile == nil {
		cfgFile, _ = mm.LoadConfigFile(paths.Config, mm.ScopeSystem)
	}

	form := c.Request().FormValue

	if val := form("recentCount"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			_ = cfgFile.Set("ui.recentCount", n)
		}
	}
	if val := form("favoritesCount"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			_ = cfgFile.Set("ui.favoritesCount", n)
		}
	}
	if val := form("maxDepth"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			_ = cfgFile.Set("scan.maxDepth", n)
		}
	}
	_ = cfgFile.Set("scan.followSymlinks", form("followSymlinks") == "on" || form("followSymlinks") == "true")
	_ = cfgFile.Set("scan.includeHidden", form("includeHidden") == "on" || form("includeHidden") == "true")

	if val := form("excludes"); val != "" {
		parts := strings.Split(val, ",")
		clean := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				clean = append(clean, t)
			}
		}
		_ = cfgFile.Set("scan.excludes", clean)
	}

	if theme := form("theme"); theme != "" {
		_ = cfgFile.Set("theme.id", theme)
	}

	if addRoot := strings.TrimSpace(form("addRoot")); addRoot != "" {
		rootsAny := cfgFile.Get("scan.roots")
		var roots []string
		if slice, ok := rootsAny.([]any); ok {
			for _, item := range slice {
				if str, ok := item.(string); ok {
					roots = append(roots, str)
				}
			}
		} else if slice, ok := rootsAny.([]string); ok {
			roots = append([]string(nil), slice...)
		}
		roots = append(roots, addRoot)
		_ = cfgFile.Set("scan.roots", roots)
	}

	if removeRoot := form("removeRoot"); removeRoot != "" {
		rootsAny := cfgFile.Get("scan.roots")
		var roots []string
		if slice, ok := rootsAny.([]any); ok {
			for _, item := range slice {
				if str, ok := item.(string); ok {
					if str != removeRoot {
						roots = append(roots, str)
					}
				}
			}
		} else if slice, ok := rootsAny.([]string); ok {
			for _, str := range slice {
				if str != removeRoot {
					roots = append(roots, str)
				}
			}
		}
		_ = cfgFile.Set("scan.roots", roots)
	}

	if err := cfgFile.Save(false); err != nil {
		return err
	}

	// Reload config in server opts
	sysFile, _ := mm.LoadConfigFile(paths.Config, mm.ScopeSystem)
	s.opts.Config, _ = mm.MergeConfig(sysFile, nil)

	return s.settingsSystem(c)
}

// saveSettingsProject handles POST /p/:projectId/settings.
func (s *Server) saveSettingsProject(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	dir, err := store.Directory()
	if err != nil {
		return err
	}

	form := c.Request().FormValue

	if action := form("action"); action == "clear_theme" {
		themePath := mm.ProjectThemePath(dir.Path)
		_ = os.Remove(themePath)
	} else {
		if val := form("wipLimit"); val != "" {
			if limit, err := strconv.Atoi(val); err == nil && limit >= 1 {
				if _, _, err := store.SetWipLimit(limit, false); err != nil {
					return err
				}
			}
		}

		if themeID := form("theme"); themeID != "" {
			cfgPath := mm.ProjectConfigPath(dir.Path)
			pCfg, _ := mm.LoadConfigFile(cfgPath, mm.ScopeProject)
			if pCfg == nil {
				pCfg, _ = mm.LoadConfigFile(cfgPath, mm.ScopeProject)
			}
			_ = pCfg.Set("theme.id", themeID)
			_ = pCfg.Save(false)
		}
	}

	return s.settingsProject(c)
}

func (s *Server) buildSystemSettings() settingsData {
	cfg := s.opts.Config
	data := settingsData{
		Scope: "system",
		Lists: listsSettingsData{
			RecentCount:    cfg.UI.RecentCount,
			FavoritesCount: cfg.UI.FavoritesCount,
		},
		Scan: scanSettingsData{
			MaxDepth:       cfg.Scan.MaxDepth,
			FollowSymlinks: cfg.Scan.FollowSymlinks,
			IncludeHidden:  cfg.Scan.IncludeHidden,
			Excludes:       strings.Join(cfg.Scan.Excludes, ", "),
		},
	}

	view := s.registry.discovery()
	data.Scan.Partial = view.Partial
	data.Scan.ScannedAt = view.ScannedAt.String()
	for i, r := range view.Roots {
		data.Scan.Roots = append(data.Scan.Roots, rootSettingsData{
			N:       i,
			Path:    r.Path,
			Missing: r.Missing,
		})
	}
	data.Scan.RootCount = len(data.Scan.Roots)

	data.Theme = s.buildThemeSettings(cfg.Theme.ID, "", "")
	return data
}

func (s *Server) buildProjectSettings(store *mm.Store) (settingsData, error) {
	dir, err := store.Directory()
	if err != nil {
		return settingsData{}, err
	}

	// The project's own config names the theme; the system config's id is not
	// the project's choice and must not highlight the select (T-0137).
	projectThemeID := ""
	if pCfg, err := mm.LoadConfigFile(mm.ProjectConfigPath(dir.Path), mm.ScopeProject); err == nil && pCfg != nil {
		if merged, err := mm.MergeConfig(nil, pCfg); err == nil {
			projectThemeID = merged.Theme.ID
		}
	}

	data := settingsData{
		Scope:     "project",
		ProjectID: dir.ProjectID,
		Wip: wipSettingsData{
			Limit: dir.WipLimit,
		},
	}

	data.Theme = s.buildThemeSettings(projectThemeID, dir.Path, projectThemeID)
	return data, nil
}

func (s *Server) buildThemeSettings(currentTheme, projectDir, projectThemeID string) themeSettingsData {
	// The badge names where the CURRENT theme resolves from — project, system
	// or builtin — which is exactly what ResolveTheme already decided, so the
	// source is read off the resolution rather than re-derived from file
	// existence.
	req := mm.ThemeRequest{
		ProjectDir:     projectDir,
		ConfigHome:     s.opts.ConfigHome,
		ProjectThemeID: projectThemeID,
		SystemThemeID:  s.opts.Config.Theme.ID,
	}
	resolved, _, _ := mm.ResolveTheme(req)
	source := string(resolved.Source)

	// The built-ins §8.7 defines, derived from the registry so the default and
	// the lite theme (T-0137) cannot drift from the library listing. The
	// default is selected when nothing is configured, because nothing
	// resolving is the definition of the default.
	defaultID := mm.BuiltinTheme().ID
	var themes []themeOption
	for _, bt := range mm.BuiltinThemes() {
		themes = append(themes, themeOption{
			ID: bt.ID, Name: bt.Name + " (Builtin)", Source: "builtin",
			Selected: currentTheme == bt.ID || (bt.ID == defaultID && currentTheme == ""),
		})
	}

	// The library files, so a theme saved there is selectable without editing
	// config.json by hand. A file theme can never be confused with a built-in:
	// the ids are validated disjoint by the config write.
	if s.opts.ConfigHome != "" {
		libDir := filepath.Join(mm.NewSystemPaths(s.opts.ConfigHome).Dir, "themes")
		if entries, err := os.ReadDir(libDir); err == nil {
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				id := strings.TrimSuffix(e.Name(), ".json")
				if mm.BuiltinLibraryTheme(id) != nil {
					continue // a file shadowing a built-in id is the user's own; the built-in row already offers it
				}
				if t, err := mm.LoadTheme(filepath.Join(libDir, e.Name())); err == nil {
					themes = append(themes, themeOption{
						ID: id, Name: t.Name + " (Library)", Source: "system",
						Selected: currentTheme == id,
					})
				}
			}
		}
	}

	return themeSettingsData{
		Current: currentTheme,
		Source:  source,
		Themes:  themes,
	}
}
