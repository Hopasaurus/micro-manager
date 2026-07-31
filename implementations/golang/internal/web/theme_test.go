package web

import (
	"net/http"
	"strings"
	"testing"
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
	body := ts.get("/settings/themes/sample-one-dark").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "theme-detail") {
		t.Error("theme-detail container missing")
	}
}

func TestThemeExport(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	r := ts.get("/settings/themes/sample-one-dark")
	r.expectStatus(http.StatusOK)

	exp := ts.get("/settings/themes/sample-one-dark/export").expectStatus(http.StatusOK)
	if !strings.Contains(exp.Header.Get("Content-Disposition"), "sample-one-dark.mm-theme.json") {
		t.Errorf("Content-Disposition = %q, want filename", exp.Header.Get("Content-Disposition"))
	}
}
