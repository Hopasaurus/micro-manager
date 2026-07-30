package mm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Sanity check against every real directory in the repository: each item line
// must parse, and must render back byte-identical.
//
// These paths reach outside the module, so the test skips when they are absent
// rather than failing - the module must stay buildable in isolation. T-0026 and
// T-0027 replace this with self-contained fixtures under testdata/.
func TestAgainstRealRepositoryFiles(t *testing.T) {
	roots := []string{
		"../../../sample-data/sample1/micro-manager",
		"../../../sample-data/sample2/micro-manager",
		"../../../sample-data/hidden/.micro-manager",
		"../../../sample-data/symbol/µmanager",
		"../../../sample-data/symbol-hidden/.µmanager",
		"../micro-manager",
	}
	total := 0
	for _, dir := range roots {
		for _, name := range []string{"backlog.md", "done.md"} {
			p := filepath.Join(dir, name)
			b, err := os.ReadFile(p)
			if err != nil {
				t.Skipf("repository fixtures not present: %v", err)
			}
			lines := strings.Split(string(b), "\n")
			if _, err := parseFrontmatter(name, lines); err != nil {
				t.Errorf("%s: frontmatter: %v", p, err)
			}
			for i, line := range lines {
				if !looksLikeItemLine(line) {
					continue
				}
				it, err := parseItemLine(name, i+1, line)
				if err != nil {
					t.Errorf("%s:%d: %v", p, i+1, err)
					continue
				}
				if name == "done.md" {
					it.State = StateDone
				}
				if got := RenderItemLine(it); got != line {
					t.Errorf("%s:%d round trip:\n got %q\nwant %q", p, i+1, got, line)
				}
				total++
			}
		}
	}
	t.Logf("parsed and round-tripped %d real item lines", total)
}
