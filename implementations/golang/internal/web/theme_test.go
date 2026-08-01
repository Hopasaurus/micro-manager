package web

import (
	"net/http"
	"strings"
	"testing"

	"micromanager/mm"
)

// §8 / T-0064 Theme view tests.

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
