package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archiveDone spans three months across two years, so the year split, the group
// ordering and the merge all have something to bite on.
//
// Every field is in canonical order, T-0007 carries an unregistered `owner`, and
// all three outcomes appear: the archived lines are compared BYTE FOR BYTE
// against these, which is the whole "nothing is lost or reformatted" assertion.
const archiveDone = `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [T-0010] July newest | created:2026-07-01 | done:2026-07-23 | outcome:shipped
- [x] [T-0009] July older | created:2026-06-01 | done:2026-07-02 | outcome:cancelled

## 2026-06

- [x] [T-0008] June | prio:low | created:2026-05-01 | done:2026-06-30 | outcome:obsolete

## 2025-12

- [x] [T-0007] Christmas | tags:infra,ci | created:2025-11-01 | done:2025-12-24 | outcome:shipped | owner:dana
`

const (
	julyNewest = "- [x] [T-0010] July newest | created:2026-07-01 | done:2026-07-23 | outcome:shipped"
	julyOlder  = "- [x] [T-0009] July older | created:2026-06-01 | done:2026-07-02 | outcome:cancelled"
	juneItem   = "- [x] [T-0008] June | prio:low | created:2026-05-01 | done:2026-06-30 | outcome:obsolete"
	decItem    = "- [x] [T-0007] Christmas | tags:infra,ci | created:2025-11-01 | done:2025-12-24 | outcome:shipped | owner:dana"
)

func archiveDir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newDir(t, map[string]string{"done.md": archiveDone})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

// exists reports whether a file is present in the directory.
func exists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func TestArchiveRollsOldMonthsIntoYearFiles(t *testing.T) {
	dir, s := archiveDir(t)

	res, tx, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if res.Cutoff != "2026-07" {
		t.Errorf("cutoff = %q, want 2026-07", res.Cutoff)
	}
	if got := strings.Join(res.Months, ","); got != "2026-06,2025-12" {
		t.Errorf("months = %q, want the moved groups newest first", got)
	}
	if res.Items != 2 {
		t.Errorf("items = %d, want 2", res.Items)
	}
	if got := strings.Join(res.Files, ","); got != "done-2026.md,done-2025.md" {
		t.Errorf("files = %q", got)
	}

	// done.md keeps the cutoff month, untouched line for line.
	done := readFile(t, dir, "done.md")
	if got := strings.Join(headings(done), ","); got != "## 2026-07" {
		t.Errorf("done.md headings = %q, want only the cutoff month\n%s", got, done)
	}
	for _, want := range []string{julyNewest, julyOlder} {
		if !strings.Contains(done, want) {
			t.Errorf("done.md lost a line it should keep:\n%s", done)
		}
	}
	for _, gone := range []string{"T-0008", "T-0007", "## 2026-06", "## 2025-12"} {
		if strings.Contains(done, gone) {
			t.Errorf("done.md still holds %q:\n%s", gone, done)
		}
	}

	// The archives hold the moved groups, byte for byte.
	a2026 := readFile(t, dir, "done-2026.md")
	if !strings.Contains(a2026, juneItem) {
		t.Errorf("done-2026.md is missing the June line verbatim:\n%s", a2026)
	}
	a2025 := readFile(t, dir, "done-2025.md")
	if !strings.Contains(a2025, decItem) {
		t.Errorf("done-2025.md is missing the December line verbatim (the "+
			"unregistered owner: field is the point):\n%s", a2025)
	}

	// The transaction reports every file it wrote, and one change per item.
	if len(tx.Files) != 3 {
		t.Errorf("wrote %d files, want done.md and two archives: %v", len(tx.Files), tx.Files)
	}
	if len(tx.Changes) != 2 {
		t.Errorf("changes = %+v, want one per archived item", tx.Changes)
	}
	for _, c := range tx.Changes {
		if c.Kind != ChangeMoved || c.Before != c.After {
			t.Errorf("change should be a move that alters nothing: %+v", c)
		}
	}

	// And the directory is still valid: archiving is not a repair, but it must
	// not break anything either.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after archiving:\n%s", violationMessages(vs))
	}
}

