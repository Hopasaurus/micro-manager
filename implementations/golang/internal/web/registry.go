package web

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The project registry (spec-gui.md §3.1, §5.3, §9.5, §10).
//
// Every route under /p/:projectId has to turn twelve hex characters into an open
// store. Nothing else in the service holds a path-to-store mapping, and nothing
// anywhere accepts a filesystem path in a URI position - a path arrives only
// through configuration, the recent and favorites lists, or an explicit open.
//
// The registry also owns the discovery cache, because §9.5 is explicit that the
// fingerprint poll detects change WITHIN a known project and never a new project
// appearing. Discovery is therefore cached with an explicit rescan rather than
// re-walked per request.

// registry resolves project ids and caches what discovery found.
type registry struct {
	mu sync.RWMutex

	configHome string
	startDir   string
	explicit   []string // directories named on the command line
	scan       mm.ScanConfig

	stores map[string]*mm.Store    // projectId -> open store
	known  map[string]mm.Directory // projectId -> what we know about it

	result    mm.DiscoveryResult
	scannedAt mm.Timestamp
	scanned   bool

	// now is the clock, injectable so a test can assert on scannedAt.
	now func() time.Time
}

func newRegistry(opts Options) *registry {
	return &registry{
		configHome: opts.ConfigHome,
		startDir:   opts.StartDir,
		explicit:   append([]string(nil), opts.Dirs...),
		scan:       opts.Config.Scan,
		stores:     map[string]*mm.Store{},
		known:      map[string]mm.Directory{},
		now:        time.Now,
	}
}

// roots is the list to scan.
//
// When scan.roots is empty the front end falls back to the directory it was
// started in, as a single root, and surfaces a prompt to configure roots. It
// MUST NOT default to scanning $HOME or / (spec-gui.md §9.5).
func (r *registry) roots() []string {
	if len(r.scan.Roots) > 0 {
		return append([]string(nil), r.scan.Roots...)
	}
	if r.startDir != "" {
		return []string{r.startDir}
	}
	return nil
}

// needsRoots reports whether the UI should prompt for scan roots.
func (r *registry) needsRoots() bool { return len(r.scan.Roots) == 0 }

// rootStatus is one configured scan root and whether it is there.
//
// A root that does not exist or is unreadable MUST be reported in settings and
// skipped, not treated as fatal (§9.5 rule 3). External drives come and go.
type rootStatus struct {
	Path    string
	Missing bool
}

// discoveryView is what the projects list, the switcher and the settings view
// all render from.
type discoveryView struct {
	Directories []mm.Directory
	Groups      []discoveryGroup
	Roots       []rootStatus

	// Partial is set when the walk hit maxResults or timeoutMs. The UI MUST say
	// so visibly: a truncated scan is indistinguishable from a missing project
	// otherwise (§5.3).
	Partial    bool
	Skipped    int
	ScannedAt  mm.Timestamp
	NeedsRoots bool
}

// discoveryGroup is one scan root and the projects under it, which is how §5.3
// renders /projects.
type discoveryGroup struct {
	Root        string
	Missing     bool
	Directories []mm.Directory
}

