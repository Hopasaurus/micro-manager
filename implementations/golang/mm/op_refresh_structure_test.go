package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshStructureCreatesMissingManagedGuide(t *testing.T) {
	dir, s := v2Dir(t)
	today, _ := ParseDate("2026-09-05")
	result, tx, err := s.RefreshStructure(RefreshStructureRequest{}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Updated || len(tx.Files) != 1 {
		t.Fatalf("result=%+v tx=%+v", result, tx)
	}
	body := readFile(t, dir, "structure.md")
	for _, want := range []string{structureRefreshChecked, structureRefreshOpen, structureRefreshClose, "board.md", "mm --help", "https://github.com/Hopasaurus/micro-manager", "project/spec-file-format.md"} {
		if !strings.Contains(body, want) {
			t.Errorf("guide missing %q", want)
		}
	}
	for _, retired := range []string{"working.NN.md", "backlog.md", "blocked:"} {
		if strings.Contains(body, retired) {
			t.Errorf("guide contains retired v1 concept %q", retired)
		}
	}
}

func TestRefreshStructurePreservesDescriptionAndUserNotesExactly(t *testing.T) {
	dir, s := v2Dir(t)
	today, _ := ParseDate("2026-09-05")
	original := renderManagedStructureV2("Old name", today, "My board description.", DefaultIDGrammar(), "\n## User notes\n\nline with spaces   \n\n")
	writeFile(t, dir, "structure.md", original)
	result, _, err := s.RefreshStructure(RefreshStructureRequest{}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Updated {
		t.Fatal("refresh did not update project-specific generated content")
	}
	body := readFile(t, dir, "structure.md")
	if !strings.Contains(body, "My board description.") || !strings.Contains(body, "line with spaces   \n\n"+structureRefreshClose) {
		t.Fatalf("preserved content changed:\n%s", body)
	}
	before, _ := os.Stat(filepath.Join(dir, "structure.md"))
	result, tx, err := s.RefreshStructure(RefreshStructureRequest{}, today)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(filepath.Join(dir, "structure.md"))
	if result.Updated || len(tx.Files) != 0 || !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("current refresh was not a no-op: %+v %+v", result, tx)
	}
}

func TestRefreshStructureProtectsUnmanagedOrMalformedFile(t *testing.T) {
	today, _ := ParseDate("2026-09-05")
	cases := map[string]string{
		"marker deleted":   "# Mine\n" + structureRefreshOpen + "\nnotes\n" + structureRefreshClose + "\n",
		"unchecked":        strings.Replace(renderManagedStructureV2("P", today, "", DefaultIDGrammar(), "\n"), "- [x]", "- [ ]", 1),
		"duplicate marker": renderManagedStructureV2("P", today, "", DefaultIDGrammar(), "\n") + structureRefreshChecked + "\n",
		"missing close":    strings.Replace(renderManagedStructureV2("P", today, "", DefaultIDGrammar(), "\n"), structureRefreshClose, "", 1),
		"duplicate open":   strings.Replace(renderManagedStructureV2("P", today, "", DefaultIDGrammar(), "\n"), structureRefreshOpen, structureRefreshOpen+"\n"+structureRefreshOpen, 1),
	}
	for name, original := range cases {
		t.Run(name, func(t *testing.T) {
			dir, s := v2Dir(t)
			writeFile(t, dir, "structure.md", original)
			_, _, err := s.RefreshStructure(RefreshStructureRequest{}, today)
			if !errors.Is(err, ErrPreconditionFailed) {
				t.Fatalf("error=%v, want ErrPreconditionFailed", err)
			}
			if got := readFile(t, dir, "structure.md"); got != original {
				t.Error("protected file changed")
			}
		})
	}
}

func TestRefreshStructureDryRunDoesNotCreate(t *testing.T) {
	dir, s := v2Dir(t)
	today, _ := ParseDate("2026-09-05")
	result, tx, err := s.RefreshStructure(RefreshStructureRequest{DryRun: true}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || !tx.DryRun || len(tx.Files) != 1 {
		t.Fatalf("result=%+v tx=%+v", result, tx)
	}
	if _, err := os.Stat(filepath.Join(dir, "structure.md")); !os.IsNotExist(err) {
		t.Fatal("dry run created structure.md")
	}
}
