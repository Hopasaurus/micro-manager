package mm

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Cross-check against check.sh, the reference implementation of I1-I10.
//
// ONE DIVERGENCE IS BY DESIGN, and it is here rather than in a comment nobody
// reads: check.sh also reports a SIBLING COLLISION — two recognized names in
// one parent (spec-file-format.md Appendix B, T-0194) — and Store.Validate
// deliberately does not. A collision is a property of the parent, and
// per-directory validation must not depend on where a board sits, or every
// mutation's pre-commit check would start reading the directory above the one
// it is writing. The Go side reports it from the checker front end
// (internal/cli's collisionFindings), which is where check.sh's copy lives too.
//
// No fixture below collides, so the comparison is unaffected. A fixture that
// did would have to filter that finding rather than "fix" either side.
//
// Two independent implementations of ten invariants WILL diverge. This finds the
// divergence for almost no effort, which is worth more than any amount of
// reasoning about whether they agree.
//
// Violation SETS are compared, not messages: the wording differs and is allowed
// to. The comparable key is (file, line), because check.sh emits no invariant
// identifiers at all - it prints `path:line: message`, and file-level findings
// carry no line. That answers the open question in the item's detail file:
// file-level findings compare as line 0 on both sides.

const checkScript = "../../../check.sh"

// finding is one violation reduced to what both implementations agree to state.
type finding struct {
	File string // relative to the directory
	Line int    // 0 when the finding is file or directory level
}

func (f finding) String() string {
	if f.Line == 0 {
		return f.File
	}
	return f.File + ":" + strconv.Itoa(f.Line)
}

// requireBash skips rather than fails when the reference validator cannot run.
// This must not block CI on a minimal container.
func requireBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available; skipping the check.sh cross-check")
	}
	if _, err := exec.LookPath("awk"); err != nil {
		t.Skip("awk is not available; skipping the check.sh cross-check")
	}
}

// runCheckSh runs the reference validator and parses its findings.
func runCheckSh(t *testing.T, dir string) []finding {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(checkScript)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", script, abs).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() > 1 {
			// Exit 1 means "found problems", which is a result. Anything else -
			// usage, setup, a missing script - is a failure of the test itself.
			t.Fatalf("check.sh %s: %v\n%s", dir, err, out)
		}
	}
	return parseCheckSh(string(out), abs)
}

// parseCheckSh reads `path[:line]: message` lines, ignoring the indented
// per-directory summary and the final total.
func parseCheckSh(output, dir string) []finding {
	var out []finding
	for _, line := range strings.Split(output, "\n") {
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "check.sh:") {
			continue
		}
		if !strings.HasPrefix(line, dir) {
			continue
		}
		rest := strings.TrimPrefix(line, dir)
		rest = strings.TrimPrefix(rest, "/")

		loc := rest
		if i := strings.Index(rest, ": "); i >= 0 {
			loc = rest[:i]
		}
		f := finding{File: loc}
		if i := strings.LastIndex(loc, ":"); i >= 0 {
			if n, err := strconv.Atoi(loc[i+1:]); err == nil {
				f.File = loc[:i]
				f.Line = n
			}
		}
		if f.File == "" {
			f.File = "." // a directory-level finding, which is how I10 reports
		}
		out = append(out, f)
	}
	return out
}

// goFindings runs this implementation's validator over the same directory.
func goFindings(t *testing.T, dir string) []finding {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	vs, err := s.Validate()
	if err != nil {
		t.Fatalf("validate %s: %v", dir, err)
	}
	out := make([]finding, 0, len(vs))
	for _, v := range vs {
		out = append(out, finding{File: v.At.File, Line: v.At.Line})
	}
	return out
}

func sortFindings(fs []finding) {
	sort.Slice(fs, func(i, j int) bool {
		if fs[i].File != fs[j].File {
			return fs[i].File < fs[j].File
		}
		return fs[i].Line < fs[j].Line
	})
}

