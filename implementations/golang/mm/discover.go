package mm

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Directory discovery (spec-file-format.md Appendix B, spec-gui.md §9.5).
//
// A micro-manager directory is recognised BY NAME ALONE; contents are not
// inspected. That is deliberate: a checker, not a walker, decides whether a
// directory is well formed, so a broken directory is still found and still
// reported rather than vanishing from the list exactly when it needs attention.

// directoryNames are the six recognised names.
//
// Both mu codepoints are accepted because they are visually identical in nearly
// every font and are trivially confused when typed: U+00B5 MICRO SIGN, which
// writers should use, and U+03BC GREEK SMALL LETTER MU, which readers must
// accept anyway.
var directoryNames = []string{
	"micro-manager", ".micro-manager",
	"µmanager", ".µmanager",
	"μmanager", ".μmanager",
}

// IsDirectoryName reports whether a directory name is a conventional
// micro-manager name.
func IsDirectoryName(name string) bool {
	return slices.Contains(directoryNames, name)
}

// Discover walks the configured roots and returns every micro-manager directory
// below them.
//
// Roots must be ABSOLUTE and already expanded: `~` and environment variables are
// the front end's business, never the library's (spec-tools.md §2.2 rule 4).
//
// A root that does not exist or cannot be read is skipped and counted, not
// fatal. External drives come and go, and one missing root must not take out
// discovery of the others.
func Discover(opts DiscoveryOptions) DiscoveryResult {
	if opts.MaxResults == 0 {
		opts.MaxResults = 500
	}
	if opts.TimeoutMS == 0 {
		opts.TimeoutMS = 5000
	}

	w := &walker{
		opts:     opts,
		deadline: time.Now().Add(time.Duration(opts.TimeoutMS) * time.Millisecond),
		seen:     map[string]bool{},
		excludes: map[string]bool{},
	}
	for _, e := range opts.Excludes {
		w.excludes[e] = true
	}

	var found []string
	for _, root := range opts.Roots {
		if w.stop() {
			w.partial = true
			break
		}
		// A root that is itself a micro-manager directory matches, rather than
		// being walked into. `mm --find ~/code/project/micro-manager` should
		// find that directory, not nothing; the reference find.sh tests each
		// root the same way.
		if IsDirectoryName(filepath.Base(strings.TrimRight(root, string(filepath.Separator)))) {
			if fi, err := os.Stat(root); err == nil && fi.IsDir() {
				canon := canonical(root)
				if !w.seen[canon] {
					w.seen[canon] = true
					w.count++
					found = append(found, canon)
				}
				continue
			}
		}
		found = append(found, w.walk(root, 0, nil)...)
	}

	res := DiscoveryResult{Partial: w.partial, Skipped: w.skipped}
	for _, path := range found {
		d := describe(path)
		res.Directories = append(res.Directories, d)
	}
	// Ordering is by project name, then by path: a list that reorders itself
	// between scans is unusable as a menu.
	sort.SliceStable(res.Directories, func(i, j int) bool {
		a, b := res.Directories[i], res.Directories[j]
		if a.Project != b.Project {
			// A directory with no project name sorts last rather than first,
			// where an empty string would otherwise put it.
			if a.Project == "" || b.Project == "" {
				return b.Project == ""
			}
			return a.Project < b.Project
		}
		return a.Path < b.Path
	})
	return res
}

type walker struct {
	opts     DiscoveryOptions
	deadline time.Time
	excludes map[string]bool

	seen    map[string]bool // canonical paths already matched, for dedup
	skipped int
	partial bool
	count   int
}

// stop reports whether the walk has hit a bound. Both bounds mark the result
// partial: a silently truncated scan is indistinguishable from a missing
// project, which is the one outcome the caller cannot recover from.
func (w *walker) stop() bool {
	if w.count >= w.opts.MaxResults {
		w.partial = true
		return true
	}
	if time.Now().After(w.deadline) {
		w.partial = true
		return true
	}
	return false
}

// walk descends one directory. ancestors carries the chain above it, which is
// what a symlink cycle is detected against.
func (w *walker) walk(dir string, depth int, ancestors []os.FileInfo) []string {
	if w.stop() {
		return nil
	}
	if w.opts.MaxDepth > 0 && depth > w.opts.MaxDepth {
		return nil
	}

	// Cycle detection, needed only when symlinks are followed. A cycle means
	// arriving back at a directory already on the path down, so comparing
	// against the ancestors is enough - and os.SameFile is the portable way to
	// ask, where device and inode are not.
	if w.opts.FollowSymlinks {
		fi, err := os.Stat(dir)
		if err != nil {
			w.skipped++
			return nil
		}
		for _, a := range ancestors {
			if os.SameFile(a, fi) {
				return nil
			}
		}
		ancestors = append(append([]os.FileInfo{}, ancestors...), fi)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		w.skipped++
		return nil
	}

	var out []string
	for _, e := range entries {
		if w.stop() {
			return out
		}
		name := e.Name()
		if !w.isDir(dir, e) {
			continue
		}
		// includeHidden defaults to true, and this is why: two of the six
		// conventional names begin with a dot, so a walker that skips hidden
		// directories by default cannot find them and is non-conforming.
		if !w.opts.IncludeHidden && strings.HasPrefix(name, ".") && !IsDirectoryName(name) {
			continue
		}
		if w.excludes[name] {
			continue
		}

		full := filepath.Join(dir, name)
		if IsDirectoryName(name) {
			// Matched: record it and DO NOT DESCEND. A micro-manager directory
			// inside another is undefined, and pruning here keeps a deep tree
			// cheap to walk.
			canon := canonical(full)
			if !w.seen[canon] {
				w.seen[canon] = true
				w.count++
				out = append(out, canon)
			}
			continue
		}
		out = append(out, w.walk(full, depth+1, ancestors)...)
	}
	return out
}

// isDir reports whether an entry is a directory, resolving symlinks only when
// the caller asked for that.
func (w *walker) isDir(parent string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 || !w.opts.FollowSymlinks {
		return false
	}
	fi, err := os.Stat(filepath.Join(parent, e.Name()))
	return err == nil && fi.IsDir()
}

// canonical resolves a path for deduplication, so the same directory reached
// from two roots appears once. A path that cannot be resolved is returned as
// given rather than dropped - being listed twice is a smaller failure than not
// being listed at all.
func canonical(path string) string {
	// Absolute FIRST, then resolve. The other order is wrong for a relative
	// path: EvalSymlinks("micro-manager") has nothing to resolve, and the
	// working directory prepended afterwards may itself run through a symlink -
	// /var -> /private/var on macOS - so the same directory canonicalises two
	// ways depending on how the caller spelled it.
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	// The path does not exist yet, or a component is unreadable. Absolute is the
	// best available answer: discovery, recent and favorites all carry entries
	// for directories that are not present right now.
	return abs
}

// describe reads a found directory's summary.
//
// A directory that cannot be read still appears in the result, with only its
// path filled in: discovery matches on name, so reporting it and letting the
// checker explain what is wrong is more useful than hiding it.
func describe(path string) Directory {
	unreadable := func() Directory {
		// Still carries an id: spec-gui.md §3.1 addresses directories by id, and
		// a directory the UI cannot read is one the user most needs to open.
		id, _ := ProjectID(path)
		return Directory{Path: path, ProjectID: id}
	}
	s, err := Open(path)
	if err != nil {
		return unreadable()
	}
	d, err := s.Directory()
	if err != nil {
		return unreadable()
	}
	return d
}
