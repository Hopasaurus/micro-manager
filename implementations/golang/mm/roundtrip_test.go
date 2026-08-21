package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Round-trip fidelity: parse, write back unchanged, get identical bytes.
//
// This is the cheapest possible guard against the three ways a writer quietly
// destroys a hand-edited file - dropping unregistered fields, reordering
// sections, and reformatting lines nobody touched. It holds by construction
// because writes are splices over the original lines rather than a re-render
// from the model, and this is what proves the construction.

// roundTripFile parses one file and writes it straight back out.
func roundTripFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(path)

	var got []byte
	switch {
	case name == "backlog.md":
		b, _ := parseBacklog(name, data)
		got = b.Edit().Bytes()
	case name == "done.md":
		d, _ := parseDone(name, data)
		got = d.Edit().Bytes()
	case strings.HasPrefix(name, "working.") && strings.HasSuffix(name, ".md"):
		w, _ := parseWorking(name, data)
		got = w.Edit().Bytes()
	default:
		// Detail files and anything else: the generic line editor.
		lines := splitLines(data)
		fm, _, _ := readHeader(name, lines)
		got = newFileEdit(name, lines, fm).Bytes()
	}

	if string(got) != string(data) {
		t.Errorf("%s: round trip changed the file\n%s", path, firstDiff(string(data), string(got)))
	}
}

// firstDiff reports the first line that differs, which is far more useful than
// two thousand-line blobs.
func firstDiff(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  want %q\n   got %q", i+1, a, b)
		}
	}
	return "(files differ only in length)"
}

// ptr is the "set this field" helper an UpdateRequest needs: every field is a
// pointer so that "leave alone" is distinguishable from "set to empty".
func ptr[T any](v T) *T { return &v }

// filesOf lists every markdown file in a directory, details/ included.
func filesOf(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			if e.Name() != "details" {
				continue
			}
			sub, err := os.ReadDir(filepath.Join(dir, "details"))
			if err != nil {
				continue
			}
			for _, d := range sub {
				if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
					out = append(out, filepath.Join(dir, "details", d.Name()))
				}
			}
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// Every fixture, clean and broken alike. A file the parser cannot make sense of
// must still come back byte for byte: the tool has to be usable on a directory
// that is already wrong.
func TestRoundTripEveryFixture(t *testing.T) {
	n := 0
	for _, f := range fixtures(t) {
		for _, path := range filesOf(t, f.Path) {
			roundTripFile(t, path)
			n++
		}
	}
	t.Logf("round-tripped %d fixture files", n)
}

// The repository's own directories, which are the files a person actually
// typed rather than ones a generator produced.
func TestRoundTripTheRepository(t *testing.T) {
	roots := []string{
		"../../../sample-data/sample1/micro-manager",
		"../../../sample-data/sample2/micro-manager",
		"../../../sample-data/hidden/.micro-manager",
		"../../../sample-data/symbol/\u00b5manager",
		"../../../sample-data/symbol-hidden/.\u00b5manager",
		"../micro-manager",
	}
	n := 0
	for _, dir := range roots {
		if _, err := os.Stat(dir); err != nil {
			t.Skipf("repository directories not present: %v", err)
		}
		for _, path := range filesOf(t, dir) {
			roundTripFile(t, path)
			n++
		}
	}
	t.Logf("round-tripped %d repository files", n)
}

// A no-op operation must produce no diff at all: no rewrite, no mtime bump,
// nothing for version control to show.
func TestNoOpOperationsWriteNothing(t *testing.T) {
	dir, s := startDir(t)

	before := map[string]string{}
	for _, name := range []string{"backlog.md", "done.md", "working.01.md", "details/T-0001.md"} {
		before[name] = readFile(t, dir, name)
	}
	stamps := map[string]os.FileInfo{}
	for name := range before {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		stamps[name] = fi
	}

	// Moving an item to where it already is.
	if _, res, err := testMoveV1(s, "T-0001", MoveRequest{Top: true}, today); err != nil {
		t.Fatalf("move: %v", err)
	} else if len(res.Files) != 0 {
		t.Errorf("a move to the current position wrote %v", res.Files)
	}

	// An update that sets a field to the value it already holds.
	if _, res, err := testUpdateV1(s, "T-0001", UpdateRequest{Prio: ptr(PrioHigh)}, today); err != nil {
		t.Fatalf("update: %v", err)
	} else if len(res.Files) != 0 {
		t.Errorf("a no-change update wrote %v", res.Files)
	}

	// Setting the WIP limit to what it already is.
	if _, res, err := s.SetWipLimit(2, false); err != nil {
		t.Fatalf("wip: %v", err)
	} else if len(res.Files) != 0 {
		t.Errorf("a no-change wip limit wrote %v", res.Files)
	}

	for name, want := range before {
		if got := readFile(t, dir, name); got != want {
			t.Errorf("%s changed:\n%s", name, firstDiff(want, got))
		}
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(stamps[name].ModTime()) {
			t.Errorf("%s was rewritten with identical content", name)
		}
	}
}

