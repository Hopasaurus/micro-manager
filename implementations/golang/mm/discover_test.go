package mm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkProject creates a real directory at root/rel with the given project name.
func mkProject(t *testing.T, root, rel, project string) string {
	t.Helper()
	dir := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(dir, InitRequest{Project: project}, today); err != nil {
		t.Fatalf("init %s: %v", rel, err)
	}
	return dir
}

func discoveredPaths(res DiscoveryResult) []string {
	out := make([]string, 0, len(res.Directories))
	for _, d := range res.Directories {
		out = append(out, d.Path)
	}
	return out
}

func containsPath(paths []string, want string) bool {
	for _, p := range paths {
		if p == want || strings.HasSuffix(p, want) {
			return true
		}
	}
	return false
}

// All six conventional names must be found, dotted variants and both mu
// codepoints included. Two of the six begin with a dot, so a walker that skips
// hidden directories cannot find them and is non-conforming.
func TestDiscoverFindsEveryConventionalName(t *testing.T) {
	root := t.TempDir()
	names := []string{
		"a/micro-manager",
		"b/.micro-manager",
		"c/\u00b5manager",  // MICRO SIGN
		"d/.\u00b5manager", // MICRO SIGN, hidden
		"e/\u03bcmanager",  // GREEK SMALL LETTER MU
		"f/.\u03bcmanager", // GREEK SMALL LETTER MU, hidden
	}
	for i, n := range names {
		mkProject(t, root, n, string(rune('A'+i)))
	}

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != len(names) {
		t.Fatalf("found %d directories, want %d:\n%v",
			len(res.Directories), len(names), discoveredPaths(res))
	}
	if res.Partial {
		t.Error("result should not be partial")
	}
	for _, n := range names {
		if !containsPath(discoveredPaths(res), filepath.Join(root, n)) {
			t.Errorf("did not find %s", n)
		}
	}
}

