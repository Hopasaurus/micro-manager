package web

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// §5.8 fixes these testids, and data-invariant MUST carry the I1-I10 identifier
// from the format spec.
func TestCheckView(t *testing.T) {
	ts, id := boardServer(t, "broken-i9-title-drift")
	body := ts.get("/p/" + id + "/check").expectStatus(http.StatusOK).Body

	for _, testid := range []string{"check", "check-run", "check-results", "check-violation-0"} {
		if !hasTestid(body, testid) {
			t.Errorf("the check view is missing data-testid=%q", testid)
		}
	}

	check := testid(t, body, "check")
	if got := attrOf(t, check, "data-violations"); got == "" || got == "0" {
		t.Errorf("data-violations = %q on a broken fixture", got)
	}

	v := testid(t, body, "check-violation-0")
	if got := attrOf(t, v, "data-invariant"); !regexp.MustCompile(`^I([1-9]|10)$`).MatchString(got) {
		t.Errorf("data-invariant = %q, want an I1-I10 identifier", got)
	}
	if attrOf(t, v, "data-file") == "" {
		t.Errorf("a violation carries no data-file: %s", v)
	}
}

// A directory with violations is a 200: the findings ARE the answer, not an
// error (spec-tools.md §5.1.12).
func TestCheckReportsRatherThanFails(t *testing.T) {
	for _, f := range []string{
		"broken-i1-duplicate-id", "broken-i5-blocked-without-reason",
		"broken-i6-done-without-outcome", "broken-i10-slot-gap",
	} {
		t.Run(f, func(t *testing.T) {
			ts, id := boardServer(t, f)
			body := ts.get("/p/" + id + "/check").expectStatus(http.StatusOK).Body

			check := testid(t, body, "check")
			if got := attrOf(t, check, "data-violations"); got == "0" {
				t.Errorf("%s reported no violations", f)
			}
			if !hasTestid(body, "check-violation-0") {
				t.Error("no violation was listed")
			}
		})
	}
}

// A clean directory says so, with a count of zero rather than an empty list the
// user has to interpret.
func TestCheckOnACleanDirectory(t *testing.T) {
	ts, id := boardServer(t, "clean-full")
	body := ts.get("/p/" + id + "/check").Body

	if got := attrOf(t, testid(t, body, "check"), "data-violations"); got != "0" {
		t.Errorf("data-violations = %q on a clean fixture", got)
	}
	if !hasTestid(body, "check-clean") {
		t.Error("a clean directory says nothing")
	}
	if hasTestid(body, "check-violation-0") {
		t.Error("a clean directory listed a violation")
	}
}

// §5.2: status-check reflects the last validation and carries the count, so a
// suite can assert cleanliness without opening this view. The two must agree.
func TestStatusBarAgreesWithTheCheckView(t *testing.T) {
	for _, f := range []string{"clean-full", "broken-i9-orphan-detail", "broken-i2-id-above-next-id"} {
		t.Run(f, func(t *testing.T) {
			ts, id := boardServer(t, f)

			board := ts.get("/p/" + id + "/board").Body
			check := ts.get("/p/" + id + "/check").Body

			fromStatus := attrOf(t, testid(t, board, "status-check"), "data-violations")
			fromView := attrOf(t, testid(t, check, "check"), "data-violations")
			if fromStatus != fromView {
				t.Errorf("status-check says %q, the check view says %q", fromStatus, fromView)
			}
		})
	}
}

// The view is the same call the CLI's --check makes, so the two cannot disagree
// about what is wrong with a directory.
func TestCheckMatchesTheLibrary(t *testing.T) {
	ts, id := boardServer(t, "broken-i8-missing-detail-file")

	store, err := mm.Open(ts.Dirs[0])
	if err != nil {
		t.Fatal(err)
	}
	want, err := store.Validate()
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + id + "/check").Body
	got := regexp.MustCompile(`data-testid="check-violation-\d+"`).FindAllString(body, -1)
	if len(got) != len(want) {
		t.Errorf("the view lists %d violations, the library reports %d", len(got), len(want))
	}
	for _, v := range want {
		if !strings.Contains(body, `data-invariant="`+v.Invariant+`"`) {
			t.Errorf("the view does not report %s", v.Invariant)
		}
	}
}

// Re-running picks up a change made outside the UI - by the CLI, or by an
// editor - which is the whole reason check-run exists rather than a page reload.
func TestCheckRunSeesAnExternalEdit(t *testing.T) {
	ts, id := boardServer(t, "clean-full")

	if got := attrOf(t, testid(t, ts.get("/p/"+id+"/check").Body, "check"), "data-violations"); got != "0" {
		t.Fatalf("the fixture starts at %q violations", got)
	}

	// An orphan detail file: I9 catches it, and nothing in the UI did it.
	orphan := filepath.Join(ts.Dirs[0], "details", "T-9999.md")
	if err := os.WriteFile(orphan, []byte("---\ndoc: detail\nid: T-9999\ntitle: Orphan\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/"+id+"/check", "HX-Request", "true").Body
	if got := attrOf(t, testid(t, body, "check"), "data-violations"); got == "0" {
		t.Error("re-running did not see the external edit")
	}
	if strings.Contains(body, "<!DOCTYPE") {
		t.Error("check-run got a whole document rather than a fragment")
	}
}
