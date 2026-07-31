package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"micromanager/mm"
)

// The app shell of spec-gui.md §5.2 is present on EVERY route, so the error
// view - the only page this item ships - is a fair place to assert it.

// testid finds an element by data-testid and returns its opening tag.
func testid(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`<[a-zA-Z]+[^>]*data-testid="` + regexp.QuoteMeta(id) + `"[^>]*>`)
	m := re.FindString(body)
	if m == "" {
		t.Fatalf("no element with data-testid=%q\n%s", id, body)
	}
	return m
}

func hasTestid(body, id string) bool {
	return regexp.MustCompile(`data-testid="` + regexp.QuoteMeta(id) + `"`).MatchString(body)
}

// attrOf reads an attribute from an opening tag.
func attrOf(t *testing.T, tag, name string) string {
	t.Helper()
	m := regexp.MustCompile(name + `="([^"]*)"`).FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	return m[1]
}

// §5.2 lists these by name. An implementation MUST NOT rename, omit, or
// conditionally drop any of them.
func TestAppShellTestids(t *testing.T) {
	ts := newTestServer(t)
	body := ts.get("/p/unknown/board").expectStatus(http.StatusNotFound).Body

	required := []string{
		"app", "app-header", "app-main", "app-nav", "app-status",
		"brand", "brand-logo", "brand-name",
		"project-switcher", "project-switcher-menu", "project-switcher-favorites",
		"project-switcher-recent", "project-switcher-all",
		"search-input", "nav-board", "nav-report", "nav-check", "nav-settings",
		"status-wip", "status-counts", "status-check",
		"toast-region", "dialog-root",
	}
	for _, id := range required {
		if !hasTestid(body, id) {
			t.Errorf("the shell is missing data-testid=%q", id)
		}
	}
}

// data-busy="false" is the quiescence signal an external suite waits on (§5.1).
// A rendered response IS the view fully reflecting server state.
func TestAppRootAttributes(t *testing.T) {
	ts := newTestServer(t)
	tag := testid(t, ts.get("/p/unknown/board").Body, "app")

	if got := attrOf(t, tag, "data-busy"); got != "false" {
		t.Errorf("data-busy = %q, want false on a rendered response", got)
	}
	if got := attrOf(t, tag, "data-theme-name"); got == "" {
		t.Error("data-theme-name is empty")
	}
	if got := attrOf(t, tag, "data-theme-source"); got != string(mm.ThemeSourceBuiltin) {
		t.Errorf("data-theme-source = %q, want builtin with no theme files", got)
	}
}

// §4.4: MM_UI_TEST=1 or ?mm-test=1 sets data-test-mode="true" and changes
// nothing else - not layout, not locators, not which operations are permitted.
func TestTestMode(t *testing.T) {
	ts := newTestServer(t)

	plain := ts.get("/p/unknown/board").Body
	if attrOf(t, testid(t, plain, "app"), "data-test-mode") != "" {
		t.Error("data-test-mode is set without being asked for")
	}

	marked := ts.get("/p/unknown/board?mm-test=1").Body
	if got := attrOf(t, testid(t, marked, "app"), "data-test-mode"); got != "true" {
		t.Errorf("data-test-mode = %q with ?mm-test=1", got)
	}
	// Locators are unchanged: the same testids in the same order.
	if extractTestids(plain) != extractTestids(marked) {
		t.Error("test mode changed which elements are present")
	}
}

func extractTestids(body string) string {
	all := regexp.MustCompile(`data-testid="([^"]*)"`).FindAllStringSubmatch(body, -1)
	var out []string
	for _, m := range all {
		out = append(out, m[1])
	}
	return strings.Join(out, ",")
}

// §8.4: every color and gui token is exposed as a custom property on the app
// root, and all styling resolves through them.
func TestThemePropertiesAreInlined(t *testing.T) {
	ts := newTestServer(t)
	body := ts.get("/p/unknown/board").Body

	for _, token := range mm.ColorTokens() {
		name := mm.CSSPropertyName("color." + token)
		if !strings.Contains(body, name+":") {
			t.Errorf("the served page defines no %s", name)
		}
	}
	for _, token := range mm.GUITokens() {
		name := mm.CSSPropertyName("gui." + token)
		if !strings.Contains(body, name+":") {
			t.Errorf("the served page defines no %s", name)
		}
	}

	// Inlined rather than fetched, which is what makes "no flash of the previous
	// theme on a project switch" true by construction (§8.7).
	if !strings.Contains(body, `<style>`) {
		t.Error("the theme is not inlined in the document")
	}
}

// A project theme re-themes the UI, and the app root says where the theme came
// from (§8.7).
func TestProjectThemeChangesTheAppRoot(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	dir := ts.Dirs[0]

	id, err := mm.ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, mm.ProjectThemePath(dir), `{
	  "schemaVersion": 1, "id": "proj", "name": "Project Theme",
	  "color": { "accent": { "base": "#ff00ff" } }
	}`)

	body := ts.get("/p/" + id + "/board").Body
	tag := testid(t, body, "app")

	if got := attrOf(t, tag, "data-theme-source"); got != string(mm.ThemeSourceProject) {
		t.Errorf("data-theme-source = %q, want project", got)
	}
	if got := attrOf(t, tag, "data-theme-name"); got != "Project Theme" {
		t.Errorf("data-theme-name = %q", got)
	}
	if !strings.Contains(body, "--mm-color-accent-base:#ff00ff") {
		t.Error("the project's accent colour did not reach the app root")
	}
}