// discovery returns the cached walk, running one the first time.
func (r *registry) discovery() discoveryView {
	r.mu.RLock()
	scanned := r.scanned
	r.mu.RUnlock()
	if !scanned {
		return r.rescan()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.view()
}

// rescan re-runs discovery. It is what POST /api/v1/scan, the projects-rescan
// button and scan.rescanOnFocus all reach.
func (r *registry) rescan() discoveryView {
	r.rescanRaw()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.view()
}

// rescanRaw re-runs discovery and returns the raw result plus the timestamp it
// was taken at, without building the render-ready view. It is what the JSON
// API calls; rescan builds on it so the two front ends cannot disagree about
// what a scan found.
func (r *registry) rescanRaw() (mm.DiscoveryResult, mm.Timestamp) {
	roots := r.roots()
	opts := r.scan.DiscoveryOptions()
	opts.Roots = roots

	result := mm.Discover(opts)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.result = result
	r.scannedAt = mm.NewTimestamp(r.now())
	r.scanned = true
	for _, d := range result.Directories {
		r.remember(d)
	}
	// Directories named on the command line or held in the lists are known
	// whether or not a scan root reaches them.
	for _, path := range r.explicit {
		r.rememberPath(path)
	}
	return r.result, r.scannedAt
}

// rawResult returns the cached walk's raw result and its timestamp, running the
// first walk of the process on demand.
func (r *registry) rawResult() (mm.DiscoveryResult, mm.Timestamp) {
	r.mu.RLock()
	scanned := r.scanned
	result, at := r.result, r.scannedAt
	r.mu.RUnlock()
	if !scanned {
		return r.rescanRaw()
	}
	return result, at
}

// view builds the render-ready form. The caller holds at least a read lock.
func (r *registry) view() discoveryView {
	v := discoveryView{
		Directories: append([]mm.Directory(nil), r.result.Directories...),
		Partial:     r.result.Partial,
		Skipped:     r.result.Skipped,
		ScannedAt:   r.scannedAt,
		NeedsRoots:  r.needsRoots(),
	}

	roots := r.roots()
	for _, root := range roots {
		v.Roots = append(v.Roots, rootStatus{Path: root, Missing: !isDir(root)})
	}

	// Group by the root each directory sits under, longest match first so a root
	// nested inside another claims its own projects.
	sorted := append([]string(nil), roots...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })

	groups := map[string][]mm.Directory{}
	var loose []mm.Directory
	for _, d := range v.Directories {
		root := ""
		for _, candidate := range sorted {
			if under(d.Path, candidate) {
				root = candidate
				break
			}
		}
		if root == "" {
			loose = append(loose, d)
			continue
		}
		groups[root] = append(groups[root], d)
	}

	for _, root := range roots {
		v.Groups = append(v.Groups, discoveryGroup{
			Root:        root,
			Missing:     !isDir(root),
			Directories: groups[root],
		})
	}
	if len(loose) > 0 {
		// A project reached through favorites or --dir but outside every scan
		// root still has to appear somewhere. §5.3 groups by root and has no
		// place for it, so it gets a group of its own rather than vanishing.
		v.Groups = append(v.Groups, discoveryGroup{Root: "", Directories: loose})
	}
	return v
}

// resolve turns a project id into an open store.
//
// An unknown id is ErrNotFound, which the handler renders as the not-found view
// with HTTP 404 - never a redirect to / (§4.1 rule 4).
func (r *registry) resolve(projectID string) (*mm.Store, error) {
	r.mu.RLock()
	store, ok := r.stores[projectID]
	scanned := r.scanned
	r.mu.RUnlock()
	if ok {
		return store, nil
	}

	if !scanned {
		// First request of the process. Walking here rather than at startup
		// keeps a slow filesystem out of the startup path.
		r.rescan()
		r.mu.RLock()
		store, ok = r.stores[projectID]
		r.mu.RUnlock()
		if ok {
			return store, nil
		}
	}

	r.mu.RLock()
	d, known := r.known[projectID]
	r.mu.RUnlock()
	if !known {
		return nil, fmt.Errorf("%w: no project %s", mm.ErrNotFound, projectID)
	}
	// Known but not open: the directory was listed by a stale scan or by a list
	// file and may have gone away since.
	return r.open(d.Path)
}

// directory returns what is known about a project without opening it, which is
// what a list of projects on an unmounted drive needs.
func (r *registry) directory(projectID string) (mm.Directory, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.known[projectID]
	return d, ok
}

// add registers a directory by path, which is how an explicit open and the
// recent and favorites lists reach the registry.
func (r *registry) add(path string) (mm.Directory, error) {
	store, err := r.open(path)
	if err != nil {
		return mm.Directory{}, err
	}
	return store.Directory()
}

