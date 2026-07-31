package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"micromanager/mm"
)

// §5.4 fixes Home's testids, and empty lists MUST render the container with
// data-count="0" and data-state="empty" rather than being omitted.
func TestHome(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	body := ts.get("/").expectStatus(http.StatusOK).Body

	for _, id := range []string{"home", "favorites-list", "recent-list", "project-open", "project-init"} {
		if !hasTestid(body, id) {
			t.Errorf("Home is missing data-testid=%q", id)
		}
	}

	for _, list := range []string{"favorites-list", "recent-list"} {
		tag := testid(t, body, list)
		if got := attrOf(t, tag, "data-count"); got != "0" {
			t.Errorf("%s data-count = %q on a fresh config home", list, got)
		}
		if got := attrOf(t, tag, "data-state"); got != "empty" {
			t.Errorf("%s data-state = %q, want empty", list, got)
		}
	}
}

// §10 rule 1: opening a project moves it to the front of recent. Until this
// item, nothing wrote the list at all.
func TestOpeningABoardRecordsIt(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	if got := attrOf(t, testid(t, ts.get("/").Body, "recent-list"), "data-count"); got != "0" {
		t.Fatalf("recent starts at %q", got)
	}

	ts.get("/p/" + id + "/board").expectStatus(http.StatusOK)

	home := ts.get("/").Body
	if got := attrOf(t, testid(t, home, "recent-list"), "data-count"); got != "1" {
		t.Errorf("recent-list data-count = %q after opening a board", got)
	}
	if !hasTestid(home, "project-card-"+id) {
		t.Error("the opened project is not on Home")
	}

	// Opening again moves it, never duplicates it (§10 rule 1).
	ts.get("/p/" + id + "/board")
	if got := attrOf(t, testid(t, ts.get("/").Body, "recent-list"), "data-count"); got != "1" {
		t.Errorf("recent-list data-count = %q after opening twice", got)
	}
}

// A poll or an htmx refresh is not an "open": a window left sitting on a board
// would otherwise rewrite the list forever.
func TestFragmentRefreshDoesNotRecordAnOpen(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	ts.get("/p/"+id+"/board", "HX-Request", "true")
	ts.get("/p/" + id + "/board?fragment=1")

	if got := attrOf(t, testid(t, ts.get("/").Body, "recent-list"), "data-count"); got != "0" {
		t.Errorf("recent-list data-count = %q; a refresh counted as an open", got)
	}
}

// §5.3: /projects renders everything discovery finds, grouped by scan root,
// with the scan time visible.
func TestProjectsList(t *testing.T) {
	ts := newTestServer(t, "clean-full", "clean-minimal", "clean-multi-slot")
	body := ts.get("/projects").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "projects") || !hasTestid(body, "projects-rescan") {
		t.Error("the projects view is missing its container or its rescan control")
	}
	if !hasTestid(body, "projects-root-0") {
		t.Error("no scan-root group was rendered")
	}

	projects := testid(t, body, "projects")
	if got := attrOf(t, projects, "data-count"); got != "3" {
		t.Errorf("data-count = %q, want 3", got)
	}
	if got := attrOf(t, projects, "data-partial"); got != "false" {
		t.Errorf("data-partial = %q", got)
	}
	if attrOf(t, projects, "data-scanned-at") == "" {
		t.Error("data-scanned-at is empty; §9.5 requires the UI to show it")
	}

	// Every project is a card with the §5.4 parts.
	for _, part := range []string{"project-card-name", "project-card-path", "project-card-wip", "project-card-favorite-toggle"} {
		if !hasTestid(body, part) {
			t.Errorf("a project card is missing %s", part)
		}
	}
}

// §5.3: a truncated scan MUST be marked and said out loud, because it is
// otherwise indistinguishable from a missing project.
func TestPartialScanIsVisible(t *testing.T) {
	ts := newTestServer(t, "clean-full", "clean-minimal", "clean-multi-slot")
	ts.registry.scan.MaxResults = 1
	ts.registry.rescan()

	body := ts.get("/projects").Body
	if got := attrOf(t, testid(t, body, "projects"), "data-partial"); got != "true" {
		t.Errorf("data-partial = %q after a truncated walk", got)
	}
	if !hasTestid(body, "projects-partial") {
		t.Error("a truncated scan is not said out loud")
	}
}

