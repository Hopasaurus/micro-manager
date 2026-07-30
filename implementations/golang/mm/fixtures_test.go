package mm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture corpus under testdata/.
//
// Every fixture is a real directory on disk rather than a string constant,
// because that is what the library actually reads and because check.sh can be
// pointed at it (T-0028). The NAME carries the expectation:
//
//	clean-*      must validate with no violations at all
//	broken-iN-*  must report a violation of IN, and of nothing else
//
// None of them is named micro-manager, .micro-manager or a mu variant, so the
// repository's own `check.sh --all` does not sweep them up and report the
// deliberately broken ones as failures.

type fixture struct {
	Name string
	Path string

	// Invariant is the one the fixture breaks, or "" when it is clean.
	// "format" is a structural problem outside I1-I10, such as a value that
	// cannot be parsed.
	Invariant string
}

// Clean reports whether the fixture must validate with no findings.
func (f fixture) Clean() bool { return f.Invariant == "" }

// brokenInvariants maps a fixture to the invariant its name promises.
//
// Kept explicit rather than derived from the name: two of them break a rule
// this implementation reports as a parse-level "format" problem rather than as
// a numbered invariant, and hiding that behind string munging would make the
// mismatch invisible.
var brokenInvariants = map[string]string{
	"broken-i1-duplicate-id":           "I1",
	"broken-i2-id-above-next-id":       "I2",
	"broken-i3-closed-in-backlog":      "I3",
	"broken-i4-idle-slot-with-id":      "I4",
	"broken-i5-blocked-without-reason": "I5",
	"broken-i6-done-without-outcome":   "I6",
	"broken-i6-wrong-month-heading":    "I6",
	"broken-i7-impossible-date":        "format", // the parser rejects the value
	"broken-i7-no-project":             "I7",
	"broken-i8-missing-detail-file":    "I8",
	"broken-i9-orphan-detail":          "I9",
	"broken-i9-title-drift":            "I9",
	"broken-i10-slot-gap":              "I10",
	"broken-i10-mixed-widths":          "I10",
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	entries, err := os.ReadDir("../testdata")
	if err != nil {
		t.Fatalf("fixture corpus missing: %v", err)
	}
	var out []fixture
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		f := fixture{Name: name, Path: filepath.Join("../testdata", name)}
		switch {
		case strings.HasPrefix(name, "clean-"):
		case strings.HasPrefix(name, "broken-"):
			inv, ok := brokenInvariants[name]
			if !ok {
				t.Fatalf("fixture %s has no expected invariant; add it to brokenInvariants", name)
			}
			f.Invariant = inv
		default:
			t.Fatalf("fixture %s must be named clean-* or broken-iN-*", name)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		t.Fatal("no fixtures found")
	}
	return out
}

// copyFixture copies a fixture into a temporary directory so a test may write
// to it. The corpus under testdata/ is read-only by convention: a test that
// mutates it in place breaks every test that runs after it.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("../testdata", name))); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dir
}

func TestFixtureCorpusIsComplete(t *testing.T) {
	fs := fixtures(t)

	clean := 0
	covered := map[string]bool{}
	for _, f := range fs {
		if f.Clean() {
			clean++
			continue
		}
		covered[f.Invariant] = true
	}
	if clean < 3 {
		t.Errorf("only %d clean fixtures; the corpus needs several shapes that must pass", clean)
	}
	// Every invariant needs a fixture that breaks it, or the validator has a
	// rule nothing exercises.
	for _, inv := range []string{"I1", "I2", "I3", "I4", "I5", "I6", "I7", "I8", "I9", "I10"} {
		if !covered[inv] {
			t.Errorf("no fixture breaks %s", inv)
		}
	}
}

func TestCleanFixturesValidate(t *testing.T) {
	for _, f := range fixtures(t) {
		if !f.Clean() {
			continue
		}
		s, err := Open(f.Path)
		if err != nil {
			t.Errorf("%s: open: %v", f.Name, err)
			continue
		}
		vs, err := s.Validate()
		if err != nil {
			t.Errorf("%s: validate: %v", f.Name, err)
			continue
		}
		if len(vs) != 0 {
			t.Errorf("%s should be clean:\n%s", f.Name, violationMessages(vs))
		}
	}
}

// A broken fixture must break the invariant it is named for, AND nothing else:
// a fixture that trips three rules cannot tell you which one a regression
// touched.
func TestBrokenFixturesBreakExactlyWhatTheyClaim(t *testing.T) {
	for _, f := range fixtures(t) {
		if f.Clean() {
			continue
		}
		s, err := Open(f.Path)
		if err != nil {
			t.Errorf("%s: open: %v", f.Name, err)
			continue
		}
		vs, err := s.Validate()
		if err != nil {
			t.Errorf("%s: validate: %v", f.Name, err)
			continue
		}
		if len(vs) == 0 {
			t.Errorf("%s should report a %s violation, got none", f.Name, f.Invariant)
			continue
		}
		for _, v := range vs {
			if v.Invariant != f.Invariant {
				t.Errorf("%s reports %s as well as %s: %s",
					f.Name, v.Invariant, f.Invariant, v)
			}
		}
	}
}

// A directory with existing violations must still open, list and report.
// Refusing to read a broken directory removes the tool exactly when it is
// needed (spec-tools.md §8).
func TestBrokenFixturesStillRead(t *testing.T) {
	for _, f := range fixtures(t) {
		if f.Clean() {
			continue
		}
		s, err := Open(f.Path)
		if err != nil {
			t.Errorf("%s: open: %v", f.Name, err)
			continue
		}
		if _, err := s.List(Filter{State: StateAll}); err != nil {
			t.Errorf("%s: list: %v", f.Name, err)
		}
		if _, err := s.Directory(); err != nil {
			t.Errorf("%s: directory: %v", f.Name, err)
		}
		p, _ := ParsePeriod("all", today)
		if _, err := s.Report(p, ReportOptions{}); err != nil {
			t.Errorf("%s: report: %v", f.Name, err)
		}
	}
}

// Format spec §4.1 rule 5: for every key except title, a trailing " #comment"
// is stripped from the value. clean-ugly puts one in the project name.
func TestFrontmatterCommentStrippingOnAValue(t *testing.T) {
	s, err := Open("../testdata/clean-ugly")
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.Project, "#") {
		t.Errorf("project = %q; rule 5 strips the trailing comment", d.Project)
	}
	if d.Project != "Clean Ugly — Ünïcode, and a" {
		t.Errorf("project = %q", d.Project)
	}
	// A title, by contrast, keeps its hash: "Fix issue #42" is an ordinary item.
	it, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.Title, "#hash") {
		t.Errorf("title = %q; titles are exempt from rule 5", it.Title)
	}
}
