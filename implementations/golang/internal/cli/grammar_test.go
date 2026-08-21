package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The declared-ID-grammar behaviour (spec-file-format.md §3.3.2), end to end:
// --init declares a grammar, every later operation reads it back from the
// directory, and an unconfigured directory behaves exactly as before.

func TestInitDeclaresGrammar(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	got := r.run("--init", "--project", "Custom", "--prefix", "X", "--id-width", "3")
	if got.Code != ExitOK {
		t.Fatalf("init: %s", got)
	}
	if got := r.run("--migrate"); got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	dir := filepath.Join(cwd, "micro-manager")
	b, err := os.ReadFile(filepath.Join(dir, "board.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"next_id: X-001", "id_prefix: X", "id_width: 3"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("backlog.md lacks %q:\n%s", want, b)
		}
	}

	// --add allocates in the declared grammar, and reports the ID.
	got = r.run("--add", "Ship the thing")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "X-001") {
		t.Fatalf("add: %s", got)
	}
	got = r.run("--add", "Ship another")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "X-002") {
		t.Fatalf("second add: %s", got)
	}

	// Every ID-taking operation parses against the declared grammar: the bare
	// number resolves, a foreign or malformed form is a usage error.
	for _, args := range [][]string{
		{"--show", "1"},
		{"--edit", "1", "--title", "Renamed"},
		{"--start", "1"},
		{"--note", "1", "first note"},
		{"--finish", "1"},
	} {
		if got := r.run(args...); got.Code != ExitOK {
			t.Errorf("%v: %s", args, got)
		}
	}

	// The remaining ID-taking operations, on a second item so states do not
	// fight each other: block, pause, unblock, move (subject AND --after), then
	// remove with its detail file.
	if got := r.run("--add", "Third"); got.Code != ExitOK || !strings.Contains(got.Stdout, "X-003") {
		t.Fatalf("third add: %s", got)
	}
	if got := r.run("--add", "Fourth"); got.Code != ExitOK || !strings.Contains(got.Stdout, "X-004") {
		t.Fatalf("fourth add: %s", got)
	}
	for _, args := range [][]string{
		{"--start", "2"},
		{"--pause", "2"},
		{"--block", "2", "waiting on a key"},
		{"--unblock", "2"},
		{"--move", "3", "--after", "2"},
		{"--remove", "2", "--force"},
	} {
		if got := r.run(args...); got.Code != ExitOK {
			t.Errorf("%v: %s", args, got)
		}
	}
	for _, args := range [][]string{
		{"--show", "x-001"},
		{"--show", "T-001"},
		{"--show", "X-1"},
		{"--show", "X-0001"},
		{"--remove", "X-001", "--keep-details"},
	} {
		if got := r.run(args...); got.Code != ExitUsage {
			t.Errorf("%v: want usage error, got %s", args, got)
		}
	}
}

func TestDefaultInitWritesNoGrammar(t *testing.T) {
	r, dir := v2Project(t)
	b, err := os.ReadFile(filepath.Join(dir, "board.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id_prefix", "id_width"} {
		if strings.Contains(string(b), key) {
			t.Errorf("default init must not write %q (absent keys = spec version 1):\n%s", key, b)
		}
	}
	got := r.run("--add", "Plain item")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "T-0001") {
		t.Fatalf("add: %s", got)
	}
	if got := r.run("--show", "t-0001"); got.Code != ExitUsage {
		t.Errorf("lowercase id should be rejected: %s", got)
	}
	if got := r.run("--show", "X-0001"); got.Code != ExitUsage {
		t.Errorf("foreign prefix should be rejected: %s", got)
	}
}

func TestStatusNextSearchRenderDeclaredIDs(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	if got := r.run("--init", "--project", "Custom", "--prefix", "MM", "--id-width", "3"); got.Code != ExitOK {
		t.Fatalf("init: %s", got)
	}
	if got := r.run("--migrate"); got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if got := r.run("--add", "First custom item"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}
	if got := r.run("--add", "Second custom item"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}

	// --status shows the item line rendered with its own ID.
	got := r.run("--status")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "MM-001") {
		t.Fatalf("status: %s", got)
	}
	if strings.Contains(got.Stdout, "T-") {
		t.Errorf("status rendered a default-grammar id:\n%s", got.Stdout)
	}

	// --next and --search likewise.
	got = r.run("--next")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "MM-001") {
		t.Fatalf("next: %s", got)
	}
	got = r.run("--search", "custom")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "MM-001") ||
		!strings.Contains(got.Stdout, "MM-002") {
		t.Fatalf("search: %s", got)
	}
}