// The seeded archive's shape is pinned: it is the file a person opens, and the
// same parser has to read it back.
func TestArchiveSeedsAReadableFile(t *testing.T) {
	dir, s := archiveDir(t)

	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 1, 1}}, today); err != nil {
		t.Fatalf("archive: %v", err)
	}
	want := `---
doc: done
version: 1
updated: 2026-07-29
---

# Done 2025

Closed items rolled out of done.md, newest month first. This file is outside
the format spec and is not validated: its items have left the ID pool, so I1
and I2 no longer cover them (spec-file-format.md §5.3, §10.5).

## 2025-12

` + decItem + "\n"

	if got := readFile(t, dir, "done-2025.md"); got != want {
		t.Errorf("archive file:\n%s\nwant:\n%s", got, want)
	}
	// Read back by the done.md parser with no findings of its own.
	d, vs := parseDoneG("done-2025.md", []byte(want), DefaultIDGrammar())
	if len(vs) != 0 {
		t.Errorf("the archive does not parse cleanly: %v", vs)
	}
	if len(d.Items) != 1 || d.Items[0].ID != "T-0007" || d.Items[0].Extra[0].Value != "dana" {
		t.Errorf("the archive did not parse back into its item: %+v", d.Items)
	}
}

// The zero Before means "the month we are living in": a bare archive keeps this
// month and rolls the rest.
func TestArchiveDefaultsToTheCurrentMonth(t *testing.T) {
	dir, s := archiveDir(t)

	res, _, err := s.Archive(ArchiveRequest{}, today) // today is 2026-07-29
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if res.Cutoff != "2026-07" {
		t.Fatalf("cutoff = %q, want the month of today", res.Cutoff)
	}
	if got := strings.Join(headings(readFile(t, dir, "done.md")), ","); got != "## 2026-07" {
		t.Errorf("done.md headings = %q", got)
	}
}

// §7 rule 6: a no-op writes nothing. No rewrite, no mtime bump, no diff.
func TestArchiveWritesNothingWhenNothingIsOldEnough(t *testing.T) {
	dir, s := archiveDir(t)
	before := readFile(t, dir, "done.md")

	res, tx, err := s.Archive(ArchiveRequest{Before: Date{2025, 1, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(tx.Files) != 0 || len(tx.Changes) != 0 {
		t.Errorf("a no-op reported writes: %v %+v", tx.Files, tx.Changes)
	}
	if res.Items != 0 || len(res.Months) != 0 || len(res.Warnings) != 0 {
		t.Errorf("a no-op reported work: %+v", res)
	}
	if got := readFile(t, dir, "done.md"); got != before {
		t.Errorf("done.md was rewritten by a no-op:\n%s", got)
	}
	if exists(t, dir, "done-2026.md") || exists(t, dir, "done-2025.md") {
		t.Error("a no-op created an archive file")
	}
}

func TestArchiveDryRunWritesNothing(t *testing.T) {
	dir, s := archiveDir(t)
	before := readFile(t, dir, "done.md")

	res, tx, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}, DryRun: true}, today)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !tx.DryRun {
		t.Error("the result should be marked as a dry run")
	}
	// A dry run reports exactly what the real run would do (§3.4).
	if len(tx.Files) != 3 || len(tx.Changes) != 2 || res.Items != 2 {
		t.Errorf("dry run under-reported: files=%v changes=%d items=%d",
			tx.Files, len(tx.Changes), res.Items)
	}
	if got := readFile(t, dir, "done.md"); got != before {
		t.Error("dry run modified done.md")
	}
	if exists(t, dir, "done-2026.md") || exists(t, dir, "done-2025.md") {
		t.Error("dry run created an archive file")
	}
}

// Within a group, newest first — the order done.md uses.
func TestArchivePreservesTheOrderWithinAGroup(t *testing.T) {
	dir, s := archiveDir(t)

	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 8, 1}}, today); err != nil {
		t.Fatalf("archive: %v", err)
	}
	group := nonBlank(sectionText(readFile(t, dir, "done-2026.md"), "## 2026-07"))
	if len(group) != 2 {
		t.Fatalf("want 2 lines in the archived July group, got %v", group)
	}
	if group[0] != julyNewest || group[1] != julyOlder {
		t.Errorf("order or content changed:\n%s\n%s", group[0], group[1])
	}
}

// A second run has to merge into the file the first one wrote, in month order.
func TestArchiveMergesIntoAnExistingArchive(t *testing.T) {
	dir, s := archiveDir(t)

	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today); err != nil {
		t.Fatalf("first archive: %v", err)
	}
	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 8, 1}}, today); err != nil {
		t.Fatalf("second archive: %v", err)
	}

	a := readFile(t, dir, "done-2026.md")
	if got := strings.Join(headings(a), ","); got != "## 2026-07,## 2026-06" {
		t.Errorf("archive headings = %q, want newest first with no duplicates\n%s", got, a)
	}
	for _, want := range []string{julyNewest, julyOlder, juneItem} {
		if strings.Count(a, want) != 1 {
			t.Errorf("the merged archive should hold %q exactly once:\n%s", want, a)
		}
	}
	if done := readFile(t, dir, "done.md"); len(headings(done)) != 0 {
		t.Errorf("done.md should have no groups left:\n%s", done)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after two archive runs:\n%s", violationMessages(vs))
	}
}

