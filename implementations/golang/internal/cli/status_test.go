package cli

import (
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// T-0071: --status, --next and --search (spec-tools.md §5.2).

// The status screen is one read over the whole directory: project and WIP,
// what is in each slot, counts by section, and the featured Ready items.
func TestStatusScreen(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "First ready", "--section", "ready", "--prio", "high")
	r.run("--add", "Oldest ready", "--section", "ready", "--created", "2026-07-01")
	r.run("--add", "Blocked thing", "--section", "blocked", "--blocked", "waiting")
	r.run("--add", "Someday thing", "--section", "someday")
	r.run("--start", "T-0001")
	r.run("--finish", "T-0001")

	got := r.run("--status")
	if got.Code != ExitOK {
		t.Fatalf("status: %s", got)
	}
	for _, want := range []string{
		"# Test Project",
		"(wip 0/1)",
		"working.01.md: idle",
		"ready:     1", // only T-0002 remains (T-0001 finished)
		"blocked:   1",
		"someday:   1",
		"done:      1",
		"next:   T-0002", // the top of Ready
		"oldest: T-0002", // never started, so it is the oldest untouched
		"created 2026-07-01",
	} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("status output lacks %q:\n%s", want, got.Stdout)
		}
	}

	// An occupied slot names its item.
	r.run("--add", "Working thing", "--section", "ready")
	r.run("--start", "T-0003")
	got = r.run("--status")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "working.01.md: - [ ] [T-0003]") {
		t.Errorf("occupied slot not shown:\n%s", got.Stdout)
	}
}

// --next prints the top of ## Ready and exits 0.
func TestNextPrintsTop(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Bottom", "--section", "ready", "--created", "2026-07-01")
	r.run("--add", "Top of ready", "--section", "ready", "--top")

	got := r.run("--next")
	if got.Code != ExitOK {
		t.Fatalf("--next: %s", got)
	}
	if !strings.Contains(got.Stdout, "T-0002  Top of ready") &&
		!strings.Contains(got.Stdout, "T-0002 Top of ready") {
		t.Errorf("--next printed the wrong item:\n%s", got.Stdout)
	}
	// The same item --list shows at the top of the section.
	list := r.run("--list", "--section", "ready")
	if !strings.Contains(list.Stdout, "T-0002") {
		t.Errorf("--list does not agree with --next:\n%s", list.Stdout)
	}
}

// --next on an empty ## Ready exits non-zero (code 3), so a script stops
// rather than starting something arbitrary.
func TestNextEmptyExitsNotFound(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Someday only", "--section", "someday")

	got := r.run("--next")
	if got.Code != ExitNotFound {
		t.Errorf("exit = %d, want %d (stderr: %s)", got.Code, ExitNotFound, got.Stderr)
	}
	if got.Stderr == "" {
		t.Error("an empty --next must say why on stderr")
	}
}

// --search finds the same things the library finds, and reports state and the
// exact navigable location per hit.
func TestSearchReportsHits(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Fix the deploy script", "--section", "ready", "--tag", "infra")
	r.run("--add", "Deploy the dashboard", "--section", "someday")
	r.run("--start", "T-0001")
	r.run("--finish", "T-0001")

	got := r.run("--search", "deploy")
	if got.Code != ExitOK {
		t.Fatalf("--search: %s", got)
	}
	for _, want := range []string{
		"T-0001",   // the finished item, title match
		"T-0002",   // the someday item, title match
		"done",     // T-0001's state
		"backlog/", // T-0002 is backlog
		"title",
		"backlog.md:", // a navigable location
		"done.md:",
	} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("search output lacks %q:\n%s", want, got.Stdout)
		}
	}
}

// --field narrows where to look, --state narrows where to look, and --regex
// switches the matcher; all three must reach the library's Search.
func TestSearchModifiers(t *testing.T) {
	r, _ := newProject(t)
	r.run("--add", "Deploy script", "--section", "ready", "--tag", "infra")
	r.run("--add", "Other infra thing", "--section", "ready", "--tag", "ci")

	// A title hit exists; a tags-only search misses it.
	title := r.run("--search", "deploy", "--field", "title")
	if !strings.Contains(title.Stdout, "T-0001") {
		t.Errorf("title search missed the title hit:\n%s", title.Stdout)
	}
	tags := r.run("--search", "deploy", "--field", "tags")
	if strings.Contains(tags.Stdout, "T-0001") {
		t.Errorf("tags search found a title hit:\n%s", tags.Stdout)
	}
	if !strings.Contains(tags.Stdout, "no matches") {
		t.Errorf("tags search should have no matches:\n%s", tags.Stdout)
	}

	// --state done excludes working and backlog items.
	onlyDone := r.run("--search", "infra", "--state", "done")
	if !strings.Contains(onlyDone.Stdout, "no matches") {
		t.Errorf("--state done found backlog items:\n%s", onlyDone.Stdout)
	}
	r.run("--start", "T-0001")
	backlogOnly := r.run("--search", "infra", "--state", "backlog")
	if strings.Contains(backlogOnly.Stdout, "T-0001") {
		t.Errorf("--state backlog found a working item:\n%s", backlogOnly.Stdout)
	}

	// A regex matches as written; a bad regex is a usage error, not a crash.
	re := r.run("--search", "^Deploy", "--regex")
	if !strings.Contains(re.Stdout, "T-0001") {
		t.Errorf("regex search missed:\n%s", re.Stdout)
	}
	bad := r.run("--search", "(", "--regex")
	if bad.Code != ExitUsage {
		t.Errorf("bad regex exit = %d, want %d", bad.Code, ExitUsage)
	}
	if code := r.run("--search", "deploy", "--field", "bogus").Code; code != ExitUsage {
		t.Errorf("--field bogus should be a usage error, got exit %d", code)
	}
}

// The search result and the library agree, because the CLI passes the query
// through rather than reimplementing the matcher (T-0042).
func TestSearchMatchesLibrary(t *testing.T) {
	r, dir := newProject(t)
	r.run("--add", "Fix the deploy script", "--section", "ready", "--tag", "infra")
	r.run("--add", "Something else", "--section", "someday")

	store, err := mm.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := store.Search(mm.SearchRequest{Query: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Item.ID != "T-0001" {
		t.Fatalf("library hits = %+v, want exactly T-0001", hits)
	}

	got := r.run("--search", "deploy")
	if !strings.Contains(got.Stdout, "T-0001") || strings.Contains(got.Stdout, "T-0002") {
		t.Errorf("CLI and library disagree:\n%s", got.Stdout)
	}
}
