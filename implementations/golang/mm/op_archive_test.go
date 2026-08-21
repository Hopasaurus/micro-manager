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

// writeFile edits a file in place, for the hand-edits these tests stage. It
// creates the parent directory, because details-YYYY/ is one of the things
// being staged.
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
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

// The same interruption, one write later: the archive AND the detail copy are
// there, and only the two deletions are outstanding.
//
// The detail half is why the move is attempted for every item leaving done.md,
// including one the archive already holds. Skipping it for a duplicate would
// take the done.md line away and leave details/T-0008.md behind it — the exact
// orphan this operation exists not to create.
func TestArchiveHealsAnInterruptedDetailMove(t *testing.T) {
	dir, s := detailArchiveDir(t)

	archived := strings.Replace(juneItem, "prio:low",
		"prio:low | detail:details-2026/T-0008.md", 1)
	writeFile(t, dir, "done-2026.md", "---\ndoc: done\nversion: 1\nupdated: 2026-07-29\n---\n\n"+
		"# Done 2026\n\n## 2026-06\n\n"+archived+"\n")
	writeFile(t, dir, "details-2026/T-0008.md", readFile(t, dir, "details/T-0008.md"))

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("the re-run must complete the move: %v", err)
	}
	if len(res.DetailsMoved) != 1 {
		t.Errorf("moves = %+v, want the outstanding one", res.DetailsMoved)
	}
	if exists(t, dir, "details/T-0008.md") {
		t.Error("details/T-0008.md survived the re-run, with no item to claim it")
	}
	if !exists(t, dir, "details-2026/T-0008.md") {
		t.Error("the archived copy was removed")
	}
	if n := strings.Count(readFile(t, dir, "done-2026.md"), "[T-0008]"); n != 1 {
		t.Errorf("the archive holds the item %d times, want 1", n)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("the healed directory should be clean:\n%s", violationMessages(vs))
	}
}

// detailArchiveDir is archiveDone with a detail file on the June item, the
// group that moves.
func detailArchiveDir(t *testing.T) (string, *Store) {
	t.Helper()
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
	return dir, s
}

// §5.6 rule 3: the detail file travels with its item and the archived line's
// detail: field is rewritten to match — one operation, not two.
//
// T-0043 shipped the other choice (leave the file, accept the I9 orphan,
// report it); T-0166 decided the format question the other way, and this is
// that decision. The directory is CLEAN afterwards, which is the whole point.
func TestArchiveTakesTheDetailFileWithIt(t *testing.T) {
	dir, s := detailArchiveDir(t)

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}

	if len(res.DetailsMoved) != 1 {
		t.Fatalf("moves = %+v, want the June item's detail file", res.DetailsMoved)
	}
	mv := res.DetailsMoved[0]
	if mv.ID != "T-0008" || mv.From != "details/T-0008.md" || mv.To != "details-2026/T-0008.md" {
		t.Errorf("move = %+v, want T-0008 details/ -> details-2026/", mv)
	}
	if len(res.DetailOrphans) != 0 {
		t.Errorf("orphans = %v, want none: the file travelled", res.DetailOrphans)
	}
	if !hasWarning(res.Warnings, "details-YYYY/") {
		t.Errorf("the move should be stated: %q", res.Warnings)
	}

	// The file is in its new home, byte for byte, and gone from the old one.
	if exists(t, dir, "details/T-0008.md") {
		t.Error("the file is still in details/, so I9 has an orphan to report")
	}
	moved := readFile(t, dir, "details-2026/T-0008.md")
	if !strings.Contains(moved, "id: T-0008") || !strings.Contains(moved, "title: June") {
		t.Errorf("the archived detail file lost its frontmatter:\n%s", moved)
	}

	// The archived line points at the new path; nothing in the live directory
	// mentions the old one.
	archive := readFile(t, dir, "done-2026.md")
	if !strings.Contains(archive, "detail:details-2026/T-0008.md") {
		t.Errorf("the archived line was not rewritten:\n%s", archive)
	}
	if strings.Contains(archive, "detail:details/T-0008.md") {
		t.Errorf("the archived line still points into details/:\n%s", archive)
	}

	// And the directory validates: no orphan, no dangling reference.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("the directory must stay clean:\n%s", violationMessages(vs))
	}
}

// §5.6 rule 4: restoring is the same move backwards, and half a restore is
// CAUGHT — which is the property that made moving the file the better choice.
func TestArchiveRestoreWithoutTheFileIsReported(t *testing.T) {
	dir, s := detailArchiveDir(t)
	if _, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// Paste the archived line back into done.md by hand — and forget the file.
	done := readFile(t, dir, "done.md")
	done = strings.Replace(done, "## 2026-07",
		"## 2026-06\n\n- [x] [T-0008] June | prio:low | detail:details-2026/T-0008.md"+
			" | created:2026-05-01 | done:2026-06-30 | outcome:obsolete\n\n## 2026-07", 1)
	writeFile(t, dir, "done.md", done)

	s2 := mustOpen(t, dir)
	vs, _ := s2.Validate()
	if len(vs) != 1 || vs[0].Invariant != "I8" {
		t.Fatalf("want one I8 finding for the restored line, got:\n%s", violationMessages(vs))
	}
	if !strings.Contains(vs[0].Message, "details-2026/T-0008.md") {
		t.Errorf("the finding should name the archived path: %s", vs[0].Message)
	}
}

