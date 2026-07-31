package web

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// Theme editor, library, import and export (spec-gui.md §8, T-0064).

type themeEditorData struct {
	ThemeID    string
	ThemeName  string
	Author     string
	Appearance string
	Colors     []tokenInput
	Gui        []tokenOption
	Warnings   []string
}

type tokenInput struct {
	Path  string
	Name  string
	Value string
}

type tokenOption struct {
	Path  string
	Name  string
	Value string
}

type themeLibraryData struct {
	Current string
	Themes  []themeSummary
}

type themeSummary struct {
	ID         string
	Name       string
	Author     string
	Appearance string
	Source     string // "builtin", "system", "project"
	Path       string
}

// themeEditor serves GET /settings/theme.
func (s *Server) themeEditor(c *echo.Context) error {
	v := s.newView(c, "Theme Editor", nil)
	v.App.Nav = "theme-editor"

	data := s.buildThemeEditor(c)
	v.Data = data

	return s.render(c, http.StatusOK, "theme", "theme-editor", v)
}

// saveThemeEditor handles POST /settings/theme.
func (s *Server) saveThemeEditor(c *echo.Context) error {
	reqTheme := c.Request().FormValue("id")
	if reqTheme == "" {
		reqTheme = "custom"
	}

	theme := mm.BuiltinTheme()
	theme.ID = reqTheme
	theme.Name = c.Request().FormValue("name")
	theme.Author = c.Request().FormValue("author")
	theme.Appearance = c.Request().FormValue("appearance")
	if theme.Color == nil {
		theme.Color = map[string]string{}
	}

	for _, token := range mm.ColorTokens() {
		if val := c.Request().FormValue("token_" + token); val != "" {
			theme.Color[token] = val
		}
	}

	warnings := theme.Validate()
	if len(warnings) > 0 {
		v := s.newView(c, "Theme Editor", nil)
		v.App.Nav = "settings"
		data := s.buildThemeEditor(c)
		for _, w := range warnings {
			data.Warnings = append(data.Warnings, w.String())
		}
		v.Data = data
		return s.render(c, http.StatusBadRequest, "theme", "theme-editor", v)
	}

	if s.opts.ConfigHome != "" {
		paths := mm.NewSystemPaths(s.opts.ConfigHome)
		_ = os.MkdirAll(filepath.Dir(paths.Theme), 0755)
		_ = theme.Save(paths.Theme, false)
	}

	return s.themeEditor(c)
}

// themeLibrary serves GET /settings/themes.
func (s *Server) themeLibrary(c *echo.Context) error {
	v := s.newView(c, "Theme Library", nil)
	v.App.Nav = "theme-library"

	data := s.buildThemeLibrary()
	v.Data = data

	return s.render(c, http.StatusOK, "theme", "theme-library", v)
}

// themeDetail serves GET /settings/themes/:themeId.
func (s *Server) themeDetail(c *echo.Context) error {
	themeID := c.Param("themeId")
	v := s.newView(c, "Theme — "+themeID, nil)
	v.App.Nav = "theme-detail"

	data := s.buildThemeDetail(themeID)
	v.Data = data

	return s.render(c, http.StatusOK, "theme", "theme-detail", v)
}

// themeExport serves GET /settings/themes/:themeId/export.
func (s *Server) themeExport(c *echo.Context) error {
	themeID := c.Param("themeId")
	theme := s.resolveExportTheme(themeID)

	data, err := theme.Bytes()
	if err != nil {
		return err
	}

	filename := fmt.Sprintf("%s.mm-theme.json", themeID)
	c.Response().Header().Set("Content-Type", "application/json")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	return c.Blob(http.StatusOK, "application/json", data)
}

