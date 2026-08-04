package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0123 — mm fix: deterministic repair of I1 duplicates / I2 ceiling after a
// merge (plan-git-support.md decision 4).

// mergedBacklog is the T-0101 reproduction: two branches both allocated
// T-0043 from the same committed next_id, git auto-merged with no conflict
// markers, and the directory has two T-0043 items. The older (Alpha) has no
// detail file; the newer (Beta) created details/T-0043.md on its branch.
const mergedBacklog = `---
doc: backlog
version: 1
project: Merged Board
next_id: T-0044
updated: 2026-08-01
---
# Backlog
## Ready
- [ ] [T-0041] First | created:2026-07-29
- [ ] [T-0042] Second | created:2026-07-29
- [ ] [T-0043] Alpha | created:2026-07-31
- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md

## Blocked

## Someday
`

const betaDetail = `---
doc: detail
id: T-0043
title: Beta
updated: 2026-08-01
---
# T-0043 — Beta

Some long form.
`

func mergedBoard(t *testing.T) string {
	t.Helper()
	return newDir(t, map[string]string{
		"backlog.md":        mergedBacklog,
		"details/T-0043.md": betaDetail,
	})
}

// The headline scenario: the merged board repairs to a valid directory — the
// older item keeps the ID, the newer is renumbered to a fresh ID, its detail
// file follows it (renamed and re-id'd in the same transaction), and next_id
// is bumped exactly once to cover the new high ID.
func TestFixRepairsTheMergedBoard(t *testing.T) {
	dir := mergedBoard(t)
	s := mustOpen(t, dir)

	// The board is broken exactly the way a merge breaks it: one duplicate.
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Invariant != "I1" || !strings.Contains(vs[0].Message, "T-0043") {
		t.Fatalf("pre-fix violations = %s, want one I1 duplicate", violationMessages(vs))
	}

	res, tx, err := s.Fix(FixRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 {
		t.Fatalf("renumberings = %d, want 1", len(res.Changes))
	}
	c := res.Changes[0]
	if c.OldID != "T-0043" || c.NewID != "T-0044" {
		t.Errorf("renumber = %s -> %s, want T-0043 -> T-0044", c.OldID, c.NewID)
	}
	if c.File != "backlog.md" {
		t.Errorf("file = %s, want backlog.md", c.File)
	}
	if c.Detail != "details/T-0043.md" {
		t.Errorf("detail = %s, want details/T-0043.md", c.Detail)
	}
	if !res.NextBumped || res.WasNext != "T-0044" || res.NextID != "T-0045" {
		t.Errorf("next_id %s -> %s (bumped=%v), want T-0044 -> T-0045 (bumped=true)",
			res.WasNext, res.NextID, res.NextBumped)
	}

	// The kept item is the older one, in place.
	if it, err := s.Get("T-0043"); err != nil || it.Title != "Alpha" {
		t.Errorf("T-0043 = %+v, %v; want Alpha", it, err)
	}
	// The loser renumbered, with the detail link following it.
	loser, err := s.Get("T-0044")
	if err != nil {
		t.Fatalf("T-0044 not found: %v", err)
	}
	if loser.Title != "Beta" || loser.Detail != "details/T-0044.md" {
		t.Errorf("renumbered item = %q detail=%q, want Beta with details/T-0044.md",
			loser.Title, loser.Detail)
	}

	// The detail file moved and its id: line was rewritten (I9).
	if _, err := os.Stat(filepath.Join(dir, "details", "T-0043.md")); !os.IsNotExist(err) {
		t.Error("details/T-0043.md still exists")
	}
	data, err := os.ReadFile(filepath.Join(dir, "details", "T-0044.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "id: T-0044") || !strings.Contains(body, "title: Beta") {
		t.Errorf("renamed detail file must carry id T-0044 and title Beta:\n%s", body)
	}

	// The directory is valid again, and check.sh agrees.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after fix:\n%s", violationMessages(vs))
	}
	requireBash(t)
	if got := runCheckSh(t, dir); len(got) != 0 {
		t.Errorf("check.sh findings after fix: %v", got)
	}
	if len(tx.Files) == 0 {
		t.Error("fix reported no touched files")
	}
}

// Running fix twice is a no-op: the second run touches nothing.
func TestFixIsIdempotent(t *testing.T) {
	dir := mergedBoard(t)
	s := mustOpen(t, dir)
	if _, _, err := s.Fix(FixRequest{}); err != nil {
		t.Fatal(err)
	}
	res, tx, err := s.Fix(FixRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 0 || res.NextBumped {
		t.Errorf("second run reported changes: %+v", res)
	}
	if len(tx.Files) != 0 {
		// A no-op must write nothing: no rewrite, no mtime bump.
		t.Errorf("second run would touch files: %v", tx.Files)
	}
}

// A dry run reports exactly what the real run does, and writes nothing.
func TestFixDryRunMatchesRealRun(t *testing.T) {
	dir := mergedBoard(t)
	s := mustOpen(t, dir)

	dryRes, dryTx, err := s.Fix(FixRequest{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(dryTx.Files) == 0 {
		t.Fatal("dry run reported no work")
	}

	// Nothing was written.
	if vs, _ := s.Validate(); len(vs) != 1 {
		t.Fatalf("dry run changed the directory:\n%s", violationMessages(vs))
	}

	realRes, realTx, err := s.Fix(FixRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(realRes.Changes) != len(dryRes.Changes) || len(realTx.Files) != len(dryTx.Files) {
		t.Errorf("dry/real disagree: changes %d/%d, files %d/%d",
			len(dryRes.Changes), len(realRes.Changes), len(dryTx.Files), len(realTx.Files))
	}
	for i := range dryRes.Changes {
		if dryRes.Changes[i] != realRes.Changes[i] {
			t.Errorf("change %d: dry %+v, real %+v", i, dryRes.Changes[i], realRes.Changes[i])
		}
	}
}

// The same ID in two homes: backlog.md AND a working slot. The slot is the
// more advanced home, so the working item keeps the ID and the backlog copy is
// renumbered.
func TestFixTwoHomesKeepsTheWorkingItem(t *testing.T) {
	working := strings.Replace(idleSlot, "status: idle", "status: working", 1)
	working = strings.Replace(working, "id: null", "id: T-0043", 1)
	working = strings.Replace(working, "title: null", "title: Working copy", 1)
	working = strings.Replace(working, "created: null", "created: 2026-07-30", 1)
	working = strings.Replace(working, "started: null", "started: 2026-07-30", 1)

	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(mergedBacklog,
			"- [ ] [T-0043] Alpha | created:2026-07-31\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md",
			"- [ ] [T-0043] Backlog copy | created:2026-08-01", 1),
		"working.01.md": working,
	})
	s := mustOpen(t, dir)

	res, _, err := s.Fix(FixRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 {
		t.Fatalf("renumberings = %d, want 1:\n%v", len(res.Changes), res.Changes)
	}
	// The working copy wins the ID.
	if it, err := s.Get("T-0043"); err != nil || it.Title != "Working copy" || it.State != StateWorking {
		t.Errorf("T-0043 = %+v, %v; want the working copy", it, err)
	}
	if it, err := s.Get("T-0044"); err != nil || it.Title != "Backlog copy" || it.State != StateBacklog {
		t.Errorf("renumbered = %+v, %v; want the backlog copy in the backlog", it, err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after fix:\n%s", violationMessages(vs))
	}
}

// A tie — same home, same created — refuses with both items named. The repair
// must never invent an answer a human should give.
func TestFixTieRefusesNamingBothItems(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(mergedBacklog,
			"- [ ] [T-0043] Alpha | created:2026-07-31\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md",
			"- [ ] [T-0043] Alpha | created:2026-08-01\n- [ ] [T-0043] Beta | created:2026-08-01", 1),
	})
	s := mustOpen(t, dir)

	_, _, err := s.Fix(FixRequest{})
	if err == nil {
		t.Fatal("tie did not refuse")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Alpha") || !strings.Contains(msg, "Beta") {
		t.Errorf("refusal should name both items: %s", msg)
	}
	// Nothing was written.
	if vs, _ := s.Validate(); len(vs) == 0 {
		t.Error("directory changed after the refusal")
	}
}

// Anything beyond I1/I2 blocks the repair: a directory with conflict markers
// must be resolved by hand first, and fix must say so rather than papering
// over it.
func TestFixRefusesOnBlockers(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(mergedBacklog,
			"## Ready\n",
			"## Ready\n\n<<<<<<< HEAD\n- [ ] [T-0046] Unmerged | created:2026-08-01\n=======\n>>>>>>> theirs\n", 1),
		"details/T-0043.md": betaDetail,
	})
	s := mustOpen(t, dir)

	_, _, err := s.Fix(FixRequest{})
	var invErr *InvariantError
	if !errors.As(err, &invErr) {
		t.Fatalf("want InvariantError, got %v", err)
	}
	found := false
	for _, v := range invErr.Violations {
		if strings.Contains(v.Message, "conflict-marker") {
			found = true
		}
	}
	if !found {
		t.Errorf("refusal should name the marker finding: %v", invErr.Violations)
	}
}

// The adopt path: the LOSER references a detail file whose frontmatter title
// matches the WINNER. The file stays where it is and the winner claims it;
// the loser releases its claim rather than pointing at a file that is not its
// own (I9).
func TestFixWinnerAdoptsTheDetailFile(t *testing.T) {
	backlog := strings.Replace(mergedBacklog,
		"- [ ] [T-0043] Alpha | created:2026-07-31\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md",
		"- [ ] [T-0043] Alpha | created:2026-07-31\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md", 1)
	// The file's title matches ALPHA (the winner), while BETA's line carries
	// the reference — the survivor of a merge whose content came from the
	// winner's branch.
	dir := newDir(t, map[string]string{
		"backlog.md":        backlog,
		"details/T-0043.md": strings.Replace(betaDetail, "title: Beta", "title: Alpha", 1),
	})
	s := mustOpen(t, dir)

	if _, _, err := s.Fix(FixRequest{}); err != nil {
		t.Fatal(err)
	}
	winner, err := s.Get("T-0043")
	if err != nil {
		t.Fatal(err)
	}
	if winner.Title != "Alpha" || winner.Detail != "details/T-0043.md" {
		t.Errorf("winner = %q detail %q, want Alpha with details/T-0043.md", winner.Title, winner.Detail)
	}
	loser, err := s.Get("T-0044")
	if err != nil {
		t.Fatal(err)
	}
	if loser.Detail != "" {
		t.Errorf("loser detail = %q, want empty", loser.Detail)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after fix:\n%s", violationMessages(vs))
	}
}

// Both duplicates claim the shared file and the file's title matches the
// LOSER: the file is renamed to the loser's new ID and the winner releases
// its claim — a winner must not point at a file that is no longer its own.
func TestFixRenameDropsTheWinnersClaim(t *testing.T) {
	backlog := strings.Replace(mergedBacklog,
		"- [ ] [T-0043] Alpha | created:2026-07-31\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md",
		"- [ ] [T-0043] Alpha | created:2026-07-31 | detail:details/T-0043.md\n- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md", 1)
	dir := newDir(t, map[string]string{
		"backlog.md":        backlog,
		"details/T-0043.md": betaDetail, // title Beta, matching the loser
	})
	s := mustOpen(t, dir)

	if _, _, err := s.Fix(FixRequest{}); err != nil {
		t.Fatal(err)
	}
	winner, err := s.Get("T-0043")
	if err != nil {
		t.Fatal(err)
	}
	if winner.Title != "Alpha" || winner.Detail != "" {
		t.Errorf("winner = %q detail %q, want Alpha with no detail claim", winner.Title, winner.Detail)
	}
	loser, err := s.Get("T-0044")
	if err != nil {
		t.Fatal(err)
	}
	if loser.Title != "Beta" || loser.Detail != "details/T-0044.md" {
		t.Errorf("loser = %q detail %q, want Beta with details/T-0044.md", loser.Title, loser.Detail)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after fix:\n%s", violationMessages(vs))
	}
}