// compareValidators asserts the two implementations report the same problems.
func compareValidators(t *testing.T, name, dir string) {
	t.Helper()
	want := runCheckSh(t, dir)
	got := goFindings(t, dir)
	sortFindings(want)
	sortFindings(got)

	if !sameFindings(want, got) {
		t.Errorf("%s: the validators disagree\n  check.sh: %v\n        go: %v",
			name, describeFindings(want), describeFindings(got))
	}
}

// sameFindings compares two sets of findings, allowing one side to be less
// precise than the other about WHERE a problem is.
//
// A finding with no line is a claim about the whole file, and it matches any
// finding in that file. This is not slack in the comparison, it is the honest
// reading of two tools with different precision: check.sh reports an I9 title
// drift against the detail file, while this implementation names the exact
// frontmatter line. Both are right about the same problem, and demanding
// identical line numbers would force the better answer down to the coarser one.
//
// What is NOT tolerated is a problem one side reports and the other does not,
// in either direction. That is the divergence this test exists to find.
func sameFindings(a, b []finding) bool {
	return coveredBy(a, b) && coveredBy(b, a)
}

func coveredBy(from, to []finding) bool {
	for _, f := range from {
		matched := false
		for _, g := range to {
			if f.File != g.File {
				continue
			}
			if f.Line == 0 || g.Line == 0 || f.Line == g.Line {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func describeFindings(fs []finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

// The comparison tolerates one side being vaguer about the line. It must not
// tolerate one side missing a problem, or the cross-check would pass by being
// permissive rather than by the validators agreeing.
func TestFindingComparisonIsNotTooForgiving(t *testing.T) {
	at := func(file string, line int) finding { return finding{File: file, Line: line} }

	cases := []struct {
		name string
		a, b []finding
		same bool
	}{
		{"identical", []finding{at("backlog.md", 12)}, []finding{at("backlog.md", 12)}, true},
		{"file-level matches a line", []finding{at("done.md", 0)}, []finding{at("done.md", 7)}, true},
		{"line matches file-level", []finding{at("done.md", 7)}, []finding{at("done.md", 0)}, true},
		{"both empty", nil, nil, true},

		{"different lines in one file", []finding{at("backlog.md", 12)}, []finding{at("backlog.md", 20)}, false},
		{"different files", []finding{at("backlog.md", 0)}, []finding{at("done.md", 0)}, false},
		{"one side found nothing", []finding{at("backlog.md", 12)}, nil, false},
		{"the other side found nothing", nil, []finding{at("backlog.md", 12)}, false},
		{"an extra finding", []finding{at("backlog.md", 12)},
			[]finding{at("backlog.md", 12), at("done.md", 3)}, false},
	}
	for _, c := range cases {
		if got := sameFindings(c.a, c.b); got != c.same {
			t.Errorf("%s: sameFindings = %v, want %v", c.name, got, c.same)
		}
	}
}

func TestValidatorAgreesWithCheckShOnFixtures(t *testing.T) {
	requireBash(t)
	for _, f := range fixtures(t) {
		// check.sh has no version-2 support at all (it is entirely
		// backlog.md/working.NN.md-shaped) - see
		// TestValidatorAgreesWithCheckShOnTheRepository's identical
		// exclusion of the live board for the full reasoning. A board.md
		// fixture is not a shape this comparison can make sense of yet.
		if _, err := os.Stat(filepath.Join(f.Path, "board.md")); err == nil {
			continue
		}
		compareValidators(t, f.Name, f.Path)
	}
}

func TestValidatorAgreesWithCheckShOnTheRepository(t *testing.T) {
	requireBash(t)
	for _, dir := range []string{
		"../../../sample-data/sample1/micro-manager",
		"../../../sample-data/sample2/micro-manager",
		"../../../sample-data/sample3/micro-manager",
		// The live board itself (../micro-manager) is version 2 as of T-0229,
		// and check.sh has no version-2 support at all (it is entirely
		// backlog.md/working.NN.md-shaped) - comparing it against a version-2
		// directory would not be cross-validating one format, it would be
		// comparing two different ones. legacy-v1, ../micro-manager's frozen
		// pre-migration snapshot (notes/add-columns.md §7.1, decision 21),
		// takes over the coverage this line used to provide: unlike the live
		// board, it never changes shape out from under this comparison.
		"../../../sample-data/legacy-v1/micro-manager",
		"../../../sample-data/hidden/.micro-manager",
		"../../../sample-data/symbol/µmanager",
		"../../../sample-data/symbol-hidden/.µmanager",
	} {
		compareValidators(t, dir, dir)
	}
}

// The important half: directories THIS implementation produced. A mutation that
// writes a directory the reference checker rejects is exactly what this exists
// to catch, and a static fixture cannot catch it.
func TestValidatorAgreesWithCheckShOnWrittenDirectories(t *testing.T) {
	requireBash(t)

	t.Run("after the whole lifecycle", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mm")
		s, _, err := initV1(dir, InitRequest{Project: "Written", Wip: 2}, today)
		if err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "init", dir)

		a, _, err := testAddV1(s, AddRequest{
			Title: "First", Prio: PrioHigh, Tags: []string{"infra", "ci"},
			Extra: []Field{{"owner", "dana"}}, DetailBody: "Long form.\n",
		}, today)
		if err != nil {
			t.Fatal(err)
		}
		b, _, err := testAddV1(s, AddRequest{Title: "Second", Blocked: "waiting on ops"}, today)
		if err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "add", dir)

		if _, _, err := testStartV1(s, a.ID, StartRequest{}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "start", dir)

		if _, _, err := testPauseV1(s, a.ID, PauseRequest{}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "pause", dir)

		if _, _, err := testStartV1(s, a.ID, StartRequest{}, today); err != nil {
			t.Fatal(err)
		}
		if _, _, err := testFinishV1(s, a.ID, FinishRequest{Note: "shipped"}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "finish", dir)

		// A month group that does not exist yet, created in newest-first order.
		if _, _, err := testFinishV1(s, b.ID, FinishRequest{
			Done: Date{2026, 5, 4}, Outcome: OutcomeCancelled,
		}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "finish into an older month", dir)

		if _, _, err := s.SetWipLimit(4, false); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "wip 4", dir)
		if _, _, err := s.SetWipLimit(1, false); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "wip 1", dir)
	})

	// T-0116: the agreement must hold under a NON-DEFAULT ID grammar too - the
	// X/3 corpus the fixtures declare. Init, add, start and finish all write
	// X-### IDs (next_id included), and both validators have to read them.
	t.Run("a non-default ID grammar", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mm")
		s, _, err := initV1(dir, InitRequest{
			Project: "Custom IDs", Wip: 2, IDPrefix: "X", IDWidth: 3,
		}, today)
		if err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "init with X/3", dir)

		a, _, err := testAddV1(s, AddRequest{Title: "First", DetailBody: "Long form.\n"}, today)
		if err != nil {
			t.Fatal(err)
		}
		if string(a.ID) != "X-001" {
			t.Fatalf("first ID = %s, want X-001", a.ID)
		}
		compareValidators(t, "add under X/3", dir)

		if _, _, err := testStartV1(s, a.ID, StartRequest{}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "start under X/3", dir)

		if _, _, err := testFinishV1(s, a.ID, FinishRequest{Note: "shipped"}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "finish under X/3", dir)
	})

	// Every fixture breaks exactly one rule, which is what makes a regression
	// legible. A directory broken in SEVERAL ways at once is a different test:
	// it is where two validators most easily report overlapping-but-different
	// sets, and each finding has to line up.
	t.Run("a directory broken several ways at once", func(t *testing.T) {
		dir := newDir(t, map[string]string{
			"backlog.md": strings.NewReplacer(
				"prio:med", "prio:URGENT", // not a prio
				"| blocked:on a thing", "", // I5: under Blocked with no reason
				"next_id: T-0011", "next_id: T-0002", // I2: IDs at or above it
			).Replace(dirBacklog),
			// I6: filed under a month its done: date does not name.
			"done.md": strings.Replace(sampleDone, "done:2026-07-23", "done:2026-03-02", 1),
		})
		compareValidators(t, "multiply broken", dir)

		// And the agreement must not be vacuous: both have to actually reject it.
		if len(goFindings(t, dir)) == 0 {
			t.Error("the Go validator found nothing in a deliberately broken directory")
		}
		if len(runCheckSh(t, dir)) == 0 {
			t.Error("check.sh found nothing in a deliberately broken directory")
		}
	})

	t.Run("after a tickler lifecycle", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mm")
		s, _, err := initV1(dir, InitRequest{Project: "Tickler", Wip: 2}, today)
		if err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "init", dir)

		// A scheduled someday item: both validators must accept what the API
		// wrote, and reject the hand-edit the API refuses.
		one, _, err := testAddV1(s, AddRequest{
			Title: "Prune", Section: SectionSomeday, Tickler: "2026-09-01@08:00",
			Created: Date{2026, 7, 20},
		}, today)
		if err != nil {
			t.Fatal(err)
		}
		rec, _, err := testAddV1(s, AddRequest{
			Title: "Watering", Section: SectionSomeday, Tickler: "first-mon@08:00",
			Created: Date{2026, 7, 27},
		}, today)
		if err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "scheduled items", dir)

		// A fire: the one-shot moves to Ready, the recurring spawns. The
		// directory both validators see next is exactly what the API wrote.
		if _, err := s.Tick(Date{2026, 8, 3}, false); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "after a tick", dir)

		// Move the fired item out of Someday: the schedule drops, and both
		// validators must keep agreeing that the directory is clean.
		if _, _, err := testMoveV1(s, one.ID, MoveRequest{Section: SectionReady}, today); err != nil {
			t.Fatal(err)
		}
		if _, _, err := testMoveV1(s, rec.ID, MoveRequest{Section: SectionReady}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "after moving both out of Someday", dir)
	})

	// Archiving is the one operation that takes items OUT of the validated
	// world, so it is the one most likely to make the two validators disagree:
	// neither reads done-YYYY.md, and both have to agree that the items which
	// left are simply gone rather than missing.
	t.Run("after an archive", func(t *testing.T) {
		dir := newDir(t, map[string]string{"done.md": archiveDone})
		s := mustOpen(t, dir)
		compareValidators(t, "before archiving", dir)

		res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
		if err != nil {
			t.Fatal(err)
		}
		if res.Items == 0 {
			t.Fatal("the fixture archived nothing; the comparison would be vacuous")
		}
		compareValidators(t, "after archiving two month groups", dir)

		// And with everything gone, including the last group: an emptied done.md
		// is a shape neither validator sees anywhere else.
		if _, _, err := s.Archive(ArchiveRequest{Before: Date{2027, 1, 1}}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "after archiving every month group", dir)
	})

	// details-YYYY/ is a directory neither validator may look into (§5.6 rule
	// 2), and the claim that it needs NO validator change rests on exactly
	// that. check.sh globs "$dir"/details/*.md and the Go checker reads
	// details/; if either one wandered, this is where it would show, because a
	// file that has moved there is claimed by no live item and would be an I9
	// orphan the moment it was seen.
	t.Run("after an archive that moves a detail file", func(t *testing.T) {
		dir := newDir(t, map[string]string{
			"done.md": strings.Replace(archiveDone,
				"- [x] [T-0008] June | prio:low",
				"- [x] [T-0008] June | prio:low | detail:details/T-0008.md", 1),
			"details/T-0008.md": "---\ndoc: detail\nid: T-0008\ntitle: June\n---\n\n# T-0008 — June\n",
		})
		s := mustOpen(t, dir)
		compareValidators(t, "before archiving", dir)

		res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 7, 1}}, today)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.DetailsMoved) != 1 {
			t.Fatalf("moves = %+v, want the one detail file", res.DetailsMoved)
		}
		compareValidators(t, "archive that moved a detail file into details-YYYY/", dir)

		// Agreement on nothing is the point here, so it has to be nothing.
		if got := goFindings(t, dir); len(got) != 0 {
			t.Errorf("the Go checker reads details-YYYY/: %v", got)
		}
		if got := runCheckSh(t, dir); len(got) != 0 {
			t.Errorf("check.sh reads details-YYYY/: %v", got)
		}
	})

	// The deliberate-violation path: --remove leaves an orphan detail file by
	// default and says so. Both validators must then report the same orphan.
	t.Run("after a remove that orphans a detail file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "mm")
		s, _, err := initV1(dir, InitRequest{Project: "Orphans"}, today)
		if err != nil {
			t.Fatal(err)
		}
		it, _, err := testAddV1(s, AddRequest{Title: "Has a detail file", DetailBody: "Body.\n"}, today)
		if err != nil {
			t.Fatal(err)
		}
		out, _, err := testRemoveV1(s, it.ID, RemoveRequest{Force: true}, today)
		if err != nil {
			t.Fatal(err)
		}
		if out.DetailOrphan == "" {
			t.Fatal("the orphan should have been reported")
		}
		compareValidators(t, "remove without --with-detail", dir)

		// And with the file deleted, both must go quiet again.
		it2, _, err := testAddV1(s, AddRequest{Title: "Another", DetailBody: "Body.\n"}, today)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := testRemoveV1(s, it2.ID, RemoveRequest{Force: true, WithDetail: true}, today); err != nil {
			t.Fatal(err)
		}
		compareValidators(t, "remove --with-detail", dir)
	})
}

