package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRefreshStructureCLIUpdatesAndPreservesUserNotes(t *testing.T) {
	r, dir := v2Project(t)
	path := filepath.Join(dir, "structure.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--refresh-structure"); got.Code != ExitOK {
		t.Fatalf("create managed guide: %s", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = []byte(strings.Replace(string(body), "Add board-specific notes here.", "my exact notes   ", 1))
	body = []byte(strings.Replace(string(body), "## Authority", "## Stale generated heading", 1))
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--refresh-structure")
	if got.Code != ExitOK || got.Stdout != "structure.md updated\n" {
		t.Fatalf("%s", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), "my exact notes   ") || !strings.Contains(string(after), "## Authority") {
		t.Fatalf("refresh did not preserve notes and replace guidance:\n%s", after)
	}

	got = r.run("--refresh-structure", "--porcelain")
	if got.Code != ExitOK || got.Stdout != "structure.md\tunchanged\n" {
		t.Fatalf("porcelain: %s", got)
	}
	got = r.run("--refresh-structure", "--json")
	for _, want := range []string{`"operation": "refresh-structure"`, `"path": "structure.md"`, `"created": false`, `"updated": false`} {
		if got.Code != ExitOK || !strings.Contains(got.Stdout, want) {
			t.Fatalf("JSON missing %s: %s", want, got)
		}
	}
	got = r.run("--help")
	if got.Code != ExitOK || !strings.Contains(got.Stdout, "read structure.md") {
		t.Fatalf("general help does not point back to structure.md: %s", got)
	}
}

func TestRefreshStructureCLIHonorsProtectionMarker(t *testing.T) {
	r, dir := v2Project(t)
	path := filepath.Join(dir, "structure.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--refresh-structure"); got.Code != ExitOK {
		t.Fatalf("create managed guide: %s", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	protected := strings.Replace(string(body), "- [x] Allow `mm --refresh-structure`", "- [ ] Allow `mm --refresh-structure`", 1)
	if err := os.WriteFile(path, []byte(protected), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--refresh-structure")
	if got.Code != ExitPrecondition || !strings.Contains(got.Stderr, "protected") {
		t.Fatalf("%s", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != protected {
		t.Fatal("protected structure.md changed")
	}
}
