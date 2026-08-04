package web

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hopasaurus/micro-manager/mm"
)

// newTestRegistry builds a registry over copies of the named fixtures under one
// scan root, plus a config home for the recent and favorites files.
func newTestRegistry(t *testing.T, fixtures ...string) (*registry, string) {
	t.Helper()
	root := t.TempDir()
	dirs := map[string]string{}
	for _, name := range fixtures {
		dirs[name] = copyFixtureTo(t, name, filepath.Join(root, name))
	}
	t.Cleanup(func() { delete(fixtureDirs, t.Name()) })
	fixtureDirs[t.Name()] = dirs

	opts := Options{
		ConfigHome: t.TempDir(),
		StartDir:   root,
		Config:     mm.DefaultConfig(),
		Logger:     newTestLogger(t),
	}
	opts.Config.Scan.Roots = []string{root}

	r := newRegistry(opts)
	r.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	return r, root
}

// fixtureDirs maps a test name to the micro-manager directories that test's
// fixtures were installed in, so a test can name a fixture rather than rebuild
// the path convention.
var fixtureDirs = map[string]map[string]string{}

func fixtureDir(t *testing.T, name string) string {
	t.Helper()
	dir, ok := fixtureDirs[t.Name()][name]
	if !ok {
		t.Fatalf("fixture %s was not installed for this test", name)
	}
	return dir
}