// themeImport handles POST /settings/themes/import.
func (s *Server) themeImport(c *echo.Context) error {
	fileHeader, err := c.FormFile("theme_file")
	if err != nil {
		return fmt.Errorf("%w: missing theme file", mm.ErrInvalidArgument)
	}

	file, err := fileHeader.Open()
	if err != nil {
		return err
	}
	defer file.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(file); err != nil {
		return err
	}

	theme, err := mm.ParseTheme("uploaded.json", buf.Bytes())
	if err != nil {
		return fmt.Errorf("%w: invalid theme: %v", mm.ErrInvalidArgument, err)
	}

	warnings := theme.Validate()
	if len(warnings) > 0 {
		return fmt.Errorf("%w: theme validation failed: %v", mm.ErrInvalidArgument, warnings[0])
	}

	overwrite := c.FormValue("overwrite") == "true"
	if s.opts.ConfigHome != "" && theme.ID != "" {
		sp := mm.NewSystemPaths(s.opts.ConfigHome)
		dest := mm.ThemeLibraryPath(sp, theme.ID)

		if isFile(dest) && !overwrite {
			// Render collision dialog (dialog-import-theme)
			v := s.newView(c, "", nil)
			v.Data = map[string]any{
				"ThemeID":   theme.ID,
				"ThemeName": theme.Name,
			}
			return s.renderFragmentAlways(c, http.StatusConflict, "item", "dialog-import-theme", v)
		}

		_ = os.MkdirAll(filepath.Dir(dest), 0755)
		_ = theme.Save(dest, false)
	}

	return s.themeLibrary(c)
}

func (s *Server) buildThemeEditor(c *echo.Context) themeEditorData {
	req := mm.ThemeRequest{
		ConfigHome:    s.opts.ConfigHome,
		SystemThemeID: s.opts.Config.Theme.ID,
	}
	resolved, _, _ := mm.ResolveTheme(req)
	data := themeEditorData{
		ThemeID:    resolved.ID,
		ThemeName:  resolved.Name,
		Author:     "",
		Appearance: resolved.Appearance,
	}

	for _, token := range mm.ColorTokens() {
		val := resolved.Color[token]
		data.Colors = append(data.Colors, tokenInput{
			Path:  token,
			Name:  mm.CSSPropertyName("color." + token),
			Value: val,
		})
	}

	for _, token := range mm.GUITokens() {
		val := resolved.GUI[token]
		data.Gui = append(data.Gui, tokenOption{
			Path:  token,
			Name:  mm.CSSPropertyName("gui." + token),
			Value: val,
		})
	}

	return data
}

func (s *Server) buildThemeLibrary() themeLibraryData {
	data := themeLibraryData{
		Current: s.opts.Config.Theme.ID,
		Themes: []themeSummary{
			{ID: "micro-manager", Name: "micro-manager", Author: "Builtin", Appearance: "auto", Source: "builtin"},
			{ID: "sample-one-dark", Name: "Sample One — Dark", Author: "Builtin", Appearance: "dark", Source: "builtin"},
		},
	}

	if s.opts.ConfigHome != "" {
		sp := mm.NewSystemPaths(s.opts.ConfigHome)
		libDir := filepath.Join(sp.Dir, "themes")
		if entries, err := os.ReadDir(libDir); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".json") {
					id := strings.TrimSuffix(e.Name(), ".json")
					if t, err := mm.LoadTheme(filepath.Join(libDir, e.Name())); err == nil {
						data.Themes = append(data.Themes, themeSummary{
							ID:         id,
							Name:       t.Name,
							Author:     t.Author,
							Appearance: string(t.Appearance),
							Source:     "system",
							Path:       filepath.Join(libDir, e.Name()),
						})
					}
				}
			}
		}
	}

	return data
}

func (s *Server) buildThemeDetail(themeID string) themeSummary {
	theme := s.resolveExportTheme(themeID)
	return themeSummary{
		ID:         themeID,
		Name:       theme.Name,
		Author:     theme.Author,
		Appearance: string(theme.Appearance),
		Source:     "system",
	}
}

func (s *Server) resolveExportTheme(themeID string) *mm.Theme {
	if themeID == "micro-manager" || themeID == "sample-one-dark" || themeID == "" {
		return mm.BuiltinTheme()
	}

	if s.opts.ConfigHome != "" {
		sp := mm.NewSystemPaths(s.opts.ConfigHome)
		libPath := mm.ThemeLibraryPath(sp, themeID)
		if t, err := mm.LoadTheme(libPath); err == nil {
			return t
		}
	}

	return mm.BuiltinTheme()
}

// unused import fix helper
var _ = (*multipart.FileHeader)(nil)
