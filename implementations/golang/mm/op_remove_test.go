package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The guard is the operation's main feature: the format's answer to "I don't
// want to do this any more" is outcome:cancelled, not deletion.
func TestRemoveRequiresForce(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "backlog.md")

	_, _, err := s.Remove("T-0002", RemoveRequest{}, today)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("want ErrPreconditionFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("the refusal should point at --finish --outcome cancelled: %v", err)
	}
	if readFile(t, dir, "backlog.md") != before {
		t.Error("a refused remove wrote to disk")
	}
}

func TestRemoveRetiresTheID(t *testing.T) {
	dir, s := startDir(t)

	out, _, err := s.Remove("T-0002", RemoveRequest{Force: true}, today)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if out.Item.ID != "T-0002" {
		t.Errorf("removal reported %q", out.Item.ID)
	}
	if strings.Contains(readFile(t, dir, "backlog.md"), "[T-0002]") {
		t.Error("the line is still there")
	}
	// I2: the counter only ever moves forward. A recycled ID would inherit the
	// removed item's history everywhere it was ever named.
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != "T-0011" {
		t.Errorf("next_id = %s, want T-0011 unchanged", d.NextID)
	}
	if _, err := s.Get("T-0002"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound after removal, got %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestRemoveFromDone(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := s.Remove("T-0009", RemoveRequest{Force: true}, today); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if strings.Contains(readFile(t, dir, "done.md"), "[T-0009]") {
		t.Error("the done line is still there")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestRemoveRefusesAWorkingItem(t *testing.T) {
	_, s := startDir(t)
	if _, _, err := s.Start("T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Remove("T-0001", RemoveRequest{Force: true}, today)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("want ErrConflict, got %v", err)
	}
}

// Either delete the detail file or report what was left behind. Silently
// leaving an invalid directory is the one thing §5.1.6 forbids.
func TestRemoveReportsTheOrphanedDetailFile(t *testing.T) {
	dir, s := startDir(t)

	out, _, err := s.Remove("T-0001", RemoveRequest{Force: true}, today)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if out.DetailOrphan != "details/T-0001.md" {
		t.Errorf("orphan = %q, want it reported", out.DetailOrphan)
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); err != nil {
		t.Error("the detail file should still be on disk")
	}
	// And the directory is now genuinely invalid, exactly as reported.
	vs, _ := s.Validate()
	found := false
	for _, v := range vs {
		if v.Invariant == "I9" && v.At.File == "details/T-0001.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an I9 orphan finding, got:\n%s", violationMessages(vs))
	}
}

func TestRemoveWithDetail(t *testing.T) {
	dir, s := startDir(t)

	out, _, err := s.Remove("T-0001", RemoveRequest{Force: true, WithDetail: true}, today)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if out.DetailDeleted != "details/T-0001.md" || out.DetailOrphan != "" {
		t.Errorf("removal = %+v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); !os.IsNotExist(err) {
		t.Error("the detail file should be gone")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestRemoveDryRun(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "backlog.md")

	if _, _, err := s.Remove("T-0001", RemoveRequest{Force: true, WithDetail: true, DryRun: true}, today); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if readFile(t, dir, "backlog.md") != before {
		t.Error("dry run wrote backlog.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); err != nil {
		t.Error("dry run deleted the detail file")
	}
}

func TestRemoveNotFound(t *testing.T) {
	_, s := startDir(t)
	if _, _, err := s.Remove("T-0099", RemoveRequest{Force: true}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}
