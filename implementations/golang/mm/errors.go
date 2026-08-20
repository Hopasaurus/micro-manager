package mm

import (
	"errors"
	"fmt"
	"strings"
)

// The error taxonomy from spec-tools.md §6.3.
//
// These are sentinels: callers match with errors.Is, and every error this
// package returns wraps exactly one of them. The mapping outward is not this
// package's business - the CLI maps them to exit codes (§10) and the UI service
// maps them to HTTP status (spec-gui.md §4.3). Neither mapping belongs here.
var (
	// ErrNotFound: an ID, slot, or directory does not exist.
	ErrNotFound = errors.New("not found")

	// ErrAmbiguous: directory resolution matched more than one candidate.
	// Ambiguity is never resolved by guessing.
	ErrAmbiguous = errors.New("ambiguous")

	// ErrInvalidArgument: a value fails the format spec - a bad date, an
	// unknown prio, a pipe in a title, a malformed tag list.
	ErrInvalidArgument = errors.New("invalid argument")

	// ErrConflict: the operation contradicts the item's current state, such as
	// starting an item that is already done, or finishing one twice.
	ErrConflict = errors.New("conflict")

	// ErrWipLimitReached: --start with every working slot occupied.
	//
	// Deliberately distinct from ErrConflict. It is the one error with a
	// routine, expected remedy, and callers branch on it to offer that remedy
	// rather than reporting a generic failure.
	ErrWipLimitReached = errors.New("wip limit reached")

	// ErrPreconditionFailed: a guard was not satisfied - remove without force,
	// or an explicitly named slot that is already occupied.
	ErrPreconditionFailed = errors.New("precondition failed")

	// ErrInvariantViolation: the requested change would produce a directory
	// that fails I1-I10. Nothing is written. Use AsInvariantError to recover
	// the violation list.
	ErrInvariantViolation = errors.New("invariant violation")

	// ErrConcurrent: the directory changed underneath the operation. The
	// caller must re-read before retrying; a blind retry would overwrite
	// somebody else's edit.
	ErrConcurrent = errors.New("concurrent modification")

	// ErrIO: a filesystem or permission failure.
	ErrIO = errors.New("io error")

	// ErrAlreadyExists: init against a directory that already holds files.
	ErrAlreadyExists = errors.New("already exists")

	// ErrVersionMismatch: a mutating operation was attempted against a
	// directory below this implementation's current format version
	// (spec-tools.md §5.3.4, spec-file-format.md §9). Read operations and
	// --migrate are exempt.
	ErrVersionMismatch = errors.New("version mismatch")
)

// InvariantError carries the violations that blocked a write.
type InvariantError struct {
	Violations []Violation
}

func (e *InvariantError) Error() string {
	if len(e.Violations) == 1 {
		return fmt.Sprintf("invariant violation: %s", e.Violations[0])
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d invariant violations:", len(e.Violations))
	for _, v := range e.Violations {
		fmt.Fprintf(&b, "\n  %s", v)
	}
	return b.String()
}

func (e *InvariantError) Unwrap() error { return ErrInvariantViolation }

// AsInvariantError recovers the violation list from an error chain.
func AsInvariantError(err error) (*InvariantError, bool) {
	var ie *InvariantError
	ok := errors.As(err, &ie)
	return ie, ok
}

// WipLimitError reports a full working file set, and carries what a caller
// needs to tell the user how to proceed.
//
// The message is most of this error's value: spec-tools.md §5.1.8 requires the
// limit, the current occupants, and the three remedies - finish something,
// pause something, or raise the limit deliberately.
type WipLimitError struct {
	Limit     int
	Occupants []Slot
}

func (e *WipLimitError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "wip limit reached (%d/%d)", e.Limit, e.Limit)
	for _, s := range e.Occupants {
		if s.Item != nil {
			fmt.Fprintf(&b, "\n  slot %0*d  %s  %s", s.Width, s.Number, s.Item.ID, s.Item.Title)
		}
	}
	b.WriteString("\nfinish one, pause one, or raise the limit with --wip")
	return b.String()
}

func (e *WipLimitError) Unwrap() error { return ErrWipLimitReached }

// AsWipLimitError recovers the occupant list from an error chain.
func AsWipLimitError(err error) (*WipLimitError, bool) {
	var we *WipLimitError
	ok := errors.As(err, &we)
	return we, ok
}

// ParseError reports a malformed line, located precisely enough to fix.
type ParseError struct {
	At      Location
	Message string
}

func (e *ParseError) Error() string { return fmt.Sprintf("%s: %s", e.At, e.Message) }

func (e *ParseError) Unwrap() error { return ErrInvalidArgument }

// parseErrf is the internal constructor for a located parse failure.
func parseErrf(file string, line int, format string, args ...any) error {
	return &ParseError{
		At:      Location{File: file, Line: line},
		Message: fmt.Sprintf(format, args...),
	}
}
