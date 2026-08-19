package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
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

	for _, id := range []string{"settings-theme", "settings-lists", "settings-scan", "x-settings-tickler", "settings-save"} {
		if !hasTestid(body, id) {
			t.Errorf("%s missing in system settings", id)
		}
	}

	// §5.9: settings-wip MUST NOT be present in system scope
	if hasTestid(body, "settings-wip") {
		t.Error("settings-wip present in system scope")
	}
}

func TestSavingScanRootReloadsRegistryAndRescans(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	newRoot := t.TempDir()
	newBoard := copyFixtureTo(t, "clean-full", filepath.Join(newRoot, "second"))

	body := ts.form(http.MethodPost, "/settings", url.Values{
		"addRoot":       {newRoot},
		"includeHidden": {"true"},
	}).expectStatus(http.StatusOK).Body

	if !strings.Contains(body, `data-path="`+newRoot+`"`) {
		t.Fatalf("saved root is absent from settings response: %s", snippetAround(body, "settings-scan-roots"))
	}
	found := false
	for _, d := range ts.registry.discovery().Directories {
		if samePath(d.Path, newBoard) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("new root was saved but its board was not discovered without a restart")
	}
}

func TestConfigAPIReloadsScanRoots(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	newRoot := t.TempDir()
	newBoard := copyFixtureTo(t, "clean-full", filepath.Join(newRoot, "api"))
	body := `{"scan":{"roots":[` + strconv.Quote(newRoot) + `]}}`

	ts.do(http.MethodPut, "/api/v1/config", strings.NewReader(body)).expectStatus(http.StatusOK)

	view := ts.registry.discovery()
	if len(view.Roots) != 1 || !samePath(view.Roots[0].Path, newRoot) {
		t.Fatalf("registry roots after config PUT = %#v, want %s", view.Roots, newRoot)
	}
	found := false
	for _, d := range view.Directories {
		if samePath(d.Path, newBoard) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("discovery after config PUT = %#v, want board under %s", view.Directories, newRoot)
	}
}

func TestRemoveExpandedScanRootRemovesStoredTildeForm(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")
	home := t.TempDir()
	t.Setenv("HOME", home)
	stored := "~/projects"
	expanded := filepath.Join(home, "projects")
	body := `{"scan":{"roots":[` + strconv.Quote(stored) + `]}}`
	ts.do(http.MethodPut, "/api/v1/config", strings.NewReader(body)).expectStatus(http.StatusOK)

	ts.form(http.MethodPost, "/settings", url.Values{
		"removeRoot":    {expanded},
		"includeHidden": {"true"},
	}).expectStatus(http.StatusOK)

	path := mm.NewSystemPaths(ts.ConfigHome).Config
	file, err := mm.LoadConfigFile(path, mm.ScopeSystem)
	if err != nil {
		t.Fatal(err)
	}
	roots, ok := file.Get("scan.roots").([]any)
	if !ok || len(roots) != 0 {
		t.Fatalf("stored scan.roots after removing expanded path = %#v, want empty list", file.Get("scan.roots"))
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
	for _, id := range []string{"settings-lists", "settings-scan", "x-settings-tickler"} {
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

// T-0207 — the Tickler section: system scope only, pre-filled from the merged
// config, and the save round-trips the interval into the system config file
// and the running view. A typo refuses the whole save and changes nothing.
func TestTicklerSettingsSection(t *testing.T) {
	ts, _ := boardServer(t, "clean-full")

	// Default: off, nothing pre-filled, the status badge says so.
	body := ts.get("/settings").expectStatus(http.StatusOK).Body
	for _, id := range []string{"x-settings-tickler", "x-settings-tickler-enabled", "x-settings-tickler-interval", "x-settings-tickler-status"} {
		if !hasTestid(body, id) {
			t.Errorf("%s missing in the Tickler section", id)
		}
	}
	if strings.Contains(testid(t, body, "x-settings-tickler-enabled"), "checked") {
		t.Error("the default must render the tickler disabled")
	}
	if got := badgeText(body, "x-settings-tickler-status"); got != "off" {
		t.Errorf("status = %q, want off", got)
	}

	// Enable at 1m: the save writes the interval and the reloaded view shows on.
	ts.form(http.MethodPost, "/settings", url.Values{
		"ticklerEnabled": {"true"}, "ticklerInterval": {"1m"},
	}).expectStatus(http.StatusOK)
	body = ts.get("/settings").expectStatus(http.StatusOK).Body
	if !strings.Contains(testid(t, body, "x-settings-tickler-enabled"), "checked") {
		t.Error("an enabled tickler must render the checkbox checked")
	}
	if got := badgeText(body, "x-settings-tickler-status"); got != "on · every 1m" {
		t.Errorf("status = %q, want on · every 1m", got)
	}
	if got := ts.Server.opts.Config.Tickler.Interval; got != "1m" {
		t.Errorf("merged interval = %q, want 1m", got)
	}
	sys, _ := mm.LoadConfigFile(mm.NewSystemPaths(ts.ConfigHome).Config, mm.ScopeSystem)
	if got, _ := sys.Get("tickler.interval").(string); got != "1m" {
		t.Errorf("config file tickler.interval = %v, want 1m", sys.Get("tickler.interval"))
	}

	// A typo refuses the whole save and changes nothing.
	before, _ := os.ReadFile(mm.NewSystemPaths(ts.ConfigHome).Config)
	bad := ts.form(http.MethodPost, "/settings", url.Values{
		"ticklerEnabled": {"true"}, "ticklerInterval": {"soon"},
	})
	if bad.Status != http.StatusBadRequest {
		t.Errorf("a non-duration interval must be refused, got %d", bad.Status)
	}
	after, _ := os.ReadFile(mm.NewSystemPaths(ts.ConfigHome).Config)
	if string(after) != string(before) {
		t.Error("a refused save must not change the config file")
	}

	// Enabled with no interval is refused too.
	if r := ts.form(http.MethodPost, "/settings", url.Values{"ticklerEnabled": {"true"}}); r.Status != http.StatusBadRequest {
		t.Errorf("enabled without an interval must be refused, got %d", r.Status)
	}

	// Disabling writes the documented null value; the merged view is off.
	ts.form(http.MethodPost, "/settings", url.Values{}).expectStatus(http.StatusOK)
	if got := ts.Server.opts.Config.Tickler.Interval; got != "" {
		t.Errorf("merged interval after off = %q, want empty", got)
	}
	if got := badgeText(ts.get("/settings").Body, "x-settings-tickler-status"); got != "off" {
		t.Errorf("status after off = %q, want off", got)
	}
}

// A project config MUST NOT set the system-scoped tickler (spec-gui.md §9.3):
// the config API refuses the whole write by name, so a board cannot turn the
// service on for itself (T-0207).
func TestProjectConfigRefusesTickler(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	resp := ts.do(http.MethodPut, "/api/v1/projects/"+id+"/config", strings.NewReader(`{"tickler":{"interval":"1m"}}`))
	if resp.Status != http.StatusBadRequest {
		t.Fatalf("a project config setting tickler must be refused, got %d body=%s", resp.Status, resp.Body)
	}

	// The system config still accepts it.
	sys := ts.do(http.MethodPut, "/api/v1/config", strings.NewReader(`{"tickler":{"interval":"1m"}}`))
	if sys.Status != http.StatusOK {
		t.Fatalf("the system config must accept tickler.interval, got %d body=%s", sys.Status, sys.Body)
	}
}
