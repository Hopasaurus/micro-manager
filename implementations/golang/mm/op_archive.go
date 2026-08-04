package mm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Archiving: rolling old month groups out of done.md (spec-tools.md §5.3,
// §5.3.1, spec-file-format.md §5.6).
//
// done.md is the one file items are never removed from, so it grows without
// bound. The format's answer is done-YYYY.md, with details-YYYY/ beside it, and
// both are deliberately OUTSIDE validation: no checker reads them, this library
// included (§5.6 rule 2).
//
// That makes this the one operation whose whole purpose is to take data out of
// the validated world, and everything awkward about it follows from that:
//
//   - Archived IDs LEAVE THE POOL. I1 stops seeing them, so nothing would
//     notice a second item claiming an archived ID. I2 still holds — next_id is
//     never decremented — but it no longer proves anything about the numbers
//     below it. §5.3 requires the tool to say so out loud, which is why this
//     returns warnings and not just a change set.
//   - A DETAIL FILE TRAVELS WITH ITS ITEM, to details-YYYY/<ID>.md, and the
//     archived line's detail: field is rewritten to match — one operation, not
//     two (§5.6 rule 3). T-0043 shipped the other choice, leaving the file in
//     details/ as an I9 orphan; T-0166 decided the format question and this is
//     what it decided. Moving the file is what keeps the LIVE directory clean,
//     and it needs no validator change: I8 and I9 name details/ exactly, so
//     details-2025/ is already invisible to them.
//   - A report over an archived period reads nothing unless it is asked to
//     include archives (ReportOptions.IncludeArchives).
//
// The move is a cut and paste in that order (§7 rule 4), and the write set
// enforces it structurally: every write happens before any delete. So the
// detail copies and the archive file land BEFORE done.md loses the group and
// BEFORE details/ gives a file up. An interruption leaves the work in both
// places rather than in neither — a re-run heals a half-written archive,
// because insertion skips an ID the archive already holds, and the worst case
// is a stale details/<ID>.md that I9 reports loudly and a person deletes.

// ArchiveRequest selects which month groups leave done.md.
type ArchiveRequest struct {
	// Before is the cutoff: every month group STRICTLY OLDER than this month
	// moves, and the month itself stays.
	//
	// Only the year and month are read. The switch is --before YYYY-MM and a
	// month group is the finest grain done.md records, so a full date is
	// rounded down to its month rather than refused.
	//
	// The zero value means the month of today: a bare Archive keeps the month
	// the directory is living in and rolls everything before it.
	Before Date

	DryRun bool
}

// ArchiveCutoff turns an age in days into the Before a request carries
// (spec-tools.md §5.3.1).
//
// --age DAYS is the same month-granular cutoff as --before, said as a policy
// instead of a date: a month group is archived once DAYS days have passed since
// its LAST day. --age 0 archives every group whose month is complete; --age 30
// keeps each month for a further thirty days.
//
// It lives here rather than in the CLI because it is a rule, not a translation:
// a UI that grows a scheduled archive must compute the same cutoff from the
// same policy, and two implementations of "which months are old enough" would
// eventually disagree by a day.
//
// The condition is monotone in the month — an older group's last day is
// earlier — so it reduces to one exclusive month, which is what Archive takes.
func ArchiveCutoff(today Date, ageDays int) (Date, error) {
	if ageDays < 0 {
		return Date{}, fmt.Errorf("%w: age:%d is negative", ErrInvalidArgument, ageDays)
	}
	limit := today.AddDays(-ageDays)
	first := Date{Year: limit.Year, Month: limit.Month, Day: 1}
	end := dateOf(first.time().AddDate(0, 1, -1)) // the last day of limit's month
	if !limit.Before(end) {
		// The month containing the limit is itself old enough, so the cutoff is
		// the month after it. Reached exactly when limit IS the last day.
		return dateOf(first.time().AddDate(0, 1, 0)), nil
	}
	return first, nil
}

// ArchiveResult reports what an archive run moved.
type ArchiveResult struct {
	// Cutoff is the month archived before, as YYYY-MM. It is reported because
	// it can come from the default: an operation whose boundary is invisible is
	// one the caller cannot check.
	Cutoff string

	// Months are the groups that moved, newest first, and Items counts the item
	// lines removed from done.md.
	Months []string
	Items  int

	// Files are the archives written, relative to the directory: one
	// done-YYYY.md per year touched.
	Files []string

	// DetailsMoved are the detail files that travelled with their items, as
	// "details/T-0007.md -> details-2025/T-0007.md". Reported because the files
	// are no longer where §5.4 says a detail file lives, and because a caller
	// restoring an archived item by hand has to move each one back (§5.6 rule
	// 4).
	DetailsMoved []DetailMove

	// DetailOrphans are detail files an archive could NOT take with it: the
	// path resolves to no file (I8's finding, not this operation's), or another
	// item still claims it. Each one stays in details/ and I9 reports it if the
	// claim was the archived item's only one.
	DetailOrphans []string

	// Warnings state what the archive cost, per §5.3. A caller that drops them
	// is the one not conforming.
	Warnings []string
}

// DetailMove is one detail file following its item out of the live directory.
type DetailMove struct {
	ID   ID
	From string // "details/T-0007.md"
	To   string // "details-2025/T-0007.md"
}

// Archive rolls month groups older than a cutoff out of done.md into
// done-YYYY.md (spec-tools.md §5.3).
//
// Whole groups move, heading included. An empty group is simply dropped: a
// month heading with no items under it has nothing to archive, and the format
// requires every "## " heading in done.md to be a month, not that a month hold
// anything.
//
// A heading that is NOT a month is left exactly where it is. It names no year
// to file under and is already reported as a violation; repairing it is Fix's
// job, not this operation's.
func (s *Store) Archive(req ArchiveRequest, today Date) (ArchiveResult, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero ArchiveResult
	before := req.Before
	if before.IsZero() {
		before = Date{Year: today.Year, Month: today.Month, Day: 1}
	}
	if before.Year < 0 || before.Month < 1 || before.Month > 12 {
		return zero, TxResult{}, fmt.Errorf(
			"%w: before:%04d-%02d is not a month (YYYY-MM)",
			ErrInvalidArgument, before.Year, before.Month)
	}
	cutoff := fmt.Sprintf("%04d-%02d", before.Year, before.Month)
	res := ArchiveResult{Cutoff: cutoff}

	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	d, de, err := t.done()
	if err != nil {
		return zero, TxResult{}, err
	}

	// The groups that move, in file order — newest first, the order the file is
	// already in.
	var moving []*monthSpan
	for _, m := range d.Months {
		if m.Month != "" && m.Month < cutoff {
			moving = append(moving, m)
		}
	}

	// Line ranges are computed against the file AS READ, before any splice
	// moves a line.
	type span struct{ head, end int } // 1-based lines; end is exclusive
	spans := make([]span, 0, len(moving))
	for _, m := range moving {
		spans = append(spans, span{head: m.HeadLine, end: groupEnd(d, de, m)})
	}

	// Bottom-up, so removing the lowest group cannot shift the range of one
	// above it.
	for i := len(spans) - 1; i >= 0; i-- {
		for n := spans[i].end - 1; n >= spans[i].head; n-- {
			de.DeleteLine(n)
		}
	}
	if len(moving) > 0 {
		dropTrailingBlank(de)
		touchUpdated(de, today)
	}

	// Now the paste, into one archive per year. This runs AFTER the splice
	// because InsertItem rewrites each item's Source to its position in the
	// archive, and a done.md splice would then shift a line number that no
	// longer refers to done.md at all.
	targets := map[int]*archiveTarget{}
	var years []int // first-use order, so the write order is stable
	duplicates := 0
	claimed := stillClaimed(t.model, moving)
	orphaned := map[string]bool{}

	for _, m := range moving {
		year, err := yearOfMonth(m.Month)
		if err != nil {
			return zero, TxResult{}, err
		}
		tgt, ok := targets[year]
		if !ok {
			if tgt, err = s.openArchive(year, t.model.grammar(), today); err != nil {
				return zero, TxResult{}, err
			}
			targets[year] = tgt
			years = append(years, year)
		}

		res.Months = append(res.Months, m.Month)
		res.Items += len(m.Items)

		// Oldest first: InsertItem always inserts at the TOP of the group, so
		// replaying the group backwards reproduces the order it had.
		for i := len(m.Items) - 1; i >= 0; i-- {
			it := m.Items[i]

			// The detail file first, and for EVERY item leaving done.md —
			// including one the archive already holds (below). A run
			// interrupted after the archive was written left the file in
			// details/, still claimed by the done.md line that is about to go;
			// skipping the move here would turn that into the orphan this
			// operation exists not to leave.
			if mv, ok := t.moveArchivedDetail(it, year, claimed); ok {
				it.Detail = mv.To // rewritten BEFORE the line is rendered (§5.6 rule 3)
				res.DetailsMoved = append(res.DetailsMoved, mv)
			} else if it.Detail != "" && it.Detail == it.DetailPath() &&
				!claimed[it.Detail] && !orphaned[it.Detail] {
				// The item's own detail file, and it is not there. That is
				// I8's finding rather than this operation's, but the caller
				// should hear that the archive is going out incomplete.
				orphaned[it.Detail] = true
				res.DetailOrphans = append(res.DetailOrphans, it.Detail)
			}

			if tgt.ids[it.ID] {
				// The archive already holds this ID: a previous run was
				// interrupted between the two writes. Do not write a second
				// copy — removing the done.md line is what completes the move.
				duplicates++
				continue
			}
			tgt.ids[it.ID] = true
			line := RenderItemLine(it)
			tgt.file.InsertItem(tgt.edit, m.Month, it)
			t.record(Change{Kind: ChangeMoved, ID: it.ID, File: tgt.name,
				Before: line, After: line})
		}
	}

	// Archives first, done.md second (§7 rule 4): the copy exists before the
	// original goes away.
	for _, y := range years {
		tgt := targets[y]
		touchUpdated(tgt.edit, today)
		if !tgt.edit.Dirty() {
			continue // every item in the group was already archived
		}
		t.ws.Add(filepath.Join(s.path, tgt.name), tgt.edit.Bytes(), tgt.stamp)
		res.Files = append(res.Files, tgt.name)
	}
	t.stage("done.md")

	res.Warnings = archiveWarnings(res, duplicates)

	txr, err := t.commit(req.DryRun)
	if err != nil {
		return zero, txr, err
	}
	return res, txr, nil
}

// archiveWarnings states the cost of the run. §5.3 makes the ID-pool warning
// mandatory; the other two exist because a caller cannot see either condition
// from the change set alone.
func archiveWarnings(res ArchiveResult, duplicates int) []string {
	if res.Items == 0 {
		return nil
	}
	var out []string
	out = append(out, fmt.Sprintf(
		"%s left the ID pool: an archive is outside the format spec and is not "+
			"validated, so I1 and I2 no longer see them (spec-file-format.md §10.5), "+
			"and a report over an archived period needs IncludeArchives to find them",
		plural(res.Items, "archived item", "archived items")))
	if n := len(res.DetailsMoved); n > 0 {
		out = append(out, fmt.Sprintf(
			"%s moved out of details/ into details-YYYY/ with the archived work "+
				"(spec-file-format.md §5.6); restoring an archived item means "+
				"moving its file back and rewriting the detail: field, or I8 "+
				"will say so",
			plural(n, "detail file", "detail files")))
	}
	if n := len(res.DetailOrphans); n > 0 {
		out = append(out, fmt.Sprintf(
			"%s could not travel with its item because the path resolves to no "+
				"file; the archived line now points at nothing: %s",
			plural(n, "detail reference", "detail references"),
			strings.Join(res.DetailOrphans, ", ")))
	}
	if duplicates > 0 {
		out = append(out, fmt.Sprintf(
			"%s already present in the archive and not written twice; a previous "+
				"run was interrupted between its two writes",
			plural(duplicates, "item was", "items were")))
	}
	return out
}

// stillClaimed lists the detail paths a SURVIVING item claims VALIDLY — where
// the path is that item's own details/<ID>.md (I8).
//
// Only a valid claim can hold a file back. An invalid one is already an I8
// finding whose message says the path is wrong and never reaches the "and the
// file exists" half, so moving the file changes nothing about it — and the
// file's rightful owner is the item leaving. Two VALID claims on one path mean
// two items with one ID (an I1 duplicate from a merge, --fix's job): there the
// survivor really would be left pointing at nothing, so the file stays.
func stillClaimed(m *dirModel, moving []*monthSpan) map[string]bool {
	leaving := map[*Item]bool{}
	for _, span := range moving {
		for _, it := range span.Items {
			leaving[it] = true
		}
	}
	claimed := map[string]bool{}
	for _, it := range m.items() {
		if !leaving[it] && it.Detail != "" && it.Detail == it.DetailPath() {
			claimed[it.Detail] = true
		}
	}
	return claimed
}

// moveArchivedDetail takes one item's detail file into details-YYYY/ and
// reports the move (§5.6 rule 3). The caller rewrites the item's detail: field
// to mv.To before the line is rendered into the archive — the two halves are
// one operation, and an implementation that does either alone is not
// conforming (spec-tools.md §5.3.1).
//
// It returns false when there is nothing to take: no detail: field, a field
// that resolves to no file (I8's finding, not this operation's), a path that is
// not this item's own (so the file belongs to whoever it is named for), or a
// file a surviving item validly claims.
func (t *tx) moveArchivedDetail(it *Item, year int, claimed map[string]bool) (DetailMove, bool) {
	if it.Detail == "" || it.Detail != it.DetailPath() {
		return DetailMove{}, false
	}
	df, ok := t.model.details[it.Detail]
	if !ok || claimed[it.Detail] {
		return DetailMove{}, false
	}

	to := fmt.Sprintf("details-%04d/%s.md", year, it.ID)
	path := filepath.Join(t.store.path, to)
	// Stamped before the write for the same reason the archive file is: a
	// re-run over a half-finished archive finds the copy already there, and the
	// stamp has to describe what is on disk or the commit aborts as a phantom
	// conflict (§7 rule 5).
	t.ws.Add(path, t.detailEdit(df).Bytes(), stampOf(path))
	t.ws.Delete(filepath.Join(t.store.path, df.Name))

	// Out of the model, not into it under a new key: details-YYYY/ is outside
	// I8 and I9 (§5.6 rule 2), and the pre-commit reparse must not find an
	// orphan where the file used to be.
	delete(t.model.details, df.Name)
	t.record(Change{Kind: ChangeMoved, ID: it.ID, File: to})
	return DetailMove{ID: it.ID, From: df.Name, To: to}, true
}

// archiveTarget is one done-YYYY.md being written.
type archiveTarget struct {
	name  string
	file  *doneFile
	edit  *fileEdit
	stamp stamp
	ids   map[ID]bool // what the archive already held
}

// openArchive parses an existing done-YYYY.md, or seeds a fresh one.
//
// An archive is NOT part of the directory model: load() deliberately does not
// read it, because nothing validates it and a --check that walked the archives
// would contradict §10.5. So it is read here, and staged with the stamp it was
// read under — the same concurrent-modification check every other file gets
// (§7 rule 5).
func (s *Store) openArchive(year int, g IDGrammar, today Date) (*archiveTarget, error) {
	name := fmt.Sprintf("done-%04d.md", year)
	path := filepath.Join(s.path, name)

	// Stamp before reading, never after: a write that lands in between then
	// makes the stamp too old and the commit fails loudly, where the other
	// order would silently overwrite it.
	st := stampOf(path)
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		// A fresh archive is seeded with the shape done.md has and then parsed
		// back, so one insertion path serves both cases.
		data = []byte(renderArchive(year, today))
		st = stamp{missing: true}
	default:
		return nil, fmt.Errorf("%w: reading %s: %v", ErrIO, name, err)
	}

	// Parse violations are discarded on purpose. An archive is unvalidated
	// (§10.5); reporting findings from a file --check will never look at would
	// invent a rule the format does not have.
	f, _ := parseDoneG(name, data, g)
	tgt := &archiveTarget{name: name, file: f, edit: f.Edit(), stamp: st, ids: map[ID]bool{}}
	for _, it := range f.Items {
		tgt.ids[it.ID] = true
	}
	return tgt, nil
}

