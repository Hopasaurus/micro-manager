package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// spec-gui.md §4.3 fixes this table. Three distinct errors map to 409, which is
// exactly why the body must carry the code: a client cannot branch on the status.
func TestErrorStatusMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("%w: T-0042", mm.ErrNotFound), http.StatusNotFound, "NotFound"},
		{fmt.Errorf("%w: two candidates", mm.ErrAmbiguous), http.StatusConflict, "Ambiguous"},
		{fmt.Errorf("%w: bad date", mm.ErrInvalidArgument), http.StatusBadRequest, "InvalidArgument"},
		{fmt.Errorf("%w: already there", mm.ErrConflict), http.StatusConflict, "Conflict"},
		{fmt.Errorf("%w: needs force", mm.ErrPreconditionFailed), http.StatusPreconditionFailed, "PreconditionFailed"},
		{fmt.Errorf("%w: changed underneath", mm.ErrConcurrent), http.StatusConflict, "Concurrent"},
		{fmt.Errorf("%w: disk", mm.ErrIO), http.StatusInternalServerError, "Io"},
		{&mm.WipLimitError{Limit: 2}, http.StatusConflict, "WipLimitReached"},
		{&mm.InvariantError{Violations: []mm.Violation{{Invariant: "I9"}}}, http.StatusUnprocessableEntity, "InvariantViolation"},
	}

	for _, c := range cases {
		status, code := statusFor(c.err)
		if status != c.status || code != c.code {
			t.Errorf("%v -> %d %s, want %d %s", c.err, status, code, c.status, c.code)
		}
	}
}

// The three 409s must be distinguishable by code alone.
func TestTheThreeConflictsAreDistinguishable(t *testing.T) {
	seen := map[string]bool{}
	for _, err := range []error{
		fmt.Errorf("%w: x", mm.ErrConflict),
		fmt.Errorf("%w: x", mm.ErrConcurrent),
		&mm.WipLimitError{Limit: 1},
	} {
		status, code := statusFor(err)
		if status != http.StatusConflict {
			t.Fatalf("%v is %d, expected all three to be 409", err, status)
		}
		if seen[code] {
			t.Errorf("code %s is used twice; a client cannot tell them apart", code)
		}
		seen[code] = true
	}
}

// §4.3: the WipLimitReached body carries wipUsed, wipLimit and the occupants -
// the data dialog-wip-limit renders its slot rows and its three remedies from.
func TestWipLimitEnvelope(t *testing.T) {
	item := mm.Item{ID: "T-0042", Title: "Fix the deploy script"}
	err := &mm.WipLimitError{
		Limit: 2,
		Occupants: []mm.Slot{
			{Number: 1, Width: 2, Item: &item},
			{Number: 2, Width: 2}, // idle
		},
	}

	env := envelopeFor(err)
	if env.OK {
		t.Error("an error envelope must not report ok")
	}
	if env.WipLimit != 2 {
		t.Errorf("wipLimit = %d", env.WipLimit)
	}
	if env.WipUsed != 1 {
		t.Errorf("wipUsed = %d, want the occupied slots only", env.WipUsed)
	}
	if len(env.Occupants) != 1 || env.Occupants[0].ID != "T-0042" || env.Occupants[0].Slot != 1 {
		t.Errorf("occupants = %+v", env.Occupants)
	}
	if env.Errors[0].Code != codeWipLimitReached {
		t.Errorf("code = %s", env.Errors[0].Code)
	}
}

// §4.3: the InvariantViolation body carries the violation list, in the shape the
// check view renders - data-invariant, data-file, data-line.
func TestInvariantEnvelope(t *testing.T) {
	err := &mm.InvariantError{Violations: []mm.Violation{
		{Invariant: "I9", Message: "title drift", At: mm.Location{File: "details/T-0001.md", Line: 4}},
		{Invariant: "I1", Message: "duplicate id", At: mm.Location{File: "backlog.md", Line: 12}},
	}}

	env := envelopeFor(err)
	if len(env.Errors) != 2 {
		t.Fatalf("want one entry per violation, got %d", len(env.Errors))
	}
	if env.Errors[0].Invariant != "I9" || env.Errors[0].File != "details/T-0001.md" || env.Errors[0].Line != 4 {
		t.Errorf("first violation = %+v", env.Errors[0])
	}
	for _, e := range env.Errors {
		if e.Code != codeInvariantViolation {
			t.Errorf("violation carries code %s", e.Code)
		}
	}
}

