package web

import (
	"regexp"
	"strings"
	"testing"
)

// ddText reads the text inside a <dd data-testid="..."> element.
func ddText(t *testing.T, body, id string) string {
	t.Helper()
	re := regexp.MustCompile(`<dd[^>]*data-testid="` + regexp.QuoteMeta(id) + `"[^>]*>([^<]*)</dd>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no dd with data-testid=%q\n%s", id, body)
	}
	return m[1]
}

// /about (spec-gui.md §4.1): version info, rendered with no project open, like
// /settings. The three spec versions are the requirement; the build version is
// shown too because a user who is told the service is broken wants to know
// which build that is.

func TestAboutView(t *testing.T) {
	ts := newTestServer(t)

	body := ts.get("/about").expectStatus(200).Body
	for _, tc := range []struct {
		testid, want string
	}{
		{"x-about-service", "micro-manager"},
		{"x-about-version", Version},
		{"x-about-spec-ui", SpecUIVersion},
		{"x-about-spec-tools", SpecToolsVersion},
		{"x-about-spec-format", SpecFormatVersion},
	} {
		if got := ddText(t, body, tc.testid); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.testid, got, tc.want)
		}
	}
}

// The spec versions are about the documents, not the build: a version bump of
// the service must not change what it says it implements. The constants are
// separate on purpose (internal/web/version.go), so the cheapest assertion here
// is that the view reads the constants and not a copy.
func TestAboutShowsSeparateSpecVersions(t *testing.T) {
	if SpecUIVersion == Version || SpecToolsVersion == Version || SpecFormatVersion == Version {
		t.Fatalf("spec versions must not track the build version: ui=%q tools=%q format=%q build=%q",
			SpecUIVersion, SpecToolsVersion, SpecFormatVersion, Version)
	}
}

// The about route is global: no project, no query, just the page. A fragment
// request (htmx nav from a project page) must render it too.
func TestAboutFragmentAndNav(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// From a project page the nav links to the global /about.
	board := ts.get("/p/" + id + "/board").expectStatus(200).Body
	if got := attrOf(t, testid(t, board, "nav-about"), "href"); got != "/about" {
		t.Errorf("nav-about href = %q, want /about", got)
	}
	if strings.Contains(testid(t, board, "nav-about"), "aria-current") {
		t.Error("nav-about is marked current on the board page")
	}

	// The fragment request renders the same view for an htmx swap.
	marked := ts.get("/about", "HX-Request", "true").expectStatus(200).Body
	if !strings.Contains(marked, `data-testid="x-about"`) {
		t.Error("the /about fragment does not render the about view")
	}
	// And on the about page itself, nav-about is current.
	about := ts.get("/about").expectStatus(200).Body
	if !strings.Contains(testid(t, about, "nav-about"), "aria-current") {
		t.Error("nav-about is not marked current on the about page")
	}
}
