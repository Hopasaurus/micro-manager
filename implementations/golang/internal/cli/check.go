package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Hopasaurus/micro-manager/mm"
)

// --check (spec-tools.md §5.1.12).
//
// This runs Store.Validate and nothing else. THE SAME CODE VALIDATES BEFORE
// EVERY MUTATION (§8), which is what makes it impossible for the tool to write
// a directory its own checker rejects. A second implementation of the ten
// invariants here — even a "quick" one for reporting — would drift, and the
// drift would surface as a file the tool wrote and then refused to read.
//
// Violations are RESULTS, NOT ERRORS: an error would mean the directory could
// not be read at all. They are reported on stdout and reflected in the exit
// code, which is 1 when any directory has a finding.

func runCheck(env Env, in *Invocation) error {
	dirs, err := checkTargets(env, in)
	if err != nil {
		return err
	}

	// Collected as well as printed: --json reports the findings as structured
	// data, and an operation cannot print as it goes and then be asked for one
	// object at the end.
	results := make([]checkResult, 0, len(dirs))

	total := 0
	problems := 0
	for _, path := range dirs {
		store, err := mm.Open(path)
		if err != nil {
			return err
		}
		// Warnings are a separate stream from violations (§3.3.2 rule 3): a
		// width outside 3-6 is reported but never a failure.
		violations, warnings, err := store.ValidateWithWarnings()
		if err != nil {
			return err
		}
		dir, err := store.Directory()
		if err != nil {
			return err
		}

		// The sibling collision of spec-file-format.md Appendix B. It is not
		// one of I1-I10 and does not come from the library's validator — a
		// collision is a property of the PARENT, and per-directory validation
		// must not depend on where a board sits. It is appended here, where
		// the checker already has the path, so that it travels with that
		// directory's findings into --json and --porcelain: a finding no
		// result carries is one a machine caller cannot see.
		violations = append(violations, collisionFindings(path)...)

		results = append(results, checkResult{
			Path: path, Project: dir.Project, Violations: violations,
			Warnings: warnings,
		})

		for _, w := range warnings {
			fmt.Fprintf(env.Stderr, "mm: warning: %s/%s: %s\n",
				dirLabel(env.Cwd, path), w.At, w.Message)
		}
		// file:line: message, sorted by path then numeric line — the format is
		// the reference checker's, so the two are diffable against each other.
		for _, v := range violations {
			fmt.Fprintf(env.Stdout, "%s/%s: %s\n", dirLabel(env.Cwd, path), v.At, v.Message)
		}
		if len(violations) > 0 {
			problems++
			total += len(violations)
			fmt.Fprintf(env.Stdout, "  %s: %d problem(s)\n", dirLabel(env.Cwd, path), len(violations))
		} else if !in.Quiet {
			fmt.Fprintf(env.Stdout, "  %s: ok -- %s: %s\n",
				dirLabel(env.Cwd, path), directoryName(dir), summarise(store, dir))
		}
	}

	env.json.setResult(toJSONCheck(results))
	for _, r := range results {
		for _, v := range r.Violations {
			env.porcelain.row(r.Path, v.At.File, strconv.Itoa(v.At.Line),
				v.Invariant, v.Message)
		}
	}

	if problems > 0 {
		fmt.Fprintf(env.Stdout, "\nmm: %d problem(s) in %d of %d director%s\n",
			total, problems, len(dirs), plural(len(dirs), "y", "ies"))
		// Not an error: --check did its job. The exit code carries the verdict,
		// so a script gates on it without parsing anything.
		return &checkFailed{}
	}
	if !in.Quiet {
		fmt.Fprintf(env.Stdout, "\nmm: ok -- %d director%s clean\n",
			len(dirs), plural(len(dirs), "y", "ies"))
	}
	return nil
}

// collisionFindings reports a second board sitting beside this one.
//
// Against each colliding directory rather than once against the parent,
// because checking ONE board has to report it: a person working in
// ./micro-manager who never runs --all would otherwise never be told that
// ./.micro-manager exists and has been collecting the other half of their
// items.
//
// The location is "." — the directory itself, the same place I10's
// directory-level findings point at — because there is no file to name. The
// message names the sibling and the fix, since neither is obvious from the
// fact alone.
func collisionFindings(path string) []mm.Violation {
	siblings := mm.CollidingSiblings(path)
	if len(siblings) == 0 {
		return nil
	}
	return []mm.Violation{{
		At: mm.Location{File: "."},
		Message: fmt.Sprintf(
			"a second micro-manager directory is beside this one (%s); keep one — "+
				"two boards in one place share no next_id, so both start at T-0001 "+
				"and the same id means two different items",
			strings.Join(siblings, ", ")),
	}}
}

// checkResult is one directory's verdict, kept so that --json can report the
// findings as data rather than as the lines a person reads.
type checkResult struct {
	Path       string
	Project    string
	Violations []mm.Violation
	Warnings   []mm.Violation
}

// checkFailed carries "the check found problems" to the exit code without
// pretending the run itself failed.
type checkFailed struct{}

func (e *checkFailed) Error() string { return "invariant violations found" }
func (e *checkFailed) Unwrap() error { return mm.ErrInvariantViolation }

// checkTargets is the one place --all changes which directories an operation
// sees. It is read-only, which is why --all is allowed here at all (§4).
func checkTargets(env Env, in *Invocation) ([]string, error) {
	if !in.All {
		res, err := resolveDir(in.Dir, env.Dir, env.Cwd)
		if err != nil {
			return nil, err
		}
		return []string{res.Path}, nil
	}

	roots := discoveryRoots(env.Cwd)
	if len(roots) == 0 {
		return nil, usagef("--check --all needs a directory to scan from")
	}
	found := mm.Discover(mm.DefaultDiscoveryOptions(roots...))
	if len(found.Directories) == 0 {
		return nil, notFoundf("no micro-manager directory found below %s", roots[0])
	}
	if found.Partial {
		fmt.Fprintf(env.Stderr,
			"mm: warning: the scan hit a limit; results may be incomplete\n")
	}
	out := make([]string, 0, len(found.Directories))
	for _, d := range found.Directories {
		out = append(out, d.Path)
	}
	return out, nil
}

// summarise is the one-line directory description --check prints when clean.
func summarise(s *mm.Store, dir mm.Directory) string {
	backlog, err := s.List(mm.Filter{})
	if err != nil {
		return ""
	}
	done, err := s.List(mm.Filter{State: mm.StateDone})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d in backlog, wip %d/%d, %d done",
		len(backlog), dir.WipUsed, dir.WipLimit, len(done))
}

func directoryName(d mm.Directory) string {
	if d.Project == "" {
		return "(no project name)"
	}
	return d.Project
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
