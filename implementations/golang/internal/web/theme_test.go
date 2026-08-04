package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// §8 / T-0064 / T-0137 Theme view tests.

func TestThemeEditorView(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings/theme").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "theme-editor") {
		t.Error("theme-editor container missing")
	}
}

func TestThemeLibraryView(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings/themes").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "theme-library") {
		t.Error("theme-library container missing")
	}
}

func TestThemeDetailView(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings/themes/" + mm.BuiltinTheme().ID).expectStatus(http.StatusOK).Body

	if !hasTestid(body, "theme-detail") {
		t.Error("theme-detail container missing")
	}
}

func TestThemeExport(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	id := mm.BuiltinTheme().ID
	r := ts.get("/settings/themes/" + id)
	r.expectStatus(http.StatusOK)

	exp := ts.get("/settings/themes/" + id + "/export").expectStatus(http.StatusOK)
	if !strings.Contains(exp.Header.Get("Content-Disposition"), id+".mm-theme.json") {
		t.Errorf("Content-Disposition = %q, want filename", exp.Header.Get("Content-Disposition"))
	}
}

// The settings theme library may only offer themes that exist. It used to list
// "sample-one-dark" - the EXAMPLE document of spec-gui.md §8.6 - as a second
// built-in, and selecting it stored an id nothing answers to (T-0098).
func TestThemeLibraryOffersOnlyRealThemes(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings/themes").expectStatus(http.StatusOK).Body

	if strings.Contains(body, "sample-one-dark") {
		t.Error("the theme library still offers sample-one-dark, which mm does not define")
	}
	if !strings.Contains(body, mm.BuiltinTheme().ID) {
		t.Errorf("the theme library does not offer the built-in %q", mm.BuiltinTheme().ID)
	}
}

// T-0137: the lite theme is a second built-in and must appear everywhere a
// theme can be chosen or browsed: the library listing, the settings select,
// the detail view and the export route.
func TestLiteThemeIsOfferedEverywhere(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")

	lib := ts.get("/settings/themes").expectStatus(http.StatusOK).Body
	if !strings.Contains(lib, "micro-manager-lite") {
		t.Error("the theme library does not list the lite theme")
	}

	settings := ts.get("/settings").expectStatus(http.StatusOK).Body
	if !strings.Contains(settings, "micro-manager-lite") {
		t.Error("the settings theme select does not offer the lite theme")
	}

	detail := ts.get("/settings/themes/micro-manager-lite").expectStatus(http.StatusOK).Body
	if !hasTestid(detail, "theme-detail") {
		t.Error("the lite theme has no detail view")
	}

	exp := ts.get("/settings/themes/micro-manager-lite/export").expectStatus(http.StatusOK)
	if !strings.Contains(exp.Body, "micro-manager-lite") {
		t.Error("exporting the lite theme does not carry its id")
	}
}

// T-0137: the editor is a preview. Every token input must carry the path the
// client maps to a custom property, the dark palette must ride along so the
// preview can show it, and the palette-from-a-colour control must be present.
func TestThemeEditorCarriesPreviewHooks(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings/theme").expectStatus(http.StatusOK).Body

	if !strings.Contains(body, `data-token-path="color.accent.base"`) {
		t.Error("the editor does not expose color.accent.base to the preview")
	}
	if !strings.Contains(body, `data-token-path="gui.space.3"`) {
		t.Error("the editor does not expose a gui token to the preview")
	}
	if !strings.Contains(body, "data-dark-tokens=") {
		t.Error("the editor does not carry the dark palette for the preview")
	}
	if !strings.Contains(body, `data-testid="x-palette-generate"`) ||
		!strings.Contains(body, `data-testid="x-palette-base"`) {
		t.Error("the palette-from-a-colour control is missing")
	}
}

// T-0137: the palette endpoint derives the whole token set from one base
// colour, for both appearances, and refuses anything the library rejects.
func TestThemePaletteEndpoint(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")

	body := ts.get("/settings/theme/palette?base=%231257c9").expectStatus(http.StatusOK).Body
	var got struct {
		Base  string            `json:"base"`
		Light map[string]string `json:"light"`
		Dark  map[string]string `json:"dark"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("palette response is not JSON: %v\n%s", err, body)
	}
	if got.Base != "#1257c9" {
		t.Errorf("base = %q", got.Base)
	}
	for _, palette := range []map[string]string{got.Light, got.Dark} {
		if len(palette) != len(mm.ColorTokens()) {
			t.Errorf("palette has %d tokens, want %d", len(palette), len(mm.ColorTokens()))
		}
		for _, token := range mm.ColorTokens() {
			if palette[token] == "" {
				t.Errorf("no %s in the derived palette", token)
			}
		}
	}
	if got.Light["accent.base"] != "#1257c9" {
		t.Errorf("the base is not the accent: %q", got.Light["accent.base"])
	}
	if got.Light["bg.base"] == got.Dark["bg.base"] {
		t.Error("light and dark palettes are the same")
	}

	bad := ts.get("/settings/theme/palette?base=blue").expectStatus(http.StatusBadRequest).Body
	if !strings.Contains(bad, "InvalidArgument") {
		t.Errorf("a bad base should be an InvalidArgument, got: %s", bad)
	}
}

// The save handler starts from the RESOLVED theme, so editing a library theme
// whose dark palette differs from the default's does not silently replace it
// with the default's (T-0137).
func TestThemeEditorSavePreservesTheDarkPalette(t *testing.T) {
	darkTheme := `{"id":"night","name":"Night","appearance":"auto",
	  "color":{"bg":{"base":"#ffffff"}},
	  "colorDark":{"bg":{"base":"#101010"},"fg":{"default":"#e0e0e0"},"accent":{"base":"#e0e0e0"},"accent":{"fg":"#101010"}}}`

	ts := newTestServerWith(t, func(o *Options) {
		o.Config.Theme.ID = "night"
	}, "clean-full")
	libDir := filepath.Join(ts.ConfigHome, mm.ConfigDirName, "themes")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "night.json"), []byte(darkTheme), 0o644); err != nil {
		t.Fatal(err)
	}

	// One edit to a light token; the form carries every light token, so the
	// saved system theme must keep the theme's own dark palette.
	form := "id=night&name=Night&appearance=auto&token_bg.base=%23fafafa"
	ts.post("/settings/theme", form,
		"Content-Type", "application/x-www-form-urlencoded").expectStatus(200)

	data, err := os.ReadFile(mm.NewSystemPaths(ts.ConfigHome).Theme)
	if err != nil {
		t.Fatalf("the save did not write a system theme: %v", err)
	}
	// The file nests tokens (colorDark: { bg: { base: ... } }), so walk the
	// document rather than unmarshal a flat map.
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("saved theme is not JSON: %v", err)
	}
	darkBase := doc["colorDark"].(map[string]any)["bg"].(map[string]any)["base"]
	if darkBase != "#101010" {
		t.Errorf("colorDark.bg.base = %v, want the theme's own #101010", darkBase)
	}
}