// Ordering is by project name, then path: a list that reorders itself between
// scans is unusable as a menu.
func TestDiscoverOrdersByProjectThenPath(t *testing.T) {
	root := t.TempDir()
	mkProject(t, root, "z/micro-manager", "Alpha")
	mkProject(t, root, "a/micro-manager", "Zulu")
	mkProject(t, root, "m/micro-manager", "Alpha")

	res := Discover(DefaultDiscoveryOptions(root))
	var got []string
	for _, d := range res.Directories {
		got = append(got, d.Project+" "+filepath.Base(filepath.Dir(d.Path)))
	}
	want := []string{"Alpha m", "Alpha z", "Zulu a"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// A directory that merely shares the name is still reported - matching is on
// name alone, and it is the checker's job to say it is not a todo directory.
func TestDiscoverMatchesOnNameAlone(t *testing.T) {
	root := t.TempDir()
	impostor := filepath.Join(root, "src", "micro-manager")
	if err := os.MkdirAll(impostor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(impostor, "README.md"), []byte("a repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 1 {
		t.Fatalf("want the impostor reported, got %v", discoveredPaths(res))
	}
	if res.Directories[0].Project != "" {
		t.Errorf("project = %q, want empty", res.Directories[0].Project)
	}
}

func TestDiscoverDoesNotDescendIntoAMatch(t *testing.T) {
	root := t.TempDir()
	outer := mkProject(t, root, "p/micro-manager", "Outer")
	// A micro-manager directory inside another is undefined; pruning at the
	// match is also what keeps a deep tree cheap to walk.
	if _, _, err := Init(filepath.Join(outer, "micro-manager"), InitRequest{Project: "Inner"}, today); err != nil {
		t.Fatal(err)
	}

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 1 {
		t.Fatalf("want only the outer directory, got %v", discoveredPaths(res))
	}
	if res.Directories[0].Project != "Outer" {
		t.Errorf("found %q", res.Directories[0].Project)
	}
}

func TestDiscoverPrunesExcludes(t *testing.T) {
	root := t.TempDir()
	mkProject(t, root, "keep/micro-manager", "Kept")
	mkProject(t, root, "node_modules/pkg/micro-manager", "Vendored")
	mkProject(t, root, ".git/micro-manager", "Internal")

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 1 || res.Directories[0].Project != "Kept" {
		t.Errorf("excludes not pruned: %v", discoveredPaths(res))
	}
}

func TestDiscoverMaxDepth(t *testing.T) {
	root := t.TempDir()
	mkProject(t, root, "one/micro-manager", "Shallow")
	mkProject(t, root, "one/two/three/four/micro-manager", "Deep")

	opts := DefaultDiscoveryOptions(root)
	opts.MaxDepth = 2
	if res := Discover(opts); len(res.Directories) != 1 {
		t.Errorf("depth 2 should find only the shallow one, got %v", discoveredPaths(res))
	}

	opts.MaxDepth = 0 // unlimited
	if res := Discover(opts); len(res.Directories) != 2 {
		t.Errorf("unlimited depth should find both, got %v", discoveredPaths(res))
	}
}

// A truncated scan that does not say so is indistinguishable from a project
// that has gone missing.
func TestDiscoverMaxResultsMarksPartial(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		mkProject(t, root, n+"/micro-manager", n)
	}

	opts := DefaultDiscoveryOptions(root)
	opts.MaxResults = 2
	res := Discover(opts)
	if len(res.Directories) != 2 {
		t.Errorf("got %d directories, want 2", len(res.Directories))
	}
	if !res.Partial {
		t.Error("a truncated result must be marked partial")
	}
}

func TestDiscoverSkipsMissingRootsWithoutFailing(t *testing.T) {
	root := t.TempDir()
	mkProject(t, root, "here/micro-manager", "Here")

	res := Discover(DefaultDiscoveryOptions(filepath.Join(root, "nope"), root))
	if len(res.Directories) != 1 {
		t.Errorf("a missing root should not take out the others: %v", discoveredPaths(res))
	}
	if res.Skipped == 0 {
		t.Error("the unreadable root should have been counted")
	}
}

// The same directory reachable from two roots appears once.
func TestDiscoverDeduplicates(t *testing.T) {
	root := t.TempDir()
	mkProject(t, root, "p/micro-manager", "Once")

	res := Discover(DefaultDiscoveryOptions(root, root, filepath.Join(root, "p")))
	if len(res.Directories) != 1 {
		t.Errorf("want one entry, got %v", discoveredPaths(res))
	}
}

// Pointing discovery straight at a directory should find it, not walk into it
// and find nothing.
func TestDiscoverRootIsItselfAMatch(t *testing.T) {
	root := t.TempDir()
	dir := mkProject(t, root, "p/micro-manager", "Direct")

	res := Discover(DefaultDiscoveryOptions(dir))
	if len(res.Directories) != 1 || res.Directories[0].Project != "Direct" {
		t.Errorf("want the root itself, got %v", discoveredPaths(res))
	}
	// And it is not reported twice when both it and its parent are roots.
	res = Discover(DefaultDiscoveryOptions(dir, filepath.Join(root, "p")))
	if len(res.Directories) != 1 {
		t.Errorf("want one entry, got %v", discoveredPaths(res))
	}
}

func TestDiscoverSymlinks(t *testing.T) {
	root := t.TempDir()
	real := t.TempDir()
	mkProject(t, real, "p/micro-manager", "Linked")
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Off by default: a link is not a directory to walk into.
	if res := Discover(DefaultDiscoveryOptions(root)); len(res.Directories) != 0 {
		t.Errorf("symlinks should not be followed by default, got %v", discoveredPaths(res))
	}

	opts := DefaultDiscoveryOptions(root)
	opts.FollowSymlinks = true
	if res := Discover(opts); len(res.Directories) != 1 {
		t.Errorf("want the linked directory, got %v", discoveredPaths(res))
	}
}

// A self-referential link must terminate the walk, not exhaust the depth limit
// one level at a time.
func TestDiscoverSymlinkCycleTerminates(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "a"), filepath.Join(deep, "loop")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	mkProject(t, root, "a/micro-manager", "Cyclic")

	opts := DefaultDiscoveryOptions(root)
	opts.FollowSymlinks = true
	opts.MaxDepth = 0 // unlimited: only cycle detection can stop this walk
	res := Discover(opts)
	if len(res.Directories) != 1 {
		t.Errorf("want one directory, got %v", discoveredPaths(res))
	}
}

// The repository's own sample-data tree is a free corpus, and the one place the
// walker meets all four naming conventions as a person actually wrote them.
func TestDiscoverAgainstTheRepository(t *testing.T) {
	root := "../../../sample-data"
	if _, err := os.Stat(root); err != nil {
		t.Skipf("repository fixtures not present: %v", err)
	}
	res := Discover(DefaultDiscoveryOptions(mustAbs(t, root)))
	if len(res.Directories) != 5 {
		t.Errorf("found %d directories, want 5:\n%v", len(res.Directories), discoveredPaths(res))
	}
	for _, d := range res.Directories {
		if d.Project == "" {
			t.Errorf("%s has no project name", d.Path)
		}
		if d.WipLimit < 1 {
			t.Errorf("%s reports a wip limit of %d", d.Path, d.WipLimit)
		}
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
