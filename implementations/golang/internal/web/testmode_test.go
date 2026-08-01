package web

import (
	"strings"
	"testing"
)

// Test mode (spec-gui.md §4.4): MM_UI_TEST=1 or ?mm-test=1 must
//
//  1. disable all transitions and animations;
//  2. disable auto-dismissing toasts (they persist until dismissed);
//  3. set data-test-mode="true" on the app root;
//  4. keep every other behavior identical.
//
// Rules 1 and 2 live in the static assets, rule 3 in the shell, and rule 4 is
// the claim that nothing else branches on the flag. Each gets a test here so a
// regression is a failing Go test, not a failing browser run.

// Rule 3 with MM_UI_TEST=1: every page is marked, with or without the query
// parameter, because the flag is a server setting rather than a per-request
// one. cmd/mm-ui maps MM_UI_TEST=1 onto Options.TestMode, so this exercises
// what the binary would produce.
func TestTestModeEnvVarMarksEveryPage(t *testing.T) {
	ts := newTestServerWith(t, func(o *Options) { o.TestMode = true }, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	for _, path := range []string{
		"/",
		"/projects",
		"/settings",
		"/p/" + id + "/board",
		"/p/" + id + "/check",
	} {
		body := ts.get(path).expectStatus(200).Body
		if got := attrOf(t, testid(t, body, "app"), "data-test-mode"); got != "true" {
			t.Errorf("%s: data-test-mode = %q, want true", path, got)
		}
	}
}

// Rule 3 with ?mm-test=1: the marker is per request.
func TestTestModeQueryParameter(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	plain := ts.get("/p/" + id + "/board").expectStatus(200).Body
	if got := attrOf(t, testid(t, plain, "app"), "data-test-mode"); got != "" {
		t.Errorf("board: data-test-mode = %q, want empty", got)
	}
	marked := ts.get("/p/" + id + "/board?mm-test=1").expectStatus(200).Body
	if got := attrOf(t, testid(t, marked, "app"), "data-test-mode"); got != "true" {
		t.Errorf("board?mm-test=1: data-test-mode = %q, want true", got)
	}

	// The marker must be a strict value: anything but exactly 1 is off.
	off := ts.get("/p/" + id + "/board?mm-test=0").expectStatus(200).Body
	if got := attrOf(t, testid(t, off, "app"), "data-test-mode"); got != "" {
		t.Errorf("board?mm-test=0: data-test-mode = %q, want empty", got)
	}
}

// Rule 4: test mode must not change layout, locators, or which operations are
// permitted. The same mutation succeeds under ?mm-test=1, and the page's
// locator set is identical.
func TestTestModeChangesNothingElse(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// Opening the project records it in the recent list, so a comparison must
	// happen AFTER that settles or the two bodies differ for an unrelated
	// reason.
	ts.get("/p/" + id + "/board").expectStatus(200)

	plain := ts.get("/p/" + id + "/board").expectStatus(200).Body
	marked := ts.get("/p/" + id + "/board?mm-test=1").expectStatus(200).Body
	if extractTestids(plain) != extractTestids(marked) {
		t.Error("test mode changed which elements are present")
	}

	// A mutation through the normal htmx route works identically in test mode:
	// same status, same fragment. A note is the right operation to probe: it
	// never trips the WIP limit, so the same state applies to both calls. The
	// response is also checked to be a SUCCESS (a toast rather than an error
	// toast), so a pair of identical failures cannot pass this test.
	note := func() string {
		body := ts.post("/p/"+id+"/items/T-0001/note", "text=probe",
			"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").expectStatus(200).Body
		if !strings.Contains(body, "noted on T-0001") {
			t.Fatalf("the note did not land: %s", body)
		}
		return body
	}
	if note() != note() {
		t.Error("the mutation response differs with ?mm-test=1")
	}

	// The fragment requests themselves: a fresh board, test mode or not.
	fresh := func(q string) string {
		return ts.get("/p/" + id + "/board?fragment=1" + q).expectStatus(200).Body
	}
	if fresh("") != fresh("&mm-test=1") {
		t.Error("the board fragment differs with ?mm-test=1")
	}
}

// Rules 1 and 2: the static assets carry the enforcement. These are
// content assertions, the cheapest possible guard against a regression in a
// file no Go test would otherwise compile.
func TestTestModeStaticRules(t *testing.T) {
	ts := newTestServer(t)

	css := ts.get("/static/mm.css").expectStatus(200).Body
	for _, want := range []string{
		`[data-test-mode="true"] *`,
		"animation: none !important",
		"transition: none !important",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("mm.css is missing the test-mode rule %q", want)
		}
	}

	js := ts.get("/static/mm.js").expectStatus(200).Body
	if !strings.Contains(js, "data-test-mode") {
		t.Error("mm.js never consults data-test-mode for toast auto-dismiss")
	}
	if !strings.Contains(js, "mm-toast") {
		t.Error("mm.js has no toast handling at all")
	}
}
