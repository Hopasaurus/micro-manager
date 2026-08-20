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

// The emptiness test (spec-file-format.md Appendix B). A directory that merely
// shares the name — a source repository, a skill package — holds neither
// backlog.md nor done.md, and discovery skips it silently rather than making
// every checker run report it forever.
func TestDiscoverSkipsANameWithNoBoardFiles(t *testing.T) {
	root := t.TempDir()
	impostor := filepath.Join(root, "src", "micro-manager")
	if err := os.MkdirAll(impostor, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(impostor, "README.md"), []byte("a repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 0 {
		t.Fatalf("want the impostor skipped, got %v", discoveredPaths(res))
	}
	// …and the walk still does not descend into it: a match is pruned whatever
	// the emptiness test then says, or a source tree of that name becomes a
	// tree to walk rather than one to skip.
	if err := os.MkdirAll(filepath.Join(impostor, "nested", "micro-manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Init(filepath.Join(impostor, "nested", "micro-manager"),
		InitRequest{Project: "Nested"}, today); err != nil {
		t.Fatal(err)
	}
	res = Discover(DefaultDiscoveryOptions(root))
	if len(res.Directories) != 0 {
		t.Fatalf("a pruned match must not be walked into, got %v", discoveredPaths(res))
	}
}

// EITHER file, not both: a board that has lost one is still a board, and
// hiding it would make a half-deleted project vanish from the checker at the
// moment it most needs attention.
func TestDiscoverFindsABoardMissingOneFile(t *testing.T) {
	for _, keep := range []string{"backlog.md", "done.md"} {
		t.Run(keep, func(t *testing.T) {
			root := t.TempDir()
			dir := mkProject(t, root, "p/micro-manager", "Half")
			drop := "done.md"
			if keep == "done.md" {
				drop = "backlog.md"
			}
			if err := os.Remove(filepath.Join(dir, drop)); err != nil {
				t.Fatal(err)
			}

			res := Discover(DefaultDiscoveryOptions(root))
			if len(res.Directories) != 1 {
				t.Fatalf("a board with only %s must still be found, got %v",
					keep, discoveredPaths(res))
			}
		})
	}
}

// A root that IS a match faces the same test: naming a source tree's
// micro-manager directory must report nothing, not invent a board.
func TestDiscoverAppliesTheTestToARootThatMatches(t *testing.T) {
	root := t.TempDir()
	impostor := filepath.Join(root, "micro-manager")
	if err := os.MkdirAll(impostor, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(Discover(DefaultDiscoveryOptions(impostor)).Directories) != 0 {
		t.Fatal("an empty name-match given as the root must not be reported")
	}

	if _, _, err := Init(impostor, InitRequest{Project: "Real"}, today); err != nil {
		t.Fatal(err)
	}
	res := Discover(DefaultDiscoveryOptions(impostor))
	if len(res.Directories) != 1 || res.Directories[0].Project != "Real" {
		t.Fatalf("a real board given as the root must be reported, got %v", discoveredPaths(res))
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
	// 7 since legacy-v1 joined the corpus (T-0229): the frozen pre-migration
	// snapshot of this repository's own board, kept to regression-test the
	// 1->2 step against real data (notes/add-columns.md §7.1, decision 21).
	if len(res.Directories) != 7 {
		t.Errorf("found %d directories, want 7:\n%v", len(res.Directories), discoveredPaths(res))
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

// One parent, one board (spec-file-format.md Appendix B, T-0194).
//
// The pair that actually happens is micro-manager beside .micro-manager: a
// rename that copied instead of moving, or two tools disagreeing about which
// spelling to create. Both start at T-0001, so the same id means two different
// items — which is why it is reported rather than tolerated.
func TestCollidingSiblings(t *testing.T) {
	root := t.TempDir()
	visible := mkProject(t, root, "proj/micro-manager", "Visible")
	hidden := mkProject(t, root, "proj/.micro-manager", "Hidden")

	if got := CollidingSiblings(visible); len(got) != 1 || got[0] != ".micro-manager" {
		t.Errorf("CollidingSiblings(visible) = %v, want [.micro-manager]", got)
	}
	// Symmetric: checking either board has to report the other, because a
	// person working in one may never run a scan that sees both.
	if got := CollidingSiblings(hidden); len(got) != 1 || got[0] != "micro-manager" {
		t.Errorf("CollidingSiblings(hidden) = %v, want [micro-manager]", got)
	}
}

func TestCollidingSiblingsIsQuietWhenThereIsNoCollision(t *testing.T) {
	root := t.TempDir()
	alone := mkProject(t, root, "solo/micro-manager", "Alone")
	if got := CollidingSiblings(alone); len(got) != 0 {
		t.Errorf("a board on its own has no siblings, got %v", got)
	}

	// A board in a DIFFERENT parent is not a collision, however close.
	mkProject(t, root, "other/micro-manager", "Other")
	if got := CollidingSiblings(alone); len(got) != 0 {
		t.Errorf("a board in another directory is not a sibling, got %v", got)
	}

	// A name-matching sibling that is not a board is the emptiness test's
	// business, not this one's: reporting an empty directory as a rival board
	// would send someone to fix the wrong thing.
	if err := os.MkdirAll(filepath.Join(root, "solo", ".micro-manager"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := CollidingSiblings(alone); len(got) != 0 {
		t.Errorf("an empty name-match is not a rival board, got %v", got)
	}
	// …until it becomes one.
	mkProject(t, root, "solo/.micro-manager", "Hidden")
	if got := CollidingSiblings(alone); len(got) != 1 {
		t.Errorf("want the sibling once it holds a board, got %v", got)
	}
}

// The names are compared as strings against the parent's entries, never probed
// as paths: a normalization-insensitive filesystem (APFS) resolves µmanager
// (U+00B5) and μmanager (U+03BC) to the same directory, so probing would report
// a rival that does not exist. This test would pass either way on Linux, and
// fails on macOS if the implementation ever goes back to probing.
func TestCollidingSiblingsDoesNotInventAUnicodeTwin(t *testing.T) {
	root := t.TempDir()
	board := mkProject(t, root, "sym/µmanager", "Micro Sign")
	if got := CollidingSiblings(board); len(got) != 0 {
		t.Errorf("the same directory under another spelling is not a sibling, got %v", got)
	}
}

func TestCollidingSiblingsSurvivesAnUnreadableParent(t *testing.T) {
	// "I cannot tell" is a no: this answers a hygiene question, not one that
	// decides whether to write.
	if got := CollidingSiblings(filepath.Join(t.TempDir(), "nothing", "micro-manager")); got != nil {
		t.Errorf("want nil for a parent that does not exist, got %v", got)
	}
	if got := CollidingSiblings(string(filepath.Separator)); got != nil {
		t.Errorf("the filesystem root has no parent to scan, got %v", got)
	}
}
