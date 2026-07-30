package mm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Every sentinel must be distinct: callers branch on identity, and two
// sentinels that compare equal would silently merge two remedies into one.
func TestSentinelsAreDistinct(t *testing.T) {
	all := map[string]error{
		"NotFound":           ErrNotFound,
		"Ambiguous":          ErrAmbiguous,
		"InvalidArgument":    ErrInvalidArgument,
		"Conflict":           ErrConflict,
		"WipLimitReached":    ErrWipLimitReached,
		"PreconditionFail":   ErrPreconditionFailed,
		"InvariantViolation": ErrInvariantViolation,
		"Concurrent":         ErrConcurrent,
		"IO":                 ErrIO,
		"AlreadyExists":      ErrAlreadyExists,
	}
	for na, a := range all {
		for nb, b := range all {
			if na == nb {
				continue
			}
			if errors.Is(a, b) {
				t.Errorf("%s and %s are not distinct", na, nb)
			}
		}
	}
}

func TestWrappedErrorsMatchTheirSentinel(t *testing.T) {
	err := fmt.Errorf("%w: T-0042 is not in backlog.md", ErrNotFound)
	if !errors.Is(err, ErrNotFound) {
		t.Error("wrapped error should match its sentinel")
	}
	if errors.Is(err, ErrConflict) {
		t.Error("wrapped error should not match a different sentinel")
	}
}

func TestInvariantError(t *testing.T) {
	e := &InvariantError{Violations: []Violation{
		{Invariant: "I1", At: Location{File: "done.md", Line: 14},
			Message: "T-0003 is already defined at backlog.md:23"},
		{Invariant: "I9", At: Location{File: "details/T-0001.md", Line: 4},
			Message: "frontmatter title does not match the item line"},
	}}

	var err error = e
	if !errors.Is(err, ErrInvariantViolation) {
		t.Fatal("InvariantError should unwrap to ErrInvariantViolation")
	}
	got, ok := AsInvariantError(err)
	if !ok || len(got.Violations) != 2 {
		t.Fatalf("AsInvariantError lost the violations: %+v %v", got, ok)
	}
	// The list must survive being wrapped further up a call stack.
	wrapped := fmt.Errorf("start T-0042: %w", err)
	got, ok = AsInvariantError(wrapped)
	if !ok || len(got.Violations) != 2 {
		t.Fatal("violations lost through an extra wrap")
	}
	msg := e.Error()
	if !strings.Contains(msg, "2 invariant violations") ||
		!strings.Contains(msg, "done.md:14") ||
		!strings.Contains(msg, "details/T-0001.md:4") {
		t.Errorf("message should locate every violation, got:\n%s", msg)
	}
}

func TestInvariantErrorSingular(t *testing.T) {
	e := &InvariantError{Violations: []Violation{
		{Invariant: "I5", At: Location{File: "backlog.md", Line: 19},
			Message: "T-0002 is under Blocked with no blocked: field"},
	}}
	if strings.Contains(e.Error(), "1 invariant violations") {
		t.Errorf("single violation should not be pluralised: %q", e.Error())
	}
}

// The message is most of this error's value. spec-tools.md §5.1.8 requires the
// limit, the occupants, and the three remedies.
func TestWipLimitErrorMessage(t *testing.T) {
	e := &WipLimitError{Limit: 3, Occupants: []Slot{
		{Number: 1, Width: 2, File: "working.01.md",
			Item: &Item{ID: "T-0018", Title: "Migrate the build cache"}},
		{Number: 2, Width: 2, File: "working.02.md",
			Item: &Item{ID: "T-0027", Title: "Fix flaky auth test"}},
		{Number: 3, Width: 2, File: "working.03.md",
			Item: &Item{ID: "T-0031", Title: "Rewrite the deploy docs"}},
	}}

	var err error = e
	if !errors.Is(err, ErrWipLimitReached) {
		t.Fatal("should unwrap to ErrWipLimitReached")
	}
	// It must NOT collapse into the generic conflict error, or callers lose the
	// one error that has a routine remedy.
	if errors.Is(err, ErrConflict) {
		t.Fatal("WipLimitReached must stay distinct from Conflict")
	}
	if _, ok := AsWipLimitError(fmt.Errorf("start: %w", err)); !ok {
		t.Fatal("occupants lost through a wrap")
	}

	msg := e.Error()
	for _, want := range []string{"3/3", "slot 01", "T-0018", "Migrate the build cache",
		"T-0027", "T-0031", "finish one", "pause one", "--wip"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestWipLimitErrorSkipsIdleSlots(t *testing.T) {
	e := &WipLimitError{Limit: 2, Occupants: []Slot{
		{Number: 1, Width: 2, File: "working.01.md", Item: &Item{ID: "T-0001", Title: "x"}},
		{Number: 2, Width: 2, File: "working.02.md"}, // idle, must not panic
	}}
	if !strings.Contains(e.Error(), "T-0001") {
		t.Error("occupied slot missing from message")
	}
}

func TestParseError(t *testing.T) {
	err := parseErrf("backlog.md", 19, "malformed item line: %s", "- [ ] nope")
	if !errors.Is(err, ErrInvalidArgument) {
		t.Error("ParseError should unwrap to ErrInvalidArgument")
	}
	want := "backlog.md:19: malformed item line: - [ ] nope"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
	var pe *ParseError
	if !errors.As(err, &pe) || pe.At.Line != 19 {
		t.Error("ParseError should carry its location")
	}
}
