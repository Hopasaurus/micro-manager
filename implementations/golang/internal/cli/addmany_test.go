package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0187 — --add-many end to end (spec-tools.md §5.2.1).
//
// The library's own tests own the line grammar and the all-or-nothing
// transaction. What is tested here is the wrapper's half: where the input comes
// from, the modifiers-are-defaults rule, every ID reaching every output mode,
// and the two refusals the CLI owns — a terminal with nothing piped in, and the
// detail switches.

// piped runs an invocation with text on stdin, which is how a bulk add is
// normally spelled.
func piped(r runner, stdin string, args ...string) result {
	return r.runWith(Env{Stdin: strings.NewReader(stdin)}, args...)
}

func TestAddManyFromStdin(t *testing.T) {
	r, _ := newProject(t)

	got := piped(r, `Fix the deploy script | prio:high | tags:infra,ci

- Rotate the leaked token | prio:high
* Pasted out of a checklist
Plain title
`, "--add-many")
	if got.Code != ExitOK {
		t.Fatalf("add-many failed: %s", got)
	}
	// Every assigned ID, in the order given: four handles for four items.
	for _, want := range []string{"T-0001", "T-0002", "T-0003", "T-0004"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("id %s missing from the output:\n%s", want, got.Stdout)
		}
	}
	if !strings.Contains(got.Stdout, "4 items added to Ready") {
		t.Errorf("the count leads the report:\n%s", got.Stdout)
	}
	// The blank line was skipped rather than becoming an empty item, and the
	// bullets were stripped rather than becoming part of a title.
	list := r.run("--list")
	if strings.Contains(list.Stdout, "- Rotate") || strings.Contains(list.Stdout, "* Pasted") {
		t.Errorf("a bullet survived into a title:\n%s", list.Stdout)
	}
	if !strings.Contains(list.Stdout, "Rotate the leaked token") {
		t.Errorf("bulleted item missing:\n%s", list.Stdout)
	}
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("the directory it wrote must pass the checker: %s", got)
	}
}

func TestAddManyFromAFile(t *testing.T) {
	r, _ := newProject(t)
	path := filepath.Join(t.TempDir(), "items.txt")
	if err := os.WriteFile(path, []byte("One\nTwo | prio:low\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--add-many", path)
	if got.Code != ExitOK {
		t.Fatalf("add-many from a file failed: %s", got)
	}
	if !strings.Contains(got.Stdout, "2 items added") {
		t.Errorf("expected two items:\n%s", got.Stdout)
	}
	// "-" is stdin even when a file would have been legal there.
	dash := piped(r, "Three\n", "--add-many", "-")
	if dash.Code != ExitOK {
		t.Fatalf("--add-many - should read stdin: %s", dash)
	}
	if !strings.Contains(dash.Stdout, "T-0003") {
		t.Errorf("expected T-0003:\n%s", dash.Stdout)
	}
}

// §5.2.1: the modifiers are defaults, and a line that names the same field wins.
func TestAddManyModifiersAreDefaults(t *testing.T) {
	r, _ := newProject(t)

	got := piped(r, "Takes the default\nNames its own | prio:low | tags:docs\n",
		"--add-many", "--prio", "high", "--tag", "infra")
	if got.Code != ExitOK {
		t.Fatalf("add-many failed: %s", got)
	}
	if !strings.Contains(got.Stdout, "prio:high | tags:infra") {
		t.Errorf("the default did not reach the first item:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "prio:low | tags:docs") {
		t.Errorf("the line did not win over the default:\n%s", got.Stdout)
	}
}

// One run, two sections: a line's own blocked: reason takes it to Blocked,
// exactly as it would under --add.
func TestAddManySpansSections(t *testing.T) {
	r, _ := newProject(t)

	got := piped(r, "Ready one\nWaiting | blocked:on the vendor\nReady two\n", "--add-many")
	if got.Code != ExitOK {
		t.Fatalf("add-many failed: %s", got)
	}
	if !strings.Contains(got.Stdout, "Ready and Blocked") {
		t.Errorf("the report should name both sections:\n%s", got.Stdout)
	}
	blocked := r.run("--list", "--section", "blocked")
	if !strings.Contains(blocked.Stdout, "Waiting") {
		t.Errorf("the blocked item is not in Blocked:\n%s", blocked.Stdout)
	}
	// I5 is why the reason had to travel with it.
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("check: %s", got)
	}
}

// §5.2.1: --top puts the batch at the top, still in the input's order.
func TestAddManyTopKeepsTheInputOrder(t *testing.T) {
	r, _ := v2Project(t)
	if got := r.run("--add", "Already here"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}

	if got := piped(r, "First\nSecond\n", "--add-many", "--top"); got.Code != ExitOK {
		t.Fatalf("add-many: %s", got)
	}
	list := r.run("--porcelain", "--list")
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(list.Stdout), "\n") {
		ids = append(ids, strings.SplitN(line, "\t", 2)[0])
	}
	want := []string{"T-0002", "T-0003", "T-0001"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("ready order = %v, want %v — a --top that reverses the batch is the bug", ids, want)
	}
}

