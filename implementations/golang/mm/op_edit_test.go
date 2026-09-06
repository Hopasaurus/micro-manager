package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditAtomicallyUpdatesItemAndDetail(t *testing.T) {
	dir, s := v2Dir(t)
	snap, err := s.ItemSnapshot("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	title, detail := "Retitled", "new detail body\n"
	today, _ := ParseDate("2026-09-05")
	it, res, err := s.Edit("T-0001", EditRequest{
		Update: UpdateRequest{Title: &title}, DetailBody: &detail, ExpectedRevision: snap.Revision,
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != title {
		t.Errorf("title = %q", it.Title)
	}
	if len(res.Files) < 2 {
		t.Fatalf("files = %v, want item and detail", res.Files)
	}
	got, err := s.Detail("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != title || got.Body != detail {
		t.Errorf("detail = %+v", got)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "details/T-0001.md")); !strings.Contains(string(data), "title: "+title) {
		t.Error("detail frontmatter title was not synchronized")
	}
}

func TestEditRejectsStaleRevisionBeforeMutation(t *testing.T) {
	dir, s := v2Dir(t)
	snap, err := s.ItemSnapshot("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	boardBefore, _ := os.ReadFile(filepath.Join(dir, "board.md"))
	detailBefore, _ := os.ReadFile(filepath.Join(dir, "details/T-0001.md"))
	title, detail := "must not land", "must not land"
	today, _ := ParseDate("2026-09-05")
	_, _, err = s.Edit("T-0001", EditRequest{Update: UpdateRequest{Title: &title}, DetailBody: &detail, ExpectedRevision: ItemRevision("stale")}, today)
	if !errors.Is(err, ErrConcurrent) {
		t.Fatalf("error = %v, want ErrConcurrent", err)
	}
	boardAfter, _ := os.ReadFile(filepath.Join(dir, "board.md"))
	detailAfter, _ := os.ReadFile(filepath.Join(dir, "details/T-0001.md"))
	if string(boardAfter) != string(boardBefore) || string(detailAfter) != string(detailBefore) {
		t.Error("stale edit wrote one or more files")
	}
	if current, _ := s.ItemRevision("T-0001"); current != snap.Revision {
		t.Error("stale edit changed revision")
	}
}

func TestEditAttachesDetailInSameTransaction(t *testing.T) {
	_, s := v2Dir(t)
	snap, err := s.ItemSnapshot("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	body := "attached"
	today, _ := ParseDate("2026-09-05")
	it, _, err := s.Edit("T-0002", EditRequest{DetailBody: &body, ExpectedRevision: snap.Revision}, today)
	if err != nil {
		t.Fatal(err)
	}
	if it.Detail == "" {
		t.Fatal("detail was not attached")
	}
	if d, err := s.Detail("T-0002"); err != nil || !strings.Contains(d.Body, body) {
		t.Fatalf("detail = %+v, %v", d, err)
	}
}

func TestEditRequiresRevision(t *testing.T) {
	_, s := v2Dir(t)
	today, _ := ParseDate("2026-09-05")
	if _, _, err := s.Edit("T-0001", EditRequest{}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error = %v", err)
	}
}

func TestEditAttachFailureWritesNeitherFile(t *testing.T) {
	dir, s := v2Dir(t)
	snap, err := s.ItemSnapshot("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	boardBefore, _ := os.ReadFile(filepath.Join(dir, "board.md"))
	if err := os.WriteFile(filepath.Join(dir, "details/T-0002.md"), []byte("orphan"), 0o644); err != nil {
		t.Fatal(err)
	}
	title, body := "must not land", "detail"
	today, _ := ParseDate("2026-09-05")
	_, _, err = s.Edit("T-0002", EditRequest{Update: UpdateRequest{Title: &title}, DetailBody: &body, ExpectedRevision: snap.Revision}, today)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("error = %v, want ErrConflict", err)
	}
	boardAfter, _ := os.ReadFile(filepath.Join(dir, "board.md"))
	if string(boardAfter) != string(boardBefore) {
		t.Error("failed detail attach wrote the item line")
	}
	detailAfter, _ := os.ReadFile(filepath.Join(dir, "details/T-0002.md"))
	if string(detailAfter) != "orphan" {
		t.Error("failed detail attach overwrote the orphan")
	}
}