// The message should read as itself, not as the sentinel's text repeated. The
// code already says which error it is.
func TestMessagesDropTheSentinelPrefix(t *testing.T) {
	err := fmt.Errorf("%w: T-0042 is not in this directory", mm.ErrNotFound)
	if got := cleanMessage(err); got != "T-0042 is not in this directory" {
		t.Errorf("message = %q", got)
	}
}

// Under /api/v1 the body is the JSON envelope of §4.3, on every failure.
func TestAPIErrorsAreJSON(t *testing.T) {
	ts := newTestServer(t)

	r := ts.get("/api/v1/nothing").expectStatus(http.StatusNotFound)
	var env errorEnvelope
	r.json(&env)

	if env.OK {
		t.Error("ok must be false")
	}
	if len(env.Errors) == 0 || env.Errors[0].Code != codeNotFound {
		t.Errorf("errors = %+v, want a NotFound code", env.Errors)
	}
	if env.Errors[0].Message == "" {
		t.Error("the entry carries no message")
	}
}

// The guards' refusals go through the same handler and carry a code too, so a
// client branches on the name rather than on 421 versus 403.
func TestGuardRefusalsCarryCodes(t *testing.T) {
	ts := newTestServer(t)

	var env errorEnvelope
	ts.get("/api/v1/health", "Host", "evil.example.com").
		expectStatus(http.StatusMisdirectedRequest).json(&env)
	if len(env.Errors) == 0 || env.Errors[0].Code != "HostNotAllowed" {
		t.Errorf("errors = %+v", env.Errors)
	}

	env = errorEnvelope{}
	ts.post("/api/v1/scan", "", "Origin", "https://evil.example.com").
		expectStatus(http.StatusForbidden).json(&env)
	if len(env.Errors) == 0 || env.Errors[0].Code != "OriginNotAllowed" {
		t.Errorf("errors = %+v", env.Errors)
	}
}

// An htmx request gets HTML it can swap, never JSON, and it carries the error
// code so the client can behave correctly whatever the presentation (§5.10).
//
// Navigating to a project that is not there renders the not-found VIEW rather
// than a toast: the user asked to go somewhere, and the answer is a page saying
// it is not here. Toasts are for operations that failed, and those arrive with
// the mutating routes (T-0058).
func TestHtmxErrorsAreFragments(t *testing.T) {
	ts := newTestServer(t)

	r := ts.get("/p/unknown/board", "HX-Request", "true")
	if r.Status != http.StatusNotFound {
		t.Fatalf("status = %d\nbody: %s", r.Status, r.Body)
	}
	if strings.HasPrefix(strings.TrimSpace(r.Body), "{") {
		t.Errorf("an htmx request got JSON: %s", r.Body)
	}
	if !strings.Contains(r.Body, codeNotFound) {
		t.Errorf("the fragment does not carry the error code: %s", r.Body)
	}
	if strings.Contains(r.Body, "<!DOCTYPE") {
		t.Errorf("an htmx request got a whole document: %s", r.Body)
	}
}

// A route that never reaches a view - an unrouted path under /api - still goes
// through the error handler, and an htmx caller gets a toast from it.
func TestHtmxToastCarriesSeverity(t *testing.T) {
	ts := newTestServer(t)

	r := ts.get("/nothing-here", "HX-Request", "true")
	if r.Status != http.StatusNotFound {
		t.Fatalf("status = %d\nbody: %s", r.Status, r.Body)
	}
	if !strings.Contains(r.Body, `data-severity="danger"`) {
		t.Errorf("the toast carries no severity: %s", r.Body)
	}
	if !strings.Contains(r.Body, codeNotFound) {
		t.Errorf("the toast does not carry the error code: %s", r.Body)
	}
}

// A panic is a 500 through Recover, and the envelope still comes out well
// formed - the service is long-lived and the next request must still work.
func TestPanicProducesAnEnvelope(t *testing.T) {
	ts := newTestServer(t)
	ts.echo.GET("/api/v1/x-panic", func(*echo.Context) error { panic("boom") })

	var env errorEnvelope
	ts.get("/api/v1/x-panic").expectStatus(http.StatusInternalServerError).json(&env)
	if len(env.Errors) == 0 || env.Errors[0].Code == "" {
		t.Errorf("a panic produced %+v", env)
	}
}
