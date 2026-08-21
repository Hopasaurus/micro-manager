package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
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
	r, dir := v2Project(t, "--slots", "2")

	// --add reports the assigned ID. It is the handle for every command that
	// follows, so silence here would be a defect, not brevity.
	got := r.run("--add", "Fix the deploy script", "--prio", "high", "--tag", "infra")
	if got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}
	if !strings.Contains(got.Stdout, "T-0001") {
		t.Errorf("add must report the id:\n%s", got.Stdout)
	}

	// Version 2 has no slots to name: "T-0001 started", not "in slot N".
	if got := r.run("--start", "1"); got.Code != ExitOK {
		t.Fatalf("start: %s", got)
	} else if !strings.Contains(got.Stdout, "started") {
		t.Errorf("start should confirm the move:\n%s", got.Stdout)
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
	r, _ := v2Project(t)
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
	r, dir := v2Project(t)
	r.run("--add", "First")
	before := readAll(t, dir)

	for _, args := range [][]string{
		{"--add", "Second", "--dry-run"},
		{"--start", "T-0001", "--dry-run"},
		{"--finish", "T-0001", "--dry-run"},
		{"--remove", "T-0001", "--force", "--dry-run"},
		{"--wip", "3", "--stage", "working", "--dry-run"},
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

// F10 (code-review-007): an empty cwd (getwd failed) means there is nothing
// to walk up from; the search must not fall back to reading the process's
// actual working directory.
func TestSearchUpwardWithEmptyCwd(t *testing.T) {
	got, err := searchUpward("")
	if got != "" || err != nil {
		t.Errorf("searchUpward(\"\") = %q, %v; want \"\", nil", got, err)
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

// --include-archives was a usage error until the library could read a
// done-YYYY.md (T-0043). The switch now reaches the option, and the report's
// "this period has been archived" warning goes quiet once it does.
func TestReportIncludeArchives(t *testing.T) {
	r, dir := newProject(t)

	archive := "---\ndoc: done\nversion: 1\n---\n\n# Done 2025\n\n## 2025-12\n\n" +
		"- [x] [T-0001] Ancient history | created:2025-11-01 | done:2025-12-24 | outcome:shipped\n"
	if err := os.WriteFile(filepath.Join(dir, "done-2025.md"), []byte(archive), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--report", "--period", "2025-12")
	if got.Code != ExitOK {
		t.Fatalf("report failed: %s", got)
	}
	if strings.Contains(got.Stdout, "T-0001") {
		t.Errorf("the archive must not be read unless asked for:\n%s", got.Stdout)
	}

	got = r.run("--report", "--period", "2025-12", "--include-archives")
	if got.Code != ExitOK {
		t.Fatalf("--include-archives should be accepted now: %s", got)
	}
	if !strings.Contains(got.Stdout, "T-0001") {
		t.Errorf("the archived item should be in the report:\n%s", got.Stdout)
	}

	// And through the machine modes, where a caller reads fields rather than prose.
	got = r.run("--report", "--period", "2025-12", "--include-archives", "--porcelain")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "T-0001") {
		t.Errorf("porcelain report: %s", got)
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
//
// T-0202 changed one half of this: --help --json is now the capability list
// (§3.4.1) rather than prose, because there IS a result to put in an envelope
// and a caller who asked for JSON should not have to scrape a help page. The
// rule the test exists for is unchanged — nothing is swallowed — and every
// other combination still answers in prose.
func TestHelpAndVersionUnderMachineModes(t *testing.T) {
	r := runner{cwd: t.TempDir()}

	prose := []struct {
		args []string
		want string
	}{
		{[]string{"--help", "--porcelain"}, "usage:"},
		{[]string{"--help", "--start", "--porcelain"}, "--slot"},
		{[]string{"--version", "--json"}, Version},
		{[]string{"--version", "--porcelain"}, Version},
	}
	for _, c := range prose {
		got := r.run(c.args...)
		if got.Code != ExitOK {
			t.Errorf("%v: exit %d, want %d", c.args, got.Code, ExitOK)
		}
		if !strings.Contains(got.Stdout, c.want) {
			t.Errorf("%v: expected %q in stdout:\n%s", c.args, c.want, got.Stdout)
		}
		if strings.HasPrefix(strings.TrimSpace(got.Stdout), "{") {
			t.Errorf("%v: should still answer in prose:\n%s", c.args, got.Stdout)
		}
	}

	// --help --json: the envelope, and the operation's page inside it when one
	// is named. Nothing is swallowed either way — that is the rule.
	for _, args := range [][]string{{"--help", "--json"}, {"--help", "--start", "--json"}} {
		got := r.run(args...)
		if got.Code != ExitOK {
			t.Errorf("%v: exit %d", args, got.Code)
		}
		var env struct {
			OK     bool `json:"ok"`
			Result struct {
				Operations []string `json:"operations"`
				Usage      string   `json:"usage"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
			t.Fatalf("%v: not the envelope: %v\n%s", args, err, got.Stdout)
		}
		if !env.OK || len(env.Result.Operations) == 0 {
			t.Errorf("%v: want the capability list, got %+v", args, env)
		}
	}
	if got := r.run("--help", "--start", "--json"); !strings.Contains(got.Stdout, "--slot") {
		t.Errorf("the named operation's page should ride along:\n%s", got.Stdout)
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

// F7 (code-review-007): the machine-mode scan honours last wins, exactly like
// the parser. --json --json=false must disable the mode (human output instead
// of an envelope), and a later =true re-enables it.
func TestMachineModeSwitchLastWins(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Something")

	got := r.run("--list", "--json", "--json=false")
	if got.Code != ExitOK {
		t.Fatalf("%s", got)
	}
	if strings.Contains(got.Stdout, `"ok"`) || !strings.Contains(got.Stdout, "# Test Project") {
		t.Errorf("--json --json=false must fall back to the human listing:\n%s", got.Stdout)
	}

	got = r.run("--list", "--json=false", "--json")
	if !strings.Contains(got.Stdout, `"ok": true`) {
		t.Errorf("--json=false --json must emit the envelope:\n%s", got.Stdout)
	}

	// The value spellings are the parser's own, case-insensitively.
	got = r.run("--list", "--json=TRUE")
	if !strings.Contains(got.Stdout, `"ok": true`) {
		t.Errorf("--json=TRUE must emit the envelope:\n%s", got.Stdout)
	}
}

// --quiet suppresses commentary, not the one value the caller cannot get any
// other way, and not errors.
func TestQuiet(t *testing.T) {
	r, _ := v2Project(t)

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

// F6 (code-review-007): a surplus positional is a typo — an unquoted
// multi-word title or query — and is refused rather than silently dropped.
// The "--"-protected form, where the positionals ARE the subject, still works.
func TestSurplusPositionalsAreRefused(t *testing.T) {
	r, _ := v2Project(t)

	for _, args := range [][]string{
		{"--add", "Title", "surplus"},
		{"--search", "query", "surplus"},
		{"--add", "Title", "--", "surplus"},
	} {
		if got := r.run(args...); got.Code != ExitUsage {
			t.Errorf("%v: exit %d, want %d\n%s", args, got.Code, ExitUsage, got)
		}
	}

	if got := r.run("--add", "--", "A title with spaces"); got.Code != ExitOK {
		t.Errorf("--add -- TITLE: %s", got)
	}
	if got := r.run("--search", "--", "a query with spaces"); got.Code != ExitOK {
		t.Errorf("--search -- QUERY: %s", got)
	}
}

// §5.2: --block and --unblock are sugar over --move, and --note is the
// highest-frequency write in daily use.
func TestBlockUnblockNote(t *testing.T) {
	r, dir := v2Project(t)
	r.run("--add", "Something")

	// I5 requires a reason, so the sugar requires one too.
	if got := r.run("--block", "T-0001"); got.Code != ExitUsage {
		t.Errorf("--block without a reason: exit = %d, want %d", got.Code, ExitUsage)
	}
	if got := r.run("--block", "T-0001", "--reason", "waiting on ops"); got.Code != ExitOK {
		t.Fatalf("block: %s", got)
	}
	board := readFileAt(t, dir, "board.md")
	if !strings.Contains(board, "reason:waiting on ops") {
		t.Errorf("the reason was not recorded:\n%s", board)
	}

	if got := r.run("--unblock", "T-0001"); got.Code != ExitOK {
		t.Fatalf("unblock: %s", got)
	}
	board = readFileAt(t, dir, "board.md")
	if strings.Contains(board, "stage:blocked") {
		t.Errorf("unblocking must move off blocked:\n%s", board)
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

// T-0191 — the emptiness test at the CLI boundary (spec-file-format.md
// Appendix B, spec-tools.md §4).
//
// The library's tests own the walk. What is checked here is the asymmetry the
// spec draws, which only a front end can express: DISCOVERY skips a
// name-matching directory that holds neither board file, and a directory the
// user NAMED is an error instead.
func TestEmptinessTestSkipsDiscoveryButNotAnExplicitPath(t *testing.T) {
	root := t.TempDir()
	// A source tree that happens to hold a directory of the name — the case
	// this repository is itself an example of.
	impostor := filepath.Join(root, "src", "micro-manager")
	if err := os.MkdirAll(impostor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(impostor, "README.md"), []byte("a repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Discovery: --find says there is nothing rather than listing it. Exit 0 —
	// an empty result is a result for this operation, unlike --next, which
	// documents a non-zero exit for an empty Ready.
	found := (runner{cwd: root}).run("--find")
	if found.Code != ExitOK {
		t.Errorf("--find over an empty tree is not an error, got: %s", found)
	}
	if !strings.Contains(found.Stdout, "no micro-manager directories found") {
		t.Errorf("--find should say it found nothing:\n%s", found.Stdout)
	}
	if strings.Contains(found.Stdout, "src") {
		t.Errorf("--find listed a directory that is not a board:\n%s", found.Stdout)
	}

	// Resolution: the downward search must not select it either, or every
	// command run in a source tree would act on a directory nobody created.
	list := (runner{cwd: root}).run("--list")
	if list.Code != ExitNotFound {
		t.Errorf("the downward search should find no board, got: %s", list)
	}

	// Named explicitly: an error, because silence would report success for a
	// command that did nothing.
	named := (runner{cwd: root}).run("--list", "--dir", impostor)
	if named.Code != ExitNotFound {
		t.Errorf("an explicit path must fail, got: %s", named)
	}
	if !strings.Contains(named.Stderr, "not a micro-manager directory") {
		t.Errorf("the error should say what is wrong:\n%s", named.Stderr)
	}

	// A real board beside it resolves cleanly — the impostor does not make the
	// resolution ambiguous, which is the practical half of the rule.
	if got := (runner{cwd: root}).run("--init", "--project", "Real"); got.Code != ExitOK {
		t.Fatal(got)
	}
	if got := (runner{cwd: filepath.Join(root, "src")}).run("--status"); got.Code != ExitOK {
		t.Errorf("a real board should resolve past the impostor, got: %s", got)
	}
}

// EITHER file, not both: a board that lost one is still discovered, and --check
// reports the loss rather than skipping the directory.
func TestABoardMissingOneFileIsStillCheckedAndFound(t *testing.T) {
	root := t.TempDir()
	if got := (runner{cwd: root}).run("--init", "--project", "Half"); got.Code != ExitOK {
		t.Fatal(got)
	}
	dir := filepath.Join(root, "micro-manager")
	if err := os.Remove(filepath.Join(dir, "done.md")); err != nil {
		t.Fatal(err)
	}

	if got := (runner{cwd: root}).run("--find"); got.Code != ExitOK ||
		!strings.Contains(got.Stdout, "micro-manager") {
		t.Errorf("a half-deleted board must still be found: %s", got)
	}
	got := (runner{cwd: root}).run("--check", "--all")
	if got.Code != ExitInvariantViolation {
		t.Errorf("want the missing file reported, got: %s", got)
	}
	if !strings.Contains(got.Stdout+got.Stderr, "done.md") {
		t.Errorf("the report should name the missing file:\n%s\n%s", got.Stdout, got.Stderr)
	}
}

// T-0194 — two boards in one parent directory (spec-file-format.md Appendix B).
//
// Resolution already refuses to choose between them, which is the ambiguity of
// §4. What this adds is that --check SAYS SO: the command a user runs to ask
// "is anything wrong here?" answered "both clean" for a situation that splits a
// project's history in two.
func TestCheckReportsTwoBoardsInOneDirectory(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	base := runner{cwd: proj}
	if got := base.run("--init", "--project", "Visible"); got.Code != ExitOK {
		t.Fatal(got)
	}
	if got := base.run("--init", "--project", "Hidden",
		"--dir", filepath.Join(proj, ".micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}

	// Checking ONE board reports it: a person working in this directory may
	// never run a scan that sees both.
	got := base.run("--check", "--dir", filepath.Join(proj, "micro-manager"))
	if got.Code != ExitInvariantViolation {
		t.Fatalf("want a finding, got: %s", got)
	}
	if !strings.Contains(got.Stdout, ".micro-manager") {
		t.Errorf("the finding must name the sibling:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "keep one") {
		t.Errorf("the finding must name the fix:\n%s", got.Stdout)
	}

	// And so does the other one — symmetric, or checking the wrong half of a
	// collision would look clean.
	if got := base.run("--check", "--dir", filepath.Join(proj, ".micro-manager")); got.Code != ExitInvariantViolation {
		t.Errorf("want a finding from the hidden board too, got: %s", got)
	}

	// --all reports both, and the exit code carries the verdict.
	all := base.run("--check", "--all")
	if all.Code != ExitInvariantViolation {
		t.Errorf("want --all to fail, got: %s", all)
	}
	if strings.Count(all.Stdout, "a second micro-manager directory") != 2 {
		t.Errorf("want the finding against each board:\n%s", all.Stdout)
	}

	// The finding travels as DATA, not only as a line: a machine caller that
	// reads the envelope must see it, or a plugin reporting "clean" for a
	// failed check is the result.
	js := base.run("--check", "--dir", filepath.Join(proj, "micro-manager"), "--json")
	var env struct {
		Result []struct {
			OK         bool `json:"ok"`
			Violations []struct {
				File    string `json:"file"`
				Message string `json:"message"`
			} `json:"violations"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(js.Stdout), &env); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, js.Stdout)
	}
	if len(env.Result) != 1 || env.Result[0].OK {
		t.Fatalf("the directory should not be reported ok: %+v", env.Result)
	}
	if len(env.Result[0].Violations) != 1 ||
		!strings.Contains(env.Result[0].Violations[0].Message, ".micro-manager") {
		t.Errorf("the violation should carry the collision: %+v", env.Result[0].Violations)
	}
	// A directory-level finding has no file to name, so it points at the
	// directory itself — the same place I10's findings do.
	if env.Result[0].Violations[0].File != "." {
		t.Errorf("file = %q, want %q", env.Result[0].Violations[0].File, ".")
	}
}

// A collision is not an invariant, and a mutation must not be blocked by one:
// the library's validator never looks at the parent, so writing to either board
// keeps working while the user decides which to keep.
func TestACollisionDoesNotBlockWriting(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	base := runner{cwd: proj}
	if got := base.run("--init", "--project", "Visible"); got.Code != ExitOK {
		t.Fatal(got)
	}
	if got := base.run("--init", "--project", "Hidden",
		"--dir", filepath.Join(proj, ".micro-manager")); got.Code != ExitOK {
		t.Fatal(got)
	}

	dir := filepath.Join(proj, "micro-manager")
	if got := base.run("--migrate", "--dir", dir); got.Code != ExitOK {
		t.Fatal(got)
	}
	if got := base.run("--add", "still works", "--dir", dir); got.Code != ExitOK {
		t.Errorf("a collision must not block a write: %s", got)
	}
	// Resolution by SEARCH still refuses, because it cannot know which board
	// the user means (§4).
	if got := base.run("--status"); got.Code != ExitNotFound {
		t.Errorf("want the ambiguity refusal, got: %s", got)
	}
}
