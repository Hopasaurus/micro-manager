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
// A micro-manager directory is recognised by its NAME, and then by one probe of
// its contents: the emptiness test. Everything else about whether it is well
// formed is a checker's business, not a walker's — a broken directory is still
// found and still reported rather than vanishing from the list exactly when it
// needs attention.
//
// The one probe is the difference between "never was a board" and "is a broken
// board", and the whole rule turns on it:
//
//	neither backlog.md nor done.md   not a board. Skipped silently: a source
//	                                 tree that shares the name would otherwise
//	                                 be permanent noise in front of the real
//	                                 problems, and nobody will ever "fix" it.
//	exactly one of the two           a board that LOST a file. Found, and left
//	                                 for the checker to report — skipping this
//	                                 would hide a half-deleted project.
//
// The skip is DISCOVERY's alone. A path the user named explicitly is never
// silently ignored; the front end reports that it is not a micro-manager
// directory (spec-tools.md §4 step 1).

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

// IsBoardDirectory applies the emptiness test of spec-file-format.md Appendix B:
// a directory is a board when it holds backlog.md or done.md.
//
// EITHER, not both. One of the two missing is a board that lost a file, which a
// checker must be able to see; only the absence of BOTH means there was never a
// board here.
//
// The name is not re-checked. Callers reach this either from the walker, which
// has already matched the name, or from a front end resolving a path the user
// named — and in that second case the name is the user's business, not a rule
// to enforce twice.
func IsBoardDirectory(path string) bool {
	for _, name := range []string{"backlog.md", "done.md"} {
		if fi, err := os.Stat(filepath.Join(path, name)); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// CollidingSiblings returns the other micro-manager directories sitting beside
// this one, by name, sorted (spec-file-format.md Appendix B).
//
// One parent holds at most one board. `micro-manager` beside `.micro-manager`
// is the pair that actually happens — a rename that copied instead of moving,
// or two tools disagreeing about which spelling to create — and it is a
// mistake because NOTHING JOINS THEM: both start at T-0001, so the same id
// means two different items, and a report over the project covers one of them.
//
// This is deliberately not part of Validate. I1-I10 are properties of one
// directory's files, and a mutation validating before it commits must not
// depend on what sits beside the board it is writing. The finding belongs to
// the tools that already look at the parent: a checker, and discovery.
//
// An unreadable parent yields nothing rather than an error. This answers "is
// there a second board here", and "I cannot tell" is a no — the caller is
// reporting a hygiene problem, not deciding whether to write.
func CollidingSiblings(path string) []string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return nil // the filesystem root has no parent to scan
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil
	}

	self := filepath.Base(abs)
	var out []string
	for _, e := range entries {
		name := e.Name()
		if name == self || !e.IsDir() || !IsDirectoryName(name) {
			continue
		}
		// A sibling that is not a board is not a collision — it is the
		// emptiness test's business, and reporting an empty directory as a
		// rival board would send someone to fix the wrong thing.
		if !IsBoardDirectory(filepath.Join(parent, name)) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
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
			// A root that is itself a match still faces the emptiness test:
			// `mm --find ./some-source-tree/micro-manager` should report
			// nothing rather than inventing a board out of a name.
			if fi, err := os.Stat(root); err == nil && fi.IsDir() && IsBoardDirectory(root) {
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
			// Matched: DO NOT DESCEND, whatever the emptiness test says next. A
			// micro-manager directory inside another is undefined, and a
			// directory that merely shares the name is not an invitation to
			// walk a source tree that pruning would otherwise have skipped.
			if !IsBoardDirectory(full) {
				continue
			}
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
