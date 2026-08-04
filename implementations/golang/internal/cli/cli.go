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
	"strings"

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

	// json and porcelain collect the two machine output modes while an operation
	// runs. Unexported and set by Run: they are machinery, not something a
	// caller supplies.
	json      *jsonOut
	porcelain *porcelainOut

	// Today is the date operations stamp onto items. A parameter because a test
	// that depends on the wall clock fails once a year at a month boundary.
	Today mm.Date

	// NoColor reflects the NO_COLOR variable (§9.1). Colour is currently not
	// emitted at all, so this is honoured trivially - but it is resolved here so
	// that adding colour later cannot accidentally read the environment further
	// down.
	NoColor bool

	// Editor and Visual are $EDITOR and $VISUAL (§3.5). VISUAL wins where both
	// are set; resolving that is resolveEditor's job, not the caller's.
	Editor string
	Visual string

	// Interactive reports whether there is a terminal to hand over to. False in
	// a pipeline or a CI job, where launching an editor would hang forever.
	Interactive bool

	// Launch runs the editor. Nil means really run it; tests supply their own so
	// that a test run never spawns vi.
	Launch Editor
}

// Run executes one invocation and returns the process exit code. It does not
// call os.Exit; cmd/mm does that with the value returned here.
func Run(env Env) int {
	// The envelope must be present on failure too, including a failure to parse
	// the very switch that asked for it — so the raw arguments are scanned
	// before parsing can reject them.
	stdout := env.Stdout
	env.json = &jsonOut{enabled: requestedSwitch(env.Args, "json")}
	env.porcelain = &porcelainOut{enabled: requestedSwitch(env.Args, "porcelain")}
	if env.json.enabled || env.porcelain.enabled {
		// §9.2 and §9.3: the machine stream is the ONLY thing on stdout. The
		// human renderers keep writing, to nowhere, so no operation needs two
		// code paths.
		env.Stdout = io.Discard
	}

	in, err := Parse(env.Args)
	if err != nil {
		return finish(env, stdout, err)
	}
	env.json.operation = string(in.Op)

	// --help and --version answer without touching a directory, so they work
	// from anywhere, including somewhere with no project at all.
	//
	// They print to the caller's stdout even under --json/--porcelain, where
	// operation output is discarded: there is no operation result to put in an
	// envelope, and swallowing the one thing the user asked for would turn a
	// request into an empty stdout and exit 0. They are meta-answers, not
	// operations (§3.4), so the §9.2 "one JSON object on stdout" contract does
	// not apply to them.
	switch {
	case in.Help:
		writeUsage(stdout, in.Op)
		return ExitOK
	case in.Version:
		fmt.Fprintf(stdout, "mm %s (micro-manager format spec %s)\n",
			Version, FormatSpecVersion)
		return ExitOK
	}

	return finish(env, stdout, dispatch(env, in))
}

// finish emits whatever the invocation produced and returns its exit code.
//
// One place decides how a result or a failure reaches the user, so that adding
// an output mode cannot leave one operation printing the old way.
func finish(env Env, stdout io.Writer, err error) int {
	// --check is the one operation whose failure is a RESULT rather than an
	// error (§5.1.12): it already reported every finding, and the exit code
	// carries the verdict. A trailing "command failed" line would read as a
	// second, separate problem.
	var failed *checkFailed
	isCheck := errors.As(err, &failed)

	// The violations are --check's result, not an error, in every output mode.
	reported := err
	if isCheck {
		reported = nil
	}
	switch {
	case env.json.enabled:
		env.json.emit(Env{Stdout: stdout}, reported)
		return exitCode(err)
	case env.porcelain.enabled:
		env.porcelain.emit(stdout, reported)
		if reported != nil {
			// A machine mode still owes a human a reason on stderr, where a
			// pipeline reading stdout will not see it.
			fmt.Fprintf(env.Stderr, "mm: %s\n", err)
		}
		return exitCode(err)
	}

	switch {
	case err == nil:
		return ExitOK
	case isCheck:
		return ExitInvariantViolation
	}
	return report(env, err)
}

// requestedSwitch scans the raw arguments, because an output mode has to survive
// a parse error in the same command line that asked for it. Last wins, exactly
// as in the parser (§3.3 rule 5): --json --json=false disables the mode even
// though a naive scan would enable it on the first mention. The value spellings
// are the parser's own, case-insensitively (true/yes/1, false/no/0).
func requestedSwitch(args []string, name string) bool {
	on := false
	for _, a := range args {
		if a == "--" {
			return on
		}
		s, value, hasValue := strings.Cut(a, "=")
		if s != "--"+name {
			continue
		}
		if !hasValue {
			on = true
			continue
		}
		switch strings.ToLower(value) {
		case "true", "yes", "1":
			on = true
		case "false", "no", "0":
			on = false
		}
	}
	return on
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