// §8: a directory that is already broken must still be usable, and archiving
// must not turn someone else's pre-existing mistake into new findings.
//
// Both cases here are about a detail file two items claim, which is only ever
// possible when at least one claim is already an I8 violation — the path
// encodes the ID, so two VALID claims mean two items with one ID.
func TestArchiveAndASharedDetailFile(t *testing.T) {
	detail := "---\ndoc: detail\nid: T-0008\ntitle: June\nupdated: 2026-06-30\n---\n\n# T-0008 — June\n"
	claimedDone := strings.Replace(archiveDone,
		"- [x] [T-0008] June | prio:low",
		"- [x] [T-0008] June | prio:low | detail:details/T-0008.md", 1)

	// A live item pointing at a file named for someone else. Its I8 finding is
	// "points at the wrong path", which does not depend on the file being
	// there, so the file leaves with the item it is actually named for.
	t.Run("a bogus claim does not hold the file back", func(t *testing.T) {
		dir := newDir(t, map[string]string{
			"done.md": claimedDone, "details/T-0008.md": detail,
		})
		backlog := readFile(t, dir, "backlog.md")
		backlog = strings.Replace(backlog, "next_id: T-0001", "next_id: T-0012", 1) +
			"- [ ] [T-0011] Also claims it | detail:details/T-0008.md | created:2026-07-01\n"
		writeFile(t, dir, "backlog.md", backlog)

		s := mustOpen(t, dir)
		before, _ := s.Validate()

		res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
		if err != nil {
			t.Fatalf("a pre-existing bad claim must not block the archive: %v", err)
		}
		if len(res.DetailsMoved) != 1 {
			t.Errorf("moves = %+v, want the file to go with T-0008", res.DetailsMoved)
		}
		if exists(t, dir, "details/T-0008.md") {
			t.Error("the file stayed behind as an orphan")
		}
		// The bogus claim keeps exactly the finding it already had.
		after, _ := s.Validate()
		if len(after) != len(before) {
			t.Errorf("findings went from %d to %d:\n%s", len(before), len(after),
				violationMessages(after))
		}
	})

	// One ID in two homes (I1, what a git merge manufactures). Both claims are
	// valid, so taking the file would leave the SURVIVOR pointing at nothing —
	// a new finding, for an item that did nothing wrong.
	t.Run("a duplicate ID keeps its file", func(t *testing.T) {
		dir := newDir(t, map[string]string{
			"done.md": claimedDone, "details/T-0008.md": detail,
		})
		backlog := readFile(t, dir, "backlog.md")
		backlog = strings.Replace(backlog, "next_id: T-0001", "next_id: T-0012", 1) +
			"- [ ] [T-0008] June | detail:details/T-0008.md | created:2026-05-01\n"
		writeFile(t, dir, "backlog.md", backlog)

		s := mustOpen(t, dir)
		res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
		if err != nil {
			t.Fatalf("a pre-existing duplicate must not block the archive: %v", err)
		}
		if len(res.DetailsMoved) != 0 {
			t.Errorf("moved %+v; the surviving twin still needs the file", res.DetailsMoved)
		}
		if !exists(t, dir, "details/T-0008.md") {
			t.Fatal("the surviving item's detail file was taken away")
		}
		// The duplicate is gone from done.md, so the board is now clean.
		if vs, _ := s.Validate(); len(vs) != 0 {
			t.Errorf("violations after archiving:\n%s", violationMessages(vs))
		}
	})
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
	if _, _, err := testFinishV1(s, "T-0001", FinishRequest{}, today); err != nil {
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
// It is the test that showed what archiving actually costs a live directory —
// 87 items out, and the 51 detail files that used to be stranded with them,
// which is what sent the question to T-0166 and the answer to T-0168. Now those
// 51 travel, and the board is CLEAN afterwards: that is the assertion. It skips
// when the directory is absent, so the module stays buildable in isolation, and
// LOGS what it covered: a skip that looks like a pass is the failure mode this
// file cannot afford.
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
	t.Logf("archived %d items in %v into %v, moving %d detail files (%d stranded)",
		res.Items, res.Months, res.Files, len(res.DetailsMoved), len(res.DetailOrphans))
	if len(res.DetailsMoved) == 0 {
		t.Error("no detail file travelled; on this board most closed items have one")
	}

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
	// One change per item, plus one per detail file that travelled.
	if want := res.Items + len(res.DetailsMoved); len(tx.Changes) != want {
		t.Errorf("%d changes for %d items and %d detail moves",
			len(tx.Changes), res.Items, len(res.DetailsMoved))
	}

	// Every moved file is in its new home and gone from its old one, and the
	// archived line names the new path.
	archives := map[string]string{}
	for _, name := range res.Files {
		archives[name] = readFile(t, dir, name)
	}
	for _, mv := range res.DetailsMoved {
		if exists(t, dir, mv.From) {
			t.Errorf("%s is still in details/", mv.From)
		}
		if !exists(t, dir, mv.To) {
			t.Errorf("%s was not written", mv.To)
		}
		found := false
		for _, body := range archives {
			if strings.Contains(body, "detail:"+mv.To) {
				found = true
			}
		}
		if !found {
			t.Errorf("no archived line points at %s", mv.To)
		}
	}

	// And the board is clean: archiving a live directory now costs it nothing
	// the checker can see, which is the whole of §5.6.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("findings after archiving the real board:\n%s", violationMessages(vs))
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