// open opens a directory and caches the store.
//
// mm.Store is safe for concurrent use and opening is cheap - it stats, it does
// not read - so stores are kept for the life of the process. A single-user tool
// does not need eviction, and a store that stayed open on a directory that
// vanished simply fails its next operation, which is the right answer anyway.
func (r *registry) open(path string) (*mm.Store, error) {
	store, err := mm.Open(path)
	if err != nil {
		return nil, err
	}
	d, err := store.Directory()
	if err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[d.ProjectID] = store
	r.known[d.ProjectID] = d
	return store, nil
}

// remember records a directory discovery found. The caller holds the write lock.
func (r *registry) remember(d mm.Directory) {
	if d.ProjectID == "" {
		return
	}
	r.known[d.ProjectID] = d
	if _, ok := r.stores[d.ProjectID]; !ok {
		if store, err := mm.Open(d.Path); err == nil {
			r.stores[d.ProjectID] = store
		}
	}
}

// rememberPath records a directory by path. The caller holds the write lock.
func (r *registry) rememberPath(path string) {
	store, err := mm.Open(path)
	if err != nil {
		// A path that is gone is still worth knowing about: §10 rule 5 renders
		// it as missing rather than dropping it. The id is derivable without
		// the directory being there.
		if id, idErr := mm.ProjectID(path); idErr == nil {
			if _, known := r.known[id]; !known {
				r.known[id] = mm.Directory{Path: path, ProjectID: id}
			}
		}
		return
	}
	d, err := store.Directory()
	if err != nil {
		return
	}
	r.stores[d.ProjectID] = store
	r.known[d.ProjectID] = d

	// Discovery may not have reached it - an explicit --dir outside every root -
	// so it belongs in the listing too.
	for _, existing := range r.result.Directories {
		if existing.ProjectID == d.ProjectID {
			return
		}
	}
	r.result.Directories = append(r.result.Directories, d)
}

// touch records that a project was opened, moving it to the front of the recent
// list (spec-gui.md §10 rule 1).
//
// It re-reads the list before writing, so a second front end running at the same
// time loses one edit rather than the whole file.
func (r *registry) touch(d mm.Directory, themeName string, maxStored int) error {
	if r.configHome == "" {
		return nil // no config home, nowhere to write; not an error
	}
	paths := mm.NewSystemPaths(r.configHome)
	now := mm.NewTimestamp(r.now())
	return mm.UpdateProjectList(paths.Recent, mm.ListRecent, func(l *mm.ProjectList) error {
		l.Touch(mm.EntryFor(d, themeName), now, maxStored)
		return nil
	})
}

// lists loads the recent and favorites files, and registers everything in them
// so that a favorite outside every scan root is still resolvable by id.
func (r *registry) lists() (recent, favorites *mm.ProjectList, err error) {
	if r.configHome == "" {
		return &mm.ProjectList{Kind: mm.ListRecent}, &mm.ProjectList{Kind: mm.ListFavorites}, nil
	}
	paths := mm.NewSystemPaths(r.configHome)
	recent, err = mm.LoadProjectList(paths.Recent, mm.ListRecent)
	if err != nil {
		return nil, nil, err
	}
	favorites, err = mm.LoadProjectList(paths.Favorites, mm.ListFavorites)
	if err != nil {
		return nil, nil, err
	}

	r.mu.Lock()
	for _, l := range []*mm.ProjectList{recent, favorites} {
		for _, e := range l.Entries {
			if _, known := r.known[e.ProjectID]; !known {
				r.rememberPath(e.Path)
			}
		}
	}
	r.mu.Unlock()
	return recent, favorites, nil
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// under reports whether path sits inside root.
//
// Both sides are canonicalised first. Discovery returns resolved paths - on
// macOS /var/folders/... comes back as /private/var/folders/... - while a
// configured root is whatever the user typed, so comparing them raw files every
// project under "no root at all".
func under(path, root string) bool {
	if root == "" {
		return false
	}
	canonRoot, err := mm.CanonicalPath(root)
	if err != nil {
		return false
	}
	canonPath, err := mm.CanonicalPath(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(canonRoot, canonPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
