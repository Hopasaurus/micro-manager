package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --dry-run (spec-tools.md §3.4).
//
// Three requirements, and the third is the one that is easy to get wrong:
//
//	1. it must write nothing;
//	2. it must report exactly what would change;
//	3. it must RETURN THE CODE THE REAL RUN WOULD HAVE RETURNED, so it can gate
//	   a script.
//
// The way to test 2 and 3 honestly is not to assert on wording, which would
// only pin today's phrasing. It is to run the same command twice against two
// identical directories — once dry, once for real — and require the two to
// agree on both the exit code and the report.

// scenario is one command, with the setup needed to make it meaningful.
type scenario struct {
	name  string
	setup [][]string
	args  []string
}

// mutations covers every operation that writes, including cases that fail:
// a dry run of a command that would be refused must be refused the same way.
var mutations = []scenario{
	{"add", nil, []string{"--add", "New thing", "--prio", "high", "--tag", "infra"}},
	{"add with a detail file", nil,
		[]string{"--add", "With detail", "--detail-text", "Long form."}},
	// --reason, not --blocked: a supplied Blocked (version 1's field) is
	// invisible to buildNewItemV2, which only infers a needs_reason stage
	// from Reason (version 2's field) - --blocked here would silently land
	// on the default "ready" stage instead of "blocked".
	{"add to blocked", nil,
		[]string{"--add", "Waiting", "--reason", "on the vendor"}},
	{"edit", [][]string{{"--add", "First"}},
		[]string{"--edit", "T-0001", "--title", "Renamed", "--prio", "low"}},
	{"edit an unregistered field", [][]string{{"--add", "First"}},
		[]string{"--edit", "T-0001", "--set", "owner=dana"}},
	{"move", [][]string{{"--add", "First"}, {"--add", "Second"}},
		[]string{"--move", "T-0002", "--top"}},
	// --stage, not --section: moveV2 reads Stage only.
	{"move between stages", [][]string{{"--add", "First"}},
		[]string{"--move", "T-0001", "--stage", "someday"}},
	{"start", [][]string{{"--add", "First"}}, []string{"--start", "T-0001"}},
	{"pause", [][]string{{"--add", "First"}, {"--start", "T-0001"}},
		[]string{"--pause", "T-0001"}},
	{"finish from the backlog", [][]string{{"--add", "First"}},
		[]string{"--finish", "T-0001", "--outcome", "cancelled"}},
	{"finish from a slot", [][]string{{"--add", "First"}, {"--start", "T-0001"}},
		[]string{"--finish", "T-0001", "--closing-note", "shipped it"}},
	{"finish into an older month", [][]string{{"--add", "First"}},
		[]string{"--finish", "T-0001", "--done", "2026-05-04"}},
	{"remove", [][]string{{"--add", "First"}},
		[]string{"--remove", "T-0001", "--force"}},
	{"remove with its detail file", [][]string{{"--add", "F", "--detail-text", "x"}},
		[]string{"--remove", "T-0001", "--force", "--with-detail"}},
	// --stage working: bare --wip refuses outright on a version-2 directory
	// (SetWipLimit's own early refusal points at --wip N --stage SLUG).
	{"wip up", nil, []string{"--wip", "3", "--stage", "working"}},
	{"wip down", nil, []string{"--wip", "1", "--stage", "working"}},

	// Failures. The dry run must reach the same verdict.
	{"start at the wip limit",
		[][]string{{"--add", "A"}, {"--add", "B"}, {"--add", "C"},
			{"--start", "T-0001"}, {"--start", "T-0002"}},
		[]string{"--start", "T-0003"}},
	{"remove without the guard", [][]string{{"--add", "First"}},
		[]string{"--remove", "T-0001"}},
	{"pause something in the backlog", [][]string{{"--add", "First"}},
		[]string{"--pause", "T-0001"}},
	{"finish an unknown id", nil, []string{"--finish", "T-9999"}},
	// Version 2 has no slot count or slot numbers to lower below - the
	// analogous conflict is lowering wip.working below how many items are
	// actually on it, which SetStageWipLimit refuses the same way.
	{"wip down onto occupied working slots",
		[][]string{{"--add", "A"}, {"--add", "B"}, {"--start", "T-0001"}, {"--start", "T-0002"}},
		[]string{"--wip", "1", "--stage", "working"}},
}

