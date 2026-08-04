package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micromanager/mm"
)

var testDay = mm.Date{Year: 2026, Month: 7, Day: 30}

// result is one invocation's outcome, captured without touching real stdio.
type result struct {
	Code   int
	Stdout string
	Stderr string
}

func (r result) String() string {
	return "exit " + string(rune('0'+r.Code)) + "\nstdout:\n" + r.Stdout + "stderr:\n" + r.Stderr
}

// runner drives Run with a fixed working directory and clock.
type runner struct {
	cwd    string
	dir    string // MM_DIR
	period string // MM_REPORT_PERIOD
}

func (r runner) run(args ...string) result {
	return r.runWith(Env{}, args...)
}

// runWith layers the caller's environment over the runner's, so a test can add
// an editor or a terminal without restating the rest.
func (r runner) runWith(base Env, args ...string) result {
	var stdout, stderr bytes.Buffer
	base.Args = args
	base.Stdout = &stdout
	base.Stderr = &stderr
	base.Cwd = r.cwd
	base.Dir = r.dir
	base.ReportPeriod = r.period
	base.Today = testDay
	code := Run(base)
	return result{Code: code, Stdout: stdout.String(), Stderr: stderr.String()}
}

// newProject returns a runner over a freshly initialised directory.
func newProject(t *testing.T, args ...string) (runner, string) {
	t.Helper()
	cwd := t.TempDir()
	r := runner{cwd: cwd}
	got := r.run(append([]string{"--init", "--project", "Test Project"}, args...)...)
	if got.Code != ExitOK {
		t.Fatalf("init failed: %s", got)
	}
	return r, filepath.Join(cwd, "micro-manager")
}

func TestRunLifecycle(t *testing.T) {
	r, dir := newProject(t, "--slots", "2")

	// --add reports the assigned ID. It is the handle for every command that
	// follows, so silence here would be a defect, not brevity.
	got := r.run("--add", "Fix the deploy script", "--prio", "high", "--tag", "infra")
	if got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}
	if !strings.Contains(got.Stdout, "T-0001") {
		t.Errorf("add must report the id:\n%s", got.Stdout)
	}

	if got := r.run("--start", "1"); got.Code != ExitOK {
		t.Fatalf("start: %s", got)
	} else if !strings.Contains(got.Stdout, "slot 1") {
		t.Errorf("start should name the slot:\n%s", got.Stdout)
	}

	if got := r.run("--pause", "T-0001"); got.Code != ExitOK {
		t.Fatalf("pause: %s", got)
	}
	if got := r.run("--start", "T-0001"); got.Code != ExitOK {
		t.Fatalf("restart: %s", got)
	}
	if got := r.run("--finish", "T-0001", "--closing-note", "shipped it"); got.Code != ExitOK {
		t.Fatalf("finish: %s", got)
	}

	// The directory the CLI produced must pass the checker it ships with.
	if got := r.run("--check"); got.Code != ExitOK {
		t.Errorf("check after the lifecycle: %s", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, "done.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "- [x] [T-0001]") {
		t.Errorf("done.md:\n%s", body)
	}
}

// spec-tools.md §10. The table is the contract a script gates on.
func TestExitCodes(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Something") // T-0001, stays in the backlog
	r.run("--add", "Another")   // T-0002, removed below

	// Absolute: the runner's working directory is a temp dir, so a path
	// relative to the source tree would resolve to nothing.
	broken := fixturePath(t, "broken-i1-duplicate-id")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"--list"}, ExitOK},
		{"invariant violation", []string{"--dir", broken, "--check"}, ExitInvariantViolation},
		{"unknown switch", []string{"--list", "--nope"}, ExitUsage},
		{"no operation", []string{"--verbose"}, ExitUsage},
		{"two operations", []string{"--list", "--check"}, ExitUsage},
		{"bad value", []string{"--add", "x", "--prio", "urgent"}, ExitUsage},
		{"unknown id", []string{"--show", "T-9999"}, ExitNotFound},
		{"unresolvable directory", []string{"--dir", "/nonexistent", "--list"}, ExitNotFound},
		// The guard: --remove without --force is refused, and refusing is the
		// documented behaviour rather than a failure of the tool.
		{"guard not satisfied", []string{"--remove", "T-0002"}, ExitPrecondition},
		{"guard satisfied", []string{"--remove", "T-0002", "--force"}, ExitOK},
		// T-0001 is in the backlog, so pausing it contradicts its state.
		{"wrong state", []string{"--pause", "T-0001"}, ExitPrecondition},
	}
	for _, c := range cases {
		if got := r.run(c.args...); got.Code != c.want {
			t.Errorf("%s: exit %d, want %d\n%s", c.name, got.Code, c.want, got)
		}
	}
}