func TestDryRunShowsDeclaredShape(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	if got := r.run("--init", "--project", "Custom", "--prefix", "X", "--id-width", "3"); got.Code != ExitOK {
		t.Fatalf("init: %s", got)
	}
	if got := r.run("--migrate"); got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	got := r.run("--add", "Drafted item", "--dry-run")
	if got.Code != ExitOK {
		t.Fatalf("dry-run add: %s", got)
	}
	if !strings.Contains(got.Stdout, "X-001") {
		t.Errorf("dry-run must show the declared shape:\n%s", got.Stdout)
	}
	// A real run after the draft really does allocate X-001.
	got = r.run("--add", "Drafted item")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "X-001") {
		t.Fatalf("real add: %s", got)
	}
}

func TestInitRejectsBadGrammarFlags(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	for _, args := range [][]string{
		{"--init", "--project", "P", "--prefix", "x"},
		{"--init", "--project", "P", "--prefix", "ABCDE"},
		{"--init", "--project", "P", "--prefix", "Tt"},
		{"--init", "--project", "P", "--id-width", "0"},
		{"--init", "--project", "P", "--id-width", "-1"},
		{"--init", "--project", "P", "--id-width", "abc"},
		{"--init", "--project", "P", "--wip-limit", "-1"},
		{"--init", "--project", "P", "--wip-limit", "abc"},
	} {
		if got := r.run(args...); got.Code != ExitUsage {
			t.Errorf("%v: want usage error, got %s", args, got)
		}
	}
}

// Unlike version 1's retired --slots (code-review-007 F2: zero silently
// became the one-slot default), version 2's --wip-limit has no such trap -
// zero and "absent" both mean the same real thing, uncapped, so an explicit
// zero is accepted rather than refused.
func TestInitWipLimitZeroMeansUncapped(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	got := r.run("--init", "--project", "P", "--wip-limit", "0")
	if got.Code != ExitOK {
		t.Fatalf("--wip-limit 0: want ok, got %s", got)
	}
	b, err := os.ReadFile(filepath.Join(cwd, "micro-manager", "board.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "wip.working") {
		t.Errorf("--wip-limit 0 must write no wip.working key:\n%s", b)
	}
}

func TestInitWarnsOnUnusualWidthNeverFails(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	got := r.run("--init", "--project", "Narrow", "--id-width", "2")
	if got.Code != ExitOK {
		t.Fatalf("a width outside 3-6 is a warning, not a failure: %s", got)
	}
	if !strings.Contains(got.Stderr, "warning") {
		t.Errorf("expected a width warning on stderr, got:\n%s", got.Stderr)
	}
	if got := r.run("--migrate"); got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	// The directory is still valid in its own grammar.
	if got := r.run("--add", "Tiny item"); got.Code != ExitOK || !strings.Contains(got.Stdout, "T-01") {
		t.Fatalf("add: %s", got)
	}
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("check of a warned-about directory must pass: %s", got)
	} else if !strings.Contains(got.Stderr, "warning") {
		t.Errorf("--check should repeat the width warning on stderr:\n%s", got.Stderr)
	}
	// And in the JSON envelope, as its own stream: warnings are not violations.
	if got := r.run("--check", "--json"); got.Code != ExitOK {
		t.Fatalf("json check: %s", got)
	} else if e := decode(t, got); len(e.Errors) != 0 {
		t.Errorf("violations are results, not errors: %+v", e.Errors)
	}
}

func TestInitExplicitDefaultsWriteNoKeys(t *testing.T) {
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	if got := r.run("--init", "--project", "P", "--prefix", "T", "--id-width", "4"); got.Code != ExitOK {
		t.Fatalf("init: %s", got)
	}
	b, err := os.ReadFile(filepath.Join(cwd, "micro-manager", "board.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "id_prefix") || strings.Contains(string(b), "id_width") {
		t.Errorf("explicit defaults are no-ops and must not write keys:\n%s", b)
	}
}
