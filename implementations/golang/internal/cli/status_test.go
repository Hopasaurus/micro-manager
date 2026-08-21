package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// T-0071: --status, --next and --search (spec-tools.md §5.2).

// The status screen is one read over the whole directory: project and WIP,
// what is in each slot, counts by section, and the featured Ready items.
// The read paths (--status, --search, --next above) still have to work
// against a version-1 directory (spec-tools.md §5.3.4's longevity policy),
// but the mutations that would ordinarily build one up (--add/--start/
// --finish) now refuse (T-0236) - so this hand-writes the post-mutation v1
// state directly via writeV1Fixture instead of driving the CLI to reach it.
// T-0001 "First ready" is already finished; T-0002 "Oldest ready", T-0003
// "Blocked thing" and T-0004 "Someday thing" are exactly where --add would
// have left them.
func TestStatusScreen(t *testing.T) {
	r, dir := newProject(t)
	writeV1Fixture(t, dir, `---
doc: backlog
version: 1
project: Test Project
next_id: T-0005
updated: 2026-07-30
---

# Backlog

## Ready

- [ ] [T-0002] Oldest ready | created:2026-07-01

## Blocked

- [ ] [T-0003] Blocked thing | blocked:waiting | created:2026-07-30

## Someday

- [ ] [T-0004] Someday thing | created:2026-07-30
`, `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0001] First ready | prio:high | created:2026-07-30 | done:2026-07-30 | outcome:shipped
`)

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

	// An occupied slot names its item: T-0005 "Working thing", already in
	// working.01.md rather than reached there via --add + --start.
	writeV1Fixture(t, dir, `---
doc: backlog
version: 1
project: Test Project
next_id: T-0006
updated: 2026-07-30
---

# Backlog

## Ready

- [ ] [T-0002] Oldest ready | created:2026-07-01

## Blocked

- [ ] [T-0003] Blocked thing | blocked:waiting | created:2026-07-30

## Someday

- [ ] [T-0004] Someday thing | created:2026-07-30
`, "")
	working := `---
doc: working
version: 1
status: working
id: T-0005
title: Working thing
prio: med
tags: null
detail: null
created: 2026-07-30
started: 2026-07-30
---

# Working

## Task

Working thing

## Plan

## Notes

## Blockers
`
	if err := os.WriteFile(filepath.Join(dir, "working.01.md"), []byte(working), 0o644); err != nil {
		t.Fatal(err)
	}

	got = r.run("--status")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "working.01.md: - [ ] [T-0005]") {
		t.Errorf("occupied slot not shown:\n%s", got.Stdout)
	}
}

// --next prints the top of ## Ready and exits 0.
func TestNextPrintsTop(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Bottom", "--stage", "ready", "--created", "2026-07-01")
	r.run("--add", "Top of ready", "--stage", "ready", "--top")

	got := r.run("--next")
	if got.Code != ExitOK {
		t.Fatalf("--next: %s", got)
	}
	if !strings.Contains(got.Stdout, "T-0002  Top of ready") &&
		!strings.Contains(got.Stdout, "T-0002 Top of ready") {
		t.Errorf("--next printed the wrong item:\n%s", got.Stdout)
	}
	// The same item --list shows at the top of the stage.
	list := r.run("--list", "--stage", "ready")
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
// writeV1Fixture overwrites a freshly-inited project's backlog.md/done.md
// directly with hand-built version-1 content, so a test can exercise a v1
// state (an item already closed, another already in Someday) that --add/
// --start/--finish can no longer reach through the CLI now that they refuse
// against a version-1 directory (T-0236). --search itself is a read
// operation and is unaffected by that guard.
func writeV1Fixture(t *testing.T, dir, backlog, done string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	if done != "" {
		if err := os.WriteFile(filepath.Join(dir, "done.md"), []byte(done), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchReportsHits(t *testing.T) {
	r, dir := newProject(t)
	writeV1Fixture(t, dir, `---
doc: backlog
version: 1
project: Test Project
next_id: T-0003
updated: 2026-07-30
---

# Backlog

## Ready

## Blocked

## Someday

- [ ] [T-0002] Deploy the dashboard | created:2026-07-30
`, `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0001] Fix the deploy script | tags:infra | created:2026-07-30 | done:2026-07-30 | outcome:shipped
`)

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
	r, _ := v2Project(t)
	r.run("--add", "Deploy script", "--stage", "ready", "--tag", "infra")
	r.run("--add", "Other infra thing", "--stage", "ready", "--tag", "ci")

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

	// --state done excludes everything still open.
	//
	// --state backlog/working is not tested here on version 2: those two
	// names collapse into one open State ("board") once an item's location
	// is a stage: rather than a section/slot, and --search's --state flag
	// (run.go's runSearch) still only accepts the three version-1 literal
	// names - it has no "board" (or "all") value to ask for that. That is a
	// real, separate CLI gap, not something this test should paper over by
	// asserting behavior the flag cannot currently express for v2.
	onlyDone := r.run("--search", "infra", "--state", "done")
	if !strings.Contains(onlyDone.Stdout, "no matches") {
		t.Errorf("--state done found an open item:\n%s", onlyDone.Stdout)
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
	r, dir := v2Project(t)
	r.run("--add", "Fix the deploy script", "--stage", "ready", "--tag", "infra")
	r.run("--add", "Something else", "--stage", "someday")

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