func projectIDOfDir(t *testing.T, dir string) string {
	t.Helper()
	id, err := mm.ProjectID(dir)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// spec-gui.md §3.1: a directory is addressed by its derived id, never by a path.
func TestRegistryResolvesByProjectID(t *testing.T) {
	r, _ := newTestRegistry(t, "clean-full", "clean-minimal")

	id := projectIDOfDir(t, fixtureDir(t, "clean-full"))
	store, err := r.resolve(id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	d, err := store.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.ProjectID != id {
		t.Errorf("resolved the wrong project: %s", d.ProjectID)
	}

	// The same id resolves to the same store, since opening twice would mean two
	// mutexes over one directory.
	again, err := r.resolve(id)
	if err != nil {
		t.Fatal(err)
	}
	if again != store {
		t.Error("resolve returned a second store for one project")
	}
}

// §4.1 rule 4: an unknown projectId renders the not-found view with 404, never a
// redirect. The registry's part of that is an ErrNotFound the handler can map.
func TestRegistryUnknownProjectID(t *testing.T) {
	r, _ := newTestRegistry(t, "clean-full")

	if _, err := r.resolve("ffffffffffff"); !errors.Is(err, mm.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// §9.5: results carry scannedAt and the UI must show it; a partial walk must be
// visible; roots that do not resolve are reported and skipped, not fatal.
func TestDiscoveryView(t *testing.T) {
	r, root := newTestRegistry(t, "clean-full", "clean-minimal", "clean-multi-slot")
	gone := filepath.Join(t.TempDir(), "unmounted")
	r.scan.Roots = append(r.scan.Roots, gone)

	v := r.discovery()

	if len(v.Directories) != 3 {
		t.Errorf("found %d directories, want 3", len(v.Directories))
	}
	if v.ScannedAt.IsZero() {
		t.Error("the view carries no scannedAt, so the UI cannot show it")
	}
	if v.ScannedAt.String() != "2026-07-30T12:00:00Z" {
		t.Errorf("scannedAt = %s", v.ScannedAt)
	}
	if v.NeedsRoots {
		t.Error("roots are configured; the prompt should be off")
	}

	var missing, present int
	for _, rs := range v.Roots {
		if rs.Missing {
			missing++
		} else {
			present++
		}
	}
	if missing != 1 || present != 1 {
		t.Errorf("roots = %+v, want one present and one missing", v.Roots)
	}

	// Grouped by scan root, which is how §5.3 renders /projects.
	var grouped int
	for _, g := range v.Groups {
		if g.Root == root {
			grouped = len(g.Directories)
		}
	}
	if grouped != 3 {
		t.Errorf("the root group holds %d directories, want 3", grouped)
	}
}

// §9.5: the fingerprint poll never notices a NEW project, so a rescan must be
// available on demand.
func TestRescanFindsANewProject(t *testing.T) {
	r, root := newTestRegistry(t, "clean-full")

	if got := len(r.discovery().Directories); got != 1 {
		t.Fatalf("want 1 directory, got %d", got)
	}

	copyFixtureTo(t, "clean-minimal", filepath.Join(root, "appeared"))

	// The cache does not notice by itself - that is the point of the item.
	if got := len(r.discovery().Directories); got != 1 {
		t.Errorf("the cached view changed without a rescan: %d", got)
	}
	if got := len(r.rescan().Directories); got != 2 {
		t.Errorf("after a rescan, want 2 directories, got %d", got)
	}
}

// §9.5: with no scan.roots the front end falls back to the directory it was
// started in and prompts. It MUST NOT default to $HOME or /.
func TestEmptyScanRootsFallBackToTheStartDirectory(t *testing.T) {
	root := t.TempDir()
	copyFixtureTo(t, "clean-full", filepath.Join(root, "here"))

	r := newRegistry(Options{
		StartDir: root,
		Config:   mm.DefaultConfig(), // no scan.roots
		Logger:   newTestLogger(t),
	})

	v := r.discovery()
	if !v.NeedsRoots {
		t.Error("with no configured roots the UI must be told to prompt")
	}
	if len(v.Roots) != 1 || v.Roots[0].Path != root {
		t.Errorf("roots = %+v, want the start directory alone", v.Roots)
	}
	if len(v.Directories) != 1 {
		t.Errorf("found %d directories under the start directory", len(v.Directories))
	}

	home, _ := os.UserHomeDir()
	for _, rs := range v.Roots {
		if rs.Path == home || rs.Path == "/" {
			t.Errorf("the fallback root is %q", rs.Path)
		}
	}
}

// A directory named on the command line is resolvable even when no scan root
// reaches it, and lands in a group of its own rather than vanishing from
// /projects.
func TestExplicitDirectoryOutsideEveryRoot(t *testing.T) {
	root := t.TempDir()
	outside := copyFixtureTo(t, "clean-full", filepath.Join(t.TempDir(), "elsewhere"))

	opts := Options{
		StartDir: root,
		Dirs:     []string{outside},
		Config:   mm.DefaultConfig(),
		Logger:   newTestLogger(t),
	}
	opts.Config.Scan.Roots = []string{root}
	r := newRegistry(opts)

	id := projectIDOfDir(t, outside)
	if _, err := r.resolve(id); err != nil {
		t.Fatalf("a --dir project did not resolve: %v", err)
	}

	v := r.discovery()
	var found bool
	for _, g := range v.Groups {
		for _, d := range g.Directories {
			if d.ProjectID == id {
				found = true
				if g.Root != "" {
					t.Errorf("a project outside every root was filed under %q", g.Root)
				}
			}
		}
	}
	if !found {
		t.Error("a --dir project outside every scan root is missing from the listing")
	}
}

// §10 rule 1: opening a project moves it to the front of recent, updating
// lastOpened. An entry already present is moved, never duplicated.
func TestOpeningAProjectUpdatesRecent(t *testing.T) {
	r, _ := newTestRegistry(t, "clean-full", "clean-minimal")

	full, err := r.add(fixtureDir(t, "clean-full"))
	if err != nil {
		t.Fatal(err)
	}
	minimal, err := r.add(fixtureDir(t, "clean-minimal"))
	if err != nil {
		t.Fatal(err)
	}

	if err := r.touch(full, "Theme A", 100); err != nil {
		t.Fatal(err)
	}
	if err := r.touch(minimal, "", 100); err != nil {
		t.Fatal(err)
	}
	if err := r.touch(full, "", 100); err != nil {
		t.Fatal(err)
	}

	recent, _, err := r.lists()
	if err != nil {
		t.Fatal(err)
	}
	if len(recent.Entries) != 2 {
		t.Fatalf("recent holds %d entries, want 2", len(recent.Entries))
	}
	if recent.Entries[0].ProjectID != full.ProjectID {
		t.Error("the just-opened project is not at the front")
	}
	if recent.Entries[0].ThemeName != "Theme A" {
		t.Errorf("re-opening dropped the theme name: %q", recent.Entries[0].ThemeName)
	}
	if recent.Entries[0].LastOpened.String() != "2026-07-30T12:00:00Z" {
		t.Errorf("lastOpened = %s", recent.Entries[0].LastOpened)
	}
}

// §10 rule 5: a favorite whose path no longer resolves is still addressable -
// it is rendered as missing, not dropped.
func TestFavoriteOnAMissingPathStaysResolvable(t *testing.T) {
	r, _ := newTestRegistry(t, "clean-full")
	gone := filepath.Join(t.TempDir(), "unmounted", "micro-manager")

	paths := mm.NewSystemPaths(r.configHome)
	if err := mm.UpdateProjectList(paths.Favorites, mm.ListFavorites, func(l *mm.ProjectList) error {
		l.Add(mm.ListEntry{Path: gone, Name: "On a drive that is not here"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	_, favorites, err := r.lists()
	if err != nil {
		t.Fatal(err)
	}
	if len(favorites.Entries) != 1 {
		t.Fatalf("the favorite was dropped: %+v", favorites.Entries)
	}
	if !favorites.Entries[0].Missing() {
		t.Error("the entry should report Missing")
	}

	id := projectIDOfDir(t, gone)
	if d, known := r.directory(id); !known || d.Path != gone {
		t.Errorf("a favorite that is not mounted is not addressable: %+v", d)
	}
	// Resolving it fails, which is the honest answer - but the id is known, so
	// the UI can render the row and offer explicit removal.
	if _, err := r.resolve(id); err == nil {
		t.Error("resolving a directory that is not there should fail")
	}
}

// Discovery must not descend into a matched directory (spec-tools.md §4), so a
// micro-manager directory inside another is not two projects.
func TestDiscoveryDoesNotDescendIntoAMatch(t *testing.T) {
	r, _ := newTestRegistry(t, "clean-full")
	nested := filepath.Join(fixtureDir(t, "clean-full"), "details")
	copyFixtureTo(t, "clean-minimal", nested)

	v := r.rescan()
	for _, d := range v.Directories {
		if d.Path == filepath.Join(nested, "micro-manager") {
			t.Error("discovery descended into a directory it had already matched")
		}
	}
}
