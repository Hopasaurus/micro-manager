package mm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The vector table is shared with every other implementation: it is hashed
// strings, not fixtures, so nothing in it needs a filesystem.
func TestProjectIDVectors(t *testing.T) {
	data, err := os.ReadFile("../testdata/projectid-vectors.json")
	if err != nil {
		t.Fatalf("vector table missing: %v", err)
	}
	var table struct {
		Vectors []struct {
			Path string `json:"path"`
			ID   string `json:"id"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatalf("vector table malformed: %v", err)
	}
	if len(table.Vectors) < 5 {
		t.Fatalf("vector table has only %d entries; it is meant to pin the algorithm", len(table.Vectors))
	}

	for _, v := range table.Vectors {
		if got := projectIDOf(v.Path); got != v.ID {
			t.Errorf("projectIDOf(%q) = %s, want %s", v.Path, got, v.ID)
		}
	}
}

// The composed and decomposed spellings of one path are one project.
func TestProjectIDNormalisesToNFC(t *testing.T) {
	const (
		composed   = "/home/u/café/micro-manager"
		decomposed = "/home/u/café/micro-manager"
	)
	if composed == decomposed {
		t.Fatal("the test strings are identical; the NFD form was lost in editing")
	}
	if a, b := projectIDOf(composed), projectIDOf(decomposed); a != b {
		t.Errorf("NFC and NFD spellings of one path disagree: %s vs %s", a, b)
	}
}

func TestProjectIDShape(t *testing.T) {
	id, err := ProjectID(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != ProjectIDLength {
		t.Errorf("id %q is %d characters, want %d", id, len(id), ProjectIDLength)
	}
	if strings.ToLower(id) != id {
		t.Errorf("id %q is not lowercase", id)
	}
	for _, r := range id {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("id %q is not hex", id)
		}
	}
}

func TestProjectIDIsStable(t *testing.T) {
	dir := t.TempDir()
	first, err := ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("two calls disagree: %s vs %s", first, second)
	}
}

// Reaching one directory by two routes must yield one id - that is what makes
// the recent and favorites lists deduplicate correctly.
func TestProjectIDCollapsesEquivalentPaths(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "code", "micro-manager")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "code"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	want, err := ProjectID(real)
	if err != nil {
		t.Fatal(err)
	}
	routes := []string{
		real + string(filepath.Separator),               // trailing separator
		filepath.Join(link, "micro-manager"),            // through a symlink
		filepath.Join(real, ".", "..", "micro-manager"), // an unclean path
	}
	for _, route := range routes {
		got, err := ProjectID(route)
		if err != nil {
			t.Fatalf("%s: %v", route, err)
		}
		if got != want {
			t.Errorf("ProjectID(%q) = %s, want %s", route, got, want)
		}
	}
}

func TestProjectIDOfARelativePath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.Mkdir("micro-manager", 0o755); err != nil {
		t.Fatal(err)
	}
	rel, err := ProjectID("micro-manager")
	if err != nil {
		t.Fatal(err)
	}
	abs, err := ProjectID(filepath.Join(dir, "micro-manager"))
	if err != nil {
		t.Fatal(err)
	}
	if rel != abs {
		t.Errorf("relative %s and absolute %s disagree", rel, abs)
	}
}

// spec-gui.md §10 rule 5: an entry whose path no longer resolves is rendered as
// missing, never dropped. It cannot be rendered without an id.
func TestProjectIDOfAPathThatDoesNotExist(t *testing.T) {
	id, err := ProjectID(filepath.Join(t.TempDir(), "unmounted", "micro-manager"))
	if err != nil {
		t.Fatalf("a missing path must still yield an id: %v", err)
	}
	if len(id) != ProjectIDLength {
		t.Errorf("id %q is the wrong shape", id)
	}
}

func TestProjectIDRejectsAnEmptyPath(t *testing.T) {
	if _, err := ProjectID(""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
	if _, err := CanonicalPath(""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// The canonical path is what the filesystem reported, NOT the normalised form.
// On a byte-exact filesystem a normalised path may not open the file it names.
func TestCanonicalPathIsNotNormalised(t *testing.T) {
	root := t.TempDir()
	name := "café" // NFD
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Skipf("cannot create a decomposed directory name: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := entries[0].Name()

	canon, err := CanonicalPath(filepath.Join(root, onDisk))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canon); err != nil {
		t.Errorf("the canonical path does not open: %v", err)
	}
	if filepath.Base(canon) != onDisk {
		t.Errorf("canonical path renamed the directory: %q became %q", onDisk, filepath.Base(canon))
	}
}

func TestDirectoryCarriesItsProjectID(t *testing.T) {
	dir := newDir(t, nil)
	d, err := mustOpen(t, dir).Directory()
	if err != nil {
		t.Fatal(err)
	}
	want, err := ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.ProjectID != want {
		t.Errorf("Directory().ProjectID = %q, want %q", d.ProjectID, want)
	}
}

// Discovery emits ids so /projects does not recompute one per row - including
// for a matched directory it could not read.
func TestDiscoveryCarriesProjectIDs(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good", "micro-manager")
	if err := os.MkdirAll(good, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"backlog.md": dirBacklog, "done.md": sampleDone, "working.01.md": idleSlot,
	} {
		if err := os.WriteFile(filepath.Join(good, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// An empty directory with a matching name: found, unreadable as a project.
	if err := os.MkdirAll(filepath.Join(root, "empty", "micro-manager"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 2 {
		t.Fatalf("want 2 directories, got %d", len(res.Directories))
	}
	for _, d := range res.Directories {
		want, err := ProjectID(d.Path)
		if err != nil {
			t.Fatal(err)
		}
		if d.ProjectID != want {
			t.Errorf("%s: ProjectID = %q, want %q", d.Path, d.ProjectID, want)
		}
	}
	if res.Directories[0].ProjectID == res.Directories[1].ProjectID {
		t.Error("two directories share one id")
	}
}