// Theme values are user data and template.CSS bypasses escaping, so a value
// that tries to close the style element is dropped.
func TestThemeValuesCannotEscapeTheStyleElement(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	dir := ts.Dirs[0]
	id, err := mm.ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}

	// A gui token takes any string, which is the opening an injection would use.
	writeJSONFile(t, mm.ProjectThemePath(dir), `{
	  "schemaVersion": 1, "id": "evil",
	  "gui": { "font": { "family": { "ui": "x</style><script>alert(1)</script>" } } }
	}`)

	body := ts.get("/p/" + id + "/board").Body
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("a theme value escaped the style element:\n%s", body)
	}
}

// §5.2: the current nav item carries aria-current="page".
func TestNavCurrentItem(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id, err := mm.ProjectID(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + id + "/board").Body
	// The board view arrives with its own item; until then this asserts the
	// shape of the shell's links, which is what nav-* has to get right.
	for _, key := range []string{"board", "report", "check", "settings"} {
		tag := testid(t, body, "nav-"+key)
		want := "/p/" + id + "/" + key
		if got := attrOf(t, tag, "href"); got != want {
			t.Errorf("nav-%s href = %q, want %q", key, got, want)
		}
	}
}

// With no project open the nav links still exist - §5.1 forbids dropping a
// required testid conditionally - and point somewhere useful.
func TestNavWithNoProject(t *testing.T) {
	ts := newTestServer(t)
	body := ts.get("/p/unknown/board").Body

	for _, key := range []string{"board", "report", "check", "settings"} {
		tag := testid(t, body, "nav-"+key)
		if got := attrOf(t, tag, "href"); got != "/projects" {
			t.Errorf("nav-%s href = %q with no project open", key, got)
		}
	}
	tag := testid(t, body, "app")
	if attrOf(t, tag, "data-project-id") != "" {
		t.Error("data-project-id is set with no project open")
	}
}

// §5.2: status-check MUST carry data-violations with the count, so a suite can
// assert cleanliness without opening the check view.
func TestStatusBar(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id, err := mm.ProjectID(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + id + "/board").Body
	check := testid(t, body, "status-check")
	if got := attrOf(t, check, "data-violations"); got != "0" {
		t.Errorf("data-violations = %q on a clean fixture", got)
	}
	if got := attrOf(t, check, "data-state"); got != "ok" {
		t.Errorf("data-state = %q on a clean fixture", got)
	}

	wip := testid(t, body, "status-wip")
	if attrOf(t, wip, "data-wip-limit") == "" || attrOf(t, wip, "data-wip-used") == "" {
		t.Errorf("status-wip carries no counts: %s", wip)
	}
}

// A directory that violates its invariants must still render, with the count
// visible - this is exactly when a person needs the UI (spec-tools.md §8).
func TestStatusBarOnABrokenDirectory(t *testing.T) {
	ts := newTestServer(t, "broken-i9-title-drift")
	id, err := mm.ProjectID(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + id + "/board").Body
	check := testid(t, body, "status-check")
	if got := attrOf(t, check, "data-violations"); got == "0" || got == "" {
		t.Errorf("data-violations = %q on a broken fixture", got)
	}
	if got := attrOf(t, check, "data-state"); got != "error" {
		t.Errorf("data-state = %q on a broken fixture", got)
	}
}

// §4.1 rule 4: an unknown projectId renders the not-found view with 404. Never
// a redirect: that would tell a bookmark the project moved.
func TestUnknownProjectRendersNotFound(t *testing.T) {
	ts := newTestServer(t)

	r := ts.get("/p/ffffffffffff/board")
	if r.Status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", r.Status)
	}
	if loc := r.Header.Get("Location"); loc != "" {
		t.Errorf("the response redirects to %q", loc)
	}
	if !hasTestid(r.Body, "error") {
		t.Error("the not-found view was not rendered")
	}
	if !hasTestid(r.Body, "app") {
		t.Error("the not-found view is outside the app shell")
	}
}

// htmx is vendored, not fetched: the service binds to loopback and must work
// with no network at all (project/architecture.md §3).
func TestVendoredScriptsAreServed(t *testing.T) {
	ts := newTestServer(t)

	body := ts.get("/p/unknown/board").Body
	for _, src := range []string{"/static/htmx.min.js", "/static/htmx-ext-sse.js", "/static/mm.js"} {
		if !strings.Contains(body, src) {
			t.Errorf("the shell does not load %s", src)
		}
		if got := ts.get(src).Status; got != http.StatusOK {
			t.Errorf("%s returned %d", src, got)
		}
	}
	// The SSE extension must load AFTER htmx core.
	if strings.Index(body, "/static/htmx.min.js") > strings.Index(body, "/static/htmx-ext-sse.js") {
		t.Error("htmx-ext-sse is loaded before htmx core")
	}
	if strings.Contains(body, "//unpkg.com") || strings.Contains(body, "//cdn.") {
		t.Error("the shell references a CDN")
	}
}

// writeJSONFile writes a config or theme file beside a fixture.
func writeJSONFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