// An interrupted run wrote the archive but not done.md (§7 rule 4). Re-running
// must complete the move rather than write a second copy.
func TestArchiveHealsAnInterruptedRun(t *testing.T) {
	dir, s := archiveDir(t)

	// The archive as the interrupted run left it: the group is already there.
	interrupted := "---\ndoc: done\nversion: 1\nupdated: 2026-07-29\n---\n\n" +
		"# Done 2026\n\n## 2026-06\n\n" + juneItem + "\n"
	if err := os.WriteFile(filepath.Join(dir, "done-2026.md"), []byte(interrupted), 0o644); err != nil {
		t.Fatal(err)
	}

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	a := readFile(t, dir, "done-2026.md")
	if n := strings.Count(a, juneItem); n != 1 {
		t.Errorf("the item is in the archive %d times, want 1:\n%s", n, a)
	}
	if strings.Contains(readFile(t, dir, "done.md"), "T-0008") {
		t.Error("done.md should have lost the duplicate")
	}
	if !hasWarning(res.Warnings, "already present in the archive") {
		t.Errorf("the skipped copy should be reported: %q", res.Warnings)
	}
}

// The detail file of an archived item is the one thing archiving cannot take
// with it (§5.4). It becomes an I9 orphan, and that MUST be reported rather
// than left silent (§5.1.6).
func TestArchiveReportsTheDetailFilesItStrands(t *testing.T) {
	detail := "---\ndoc: detail\nid: T-0008\ntitle: June\nupdated: 2026-06-30\n---\n\n# T-0008 — June\n"
	dir := newDir(t, map[string]string{
		"done.md": strings.Replace(archiveDone,
			"- [x] [T-0008] June | prio:low",
			"- [x] [T-0008] June | prio:low | detail:details/T-0008.md", 1),
		"details/T-0008.md": detail,
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive must not be blocked by the orphan it creates: %v", err)
	}
	if got := strings.Join(res.DetailOrphans, ","); got != "details/T-0008.md" {
		t.Errorf("orphans = %q, want the archived item's detail file", got)
	}
	if !hasWarning(res.Warnings, "no longer referenced") {
		t.Errorf("the orphan should be warned about: %q", res.Warnings)
	}
	// The file is still there: it holds the long-form record and deleting it
	// would be the data loss this operation exists to avoid.
	if !exists(t, dir, "details/T-0008.md") {
		t.Fatal("the detail file was deleted")
	}
	// And the finding is real, not hidden: exactly one I9 orphan, nothing else.
	vs, _ := s.Validate()
	if len(vs) != 1 || vs[0].Invariant != "I9" || vs[0].At.File != "details/T-0008.md" {
		t.Errorf("want exactly one I9 orphan finding, got:\n%s", violationMessages(vs))
	}
}

// A heading that is not a month names no year to file under. It is already a
// violation; this operation is not its repair.
func TestArchiveLeavesANonMonthHeadingAlone(t *testing.T) {
	dir := newDir(t, map[string]string{
		"done.md": strings.Replace(archiveDone, "## 2025-12", "## 2025", 1),
	})
	s := mustOpen(t, dir)

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if got := strings.Join(res.Months, ","); got != "2026-06" {
		t.Errorf("months = %q, want only the real month", got)
	}
	done := readFile(t, dir, "done.md")
	if !strings.Contains(done, "## 2025") || !strings.Contains(done, "T-0007") {
		t.Errorf("the bad heading and its item should be untouched:\n%s", done)
	}
	if exists(t, dir, "done-2025.md") {
		t.Error("a non-month heading must not produce an archive")
	}
}

// A month heading with nothing under it has nothing to archive: the heading goes
// and no archive is created for it.
func TestArchiveDropsAnEmptyMonthGroup(t *testing.T) {
	dir := newDir(t, map[string]string{
		"done.md": strings.Replace(archiveDone, "\n"+decItem+"\n", "\n", 1),
	})
	s := mustOpen(t, dir)

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if res.Items != 1 || strings.Join(res.Months, ",") != "2026-06,2025-12" {
		t.Errorf("result = %+v, want both groups moved but only one item", res)
	}
	if exists(t, dir, "done-2025.md") {
		t.Error("an empty group must not produce an archive file")
	}
	done := readFile(t, dir, "done.md")
	if strings.Contains(done, "2025-12") {
		t.Errorf("the empty heading should be gone:\n%s", done)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// done.md must survive losing its last group as a well-formed file: frontmatter
// intact, prose intact, one trailing newline and no blank line before it.
func TestArchiveLeavesDoneTidyWhenEverythingGoes(t *testing.T) {
	dir, s := archiveDir(t)

	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2027, 1, 1}}, today); err != nil {
		t.Fatalf("archive: %v", err)
	}
	done := readFile(t, dir, "done.md")
	want := "---\ndoc: done\nversion: 1\nupdated: 2026-07-29\n---\n\n# Done\n"
	if done != want {
		t.Errorf("done.md =\n%q\nwant\n%q", done, want)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("an emptied done.md must still be valid:\n%s", violationMessages(vs))
	}
	// And it must still be a file the next --finish can write into.
	if _, _, err := s.Finish("T-0001", FinishRequest{}, today); err != nil {
		t.Fatalf("finish into an emptied done.md: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after finishing into an emptied done.md:\n%s",
			violationMessages(vs))
	}
}

