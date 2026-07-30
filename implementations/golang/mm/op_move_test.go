package mm

import (
	"errors"
	"strings"
	"testing"
)

// moveBacklog has four items in Ready so position arithmetic has room to be
// wrong in both directions.
const moveBacklog = `---
doc: backlog
version: 1
project: Move Tests
next_id: T-0011
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-0001] One | prio:high | created:2026-07-29
- [ ] [T-0002] Two | prio:med | created:2026-07-29
- [ ] [T-0003] Three | prio:med | created:2026-07-29
- [ ] [T-0004] Four | prio:low | created:2026-07-29

## Blocked

- [ ] [T-0005] Waiting | prio:low | created:2026-07-29 | blocked:on a thing

## Someday

- [ ] [T-0006] Maybe | prio:low | created:2026-07-29
`

func moveDir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newDir(t, map[string]string{
		"backlog.md": moveBacklog,
		"done.md":    "---\ndoc: done\nversion: 1\n---\n\n# Done\n",
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

func readyIDs(t *testing.T, s *Store) []ID {
	t.Helper()
	items, err := s.List(Filter{Section: SectionReady})
	if err != nil {
		t.Fatal(err)
	}
	return idsOfValues(items)
}

func TestMoveWithinSection(t *testing.T) {
	cases := []struct {
		name string
		id   ID
		req  MoveRequest
		want []ID
	}{
		{"top", "T-0003", MoveRequest{Top: true}, []ID{"T-0003", "T-0001", "T-0002", "T-0004"}},
		{"end", "T-0001", MoveRequest{End: true}, []ID{"T-0002", "T-0003", "T-0004", "T-0001"}},
		{"position 1", "T-0004", MoveRequest{Position: 1}, []ID{"T-0004", "T-0001", "T-0002", "T-0003"}},
		{"position 2", "T-0004", MoveRequest{Position: 2}, []ID{"T-0001", "T-0004", "T-0002", "T-0003"}},
		// Downward moves are where off-by-one lives: position is counted on the
		// section as displayed, which still includes the item being moved.
		{"position 4 downward", "T-0001", MoveRequest{Position: 4}, []ID{"T-0002", "T-0003", "T-0004", "T-0001"}},
		{"position 3 downward", "T-0001", MoveRequest{Position: 3}, []ID{"T-0002", "T-0003", "T-0001", "T-0004"}},
		{"before", "T-0004", MoveRequest{Before: "T-0002"}, []ID{"T-0001", "T-0004", "T-0002", "T-0003"}},
		{"after", "T-0001", MoveRequest{After: "T-0003"}, []ID{"T-0002", "T-0003", "T-0001", "T-0004"}},
		{"after last", "T-0001", MoveRequest{After: "T-0004"}, []ID{"T-0002", "T-0003", "T-0004", "T-0001"}},
		{"before first", "T-0003", MoveRequest{Before: "T-0001"}, []ID{"T-0003", "T-0001", "T-0002", "T-0004"}},
	}
	for _, c := range cases {
		_, s := moveDir(t)
		if _, _, err := s.Move(c.id, c.req, today); err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := readyIDs(t, s); !sameIDs(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
		if vs, _ := s.Validate(); len(vs) != 0 {
			t.Errorf("%s: violations:\n%s", c.name, violationMessages(vs))
		}
	}
}

func TestMoveBetweenSections(t *testing.T) {
	// Ready -> Someday, appended by default.
	dir, s := moveDir(t)
	it, _, err := s.Move("T-0002", MoveRequest{Section: SectionSomeday}, today)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if it.Section != SectionSomeday {
		t.Errorf("section = %q", it.Section)
	}
	someday, _ := s.List(Filter{Section: SectionSomeday})
	if got := idsOfValues(someday); !sameIDs(got, []ID{"T-0006", "T-0002"}) {
		t.Errorf("someday = %v, want T-0006 then T-0002 (appended)", got)
	}
	if got := readyIDs(t, s); !sameIDs(got, []ID{"T-0001", "T-0003", "T-0004"}) {
		t.Errorf("ready = %v", got)
	}
	// The line really moved across the heading boundary in the file.
	out := readDirFile(t, dir, "backlog.md")
	if strings.Index(out, "T-0002") < strings.Index(out, "## Someday") {
		t.Errorf("the item did not cross into Someday:\n%s", out)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}

	// A selector is interpreted in the DESTINATION section.
	_, s = moveDir(t)
	if _, _, err := s.Move("T-0002", MoveRequest{Section: SectionSomeday, Top: true}, today); err != nil {
		t.Fatal(err)
	}
	someday, _ = s.List(Filter{Section: SectionSomeday})
	if got := idsOfValues(someday); !sameIDs(got, []ID{"T-0002", "T-0006"}) {
		t.Errorf("--top in destination: got %v", got)
	}
}

// I5 ties blocked: to the section, so a section change has to carry it.
func TestMoveIntoBlockedNeedsAReason(t *testing.T) {
	_, s := moveDir(t)
	_, _, err := s.Move("T-0001", MoveRequest{Section: SectionBlocked}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "--blocked") {
		t.Errorf("the message should name the remedy: %v", err)
	}

	dir, s := moveDir(t)
	it, _, err := s.Move("T-0001",
		MoveRequest{Section: SectionBlocked, Blocked: "waiting on review"}, today)
	if err != nil {
		t.Fatalf("move with a reason: %v", err)
	}
	if it.Blocked != "waiting on review" {
		t.Errorf("blocked = %q", it.Blocked)
	}
	if !strings.Contains(readDirFile(t, dir, "backlog.md"), "blocked:waiting on review") {
		t.Error("the reason was not written")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// Moving out of Blocked drops the reason, or I5 fails the other way.
func TestMoveOutOfBlockedDropsTheReason(t *testing.T) {
	dir, s := moveDir(t)
	it, _, err := s.Move("T-0005", MoveRequest{Section: SectionReady, Top: true}, today)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if it.Blocked != "" {
		t.Errorf("blocked should be dropped, got %q", it.Blocked)
	}
	out := readDirFile(t, dir, "backlog.md")
	if strings.Contains(out, "blocked:on a thing") {
		t.Errorf("the reason survived the move out of Blocked:\n%s", out)
	}
	if got := readyIDs(t, s); got[0] != "T-0005" {
		t.Errorf("ready = %v", got)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}

	// Supplying a reason for a non-Blocked destination is a mistake, not a hint.
	_, s = moveDir(t)
	if _, _, err := s.Move("T-0005",
		MoveRequest{Section: SectionReady, Top: true, Blocked: "why"}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// Out of range is an error, not a clamp: landing near what was asked for is
// worse than refusing.
func TestMovePositionOutOfRange(t *testing.T) {
	dir, s := moveDir(t)
	before := readDirFile(t, dir, "backlog.md")

	_, _, err := s.Move("T-0001", MoveRequest{Position: 5}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "holds 4 item(s)") {
		t.Errorf("the message should state the real length: %v", err)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a refused move wrote to the file")
	}

	// Position 4 is the last slot and must be accepted.
	if _, _, err := s.Move("T-0001", MoveRequest{Position: 4}, today); err != nil {
		t.Errorf("position 4 of 4 should be valid: %v", err)
	}

	// Cross-section: one past the end appends.
	_, s = moveDir(t)
	if _, _, err := s.Move("T-0001",
		MoveRequest{Section: SectionSomeday, Position: 2}, today); err != nil {
		t.Errorf("appending to a 1-item section should be valid: %v", err)
	}
	_, s = moveDir(t)
	if _, _, err := s.Move("T-0001",
		MoveRequest{Section: SectionSomeday, Position: 3}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("two past the end should be refused, got %v", err)
	}
}

func TestMoveSelectorValidation(t *testing.T) {
	_, s := moveDir(t)
	cases := []struct {
		name string
		id   ID
		req  MoveRequest
		want error
	}{
		{"no destination", "T-0001", MoveRequest{}, ErrInvalidArgument},
		{"two selectors", "T-0001", MoveRequest{Top: true, End: true}, ErrInvalidArgument},
		{"position and before", "T-0001", MoveRequest{Position: 1, Before: "T-0002"}, ErrInvalidArgument},
		{"before itself", "T-0001", MoveRequest{Before: "T-0001"}, ErrInvalidArgument},
		{"after itself", "T-0001", MoveRequest{After: "T-0001"}, ErrInvalidArgument},
		{"unknown item", "T-9999", MoveRequest{Top: true}, ErrNotFound},
		{"unknown section", "T-0001", MoveRequest{Section: "Later"}, ErrInvalidArgument},
	}
	for _, c := range cases {
		if _, _, err := s.Move(c.id, c.req, today); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

// --before/--after name an item in the DESTINATION section; anywhere else is a
// mistake worth reporting rather than guessing at.
func TestMoveRelativeToItemInAnotherSection(t *testing.T) {
	_, s := moveDir(t)
	_, _, err := s.Move("T-0001", MoveRequest{Before: "T-0006"}, today) // T-0006 is in Someday
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "not in the destination section") {
		t.Errorf("message: %v", err)
	}

	// With --section it becomes valid, because the destination changes.
	_, s = moveDir(t)
	if _, _, err := s.Move("T-0001",
		MoveRequest{Section: SectionSomeday, Before: "T-0006"}, today); err != nil {
		t.Errorf("should be valid once the destination is Someday: %v", err)
	}
	someday, _ := s.List(Filter{Section: SectionSomeday})
	if got := idsOfValues(someday); !sameIDs(got, []ID{"T-0001", "T-0006"}) {
		t.Errorf("got %v", got)
	}
}

// Only backlog items move: done.md ordering is meaningless beyond its month
// grouping, and slots are interchangeable.
func TestMoveRefusesNonBacklogItems(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md":    moveBacklog,
		"working.02.md": strings.Replace(busySlot, "id: T-0042", "id: T-0007", 1),
	})
	s := mustOpen(t, dir)

	for _, id := range []ID{"T-0007", "T-0010"} {
		_, _, err := s.Move(id, MoveRequest{Top: true}, today)
		if err == nil {
			t.Errorf("%s should not be movable", id)
			continue
		}
		if !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: want ErrConflict or ErrNotFound, got %v", id, err)
		}
		if errors.Is(err, ErrConflict) && !strings.Contains(err.Error(), "--start") {
			t.Errorf("%s: the message should point at the right operation: %v", id, err)
		}
	}
}

// A move that changes nothing writes nothing.
func TestMoveNoOp(t *testing.T) {
	dir, s := moveDir(t)
	before := readDirFile(t, dir, "backlog.md")

	if _, res, err := s.Move("T-0001", MoveRequest{Top: true}, today); err != nil {
		t.Fatal(err)
	} else if len(res.Files) != 0 {
		t.Errorf("moving an item to where it already is should write nothing, got %v", res.Files)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a no-op move rewrote the file")
	}
}

func TestMoveDryRun(t *testing.T) {
	dir, s := moveDir(t)
	before := readDirFile(t, dir, "backlog.md")

	it, res, err := s.Move("T-0004", MoveRequest{Top: true, DryRun: true}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Changes) != 1 || res.Changes[0].Kind != ChangeMoved {
		t.Errorf("res = %+v", res)
	}
	if it.Pos != 1 {
		t.Errorf("the dry run should report the resulting position, got %d", it.Pos)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a dry run wrote to the file")
	}
}

// Unregistered fields must survive a move, like every other operation.
func TestMovePreservesUnregisteredFields(t *testing.T) {
	src := strings.Replace(moveBacklog,
		"- [ ] [T-0001] One | prio:high | created:2026-07-29",
		"- [ ] [T-0001] One | prio:high | created:2026-07-29 | owner:dlh", 1)
	dir := newDir(t, map[string]string{
		"backlog.md": src,
		"done.md":    "---\ndoc: done\nversion: 1\n---\n\n# Done\n",
	})
	s := mustOpen(t, dir)
	if _, _, err := s.Move("T-0001", MoveRequest{Section: SectionSomeday}, today); err != nil {
		t.Fatal(err)
	}
	if out := readDirFile(t, dir, "backlog.md"); !strings.Contains(out, "owner:dlh") {
		t.Errorf("the move destroyed an unregistered field:\n%s", out)
	}
}
