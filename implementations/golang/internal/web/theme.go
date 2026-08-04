package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Theme editor, library, import and export (spec-gui.md §8, T-0064, T-0137).

// themeEditorData fills the editor form. DarkTokens is the resolved dark
// palette as JSON, for the live preview: the editor edits the light palette
// only, and the preview must still show what the dark half looks like (§8.7).
type themeEditorData struct {
	ThemeID    string
	ThemeName  string
	Author     string
	Appearance string
	Colors     []tokenInput
	Gui        []tokenOption
	Warnings   []string
	DarkTokens string
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

	// Start from the RESOLVED theme, not the built-in default: the editor
	// carries every colour and gui token, but it has no colourDark fields, so
	// a save built on the default would silently replace a library theme's
	// dark palette with the default's (T-0137). What the user was looking at
	// is the best description of what they meant to keep.
	req := mm.ThemeRequest{
		ConfigHome:    s.opts.ConfigHome,
		SystemThemeID: s.opts.Config.Theme.ID,
	}
	resolved, _, _ := mm.ResolveTheme(req)

	theme := mm.BuiltinTheme()
	theme.ID = reqTheme
	theme.Name = c.Request().FormValue("name")
	theme.Author = c.Request().FormValue("author")
	theme.Appearance = c.Request().FormValue("appearance")
	if theme.Color == nil {
		theme.Color = map[string]string{}
	}
	theme.ColorDark = resolved.ColorDark
	theme.Brand = resolved.Brand

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

	// The dark palette rides along as JSON so the live preview can show it
	// even when the OS is light (§8.7). The values are the RESOLVED ones: an
	// edit to a light token changes the light half, and the dark half is what
	// the user last had it as.
	if darkJSON, err := json.Marshal(resolved.ColorDark); err == nil {
		data.DarkTokens = string(darkJSON)
	}

	// §11 rule 7: warn on contrast failures, never block. The warning is part
	// of the editor page, so it travels with every render - the initial load
	// AND the save response, which re-renders the editor.
	data.Warnings = append(data.Warnings, contrastWarnings(resolved)...)

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
	// Described FROM the built-in themes, so this listing and the JSON API's
	// cannot drift from each other or from the document either one exports.
	// "sample-one-dark" used to sit here too: it is the EXAMPLE theme of
	// spec-gui.md §8.6, not a theme mm has, and selecting it silently served
	// the built-in (§8.7 defines the built-in default; T-0137 adds the lite
	// theme as a second built-in entry).
	data := themeLibraryData{
		Current: s.opts.Config.Theme.ID,
	}
	for _, bt := range mm.BuiltinThemes() {
		data.Themes = append(data.Themes, themeSummary{
			ID: bt.ID, Name: bt.Name, Author: "Builtin", Appearance: bt.Appearance, Source: "builtin",
		})
	}

	if s.opts.ConfigHome != "" {
		sp := mm.NewSystemPaths(s.opts.ConfigHome)
		libDir := filepath.Join(sp.Dir, "themes")
		if entries, err := os.ReadDir(libDir); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".json") {
					id := strings.TrimSuffix(e.Name(), ".json")
					if mm.BuiltinLibraryTheme(id) != nil {
						// A file shadowing a built-in id is an override the user
						// imported on purpose; it is not a second theme to list.
						continue
					}
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

// contrastWarnings renders §11 rule 7 checks as editor warnings. Each failing
// pair is one line; the pairs come from mm.CheckContrast, which measures the
// two the spec names.
//
// Appearance decides the palette to judge. An "auto" theme carries both a
// light and a dark palette and either could be shown at runtime, so both are
// checked and the failing one is named.
func contrastWarnings(r mm.ResolvedTheme) []string {
	var warns []string
	check := func(dark bool, label string) {
		for _, p := range r.CheckContrast(dark) {
			if p.Passes {
				continue
			}
			warns = append(warns, fmt.Sprintf(
				"%s: contrast %.2f:1 between %s and %s is below WCAG AA (4.5:1)",
				label, p.Ratio,
				mm.CSSPropertyName("color."+p.Foreground),
				mm.CSSPropertyName("color."+p.Background)))
		}
	}
	switch r.Appearance {
	case "dark":
		check(true, "Dark palette")
	case "light":
		check(false, "Light palette")
	default:
		check(false, "Light palette")
		check(true, "Dark palette")
	}
	return warns
}

func (s *Server) resolveExportTheme(themeID string) *mm.Theme {
	// The empty id is the built-in too: nothing configured resolves to it.
	if bt := mm.BuiltinTheme(); themeID == bt.ID || themeID == "" {
		return bt
	}

	// A built-in library theme (the lite theme, T-0137) exports like any
	// library entry even though no file backs it.
	if bt := mm.BuiltinLibraryTheme(themeID); bt != nil {
		return bt
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

// isFile reports whether path names an existing regular file.
func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// themePalette serves GET /settings/theme/palette?base=#rrggbb.
//
// Internal helper for the theme editor's "generate palette" control (T-0137):
// given one base colour it returns the harmonized color.* token set for both
// appearances, derived by mm.HarmonizedPalette. The colour math lives in the
// library so a future TUI theme editor derives the same tokens from the same
// base; this endpoint is how the web editor reaches it without duplicating it
// in mm.js. It is not in the spec's route table, like /p/:projectId/shell.
func (s *Server) themePalette(c *echo.Context) error {
	base := c.QueryParam("base")
	if base == "" {
		return fmt.Errorf("%w: palette needs a base colour", mm.ErrInvalidArgument)
	}

	light, err := mm.HarmonizedPalette(base, false)
	if err != nil {
		return fmt.Errorf("%w: %v", mm.ErrInvalidArgument, err)
	}
	dark, err := mm.HarmonizedPalette(base, true)
	if err != nil {
		return fmt.Errorf("%w: %v", mm.ErrInvalidArgument, err)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"base":  base,
		"light": light,
		"dark":  dark,
	})
}