// prepare builds a version-2 project with a wip.working cap of 2 and runs
// the scenario's setup.
func prepare(t *testing.T, s scenario) (runner, string) {
	t.Helper()
	r, dir := v2Project(t, "--slots", "2")
	for _, args := range s.setup {
		if got := r.run(args...); got.Code != ExitOK {
			t.Fatalf("%s: setup %v: %s", s.name, args, got)
		}
	}
	return r, dir
}

// normalise removes the two things that legitimately differ between two runs in
// two different temporary directories: the path itself, and the dry-run marker.
func normalise(text, dir string) string {
	text = strings.ReplaceAll(text, dir, "<DIR>")
	text = strings.ReplaceAll(text, filepath.Dir(dir), "<CWD>")
	return strings.ReplaceAll(text, "would: ", "")
}

func TestDryRunMatchesTheRealRun(t *testing.T) {
	for _, s := range mutations {
		t.Run(s.name, func(t *testing.T) {
			dryRunner, dryDir := prepare(t, s)
			realRunner, realDir := prepare(t, s)

			before := readAll(t, dryDir)
			dry := dryRunner.run(append(append([]string{}, s.args...), "--dry-run")...)
			after := readAll(t, dryDir)
			real := realRunner.run(s.args...)

			// 1. Nothing written.
			if after != before {
				t.Errorf("the dry run wrote to disk")
			}

			// 3. The same verdict. A dry run that reported success for a command
			// that would be refused is worse than useless: it is a script that
			// proceeds into a failure.
			if dry.Code != real.Code {
				t.Errorf("exit %d dry, %d real\ndry stderr: %sreal stderr: %s",
					dry.Code, real.Code, dry.Stderr, real.Stderr)
			}

			// 2. The same report.
			gotOut := normalise(dry.Stdout, dryDir)
			wantOut := normalise(real.Stdout, realDir)
			if gotOut != wantOut {
				t.Errorf("reports differ\n dry:\n%s\nreal:\n%s", gotOut, wantOut)
			}
			gotErr := normalise(dry.Stderr, dryDir)
			wantErr := normalise(real.Stderr, realDir)
			if gotErr != wantErr {
				t.Errorf("errors differ\n dry:\n%s\nreal:\n%s", gotErr, wantErr)
			}
		})
	}
}

// A dry run has to say it is one, or reading the output is not a way to check a
// command before running it.
func TestDryRunIsMarkedAsSuch(t *testing.T) {
	for _, s := range mutations {
		r, _ := prepare(t, s)
		got := r.run(append(append([]string{}, s.args...), "--dry-run")...)
		if got.Code != ExitOK {
			continue // the failure cases have nothing to mark
		}
		if !strings.Contains(got.Stdout, "would") {
			t.Errorf("%s: the output does not say it was a dry run:\n%s", s.name, got.Stdout)
		}
	}
}

// --init is a mutation too, and the one that creates the directory it reports.
func TestDryRunInit(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}

	got := r.run("--init", "--project", "Dry", "--wip-limit", "2", "--dry-run")
	if got.Code != ExitOK {
		t.Fatalf("%s", got)
	}
	if !strings.Contains(got.Stdout, "would") {
		t.Errorf("unmarked:\n%s", got.Stdout)
	}
	// Four files named (version 2: no working files), none of them on disk.
	for _, name := range []string{"board.md", "done.md", "_template.md", "structure.md"} {
		if !strings.Contains(got.Stdout, name) {
			t.Errorf("%s is not in the report:\n%s", name, got.Stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(cwd, "micro-manager")); !os.IsNotExist(err) {
		t.Error("the dry run created the directory")
	}
}

// §3.4 lists --dry-run as valid with EVERY operation, so a read-only one accepts
// it and is unaffected: it computes no change and writes nothing already.
func TestDryRunOnReadOnlyOperations(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Something")

	for _, args := range [][]string{
		{"--list"}, {"--show", "T-0001"}, {"--check"}, {"--find"},
	} {
		plain := r.run(args...)
		dry := r.run(append(append([]string{}, args...), "--dry-run")...)
		if plain.Code != dry.Code || plain.Stdout != dry.Stdout {
			t.Errorf("%v: --dry-run changed a read-only operation\nplain:\n%sdry:\n%s",
				args, plain.Stdout, dry.Stdout)
		}
	}
}
