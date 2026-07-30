// Package cli is the command line wrapper over the mm library.
//
// It owns argument parsing, terminal interaction, rendering and exit codes, and
// nothing else. Any rule a user could also want from the UI service belongs in
// package mm, not here (spec-tools.md §2.5).
package cli

import (
	"errors"
	"fmt"
	"io"

	"micromanager/mm"
)

// Exit codes, from spec-tools.md §10.
const (
	ExitOK                 = 0 // success
	ExitInvariantViolation = 1 // --check found problems, or a write was rejected
	ExitUsage              = 2 // unknown switch, no operation, two operations
	ExitNotFound           = 3 // unknown ID, unresolvable or ambiguous directory
	ExitPrecondition       = 4 // WIP limit reached, wrong state, guard unmet
	ExitConcurrent         = 5 // directory changed underneath the operation
	ExitIO                 = 6 // filesystem or environment failure
)

// Version is the tool version, and the format spec version it implements.
const (
	Version           = "0.1.0"
	FormatSpecVersion = "1"
)

// Env carries everything the wrapper is allowed to know about the process that
// the library is not: streams, arguments and resolved environment values. It is
// a parameter so that tests drive Run without touching real stdio.
//
// EVERY ENVIRONMENT VARIABLE IS RESOLVED INTO A FIELD HERE. The library reads
// none of them (spec-tools.md §2.2 rule 4, §3.5), and neither does anything
// below this struct: one place reads the environment, and it is cmd/mm.
type Env struct {
	Args   []string
	Stdout io.Writer
	Stderr io.Writer

	// Dir is MM_DIR, already read by the caller, or empty.
	Dir string

	// ReportPeriod is MM_REPORT_PERIOD, or empty.
	ReportPeriod string

	// Cwd is the working directory, used by directory resolution (§4 steps 3
	// and 4). Passed in rather than read below so resolution is testable
	// without chdir, which is process-global.
	Cwd string

	// Today is the date operations stamp onto items. A parameter because a test
	// that depends on the wall clock fails once a year at a month boundary.
	Today mm.Date

	// NoColor reflects the NO_COLOR variable (§9.1). Colour is currently not
	// emitted at all, so this is honoured trivially - but it is resolved here so
	// that adding colour later cannot accidentally read the environment further
	// down.
	NoColor bool
}

// Run executes one invocation and returns the process exit code. It does not
// call os.Exit; cmd/mm does that with the value returned here.
func Run(env Env) int {
	in, err := Parse(env.Args)
	if err != nil {
		return report(env, err)
	}

	// --help and --version answer without touching a directory, so they work
	// from anywhere, including somewhere with no project at all.
	switch {
	case in.Help:
		writeUsage(env.Stdout, in.Op)
		return ExitOK
	case in.Version:
		fmt.Fprintf(env.Stdout, "mm %s (micro-manager format spec %s)\n",
			Version, FormatSpecVersion)
		return ExitOK
	}

	// §9.2 and §9.3 are not built yet (T-0036, T-0038). A flag that is accepted
	// and then ignored is worse than one that is refused: a script would parse
	// human output as JSON and get nonsense.
	if in.JSON {
		return report(env, usagef("--json is not implemented yet (T-0036)"))
	}
	if in.Porcelain {
		return report(env, usagef("--porcelain is not implemented yet (T-0038)"))
	}

	if err := dispatch(env, in); err != nil {
		// --check is the one operation whose failure is a RESULT rather than an
		// error (§5.1.12): it already printed every finding, and the exit code
		// carries the verdict. Printing "mm: invariant violations found" after a
		// list of violations adds nothing and reads as a second, separate failure.
		var failed *checkFailed
		if errors.As(err, &failed) {
			return ExitInvariantViolation
		}
		return report(env, err)
	}
	return ExitOK
}

// report writes an error to stderr and returns its exit code.
//
// Errors always print, even under --quiet: suppressing the reason a command
// failed leaves the user with nothing but a number.
func report(env Env, err error) int {
	code := exitCode(err)
	if isUsage(err) {
		fmt.Fprintf(env.Stderr, "mm: %s\nTry 'mm --help'.\n", err)
	} else {
		fmt.Fprintf(env.Stderr, "mm: %s\n", err)
	}
	return code
}