// The property the operation exists for. A bad line means NOTHING is written,
// and the error names the line to fix — counted in INPUT lines, blanks
// included, because that is what the user is looking at.
func TestAddManyIsAllOrNothing(t *testing.T) {
	r, dir := newProject(t)
	before := readFile(t, dir+"/backlog.md")

	got := piped(r, "Fine\n\nAlso fine\nBad | prio:urgent\nNever reached\n", "--add-many")
	if got.Code != ExitUsage {
		t.Fatalf("a bad line should be a usage error, got %s", got)
	}
	if !strings.Contains(got.Stderr, "line 4") {
		t.Errorf("the error must name the input line:\n%s", got.Stderr)
	}
	if after := readFile(t, dir+"/backlog.md"); after != before {
		t.Errorf("a failed batch wrote to backlog.md:\n%s", after)
	}

	// A failure the LIBRARY raises (a pipe in a title is caught when the item
	// is built) is located the same way, though it counts requests and the
	// input has blank lines the requests do not.
	pipe := piped(r, "\n\nFine\nBad |title | prio:high\n", "--add-many")
	if pipe.Code != ExitUsage {
		t.Fatalf("a pipe in a title should be a usage error, got %s", pipe)
	}
	if !strings.Contains(pipe.Stderr, "line 4") {
		t.Errorf("the error must name the input line, not the request index:\n%s", pipe.Stderr)
	}
}

// A pasted item line brings an id, and honouring it would reuse a number (I2).
func TestAddManyRefusesPastedItemLines(t *testing.T) {
	r, _ := newProject(t)

	got := piped(r, "- [ ] [T-0042] Fix the deploy script | prio:high\n", "--add-many")
	if got.Code != ExitUsage {
		t.Fatalf("a pasted item line should be refused, got %s", got)
	}
	if !strings.Contains(got.Stderr, "remove the [T-0042]") {
		t.Errorf("the error should name the fix:\n%s", got.Stderr)
	}
}

func TestAddManyDryRunWritesNothing(t *testing.T) {
	r, dir := newProject(t)
	before := readFile(t, dir+"/backlog.md")

	got := piped(r, "One\nTwo\n", "--add-many", "--dry-run")
	if got.Code != ExitOK {
		t.Fatalf("dry run failed: %s", got)
	}
	// The ids a real run would allocate are part of what a dry run reports.
	if !strings.Contains(got.Stdout, "T-0001") || !strings.Contains(got.Stdout, "T-0002") {
		t.Errorf("a dry run must report what it would allocate:\n%s", got.Stdout)
	}
	if after := readFile(t, dir+"/backlog.md"); after != before {
		t.Errorf("a dry run wrote the file")
	}
}

