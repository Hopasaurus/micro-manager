package cli

import (
	"errors"
	"fmt"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Error to exit code mapping (spec-tools.md §10).
//
// THIS MAPPING LIVES HERE AND NOWHERE ELSE. The library returns a typed error
// and says nothing about processes; the UI service maps the same taxonomy to
// HTTP status (spec-gui.md §4.3). A library that knew about exit codes would
// have to know which front end it was in.

// notFoundError and ioError are the wrapper's own errors, for failures that
// happen before the library is reached: resolution finding nothing, or finding
// too much.
type notFoundError struct{ msg string }

func (e *notFoundError) Error() string { return e.msg }
func (e *notFoundError) Unwrap() error { return mm.ErrNotFound }

func notFoundf(format string, args ...any) error {
	return &notFoundError{msg: fmt.Sprintf(format, args...)}
}

type ioError struct{ msg string }

func (e *ioError) Error() string { return e.msg }
func (e *ioError) Unwrap() error { return mm.ErrIO }

func ioErrorf(format string, args ...any) error {
	return &ioError{msg: fmt.Sprintf(format, args...)}
}

// exitCode maps an error to its process exit code.
//
// Order matters where a sentinel could match more than one arm. ErrWipLimitReached
// is checked before ErrConflict for that reason: it is deliberately a distinct
// error because it is the one failure with a routine remedy, and collapsing it
// into the generic conflict code would throw that distinction away at the last
// step.
func exitCode(err error) int {
	switch {
	case err == nil:
		return ExitOK

	case errors.Is(err, mm.ErrInvariantViolation):
		return ExitInvariantViolation

	// A usage error is the wrapper's own: an unknown switch, no operation, two
	// operations. ErrInvalidArgument from the library is the same class of
	// mistake seen one layer down - a bad date, an unknown prio - and gets the
	// same code, because to the user it is the same kind of wrong.
	case isUsage(err), errors.Is(err, mm.ErrInvalidArgument):
		return ExitUsage

	case errors.Is(err, mm.ErrNotFound), errors.Is(err, mm.ErrAmbiguous):
		return ExitNotFound

	case errors.Is(err, mm.ErrWipLimitReached),
		errors.Is(err, mm.ErrPreconditionFailed),
		errors.Is(err, mm.ErrConflict),
		errors.Is(err, mm.ErrAlreadyExists):
		return ExitPrecondition

	case errors.Is(err, mm.ErrConcurrent):
		return ExitConcurrent

	case errors.Is(err, mm.ErrIO):
		return ExitIO
	}
	// An error with no sentinel is a bug in this program rather than a mistake
	// by the user. It is reported as I/O because that is the code for "the tool
	// could not do its job", and it is never silently mapped to success.
	return ExitIO
}

func isUsage(err error) bool {
	var ue *UsageError
	return errors.As(err, &ue)
}

// errorCode names the error for the JSON envelope (§9.2), using the §6.3
// vocabulary rather than the exit code, so a machine caller reads the same names
// the specification uses.
func errorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, mm.ErrInvariantViolation):
		return "InvariantViolation"
	case isUsage(err), errors.Is(err, mm.ErrInvalidArgument):
		return "InvalidArgument"
	case errors.Is(err, mm.ErrAmbiguous):
		return "Ambiguous"
	case errors.Is(err, mm.ErrNotFound):
		return "NotFound"
	case errors.Is(err, mm.ErrWipLimitReached):
		return "WipLimitReached"
	case errors.Is(err, mm.ErrPreconditionFailed):
		return "PreconditionFailed"
	case errors.Is(err, mm.ErrAlreadyExists):
		return "AlreadyExists"
	case errors.Is(err, mm.ErrConflict):
		return "Conflict"
	case errors.Is(err, mm.ErrConcurrent):
		return "Concurrent"
	case errors.Is(err, mm.ErrIO):
		return "Io"
	}
	return "Io"
}
