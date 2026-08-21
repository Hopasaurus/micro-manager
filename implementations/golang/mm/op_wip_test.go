package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetWipLimitGrows(t *testing.T) {
	dir, s := startDir(t) // two slots

	d, res, err := s.SetWipLimit(4, false)
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if d.WipLimit != 4 {
		t.Errorf("limit = %d, want 4", d.WipLimit)
	}
	if len(res.Changes) != 2 {
		t.Errorf("want two files created, got %+v", res.Changes)
	}
	for _, name := range []string{"working.03.md", "working.04.md"} {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("missing %s", name)
		}
		if !strings.Contains(string(body), "status: idle") {
			t.Errorf("%s is not an idle slot:\n%s", name, body)
		}
	}
	// The new files use the directory's existing digit width (I10).
	if _, err := os.Stat(filepath.Join(dir, "working.3.md")); err == nil {
		t.Error("a slot was created at the wrong width")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
	// And the new slots work.
	if _, _, err := testStartV1(s, "T-0001", StartRequest{Slot: 4}, today); err != nil {
		t.Errorf("start into a new slot: %v", err)
	}
}

func TestSetWipLimitShrinks(t *testing.T) {
	dir, s := startDir(t)

	d, res, err := s.SetWipLimit(1, false)
	if err != nil {
		t.Fatalf("shrink: %v", err)
	}
	if d.WipLimit != 1 {
		t.Errorf("limit = %d, want 1", d.WipLimit)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeDeleted {
		t.Errorf("want one deletion, got %+v", res.Changes)
	}
	if _, err := os.Stat(filepath.Join(dir, "working.02.md")); !os.IsNotExist(err) {
		t.Error("working.02.md should be gone")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// The rule that matters: an occupied slot is never deleted and never
// renumbered, because renumbering moves an item's file for a reason that has
// nothing to do with the item.
func TestSetWipLimitRefusesToDeleteAnOccupiedSlot(t *testing.T) {
	dir, s := startDir(t)
	if _, _, err := testStartV1(s, "T-0001", StartRequest{Slot: 2}, today); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, dir, "working.02.md")

	_, _, err := s.SetWipLimit(1, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "T-0001") {
		t.Errorf("the error should name the occupant: %v", err)
	}
	if readFile(t, dir, "working.02.md") != before {
		t.Error("the occupied slot was touched")
	}
	// Slot 1 is idle, but it is not the highest-numbered, so it cannot go
	// either - the numbering has to stay contiguous from 1.
	if _, err := os.Stat(filepath.Join(dir, "working.01.md")); err != nil {
		t.Error("working.01.md should still exist")
	}
}

func TestSetWipLimitNoOpWritesNothing(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "working.01.md")

	_, res, err := s.SetWipLimit(2, false)
	if err != nil {
		t.Fatalf("no-op: %v", err)
	}
	if len(res.Files) != 0 {
		t.Errorf("a no-op must write nothing, got %v", res.Files)
	}
	if readFile(t, dir, "working.01.md") != before {
		t.Error("a no-op rewrote a file")
	}
}

func TestSetWipLimitValidatesItsArgument(t *testing.T) {
	_, s := startDir(t)

	if _, _, err := s.SetWipLimit(0, false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("zero: want ErrInvalidArgument, got %v", err)
	}
	if _, _, err := s.SetWipLimit(-3, false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative: want ErrInvalidArgument, got %v", err)
	}
	// 100 slots cannot be numbered at width 2 without mixing widths (I10).
	if _, _, err := s.SetWipLimit(100, false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("too wide: want ErrInvalidArgument, got %v", err)
	}
}

func TestSetWipLimitDryRun(t *testing.T) {
	dir, s := startDir(t)

	_, res, err := s.SetWipLimit(3, true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(res.Files) != 1 {
		t.Errorf("dry run should report the file it would create, got %v", res.Files)
	}
	if _, err := os.Stat(filepath.Join(dir, "working.03.md")); !os.IsNotExist(err) {
		t.Error("dry run created the file")
	}
}

// A directory created at a wider slot width keeps that width when it grows.
func TestSetWipLimitKeepsTheDirectoryWidth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	s, _, err := Init(dir, InitRequest{Project: "Wide", Wip: 2, SlotWidth: 3}, today)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetWipLimit(3, false); err != nil {
		t.Fatalf("grow: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "working.003.md")); err != nil {
		t.Error("the new slot should be working.003.md")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}