func TestAddManyOutputModes(t *testing.T) {
	r, _ := newProject(t)
	const input = "One | prio:high\nTwo | tags:infra\n"

	// --json: result is the ARRAY of created items, and changes has one entry
	// per item, all in one envelope.
	got := piped(r, input, "--add-many", "--json")
	if got.Code != ExitOK {
		t.Fatalf("json run failed: %s", got)
	}
	var env struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Result    []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"result"`
		Changes []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("envelope is not JSON: %v\n%s", err, got.Stdout)
	}
	if !env.OK || env.Operation != "add-many" {
		t.Errorf("envelope = %+v", env)
	}
	if len(env.Result) != 2 || env.Result[0].ID != "T-0001" || env.Result[1].ID != "T-0002" {
		t.Errorf("result should be both items in order: %+v", env.Result)
	}
	if len(env.Changes) != 2 {
		t.Errorf("expected one change per item: %+v", env.Changes)
	}

	// --porcelain: one record per created item, same columns as --add.
	r2, _ := newProject(t)
	porc := piped(r2, input, "--add-many", "--porcelain")
	lines := strings.Split(strings.TrimSpace(porc.Stdout), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected two records:\n%s", porc.Stdout)
	}
	if fields := strings.Split(lines[0], "\t"); len(fields) != 6 || fields[0] != "T-0001" {
		t.Errorf("porcelain record = %q", lines[0])
	}

	// --quiet: the ids and nothing else. Quiet suppresses commentary, not the
	// values the caller came for.
	r3, _ := newProject(t)
	quiet := piped(r3, input, "--add-many", "--quiet")
	if strings.TrimSpace(quiet.Stdout) != "T-0001\nT-0002" {
		t.Errorf("quiet output = %q", quiet.Stdout)
	}
}

// The two refusals the wrapper owns.
func TestAddManyRefusals(t *testing.T) {
	r, _ := newProject(t)

	// A terminal with nothing piped in would look like a hang.
	tty := r.runWith(Env{Interactive: true, Stdin: strings.NewReader("")}, "--add-many")
	if tty.Code != ExitUsage {
		t.Errorf("reading a terminal should be refused, got %s", tty)
	}
	if !strings.Contains(tty.Stderr, "pipe a list in") {
		t.Errorf("the refusal should say what to do instead:\n%s", tty.Stderr)
	}

	// One body cannot belong to N items.
	for _, sw := range [][]string{{"--detail"}, {"--detail-text", "x"}, {"--detail-file", "x"}} {
		got := piped(r, "One\n", append([]string{"--add-many"}, sw...)...)
		if got.Code != ExitUsage {
			t.Errorf("%v should be refused, got %s", sw, got)
		}
	}

	// An input with nothing in it is a usage error rather than a silent no-op:
	// a caller that piped an empty file wanted something to happen.
	empty := piped(r, "\n\n\n", "--add-many")
	if empty.Code != ExitUsage {
		t.Errorf("an empty batch should be refused, got %s", empty)
	}
	if !strings.Contains(empty.Stderr, "one item per line") {
		t.Errorf("the refusal should say what input looks like:\n%s", empty.Stderr)
	}
}

// --add-many is one operation like any other: it cannot be combined with a
// second one, and its help page exists.
func TestAddManyIsAnOrdinaryOperation(t *testing.T) {
	r, _ := newProject(t)

	two := piped(r, "One\n", "--add-many", "--add", "Also")
	if two.Code != ExitUsage {
		t.Errorf("two operations should be a usage error, got %s", two)
	}
	help := r.run("--help", "--add-many")
	if help.Code != ExitOK || !strings.Contains(help.Stdout, "one per input line") {
		t.Errorf("--help --add-many should print its page:\n%s", help.Stdout)
	}
	if !strings.Contains(r.run("--help").Stdout, "--add-many") {
		t.Errorf("the general help should list the operation")
	}
}