// An edit rewrites the line it changes and leaves every other line alone.
// Whitespace churn across a file hides the one line that actually changed.
func TestEditTouchesOnlyItsOwnLine(t *testing.T) {
	dir, s := startDir(t)
	before := strings.Split(readFile(t, dir, "backlog.md"), "\n")

	if _, _, err := testUpdateV1(s, "T-0002", UpdateRequest{Prio: ptr(PrioLow)}, today); err != nil {
		t.Fatal(err)
	}
	after := strings.Split(readFile(t, dir, "backlog.md"), "\n")

	if len(before) != len(after) {
		t.Fatalf("line count changed: %d -> %d", len(before), len(after))
	}
	var changed []int
	for i := range before {
		if before[i] != after[i] {
			changed = append(changed, i+1)
		}
	}
	// Exactly one line: the item's own. The frontmatter's updated: field
	// already reads today, and SetFM does not rewrite a value that is already
	// correct - which is the same rule that makes a no-op write nothing.
	if len(changed) != 1 {
		t.Errorf("changed lines %v, want only the item line", changed)
		for _, n := range changed {
			t.Logf("  line %d:\n    was %q\n    now %q", n, before[n-1], after[n-1])
		}
	} else if !strings.Contains(after[changed[0]-1], "[T-0002]") {
		t.Errorf("the changed line is not the item: %q", after[changed[0]-1])
	}
}

// An unregistered field must survive the whole lifecycle. It is the format's
// extension point, and a tool that drops it destroys data written by another
// tool that understood it.
func TestExtraFieldsSurviveEveryTransition(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	s, _, err := Init(dir, InitRequest{Project: "Extras"}, today)
	if err != nil {
		t.Fatal(err)
	}

	it, _, err := testAddV1(s, AddRequest{
		Title: "Carries baggage",
		Tags:  []string{"infra"},
		Extra: []Field{{"owner", "dana"}, {"ticket", "ACME-42"}},
	}, today)
	if err != nil {
		t.Fatal(err)
	}

	hasExtras := func(stage string) {
		t.Helper()
		got, err := s.Get(it.ID)
		if err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		if len(got.Extra) != 2 {
			t.Errorf("%s: extra = %+v, want owner and ticket", stage, got.Extra)
			return
		}
		if got.Extra[0] != (Field{"owner", "dana"}) || got.Extra[1] != (Field{"ticket", "ACME-42"}) {
			t.Errorf("%s: extra = %+v", stage, got.Extra)
		}
	}

	hasExtras("added")
	if _, _, err := testMoveV1(s, it.ID, MoveRequest{Section: SectionSomeday}, today); err != nil {
		t.Fatal(err)
	}
	hasExtras("moved")
	if _, _, err := testMoveV1(s, it.ID, MoveRequest{Section: SectionReady}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStartV1(s, it.ID, StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	hasExtras("started")
	if _, _, err := testUpdateV1(s, it.ID, UpdateRequest{Title: ptr("Renamed while working")}, today); err != nil {
		t.Fatal(err)
	}
	hasExtras("updated in a slot")
	if _, _, err := testPauseV1(s, it.ID, PauseRequest{}, today); err != nil {
		t.Fatal(err)
	}
	hasExtras("paused")
	if _, _, err := testFinishV1(s, it.ID, FinishRequest{}, today); err != nil {
		t.Fatal(err)
	}
	hasExtras("finished")

	if line := readFile(t, dir, "done.md"); !strings.Contains(line, "owner:dana | ticket:ACME-42") {
		t.Errorf("done.md lost the field order:\n%s", line)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}
