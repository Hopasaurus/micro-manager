package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteFileAtomicCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "backlog.md")

	if err := writeFileAtomic(p, []byte("first\n")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if got := readFileString(t, p); got != "first\n" {
		t.Errorf("got %q", got)
	}
	if err := writeFileAtomic(p, []byte("second\n")); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got := readFileString(t, p); got != "second\n" {
		t.Errorf("got %q", got)
	}
}

// A temp file left behind looks like data loss to a user reading the directory.
func TestWriteFileAtomicLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "done.md")
	if err := writeFileAtomic(p, []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "done.md" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory should hold only the target, got %v", names)
	}
}

func TestWriteFileAtomicFailureLeavesNoDebris(t *testing.T) {
	dir := t.TempDir()
	// A directory where the target path is itself a directory: the rename fails.
	p := filepath.Join(dir, "blocked")
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	err := writeFileAtomic(p, []byte("x\n"))
	if err == nil {
		t.Fatal("renaming over a directory should fail")
	}
	if !errors.Is(err, ErrIO) {
		t.Errorf("want ErrIO, got %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mm-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// CreateTemp makes 0600 files; silently tightening a file the user had made
// group-readable would be a surprising side effect of an unrelated edit.
func TestWriteFileAtomicPreservesMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "backlog.md")
	if err := os.WriteFile(p, []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(p, []byte("b\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode = %o, want 644", fi.Mode().Perm())
	}
}

func TestWriteSetEmptyIsNoOp(t *testing.T) {
	var ws writeSet
	if !ws.Empty() {
		t.Error("a fresh set should be empty")
	}
	if err := ws.Commit(); err != nil {
		t.Errorf("committing nothing should succeed: %v", err)
	}
}

func TestWriteSetCommitsInOrder(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "working.01.md")
	b := filepath.Join(dir, "backlog.md")
	writeString(t, b, "backlog before\n")

	var ws writeSet
	// The order --start uses: the copy is written before the original is
	// removed, so an interruption duplicates rather than destroys.
	ws.Add(a, []byte("working after\n"), stampOf(a))
	ws.Add(b, []byte("backlog after\n"), stampOf(b))

	if got := ws.Paths(); len(got) != 2 {
		t.Errorf("Paths() = %v", got)
	}
	if err := ws.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if readFileString(t, a) != "working after\n" || readFileString(t, b) != "backlog after\n" {
		t.Error("files not written")
	}
}

// §7 rule 5: the directory changed underneath us, so nothing is written.
func TestWriteSetDetectsConcurrentModification(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "backlog.md")
	writeString(t, p, "original\n")

	was := stampOf(p)

	// Somebody else edits the file after we read it.
	time.Sleep(10 * time.Millisecond)
	writeString(t, p, "edited by someone else\n")

	var ws writeSet
	ws.Add(p, []byte("our version\n"), was)

	err := ws.Commit()
	if err == nil {
		t.Fatal("a concurrent edit must be detected")
	}
	if !errors.Is(err, ErrConcurrent) {
		t.Errorf("want ErrConcurrent, got %v", err)
	}
	if !strings.Contains(err.Error(), "re-read before retrying") {
		t.Errorf("the message should name the remedy: %v", err)
	}
	// Nothing was written: the other edit survives intact.
	if got := readFileString(t, p); got != "edited by someone else\n" {
		t.Errorf("the conflicting write must not be applied, got %q", got)
	}
}

// The check runs before the FIRST write, so a conflict on the second file does
// not leave the first one already replaced.
func TestWriteSetAbortsBeforeAnyWrite(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "backlog.md")
	b := filepath.Join(dir, "done.md")
	writeString(t, a, "a original\n")
	writeString(t, b, "b original\n")

	wasA, wasB := stampOf(a), stampOf(b)
	time.Sleep(10 * time.Millisecond)
	writeString(t, b, "b changed elsewhere\n") // only the second file conflicts

	var ws writeSet
	ws.Add(a, []byte("a new\n"), wasA)
	ws.Add(b, []byte("b new\n"), wasB)

	if err := ws.Commit(); !errors.Is(err, ErrConcurrent) {
		t.Fatalf("want ErrConcurrent, got %v", err)
	}
	if got := readFileString(t, a); got != "a original\n" {
		t.Errorf("the first file was written despite the abort: %q", got)
	}
}

func TestWriteSetCreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "working.02.md")

	was := stampOf(p) // missing
	if !was.missing {
		t.Fatal("stamp of an absent file should be missing")
	}
	var ws writeSet
	ws.Add(p, []byte("new slot\n"), was)
	if err := ws.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if readFileString(t, p) != "new slot\n" {
		t.Error("file not created")
	}
}

// Creating a file that appeared underneath us is also a conflict: another
// process got there first, and overwriting would destroy its work.
func TestWriteSetDetectsFileAppearing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "working.02.md")
	was := stampOf(p) // missing

	writeString(t, p, "created by someone else\n")

	var ws writeSet
	ws.Add(p, []byte("ours\n"), was)
	if err := ws.Commit(); !errors.Is(err, ErrConcurrent) {
		t.Fatalf("want ErrConcurrent, got %v", err)
	}
	if got := readFileString(t, p); got != "created by someone else\n" {
		t.Errorf("the other file was clobbered: %q", got)
	}
}

func TestWriteSetDelete(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "working.03.md")
	writeString(t, p, "to be removed\n")

	var ws writeSet
	ws.Delete(p)
	if ws.Empty() {
		t.Error("a set with a deletion is not empty")
	}
	if err := ws.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("file not removed")
	}
	// Deleting something already gone is not an error: the end state is what
	// was asked for.
	var ws2 writeSet
	ws2.Delete(p)
	if err := ws2.Commit(); err != nil {
		t.Errorf("deleting an absent file should succeed: %v", err)
	}
}

func TestStampEquality(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	missing := stampOf(p)
	writeString(t, p, "x\n")
	present := stampOf(p)

	if missing.equal(present) || present.equal(missing) {
		t.Error("a missing file must not compare equal to a present one")
	}
	if !present.equal(stampOf(p)) {
		t.Error("an unchanged file should compare equal")
	}
	if !missing.equal(stamp{missing: true}) {
		t.Error("two missing stamps should compare equal")
	}
}

// ---------------------------------------------------------------------------

func writeString(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFileString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
