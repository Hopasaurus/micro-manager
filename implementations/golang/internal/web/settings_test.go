package web

import (
	"net/http"
	"net/url"
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