// find.sh is the oracle for DISCOVERY, exactly as check.sh is for the ten
// invariants (T-0191).
//
// The emptiness test of spec-file-format.md Appendix B is now implemented
// twice — once in bash, once here — and this repository's own experience is
// that two implementations of one rule drift. So the two are run over the same
// real tree and their answers compared: the repository holds eight boards, one
// deliberate name-collision fixture that both must skip
// (sample-data/not-a-board), and a plugin source directory that is named
// pi-mm precisely to stay out of the way.
func TestDiscoveryAgreesWithFindSh(t *testing.T) {
	requireBash(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../../find.sh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(script); err != nil {
		t.Skipf("find.sh is not available: %v", err)
	}

	// find.sh exits 1 when it finds nothing, which is a result rather than a
	// failure; anything above that is the test's own problem.
	out, err := exec.Command("bash", script, root).CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() > 1 {
			t.Fatalf("find.sh: %v\n%s", err, out)
		}
	}

	shell := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		shell[canonical(line)] = true
	}

	golang := map[string]bool{}
	// MaxDepth 0: the deepest board here is further down than the default of
	// six, and a depth limit the shell script does not share would look like a
	// disagreement about the rule.
	opts := DefaultDiscoveryOptions(root)
	opts.MaxDepth = 0
	res := Discover(opts)
	for _, d := range res.Directories {
		golang[canonical(d.Path)] = true
	}

	for path := range shell {
		if !golang[path] {
			t.Errorf("find.sh found %s; Discover did not", path)
		}
	}
	for path := range golang {
		if !shell[path] {
			t.Errorf("Discover found %s; find.sh did not", path)
		}
	}
	if len(golang) == 0 {
		t.Fatal("neither found anything; the cross-check proved nothing")
	}

	// The fixture is the case the rule exists for, and it must be absent from
	// BOTH sides rather than merely from the same side twice.
	fixture := canonical(filepath.Join(root, "sample-data", "not-a-board", "micro-manager"))
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("the name-collision fixture is missing: %v", err)
	}
	if shell[fixture] || golang[fixture] {
		t.Errorf("a directory with no board files was discovered: %s", fixture)
	}
	t.Logf("cross-checked %d discovered directories against find.sh", len(golang))
}