// §5.1.12: violations are results, not errors. --check prints its findings and
// says nothing else — a second "command failed" line would read as a separate
// problem.
func TestCheckReportsViolationsWithoutAnErrorLine(t *testing.T) {
	broken := fixturePath(t, "broken-i9-orphan-detail")
	r := runner{cwd: t.TempDir()}

	got := r.run("--dir", broken, "--check")
	if got.Code != ExitInvariantViolation {
		t.Errorf("exit = %d, want %d", got.Code, ExitInvariantViolation)
	}
	if !strings.Contains(got.Stdout, "orphan") {
		t.Errorf("the finding should be on stdout:\n%s", got.Stdout)
	}
	if got.Stderr != "" {
		t.Errorf("nothing belongs on stderr here:\n%s", got.Stderr)
	}
}

// §3.4: --dry-run exercises the whole path and writes nothing, and returns the
// code the real run would, so a script can gate on it.
func TestDryRunWritesNothing(t *testing.T) {
	r, dir := newProject(t)
	r.run("--add", "First")
	before := readAll(t, dir)

	for _, args := range [][]string{
		{"--add", "Second", "--dry-run"},
		{"--start", "T-0001", "--dry-run"},
		{"--finish", "T-0001", "--dry-run"},
		{"--remove", "T-0001", "--force", "--dry-run"},
		{"--wip", "3", "--dry-run"},
	} {
		got := r.run(args...)
		if got.Code != ExitOK {
			t.Errorf("%v: %s", args, got)
		}
		if !strings.Contains(got.Stdout, "would") && !strings.Contains(got.Stdout, "wip") {
			t.Errorf("%v: a dry run should say what it would do:\n%s", args, got.Stdout)
		}
	}
	if after := readAll(t, dir); after != before {
		t.Error("a dry run wrote to disk")
	}
}

