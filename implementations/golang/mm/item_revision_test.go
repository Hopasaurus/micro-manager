package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestItemRevisionCoversExactDetail(t *testing.T) {
	dir, s := v2Dir(t)
	before, err := s.ItemSnapshot("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if before.Detail == nil {
		t.Fatal("fixture item has no detail")
	}
	data, err := os.ReadFile(filepath.Join(dir, before.Detail.Path))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-2] ^= 1
	if err := os.WriteFile(filepath.Join(dir, before.Detail.Path), data, 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if after == before.Revision {
		t.Error("detail-only edit did not change revision")
	}
}

func TestItemRevisionIgnoresOtherItems(t *testing.T) {
	_, s := v2Dir(t)
	before, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	title := "another title"
	today, _ := ParseDate("2026-09-05")
	if _, _, err := s.Update("T-0002", UpdateRequest{Title: &title}, today); err != nil {
		t.Fatal(err)
	}
	after, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Errorf("unrelated item changed revision: %s -> %s", before, after)
	}
}

func TestItemRevisionCoversFullItemLine(t *testing.T) {
	_, s := v2Dir(t)
	before, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	title := "changed item line"
	today, _ := ParseDate("2026-09-05")
	if _, _, err := s.Update("T-0001", UpdateRequest{Title: &title}, today); err != nil {
		t.Fatal(err)
	}
	after, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Error("item-line edit did not change revision")
	}
}

func TestItemRevisionCoversExactItemLinePresentation(t *testing.T) {
	dir, s := v2Dir(t)
	before, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "board.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(data), " | created:2026-07-20 | owner:dana", " | owner:dana | created:2026-07-20", 1)
	if changed == string(data) {
		t.Fatal("fixture item line did not match")
	}
	if err := os.WriteFile(path, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := s.ItemRevision("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if after == before {
		t.Error("exact item-line presentation change did not change revision")
	}
}

func TestItemRevisionNotFound(t *testing.T) {
	_, s := v2Dir(t)
	if _, err := s.ItemRevision("T-9999"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}
