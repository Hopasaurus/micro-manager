package mm

import (
	"os"
	"path/filepath"
	"testing"
)

// fingerprintOf opens a directory and fingerprints it, failing the test on any
// error - every caller here is asserting on the value, not on the error path.
func fingerprintOf(t *testing.T, dir string) Fingerprint {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	f, err := s.Fingerprint()
	if err != nil {
		t.Fatalf("fingerprint %s: %v", dir, err)
	}
	if f == "" {
		t.Fatalf("fingerprint %s: empty", dir)
	}
	return f
}

// touch rewrites a file with new content and a distinct mtime. Size alone is
// not enough: a same-length edit must still register.
func touch(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	bumpMtime(t, path)
}

// bumpMtime moves a file's mtime forward by a second. Filesystem timestamp
// granularity varies, and a test that writes twice inside one tick would
// otherwise fail on a coarse filesystem for no reason the code can fix.
func bumpMtime(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	later := fi.ModTime().Add(2e9) // 2s
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestFingerprintIsStableAndOpaque(t *testing.T) {
	dir := copyFixture(t, "clean-full")

	first := fingerprintOf(t, dir)
	second := fingerprintOf(t, dir)
	if first != second {
		t.Fatalf("fingerprint changed with no edit:\n  %s\n  %s", first, second)
	}
	if len(first.String()) != 64 {
		t.Errorf("want a 64-character hex digest, got %d characters: %s", len(first.String()), first)
	}
}

// Every file the format spec owns must move the value when it changes. The
// baseline is taken per subtest from that subtest's own copy: mtime is part of
// the input, so two copies of one fixture legitimately fingerprint differently.
func TestFingerprintChangesWithEveryOwnedFile(t *testing.T) {
	cases := []struct {
		name string
		edit func(t *testing.T, dir string)
	}{
		{"backlog edited", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "backlog.md")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			touch(t, p, string(data)+"\n")
		}},
		{"done edited", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "done.md")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			touch(t, p, string(data)+"\n")
		}},
		{"working slot edited", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "working.01.md")
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			touch(t, p, string(data)+"\n")
		}},
		{"slot file added", func(t *testing.T, dir string) {
			touch(t, filepath.Join(dir, "working.09.md"), "---\ndoc: working\n---\n")
		}},
		{"detail file added", func(t *testing.T, dir string) {
			touch(t, filepath.Join(dir, "details", "T-9999.md"), "---\ndoc: detail\n---\n")
		}},
		{"archive added", func(t *testing.T, dir string) {
			touch(t, filepath.Join(dir, "done-2025.md"), "---\ndoc: done\n---\n")
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := copyFixture(t, "clean-full")
			before := fingerprintOf(t, dir)
			c.edit(t, dir)
			if got := fingerprintOf(t, dir); got == before {
				t.Errorf("%s did not change the fingerprint", c.name)
			}
		})
	}
}

// A same-size edit is the case a size-only check would miss.
func TestFingerprintNoticesASameSizeEdit(t *testing.T) {
	dir := copyFixture(t, "clean-minimal")
	p := filepath.Join(dir, "backlog.md")

	before := fingerprintOf(t, dir)
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	touch(t, p, string(data)) // identical bytes, later mtime
	if fingerprintOf(t, dir) == before {
		t.Error("a rewrite with the same size did not change the fingerprint")
	}
}

// Deleting a file must be as visible as adding one: the listing is part of the
// input, not just the stat of each file in it.
func TestFingerprintNoticesADeletion(t *testing.T) {
	dir := copyFixture(t, "clean-multi-slot")
	before := fingerprintOf(t, dir)

	if err := os.Remove(filepath.Join(dir, "working.03.md")); err != nil {
		t.Fatal(err)
	}
	if fingerprintOf(t, dir) == before {
		t.Error("removing a slot file did not change the fingerprint")
	}
}

// spec-gui.md §8.2: theme.json and config.json are outside the file-format spec.
// A theme edit is not a data change and must not look like one.
func TestFingerprintIgnoresUnownedFiles(t *testing.T) {
	dir := copyFixture(t, "clean-full")
	before := fingerprintOf(t, dir)

	for _, name := range []string{"theme.json", "config.json", "structure.md", ".backlog.md.swp"} {
		touch(t, filepath.Join(dir, name), `{"schemaVersion":1}`)
		if got := fingerprintOf(t, dir); got != before {
			t.Errorf("%s moved the fingerprint:\n  %s\n  %s", name, before, got)
		}
	}

	// details/_template.md is a template, exempt from I9 and not an item's data.
	touch(t, filepath.Join(dir, "details", "_scratch.md"), "notes\n")
	if got := fingerprintOf(t, dir); got != before {
		t.Errorf("a details/ template moved the fingerprint:\n  %s\n  %s", before, got)
	}
}

// spec-tools.md §2.4 rule 1: it MUST NOT require reading or parsing file bodies.
// An unreadable file still stats, so the fingerprint must still come back.
func TestFingerprintDoesNotReadBodies(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything; the permission bit proves nothing")
	}
	dir := copyFixture(t, "clean-minimal")
	p := filepath.Join(dir, "backlog.md")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o644) })

	if _, err := os.ReadFile(p); err == nil {
		t.Skip("the file is still readable; the filesystem ignores mode bits")
	}
	fingerprintOf(t, dir) // fails the test if it errors or comes back empty
}

// A directory that already violates its invariants must still fingerprint:
// the UI polls this before it knows whether the directory is clean.
func TestFingerprintOfABrokenDirectory(t *testing.T) {
	for _, f := range fixtures(t) {
		if f.Clean() {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			fingerprintOf(t, copyFixture(t, f.Name))
		})
	}
}

func TestFingerprintOfAMissingDirectory(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(s.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Fingerprint(); err == nil {
		t.Error("want an error for a directory that no longer exists, got nil")
	}
}

func TestDoneArchiveNames(t *testing.T) {
	cases := map[string]bool{
		"done-2025.md":  true,
		"done-1999.md":  true,
		"done.md":       false,
		"done-25.md":    false,
		"done-20255.md": false,
		"done-two5.md":  false,
		"done-2025.txt": false,
		"done-.md":      false,
	}
	for name, want := range cases {
		if got := isDoneArchiveName(name); got != want {
			t.Errorf("isDoneArchiveName(%q) = %v, want %v", name, got, want)
		}
	}
}