// renderArchive is the seed for a new done-YYYY.md.
//
// It is deliberately the shape of done.md: a person opening an archive finds
// the file they already know how to read, and the same parser can read it back.
// The prose says what the format spec says, because the file itself is the only
// place a reader will look.
func renderArchive(year int, today Date) string {
	fm := NewFrontmatter()
	fm.Set("doc", "done")
	fm.Set("version", "1")
	fm.Set("updated", today.String())
	return fm.Render() + "\n# Done " + strconv.Itoa(year) + `

Closed items rolled out of done.md, newest month first. This file is outside
the format spec and is not validated: its items have left the ID pool, so I1
and I2 no longer cover them (spec-file-format.md §5.3, §10.5).
`
}

// groupEnd returns the line one past the last line of a month group.
func groupEnd(d *doneFile, e *fileEdit, m *monthSpan) int {
	for _, other := range d.Months {
		if other.HeadLine > m.HeadLine {
			return other.HeadLine
		}
	}
	// The last group runs to the end of the file. splitLines gives a file that
	// ends in a newline a final EMPTY element, and that element IS the
	// newline: including it in the range would strip the file's last newline.
	if n := len(e.lines); n > 0 && e.lines[n-1] == "" {
		return n
	}
	return len(e.lines) + 1
}

// dropTrailingBlank removes a blank line left at the end of the file by a group
// that used to follow it.
//
// Removing a group leaves the blank that separated it from the group above,
// which is correct in the middle of the file and untidy at the end of it. This
// is the same collapse fileEdit.RemoveItem does for a single line.
func dropTrailingBlank(e *fileEdit) {
	for n := len(e.lines); n >= 2 && e.lines[n-1] == "" && e.lines[n-2] == ""; n-- {
		e.DeleteLine(n - 1)
	}
}

// yearOfMonth reads the year of a YYYY-MM heading.
func yearOfMonth(month string) (int, error) {
	if !isMonth(month) {
		return 0, fmt.Errorf("%w: %q is not a month heading (YYYY-MM)", ErrInvalidArgument, month)
	}
	year, err := strconv.Atoi(month[:4])
	if err != nil {
		return 0, fmt.Errorf("%w: %q has no year", ErrInvalidArgument, month)
	}
	return year, nil
}

// plural renders "1 item" and "2 items" without the caller composing it.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}