// Only the month is read, so a full date is rounded down rather than refused -
// but a month that does not exist is an error, like every other bad date.
func TestArchiveCutoffValidation(t *testing.T) {
	_, s := archiveDir(t)

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 23}, DryRun: true}, today)
	if err != nil {
		t.Fatalf("a day inside the cutoff month should round down: %v", err)
	}
	if res.Cutoff != "2026-07" || res.Items != 2 {
		t.Errorf("result = %+v, want the whole month kept", res)
	}

	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 13, 1}}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("month 13 = %v, want ErrInvalidArgument", err)
	}
}

// --age DAYS is the same cutoff as --before, said as a policy (spec-tools.md
// §5.3.1): a month group is archived once DAYS days have passed since its LAST
// day. Every case here is a boundary, because the interesting ones all are —
// the day a month completes, the day it becomes old enough, and the year end.
func TestArchiveCutoffFromAge(t *testing.T) {
	cases := []struct {
		today Date
		age   int
		want  string // the exclusive month cutoff
		why   string
	}{
		{Date{2026, 8, 4}, 0, "2026-08",
			"age 0 mid-month archives every COMPLETE month, not the one being lived in"},
		{Date{2026, 8, 31}, 0, "2026-09",
			"on the last day of a month, age 0 archives that month too"},
		{Date{2026, 8, 4}, 30, "2026-07",
			"30 days after August 4th is July 5th: June is old enough, July is not"},
		{Date{2026, 8, 4}, 35, "2026-07",
			"exactly 35 days after June's last day: old enough, so June still goes"},
		{Date{2026, 8, 4}, 36, "2026-06",
			"one day short for June, so the cutoff steps back a month"},
		{Date{2026, 1, 15}, 15, "2026-01",
			"15 days after December's last day, across the year boundary"},
		{Date{2024, 3, 1}, 1, "2024-03",
			"February 29th is the last day of its month in a leap year"},
	}
	for _, c := range cases {
		got, err := ArchiveCutoff(c.today, c.age)
		if err != nil {
			t.Errorf("ArchiveCutoff(%s, %d): %v", c.today, c.age, err)
			continue
		}
		if got.Month7() != c.want {
			t.Errorf("ArchiveCutoff(%s, %d) = %s, want %s — %s",
				c.today, c.age, got.Month7(), c.want, c.why)
		}
		if got.Day != 1 {
			t.Errorf("ArchiveCutoff(%s, %d) = %s, want day 1: the cutoff is a month",
				c.today, c.age, got)
		}
	}

	if _, err := ArchiveCutoff(Date{2026, 8, 4}, -1); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("age -1 = %v, want ErrInvalidArgument", err)
	}
}

// The cutoff an age produces must archive exactly the groups the policy names,
// which is the claim ArchiveCutoff makes and the table above only implies.
func TestArchiveByAgeMovesTheRightGroups(t *testing.T) {
	_, s := archiveDir(t)

	// today is 2026-07-29 (the fixture's). June ended 06-30, 29 days ago.
	before, err := ArchiveCutoff(today, 30)
	if err != nil {
		t.Fatal(err)
	}
	res, _, err := s.Archive(ArchiveRequest{Before: before, DryRun: true}, today)
	if err != nil {
		t.Fatal(err)
	}
	if res.Cutoff != "2026-06" {
		t.Errorf("cutoff = %s, want 2026-06: June is one day short of 30", res.Cutoff)
	}
	if got := strings.Join(res.Months, ","); got != "2025-12" {
		t.Errorf("months = %q, want only 2025-12", got)
	}

	// One day less of grace and June is old enough.
	before, err = ArchiveCutoff(today, 29)
	if err != nil {
		t.Fatal(err)
	}
	res, _, err = s.Archive(ArchiveRequest{Before: before, DryRun: true}, today)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Months, ","); got != "2026-06,2025-12" {
		t.Errorf("months = %q, want 2026-06,2025-12", got)
	}
}

