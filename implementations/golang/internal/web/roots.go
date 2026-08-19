package web

import (
	"os"
	"path/filepath"
)

// ExpandRoots expands user-facing scan roots into the absolute paths consumed
// by discovery (spec-gui.md §9.5 rule 2). An unset environment variable
// drops its root instead of accidentally turning $WORK/code into /code.
func ExpandRoots(roots []string, home string) []string {
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		expanded, ok := expandRoot(root, home)
		if !ok {
			continue
		}
		if abs, err := filepath.Abs(expanded); err == nil {
			out = append(out, abs)
		}
	}
	return out
}

func expandRoot(path, home string) (string, bool) {
	if path == "~" || (len(path) > 1 && path[:2] == "~/") {
		if home != "" {
			path = filepath.Join(home, path[1:])
		}
	}
	missing := false
	expanded := os.Expand(path, func(key string) string {
		value, ok := os.LookupEnv(key)
		if !ok {
			missing = true
		}
		return value
	})
	return expanded, !missing
}
