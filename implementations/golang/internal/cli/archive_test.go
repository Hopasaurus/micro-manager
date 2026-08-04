package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0167 — --archive end to end: the first of the §5.3 optional operations to
// reach the CLI.
//
// The library's own tests own the splice; what is tested here is the wrapper's
// half — both spellings of the cutoff resolving to one month, the mandatory
// warning reaching stderr, dry-run writing nothing, and the three output modes
// agreeing about what happened.

// The clock is testDay, 2026-07-30. The groups straddle it so that the default
// cutoff, an explicit --before and an --age all have something to separate.
const archiveDoneCLI = `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [T-0010] July | created:2026-07-01 | done:2026-07-23 | outcome:shipped

## 2026-06

- [x] [T-0008] June | created:2026-05-01 | done:2026-06-30 | outcome:shipped

## 2025-12

- [x] [T-0007] Christmas | created:2025-11-01 | done:2025-12-24 | outcome:shipped
`

func archiveProject(t *testing.T) (runner, string) {
	t.Helper()
	r, dir := newProject(t)
	if err := os.WriteFile(filepath.Join(dir, "done.md"), []byte(archiveDoneCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	// I2: every ID is below next_id, and the fixture's are already spent.
	backlog := readFile(t, filepath.Join(dir, "backlog.md"))
	backlog = strings.Replace(backlog, "next_id: T-0001", "next_id: T-0011", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("fixture should start clean: %s", got)
	}
	return r, dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestArchiveCLIRollsOldMonthsOut(t *testing.T) {
	r, dir := archiveProject(t)

	// Dry-run first: same report, nothing on disk.
	dry := r.run("--archive", "--before", "2026-07", "--dry-run")
	if dry.Code != ExitOK {
		t.Fatalf("dry-run: %s", dry)
	}
	if !strings.Contains(dry.Stdout, "would: archived 2 item(s) from 2 month(s) before 2026-07") {
		t.Errorf("dry-run stdout:\n%s", dry.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "done-2025.md")); err == nil {
		t.Error("a dry run wrote an archive")
	}

	got := r.run("--archive", "--before", "2026-07")
	if got.Code != ExitOK {
		t.Fatalf("archive: %s", got)
	}
	for _, want := range []string{"archived 2 item(s)", "2026-06", "2025-12",
		"wrote done-2026.md", "wrote done-2025.md"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, got.Stdout)
		}
	}

	// July stays; the two older groups have gone to their years.
	done := readFile(t, filepath.Join(dir, "done.md"))
	if !strings.Contains(done, "T-0010") {
		t.Error("the month that stays lost its item")
	}
	if strings.Contains(done, "T-0008") || strings.Contains(done, "T-0007") {
		t.Errorf("an archived item is still in done.md:\n%s", done)
	}
	if a := readFile(t, filepath.Join(dir, "done-2026.md")); !strings.Contains(a, "T-0008") {
		t.Errorf("done-2026.md:\n%s", a)
	}
	if a := readFile(t, filepath.Join(dir, "done-2025.md")); !strings.Contains(a, "T-0007") {
		t.Errorf("done-2025.md:\n%s", a)
	}

	// The directory still validates: an archive is outside the invariants, so
	// removing items from done.md must leave nothing to report.
	if check := r.run("--check"); check.Code != ExitOK {
		t.Fatalf("check after archive: %s", check)
	}

	// Running again finds nothing to do and still succeeds.
	again := r.run("--archive", "--before", "2026-07")
	if again.Code != ExitOK || !strings.Contains(again.Stdout, "nothing to archive") {
		t.Errorf("second run: %s", again)
	}
}

// §5.3 makes the ID-pool warning mandatory, and this is the operation that
// takes data out of the checked set — so --quiet must not swallow it.
func TestArchiveCLIAlwaysWarnsAboutTheIDPool(t *testing.T) {
	r, _ := archiveProject(t)

	got := r.run("--archive", "--before", "2026-07", "--quiet")
	if got.Code != ExitOK {
		t.Fatalf("archive: %s", got)
	}
	if got.Stdout != "" {
		t.Errorf("--quiet should print no commentary on stdout:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stderr, "left the ID pool") {
		t.Errorf("the ID-pool warning must survive --quiet:\n%s", got.Stderr)
	}
}

// The two spellings of the cutoff are one cutoff. --age is resolved through
// mm.ArchiveCutoff, so this checks the wiring, not the arithmetic (mm's own
// TestArchiveCutoffFromAge owns that).
func TestArchiveCLIAgeAndBeforeAgree(t *testing.T) {
	// testDay is 2026-07-30. June ended 06-30, thirty days earlier, so --age 30
	// takes June and older — the same set as --before 2026-07.
	byAge, _ := archiveProject(t)
	age := byAge.run("--archive", "--age", "30", "--json")
	if age.Code != ExitOK {
		t.Fatalf("--age: %s", age)
	}
	byBefore, _ := archiveProject(t)
	before := byBefore.run("--archive", "--before", "2026-07", "--json")
	if before.Code != ExitOK {
		t.Fatalf("--before: %s", before)
	}

	type result struct {
		Cutoff string
		Months []string
		Items  int
	}
	parse := func(body string) result {
		var env struct{ Result result }
		if err := json.Unmarshal([]byte(body), &env); err != nil {
			t.Fatalf("json: %v\n%s", err, body)
		}
		return env.Result
	}
	a, b := parse(age.Stdout), parse(before.Stdout)
	if a.Cutoff != b.Cutoff || a.Items != b.Items || strings.Join(a.Months, ",") != strings.Join(b.Months, ",") {
		t.Errorf("--age 30 = %+v, --before 2026-07 = %+v; they name one cutoff", a, b)
	}
	if a.Cutoff != "2026-07" {
		t.Errorf("cutoff = %s, want 2026-07", a.Cutoff)
	}

	// One day of extra grace and June stays: the boundary is real, not a
	// rounding of the month.
	r, _ := archiveProject(t)
	got := r.run("--archive", "--age", "31", "--json")
	if got.Code != ExitOK {
		t.Fatalf("--age 31: %s", got)
	}
	if res := parse(got.Stdout); strings.Join(res.Months, ",") != "2025-12" {
		t.Errorf("--age 31 months = %v, want only 2025-12", res.Months)
	}
}

// A bare --archive keeps the month the board is living in.
func TestArchiveCLIDefaultsToTheCurrentMonth(t *testing.T) {
	r, dir := archiveProject(t)

	got := r.run("--archive")
	if got.Code != ExitOK {
		t.Fatalf("archive: %s", got)
	}
	if !strings.Contains(got.Stdout, "before 2026-07") {
		t.Errorf("the cutoff must be reported, it came from a default:\n%s", got.Stdout)
	}
	if done := readFile(t, filepath.Join(dir, "done.md")); !strings.Contains(done, "T-0010") {
		t.Error("the current month was archived")
	}
}

func TestArchiveCLIJSONAndPorcelain(t *testing.T) {
	r, _ := archiveProject(t)

	porc := r.run("--archive", "--before", "2026-07", "--porcelain", "--dry-run")
	if porc.Code != ExitOK {
		t.Fatalf("porcelain: %s", porc)
	}
	// One record per item moved: id, then the archive it landed in.
	for _, want := range []string{"T-0008\tdone-2026.md", "T-0007\tdone-2025.md"} {
		if !strings.Contains(porc.Stdout, want) {
			t.Errorf("porcelain is missing %q:\n%s", want, porc.Stdout)
		}
	}

	got := r.run("--archive", "--before", "2026-07", "--json")
	if got.Code != ExitOK {
		t.Fatalf("json: %s", got)
	}
	var env struct {
		OK        bool
		Operation string
		Warnings  []string
		Result    struct {
			Cutoff        string
			Months        []string
			Items         int
			Files         []string
			DetailOrphans []string
		}
		Changes []struct {
			Kind string
			ID   string
			File string
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if !env.OK || env.Operation != "archive" {
		t.Errorf("envelope: ok=%v operation=%q", env.OK, env.Operation)
	}
	if env.Result.Cutoff != "2026-07" || env.Result.Items != 2 {
		t.Errorf("result = %+v", env.Result)
	}
	if len(env.Result.Files) != 2 {
		t.Errorf("files = %v, want two archives", env.Result.Files)
	}
	if len(env.Warnings) == 0 {
		t.Error("the ID-pool warning belongs in the envelope, not only on stderr")
	}
	if len(env.Changes) == 0 {
		t.Error("the change set is empty; a caller cannot see what moved")
	}
	// §9.2: a list is a list even when it is empty. This fixture has no detail
	// files, so detailOrphans is the empty case.
	if env.Result.DetailOrphans == nil {
		t.Error("detailOrphans is null; an absent list makes a caller test before iterating")
	}
}

// A detail file belonging to an archived item is stranded today: the library
// does not move it to details-YYYY/ yet (T-0168). What must NOT happen is
// silence — spec-tools.md §5.1.6's rule for --remove applies here for the same
// reason, so the run names the file and --check reports it afterwards.
func TestArchiveCLIReportsTheDetailFilesItStrands(t *testing.T) {
	r, dir := archiveProject(t)

	done := readFile(t, filepath.Join(dir, "done.md"))
	done = strings.Replace(done,
		"- [x] [T-0007] Christmas | created:2025-11-01",
		"- [x] [T-0007] Christmas | detail:details/T-0007.md | created:2025-11-01", 1)
	if err := os.WriteFile(filepath.Join(dir, "done.md"), []byte(done), 0o644); err != nil {
		t.Fatal(err)
	}
	detail := "---\ndoc: detail\nid: T-0007\ntitle: Christmas\nupdated: 2025-12-24\n---\n\n# Christmas\n"
	if err := os.WriteFile(filepath.Join(dir, "details", "T-0007.md"), []byte(detail), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("fixture should start clean: %s", got)
	}

	got := r.run("--archive", "--before", "2026-07")
	if got.Code != ExitOK {
		t.Fatalf("archive: %s", got)
	}
	if !strings.Contains(got.Stderr, "details/T-0007.md") {
		t.Errorf("the stranded detail file must be named:\n%s", got.Stderr)
	}

	// And it is a real finding, not just a warning about one.
	check := r.run("--check")
	if check.Code != ExitInvariantViolation {
		t.Fatalf("check after archive = %d, want an I9 finding: %s", check.Code, check)
	}
	if !strings.Contains(check.Stdout, "details/T-0007.md") {
		t.Errorf("check:\n%s", check.Stdout)
	}
}

func TestArchiveCLIRejectsABadCutoff(t *testing.T) {
	r, _ := archiveProject(t)

	cases := [][]string{
		{"--archive", "--before", "2026-13"},                // not a month
		{"--archive", "--before", "July"},                   // not a date at all
		{"--archive", "--age", "thirty"},                    // not a number
		{"--archive", "--age", "-1"},                        // not a duration
		{"--archive", "--before", "2026-07", "--age", "30"}, // two spellings of one cutoff
	}
	for _, args := range cases {
		if got := r.run(args...); got.Code != ExitUsage {
			t.Errorf("%v = exit %d, want %d:\n%s", args, got.Code, ExitUsage, got)
		}
	}

	// A full date is accepted and its day ignored: the switch's grain is a month.
	if got := r.run("--archive", "--before", "2026-07-23", "--dry-run"); got.Code != ExitOK {
		t.Errorf("a full date should round down to its month: %s", got)
	} else if !strings.Contains(got.Stdout, "before 2026-07") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}
