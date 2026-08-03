package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micromanager/mm"
)

// §5.9 Settings view tests.

func TestSystemSettingsView(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	body := ts.get("/settings").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "settings") {
		t.Error("settings container missing")
	}
	if got := attrOf(t, testid(t, body, "settings"), "data-scope"); got != "system" {
		t.Errorf("data-scope = %q, want system", got)
	}

	for _, id := range []string{"settings-theme", "settings-lists", "settings-scan", "settings-save"} {
		if !hasTestid(body, id) {
			t.Errorf("%s missing in system settings", id)
		}
	}

	// §5.9: settings-wip MUST NOT be present in system scope
	if hasTestid(body, "settings-wip") {
		t.Error("settings-wip present in system scope")
	}
}

func TestProjectSettingsView(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/settings").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "settings") {
		t.Error("settings container missing")
	}
	if got := attrOf(t, testid(t, body, "settings"), "data-scope"); got != "project" {
		t.Errorf("data-scope = %q, want project", got)
	}

	for _, id := range []string{"settings-theme", "settings-wip", "settings-save"} {
		if !hasTestid(body, id) {
			t.Errorf("%s missing in project settings", id)
		}
	}

	// §5.9: settings-lists and settings-scan MUST be absent in project scope
	for _, id := range []string{"settings-lists", "settings-scan"} {
		if hasTestid(body, id) {
			t.Errorf("%s present in project scope (MUST be absent)", id)
		}
	}
}

func TestSaveProjectSettingsWipLimit(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	r := ts.form(http.MethodPost, "/p/"+id+"/settings", url.Values{"wipLimit": {"4"}})
	r.expectStatus(http.StatusOK)

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	dir, err := store.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if dir.WipLimit != 4 {
		t.Errorf("WIP limit = %d, want 4", dir.WipLimit)
	}
}

// The project settings page highlights the PROJECT's chosen theme, not the
// system config's. The two used to be conflated, so selecting a theme for the
// project pointed the select at whatever the system had (T-0137).
func TestProjectSettingsHighlightTheProjectTheme(t *testing.T) {
	low := `{"id":"proj-theme","name":"Project Theme","appearance":"light",
	  "color":{"bg":{"base":"#ffffff"},"fg":{"default":"#111111"},"accent":{"base":"#1257c9"},"accent":{"fg":"#ffffff"}}}`

	ts, id := boardServer(t, "clean-full")
	libDir := filepath.Join(ts.ConfigHome, mm.ConfigDirName, "themes")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "proj-theme.json"), []byte(low), 0o644); err != nil {
		t.Fatal(err)
	}

	// The project names the library theme; the system names the lite theme.
	ts.form(http.MethodPost, "/p/"+id+"/settings", url.Values{"theme": {"proj-theme"}}).expectStatus(200)
	ts.form(http.MethodPost, "/settings", url.Values{"theme": {"micro-manager-lite"}}).expectStatus(200)

	body := ts.get("/p/" + id + "/settings").expectStatus(http.StatusOK).Body
	if !strings.Contains(body, `<option value="proj-theme" selected>`) {
		t.Errorf("the project settings select does not highlight the project's theme:\n%s",
			snippetAround(body, "settings-theme-select"))
	}
	if strings.Contains(body, `<option value="micro-manager-lite" selected>`) {
		t.Error("the project settings select highlights the system theme instead of the project's")
	}
}

// snippetAround returns the surrounding text of a marker, for failure output.
func snippetAround(haystack, marker string) string {
	i := strings.Index(haystack, marker)
	if i < 0 {
		return haystack
	}
	start := i - 200
	if start < 0 {
		start = 0
	}
	end := i + 300
	if end > len(haystack) {
		end = len(haystack)
	}
	return haystack[start:end]
}