// §4, in order. Each step is checked against the one below it, because a step
// that never fires looks identical to one that is wrong.
func TestDirectoryResolution(t *testing.T) {
	root := t.TempDir()

	// A project two levels down, so the upward search has somewhere to walk.
	deep := filepath.Join(root, "src", "pkg")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	base := runner{cwd: root}
	if got := base.run("--init", "--project", "Root"); got.Code != ExitOK {
		t.Fatal(got)
	}
	other := filepath.Join(t.TempDir(), "elsewhere")
	if got := (runner{cwd: other}).run("--init", "--project", "Elsewhere",
		"--dir", filepath.Join(other, "micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}

	// 1. --dir beats everything, including MM_DIR.
	r := runner{cwd: deep, dir: filepath.Join(root, "micro-manager")}
	got := r.run("--dir", filepath.Join(other, "micro-manager"), "--list", "--verbose")
	if !strings.Contains(got.Stderr, "from --dir") {
		t.Errorf("--dir should win:\n%s", got.Stderr)
	}

	// 2. MM_DIR, when there is no --dir.
	got = r.run("--list", "--verbose")
	if !strings.Contains(got.Stderr, "from MM_DIR") {
		t.Errorf("MM_DIR should be used:\n%s", got.Stderr)
	}

	// 3. Upward from the working directory.
	r = runner{cwd: deep}
	got = r.run("--list", "--verbose")
	if !strings.Contains(got.Stderr, "upward") {
		t.Errorf("the upward search should find the root project:\n%s", got.Stderr)
	}

	// 4. Downward, when there is nothing above.
	down := t.TempDir()
	if got := (runner{cwd: down}).run("--init", "--project", "Below",
		"--dir", filepath.Join(down, "sub", "micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}
	got = (runner{cwd: down}).run("--list", "--verbose")
	if !strings.Contains(got.Stderr, "downward") {
		t.Errorf("the downward search should find it:\n%s", got.Stderr)
	}

	// An invalid MM_DIR is an error NAMING THE VARIABLE, never a silent fallback
	// to a search: a stale export would otherwise send every command elsewhere.
	got = (runner{cwd: deep, dir: "/nonexistent"}).run("--list")
	if got.Code != ExitNotFound || !strings.Contains(got.Stderr, "MM_DIR") {
		t.Errorf("want an error naming MM_DIR, got: %s", got)
	}
}

// Ambiguity is never resolved by guessing: the candidates are listed so the
// user can name one.
func TestAmbiguousResolutionListsCandidates(t *testing.T) {
	root := t.TempDir()
	r := runner{cwd: root}
	if got := r.run("--init", "--project", "One",
		"--dir", filepath.Join(root, "micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}
	if got := r.run("--init", "--project", "Two",
		"--dir", filepath.Join(root, ".micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}

	got := (runner{cwd: filepath.Join(root)}).run("--list")
	if got.Code != ExitNotFound {
		t.Fatalf("exit = %d, want %d\n%s", got.Code, ExitNotFound, got)
	}
	for _, want := range []string{"One", "Two", "--dir"} {
		if !strings.Contains(got.Stderr, want) {
			t.Errorf("the error should mention %q:\n%s", want, got.Stderr)
		}
	}
}

// §5.1.11 precedence, and the rule that a bad MM_REPORT_PERIOD is a usage error
// naming the variable rather than a silent fallback.
func TestReportPeriodPrecedence(t *testing.T) {
	r, _ := newProject(t)

	got := r.run("--report")
	if !strings.Contains(got.Stdout, "last-week") || !strings.Contains(got.Stdout, "from default") {
		t.Errorf("the default period and its source must be stated:\n%s", got.Stdout)
	}

	env := r
	env.period = "this-week"
	got = env.run("--report")
	if !strings.Contains(got.Stdout, "from environment") {
		t.Errorf("MM_REPORT_PERIOD should be used and named:\n%s", got.Stdout)
	}

	got = env.run("--report", "--period", "2026-W31")
	if !strings.Contains(got.Stdout, "2026-W31") || !strings.Contains(got.Stdout, "from switch") {
		t.Errorf("a switch beats the variable:\n%s", got.Stdout)
	}

	bad := r
	bad.period = "last-fortnight"
	got = bad.run("--report")
	if got.Code != ExitUsage {
		t.Errorf("exit = %d, want %d", got.Code, ExitUsage)
	}
	if !strings.Contains(got.Stderr, "MM_REPORT_PERIOD") {
		t.Errorf("the error must name the variable:\n%s", got.Stderr)
	}
}

func TestHelpAndVersion(t *testing.T) {
	r := runner{cwd: t.TempDir()}

	got := r.run("--help")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "usage:") {
		t.Errorf("--help: %s", got)
	}
	// --help with an operation prints that operation's page (§3.4).
	got = r.run("--help", "--start")
	if !strings.Contains(got.Stdout, "--slot") || strings.Contains(got.Stdout, "usage: mm --OPERATION") {
		t.Errorf("--help --start should print the start page:\n%s", got.Stdout)
	}
	got = r.run("--version")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, Version) {
		t.Errorf("--version: %s", got)
	}
	// Both work with no project anywhere in sight.
	if got := r.run("--help"); got.Code != ExitOK {
		t.Errorf("--help must not need a directory: %s", got)
	}
}

// F1 (code-review-007): --help and --version are answers, not operation
// output, so a machine mode must not swallow them. An empty stdout with exit
// 0 would look exactly like a successful run that produced nothing.
func TestHelpAndVersionUnderMachineModes(t *testing.T) {
	r := runner{cwd: t.TempDir()}

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--help"}, "usage:"},
		{[]string{"--help", "--start"}, "--slot"},
		{[]string{"--version"}, Version},
	}
	for _, mode := range []string{"--json", "--porcelain"} {
		for _, c := range cases {
			args := append(append([]string{}, c.args...), mode)
			got := r.run(args...)
			if got.Code != ExitOK {
				t.Errorf("%v: exit %d, want %d", args, got.Code, ExitOK)
			}
			if !strings.Contains(got.Stdout, c.want) {
				t.Errorf("%v: expected %q in stdout:\n%s", args, c.want, got.Stdout)
			}
		}
	}
}

// Both machine modes are implemented now (T-0036, T-0038). What must stay true
// is that a mode never silently degrades into human output: a script would
// parse prose and get nonsense.
func TestMachineModesDoNotDegradeIntoProse(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Something")

	for _, flag := range []string{"--json", "--porcelain"} {
		got := r.run("--list", flag)
		if got.Code != ExitOK {
			t.Errorf("%s: %s", flag, got)
		}
		if strings.Contains(got.Stdout, "# Test Project") {
			t.Errorf("%s emitted the human listing:\n%s", flag, got.Stdout)
		}
	}
}

// --quiet suppresses commentary, not the one value the caller cannot get any
// other way, and not errors.
func TestQuiet(t *testing.T) {
	r, _ := newProject(t)

	got := r.run("--add", "Something", "--quiet")
	if strings.TrimSpace(got.Stdout) != "T-0001" {
		t.Errorf("--add --quiet should print just the id, got %q", got.Stdout)
	}
	got = r.run("--show", "T-9999", "--quiet")
	if got.Stderr == "" {
		t.Error("errors must print even under --quiet")
	}
}

// fixturePath returns an absolute path to a testdata fixture, skipping the test
// when the corpus is not present rather than failing on a missing file.
func fixturePath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Skipf("fixture corpus not present: %v", err)
	}
	return abs
}

func readAll(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.WriteString(path)
		b.Write(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// §5.2: --block and --unblock are sugar over --move, and --note is the
// highest-frequency write in daily use.
func TestBlockUnblockNote(t *testing.T) {
	r, dir := newProject(t)
	r.run("--add", "Something")

	// I5 requires a reason, so the sugar requires one too.
	if got := r.run("--block", "T-0001"); got.Code != ExitUsage {
		t.Errorf("--block without a reason: exit = %d, want %d", got.Code, ExitUsage)
	}
	if got := r.run("--block", "T-0001", "--reason", "waiting on ops"); got.Code != ExitOK {
		t.Fatalf("block: %s", got)
	}
	backlog := readFileAt(t, dir, "backlog.md")
	if !strings.Contains(backlog, "blocked:waiting on ops") {
		t.Errorf("the reason was not recorded:\n%s", backlog)
	}

	if got := r.run("--unblock", "T-0001"); got.Code != ExitOK {
		t.Fatalf("unblock: %s", got)
	}
	backlog = readFileAt(t, dir, "backlog.md")
	if strings.Contains(backlog, "blocked:") {
		t.Errorf("unblocking must drop the reason (I5):\n%s", backlog)
	}

	// --note takes the id as its value and the text positionally.
	if got := r.run("--note", "T-0001", "found the cause"); got.Code != ExitOK {
		t.Fatalf("note: %s", got)
	}
	if !strings.Contains(readFileAt(t, dir, "details/T-0001.md"), "found the cause") {
		t.Error("the note was not recorded")
	}
	if got := r.run("--note", "T-0001"); got.Code != ExitUsage {
		t.Errorf("--note with no text: exit = %d, want %d", got.Code, ExitUsage)
	}

	if got := r.run("--check"); got.Code != ExitOK {
		t.Errorf("check: %s", got)
	}
}

func readFileAt(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
