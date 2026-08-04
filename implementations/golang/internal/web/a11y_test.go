package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Accessibility (spec-gui.md §11). Most of the section is structure that a
// conformance suite reads off the DOM; the tests here assert the pieces that
// are easy to regress silently and cheap to check in Go: the contrast warning
// in the theme editor, the focus ring CSS, the role/aria contract, the drag
// announcements, the dialog focus trap, and the reduced-motion rule.

// §11 rule 7: the editor warns when a pair fails and MUST NOT block saving.
func TestThemeEditorContrastWarnings(t *testing.T) {
	low := `{"id":"lowcontrast","name":"Low Contrast","appearance":"light",
	  "color":{"fg.default":"#111111","bg.base":"#111111",
	           "accent.fg":"#222222","accent.base":"#111111"}}`

	ts := newTestServerWith(t, func(o *Options) {
		o.Config.Theme.ID = "lowcontrast"
	}, "clean-full")
	libDir := filepath.Join(ts.ConfigHome, mm.ConfigDirName, "themes")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("mkdir themes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libDir, "lowcontrast.json"), []byte(low), 0o644); err != nil {
		t.Fatalf("write theme: %v", err)
	}

	// The editor page itself warns, so a suite does not need to type anything.
	body := ts.get("/settings/theme").expectStatus(200).Body
	if !strings.Contains(body, "below WCAG AA") {
		t.Fatalf("editor shows no contrast warning for a failing theme:\n%s", body)
	}
	if !strings.Contains(body, "--mm-color-fg-default") || !strings.Contains(body, "--mm-color-bg-base") {
		t.Error("the warning does not name the failing token pair")
	}
}

func TestThemeEditorContrastWarningsDoNotBlockSave(t *testing.T) {
	ts := newTestServerWith(t, func(o *Options) {
		o.Config.Theme.ID = "lowcontrast"
	}, "clean-full")

	form := strings.Join([]string{
		"id=lowcontrast",
		"name=Low+Contrast",
		"appearance=light",
		"token_fg.default=%23111111",
		"token_bg.base=%23111111",
		"token_accent.fg=%23222222",
		"token_accent.base=%23111111",
	}, "&")

	// Saving a failing theme SUCCEEDS (200, editor re-rendered) and the
	// response carries the warning. A 4xx would be the spec violation.
	res := ts.post("/settings/theme", form,
		"Content-Type", "application/x-www-form-urlencoded").expectStatus(200)
	if !strings.Contains(res.Body, "below WCAG AA") {
		t.Error("the save response does not warn about the failing pair")
	}
}

func TestThemeEditorCompliantThemeHasNoWarnings(t *testing.T) {
	ts := newTestServer(t) // builtin theme, no config home files

	body := ts.get("/settings/theme").expectStatus(200).Body
	if strings.Contains(body, "below WCAG AA") {
		t.Errorf("the built-in theme triggers a contrast warning:\n%s", body)
	}
}

// The structural rules of §11, asserted against what a suite would read: CSS
// for rules 1 and 6, JS behaviour for rules 4 and 5, and the rendered DOM for
// rules 2 and 3.
func TestAccessibilitySurface(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// Rule 1: focus ring from the border-focus token.
	css := ts.get("/static/mm.css").expectStatus(200).Body
	if !strings.Contains(css, ":focus-visible") || !strings.Contains(css, "--mm-color-border-focus") {
		t.Error("mm.css has no focus-visible ring drawn from --mm-color-border-focus")
	}
	// Rule 6: reduced motion kills non-essential animation.
	if !strings.Contains(css, "prefers-reduced-motion") {
		t.Error("mm.css has no prefers-reduced-motion rule")
	}

	// Rules 4 and 5: the behaviour lives in mm.js.
	js := ts.get("/static/mm.js").expectStatus(200).Body
	for _, want := range []string{
		"setAttribute('aria-label', text)", // drag announcements (§11 rule 4)
		"focusFirst",                       // dialog focus on open
		"closeDialog",                      // and restore on close
	} {
		if !strings.Contains(js, want) {
			t.Errorf("mm.js is missing %q", want)
		}
	}

	// Rules 2 and 3: the board page itself.
	body := ts.get("/p/" + id + "/board").expectStatus(200).Body
	for _, want := range []string{
		`data-testid="board"`,
		`aria-live="polite"`,
		`role="list"`,
		`role="listitem"`,
		`aria-label="Priority `,
		`aria-label="Actions for `,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("board page is missing %q", want)
		}
	}

	// The item panel is a dialog; the four mutation prompts and the conflict
	// dialog carry role=dialog and name themselves for assistive tech.
	panel := ts.get("/p/" + id + "/item/T-0001").expectStatus(200).Body
	if !strings.Contains(panel, `role="dialog"`) || !strings.Contains(panel, "aria-modal") {
		t.Error("the item panel is not role=dialog")
	}
}
