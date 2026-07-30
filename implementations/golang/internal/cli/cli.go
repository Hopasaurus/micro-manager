// Package cli is the command line wrapper over the mm library.
//
// It owns argument parsing, terminal interaction, rendering and exit codes, and
// nothing else. Any rule a user could also want from the UI service belongs in
// package mm, not here (spec-tools.md §2.5).
package cli

import "io"

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

// Env carries everything the wrapper is allowed to know about the process that
// the library is not: streams, arguments and resolved environment values. It is
// a parameter so that tests drive Run without touching real stdio.
type Env struct {
	Args   []string
	Stdout io.Writer
	Stderr io.Writer
	// Dir is MM_DIR already resolved by the caller, or empty.
	Dir string
}

// Run executes one invocation and returns the process exit code. It does not
// call os.Exit; cmd/mm does that with the value returned here.
func Run(env Env) int {
	return ExitUsage // T-0030 replaces this with the switch parser
}