// §9.5: a rescan must be available on demand, because fingerprint polling never
// sees a NEW project appearing.
func TestRescanFindsAProjectAddedSinceStartup(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	root := ts.Server.opts.Config.Scan.Roots[0]

	if got := attrOf(t, testid(t, ts.get("/projects").Body, "projects"), "data-count"); got != "1" {
		t.Fatalf("data-count = %q at startup", got)
	}

	copyFixtureTo(t, "clean-minimal", root+"/appeared")

	if got := attrOf(t, testid(t, ts.get("/projects").Body, "projects"), "data-count"); got != "1" {
		t.Error("the cached list changed without a rescan")
	}
	body := ts.post("/projects/rescan", "", "HX-Request", "true").expectStatus(http.StatusOK).Body
	if got := attrOf(t, testid(t, body, "projects"), "data-count"); got != "2" {
		t.Errorf("data-count = %q after a rescan", got)
	}
}

// §10: the favorite toggle uses aria-pressed to expose state, and adding then
// removing returns to where it started.
func TestFavoriteToggle(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	ts.get("/p/" + id + "/board") // so it is on Home to toggle

	toggle := testid(t, ts.get("/").Body, "project-card-favorite-toggle")
	if got := attrOf(t, toggle, "aria-pressed"); got != "false" {
		t.Errorf("aria-pressed = %q before favouriting", got)
	}

	ts.form(http.MethodPost, "/p/"+id+"/favorite", url.Values{}).expectStatus(http.StatusOK)

	home := ts.get("/").Body
	if got := attrOf(t, testid(t, home, "favorites-list"), "data-count"); got != "1" {
		t.Errorf("favorites-list data-count = %q after favouriting", got)
	}
	if got := attrOf(t, testid(t, home, "project-card-"+id), "data-favorite"); got != "true" {
		t.Errorf("the card says data-favorite=%q", got)
	}

	// And it is written where the TUI will look for it (§10).
	favorites, err := mm.LoadProjectList(mm.NewSystemPaths(ts.ConfigHome).Favorites, mm.ListFavorites)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites.Entries) != 1 || favorites.Entries[0].ProjectID != id {
		t.Errorf("favorites.json holds %+v", favorites.Entries)
	}

	ts.form(http.MethodPost, "/p/"+id+"/favorite", url.Values{})
	if got := attrOf(t, testid(t, ts.get("/").Body, "favorites-list"), "data-count"); got != "0" {
		t.Errorf("favorites-list data-count = %q after un-favouriting", got)
	}
}

// §10 rule 5: an entry whose path no longer resolves is rendered with
// data-missing="true" and MUST NOT be silently removed.
func TestMissingProjectIsRenderedNotDropped(t *testing.T) {
	ts := newTestServer(t)
	gone := t.TempDir() + "/unmounted/micro-manager"

	if err := mm.UpdateProjectList(mm.NewSystemPaths(ts.ConfigHome).Favorites,
		mm.ListFavorites, func(l *mm.ProjectList) error {
			l.Add(mm.ListEntry{Path: gone, Name: "On a drive that is not here"})
			return nil
		}); err != nil {
		t.Fatal(err)
	}

	body := ts.get("/").Body
	if got := attrOf(t, testid(t, body, "favorites-list"), "data-count"); got != "1" {
		t.Fatalf("favorites-list data-count = %q; the entry was dropped", got)
	}
	id, err := mm.ProjectID(gone)
	if err != nil {
		t.Fatal(err)
	}
	card := testid(t, body, "project-card-"+id)
	if got := attrOf(t, card, "data-missing"); got != "true" {
		t.Errorf("data-missing = %q", got)
	}
	if !strings.Contains(body, "not reachable") {
		t.Error("the card does not say it is unreachable")
	}
}

// §9.5: with no scan.roots the UI must surface a prompt to configure them, and
// must not have scanned $HOME or / to fill the gap.
func TestHomePromptsForScanRoots(t *testing.T) {
	ts := newTestServer(t)
	ts.registry.scan.Roots = nil

	body := ts.get("/").Body
	if !hasTestid(body, "home-needs-roots") {
		t.Error("Home does not prompt for scan roots")
	}
}
