package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"micromanager/mm"
)

// Directory resolution (spec-tools.md §4).
//
// Four steps, first hit wins, and the last two SEARCH. The searching is why
// this lives in the wrapper: it reads the working directory and the
// environment, and the library is forbidden both (§2.2 rule 4). What the
// library receives is one absolute path, already decided.
//
// AMBIGUITY IS NEVER RESOLVED SILENTLY. Where a step finds more than one
// candidate the tool fails, listing them with their project names, because
// picking one would mean writing to a directory the user did not name.

// Resolution records which directory was chosen and how, so that --verbose and
// the JSON envelope can say.
type Resolution struct {
	Path   string
	Source string // "--dir", "MM_DIR", "upward search", "downward search"
}

// resolveDir applies §4 in order.
//
// cwd is passed in rather than read here so that the whole of resolution is
// testable without chdir, which is process-global and cannot be done safely in
// a parallel test.
func resolveDir(dirFlag, mmDir, cwd string) (Resolution, error) {
	// 1. --dir, used VERBATIM. If it is not a micro-manager directory that is an
	//    error, not the start of a search: the user named a place.
	if dirFlag != "" {
		abs, err := absolute(dirFlag, cwd)
		if err != nil {
			return Resolution{}, err
		}
		if !looksLikeDirectory(abs) {
			return Resolution{}, notFoundf(
				"%s is not a micro-manager directory (no backlog.md)", dirFlag)
		}
		return Resolution{Path: abs, Source: "--dir"}, nil
	}

	// 2. MM_DIR, treated the same way. An empty variable is unset (§3.5), and an
	//    invalid one is an error naming the variable rather than a silent
	//    fallback - a stale export in a shell profile would otherwise send every
	//    command to the wrong project.
	if mmDir != "" {
		abs, err := absolute(mmDir, cwd)
		if err != nil {
			return Resolution{}, err
		}
		if !looksLikeDirectory(abs) {
			return Resolution{}, notFoundf(
				"MM_DIR is set to %s, which is not a micro-manager directory", mmDir)
		}
		return Resolution{Path: abs, Source: "MM_DIR"}, nil
	}

	// 3. Upward from the current directory: the common case of running the tool
	//    from anywhere inside a project.
	if found, err := searchUpward(cwd); err != nil {
		return Resolution{}, err
	} else if found != "" {
		return Resolution{Path: found, Source: "upward search"}, nil
	}

	// 4. Downward, using the discovery rules.
	res := mm.Discover(mm.DefaultDiscoveryOptions(cwd))
	switch len(res.Directories) {
	case 0:
		return Resolution{}, notFoundf(
			"no micro-manager directory found from %s; name one with --dir, "+
				"set MM_DIR, or create one with --init --project NAME", cwd)
	case 1:
		return Resolution{Path: res.Directories[0].Path, Source: "downward search"}, nil
	default:
		return Resolution{}, notFoundf("%s", ambiguous(res.Directories))
	}
}

// searchUpward walks from cwd to the filesystem root looking for a directory
// that CONTAINS a micro-manager directory.
//
// It stops at the first level that has one, and an ambiguity there is an error:
// two todo directories side by side is a real situation and neither is more
// correct than the other.
func searchUpward(cwd string) (string, error) {
	// An empty cwd means the process could not report its directory (getwd
	// failed). There is nothing to walk up from — and filepath.Dir("") is ".",
	// which would silently read the process's actual working directory, the one
	// global this function exists to avoid (code-review-007 F10).
	if cwd == "" {
		return "", nil
	}
	dir := cwd
	for {
		entries, err := os.ReadDir(dir)
		if err == nil {
			var found []mm.Directory
			for _, e := range entries {
				if !e.IsDir() || !mm.IsDirectoryName(e.Name()) {
					continue
				}
				path := filepath.Join(dir, e.Name())
				d := mm.Directory{Path: path}
				if s, err := mm.Open(path); err == nil {
					if got, err := s.Directory(); err == nil {
						d = got
					}
				}
				found = append(found, d)
			}
			switch len(found) {
			case 0:
			case 1:
				return found[0].Path, nil
			default:
				return "", notFoundf("%s", ambiguous(found))
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil // reached the root
		}
		dir = parent
	}
}

// ambiguous renders the candidates with their project names. Listing them is
// the whole point: the user has to be able to pick one for the next run.
func ambiguous(dirs []mm.Directory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d micro-manager directories found; name one with --dir:", len(dirs))
	for _, d := range dirs {
		project := d.Project
		if project == "" {
			project = "(no project name)"
		}
		fmt.Fprintf(&b, "\n  %s  %s", d.Path, project)
	}
	return b.String()
}

// looksLikeDirectory checks for backlog.md rather than for the directory's name.
//
// §4 step 1 says --dir is used verbatim, so a directory called anything at all
// is acceptable when it is named explicitly; what makes it usable is holding
// the files. Discovery is the opposite - it matches on name alone - and the two
// rules are deliberately different.
func looksLikeDirectory(path string) bool {
	fi, err := os.Stat(filepath.Join(path, "backlog.md"))
	return err == nil && !fi.IsDir()
}

// absolute resolves a path against an explicit working directory.
func absolute(path, cwd string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	if cwd == "" {
		return "", ioErrorf("cannot resolve the relative path %s: no working directory", path)
	}
	return filepath.Clean(filepath.Join(cwd, path)), nil
}

// discoveryRoots returns the roots for --all.
//
// §4: --all applies across every directory found by the downward search. Scan
// roots from a shared config file (spec-gui.md §9.5) take precedence once that
// file exists; until then the working directory is the root.
func discoveryRoots(cwd string) []string {
	if cwd == "" {
		return nil
	}
	return []string{cwd}
}