// §8: a directory that is already broken must still be usable. A pre-existing
// violation elsewhere in done.md must neither block the archive nor be blamed on
// it — and must not be quietly repaired either.
func TestArchiveOnAnAlreadyBrokenDirectory(t *testing.T) {
	// I6: the July item loses its outcome. July is the month that STAYS, so the
	// finding is still there afterwards to be compared.
	dir := newDir(t, map[string]string{
		"done.md": strings.Replace(archiveDone,
			" | done:2026-07-23 | outcome:shipped", " | done:2026-07-23", 1),
	})
	s := mustOpen(t, dir)
	before, _ := s.Validate()
	if len(before) != 1 || before[0].Invariant != "I6" {
		t.Fatalf("the fixture should break exactly I6:\n%s", violationMessages(before))
	}

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("a pre-existing violation must not block the archive: %v", err)
	}
	if res.Items != 2 {
		t.Errorf("items = %d, want the two old ones moved anyway", res.Items)
	}
	after, _ := s.Validate()
	if len(after) != 1 || after[0].Invariant != "I6" {
		t.Errorf("the pre-existing finding should survive untouched:\n%s",
			violationMessages(after))
	}
}

// The real thing, at real scale: this repository's own board, which is the only
// corpus here where nearly every closed item carries a detail file.
//
// It is the test that showed what archiving actually costs a live directory -
// 87 items out, 51 detail files stranded - and it exists so that number cannot
// change unnoticed. It skips when the directory is absent, so the module stays
// buildable in isolation, and LOGS what it covered: a skip that looks like a
// pass is the failure mode this file cannot afford.
func TestArchiveOnTheRepositorysOwnBoard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	if err := os.CopyFS(dir, os.DirFS("../micro-manager")); err != nil {
		t.Skipf("the implementation's own directory is not present: %v", err)
	}
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("the real board should start clean:\n%s", violationMessages(vs))
	}
	before, err := s.List(Filter{State: StateDone})
	if err != nil {
		t.Fatal(err)
	}

	// Everything before the month the board is living in.
	res, tx, err := s.Archive(ArchiveRequest{Before: Date{2026, 8, 1}}, Date{2026, 8, 4})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if res.Items == 0 || len(res.Months) == 0 {
		t.Fatalf("nothing was archived from a board with %d closed items", len(before))
	}
	t.Logf("archived %d items in %v into %v, stranding %d detail files",
		res.Items, res.Months, res.Files, len(res.DetailOrphans))

	// Every closed item is still accounted for: what left done.md is exactly
	// what arrived in the archives.
	after, err := s.List(Filter{State: StateDone})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before)-res.Items {
		t.Errorf("done.md holds %d closed items, want %d - %d",
			len(after), len(before), res.Items)
	}
	archived := 0
	for _, name := range res.Files {
		d, vs := parseDoneG(name, []byte(readFile(t, dir, name)), s.mustGrammar(t))
		if len(vs) != 0 {
			t.Errorf("%s does not parse cleanly: %v", name, vs)
		}
		archived += len(d.Items)
	}
	if archived != res.Items {
		t.Errorf("the archives hold %d items, want the %d that left", archived, res.Items)
	}
	if len(tx.Changes) != res.Items {
		t.Errorf("%d changes for %d items", len(tx.Changes), res.Items)
	}

	// The only findings are the stranded detail files, every one of them
	// reported by the operation.
	vs, _ := s.Validate()
	reported := map[string]bool{}
	for _, p := range res.DetailOrphans {
		reported[p] = true
	}
	for _, v := range vs {
		if v.Invariant != "I9" || !reported[v.At.File] {
			t.Errorf("unreported finding after archiving: %s", v)
		}
	}
	if len(vs) != len(res.DetailOrphans) {
		t.Errorf("%d findings for %d reported orphans", len(vs), len(res.DetailOrphans))
	}
}

// mustGrammar reads the directory's declared ID grammar or fails the test.
func (s *Store) mustGrammar(t *testing.T) IDGrammar {
	t.Helper()
	g, err := s.Grammar()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
