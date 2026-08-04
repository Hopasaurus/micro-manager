package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var today = Date{2026, 7, 29}

func readDirFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAddAppendsToBottomByDefault(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)

	it, res, err := s.Add(AddRequest{Title: "Fix the deploy script", Prio: PrioHigh,
		Tags: []string{"infra", "ci"}}, today)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if it.ID != "T-0011" {
		t.Errorf("ID = %q, want T-0011 from next_id", it.ID)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeCreated {
		t.Errorf("changes = %+v", res.Changes)
	}

	// Bottom of Ready, not the top: a new item is not automatically urgent.
	items, _ := s.List(Filter{Section: SectionReady})
	want := []ID{"T-0001", "T-0005", "T-0011"}
	if got := idsOfValues(items); !sameIDs(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// next_id advanced.
	d, _ := s.Directory()
	if d.NextID != "T-0012" {
		t.Errorf("next_id = %q, want T-0012", d.NextID)
	}
	// The written line carries canonical field order.
	out := readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out,
		"- [ ] [T-0011] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29") {
		t.Errorf("line not as expected:\n%s", out)
	}
	// And the directory is still valid.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("add produced violations:\n%s", violationMessages(vs))
	}
}

func TestAddTop(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	if _, _, err := s.Add(AddRequest{Title: "Rotate the leaked token", Top: true}, today); err != nil {
		t.Fatal(err)
	}
	items, _ := s.List(Filter{Section: SectionReady})
	if items[0].ID != "T-0011" {
		t.Errorf("--top should insert first, got %v", idsOfValues(items))
	}
}

func TestAddIntoSections(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))

	if _, _, err := s.Add(AddRequest{Title: "Someday thing", Section: SectionSomeday}, today); err != nil {
		t.Fatal(err)
	}
	// A blocked reason implies the section.
	it, _, err := s.Add(AddRequest{Title: "Waiting thing", Blocked: "on a decision"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if it.Section != SectionBlocked {
		t.Errorf("a blocked: reason should imply Blocked, got %q", it.Section)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestAddValidation(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	cases := []struct {
		name string
		req  AddRequest
		want string
	}{
		{"empty title", AddRequest{Title: "   "}, "needs a title"},
		{"pipe in title", AddRequest{Title: "Fix a | b"}, "may not contain"},
		{"blocked without section", AddRequest{Title: "x", Section: SectionBlocked}, "needs a blocked: reason"},
		{"reason outside blocked", AddRequest{Title: "x", Section: SectionReady, Blocked: "why"}, "only belongs in Blocked"},
		{"bad prio", AddRequest{Title: "x", Prio: "URGENT"}, "prio:URGENT"},
		{"bad tag", AddRequest{Title: "x", Tags: []string{"a b"}}, "malformed tag"},
		{"pipe in reason", AddRequest{Title: "x", Blocked: "a|b"}, "may not contain"},
		{"unknown section", AddRequest{Title: "x", Section: "Later"}, "unknown section"},
	}
	for _, c := range cases {
		_, _, err := s.Add(c.req, today)
		if err == nil {
			t.Errorf("%s: should fail", c.name)
			continue
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", c.name, err)
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: message %q should mention %q", c.name, err, c.want)
		}
	}
	// None of those wrote anything.
	d, _ := s.Directory()
	if d.NextID != "T-0011" {
		t.Errorf("a rejected add must not consume an ID, next_id = %q", d.NextID)
	}
}

// Unregistered fields are the format's extension point and must survive.
func TestAddPreservesUnregisteredFields(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	_, _, err := s.Add(AddRequest{Title: "With extras",
		Extra: []Field{{"owner", "dlh"}, {"estimate", "3d"}}}, today)
	if err != nil {
		t.Fatal(err)
	}
	out := readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out, "| owner:dlh | estimate:3d") {
		t.Errorf("unregistered fields lost:\n%s", out)
	}
	it, _ := s.Get("T-0011")
	if len(it.Extra) != 2 || it.Extra[0].Key != "owner" {
		t.Errorf("Extra did not round-trip: %+v", it.Extra)
	}
}

func TestAddWithDetailFile(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	it, res, err := s.Add(AddRequest{Title: "Needs explaining",
		DetailBody: "## Context\n\nA long story."}, today)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if it.Detail != "details/T-0011.md" {
		t.Errorf("detail = %q", it.Detail)
	}
	if len(res.Changes) != 2 {
		t.Errorf("want 2 changes (item + detail), got %+v", res.Changes)
	}
	body := readDirFile(t, dir, "details/T-0011.md")
	// I9 requires id and title to match the item line exactly.
	if !strings.Contains(body, "id: T-0011") || !strings.Contains(body, "title: Needs explaining") {
		t.Errorf("detail frontmatter wrong:\n%s", body)
	}
	if !strings.Contains(body, "A long story.") {
		t.Error("body lost")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// A dry run must exercise the whole path and write nothing.
func TestAddDryRun(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	it, res, err := s.Add(AddRequest{Title: "Not really", DryRun: true}, today)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.DryRun || len(res.Changes) != 1 {
		t.Errorf("res = %+v", res)
	}
	// It still reports the ID it would have used and the file it would touch.
	if it.ID != "T-0011" {
		t.Errorf("ID = %q", it.ID)
	}
	if len(res.Files) != 1 || !strings.HasSuffix(res.Files[0], "backlog.md") {
		t.Errorf("Files = %v", res.Files)
	}
	if after := readDirFile(t, dir, "backlog.md"); after != before {
		t.Error("a dry run wrote to the file")
	}
}

// Pre-existing violations must not block an unrelated change: refusing would
// leave the user unable to use the tool to repair what it complains about.
func TestTransactionAllowsPreExistingViolations(t *testing.T) {
	broken := strings.Replace(dirBacklog, "prio:med", "prio:URGENT", 1)
	dir := newDir(t, map[string]string{"backlog.md": broken})
	s := mustOpen(t, dir)

	if vs, _ := s.Validate(); len(vs) == 0 {
		t.Fatal("fixture should start broken")
	}
	if _, _, err := s.Add(AddRequest{Title: "Unrelated work"}, today); err != nil {
		t.Fatalf("a pre-existing violation must not block an unrelated add: %v", err)
	}
	if _, err := s.Get("T-0011"); err != nil {
		t.Errorf("the item should have been written: %v", err)
	}
	// And the pre-existing problem is still reported, not silently absorbed.
	if vs, _ := s.Validate(); !hasViolation(vs, invFormat, "prio:URGENT") {
		t.Errorf("the original violation vanished:\n%s", violationMessages(vs))
	}
}

// Validation runs before the write, so a change that would break an invariant
// leaves the directory untouched.
func TestTransactionRejectsIntroducedViolation(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	// next_id already used by an existing item: the new ID would collide, and
	// would also sit at or above next_id.
	bad := strings.Replace(dirBacklog, "next_id: T-0011", "next_id: T-0001", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	before = readDirFile(t, dir, "backlog.md")

	_, _, err := s.Add(AddRequest{Title: "Would collide"}, today)
	if err == nil {
		t.Fatal("an add that duplicates an existing ID must be rejected")
	}
	if !errors.Is(err, ErrInvariantViolation) {
		t.Fatalf("want ErrInvariantViolation, got %v", err)
	}
	ie, ok := AsInvariantError(err)
	if !ok || len(ie.Violations) == 0 {
		t.Fatal("the violation list should come back with the error")
	}
	if after := readDirFile(t, dir, "backlog.md"); after != before {
		t.Error("a rejected transaction wrote to the file")
	}
}

// T-0013: the directory changed underneath the transaction.
func TestTransactionDetectsConcurrentModification(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)

	// Hold a transaction open across somebody else's edit by using the low
	// level API the way Add does.
	tx, err := s.begin()
	if err != nil {
		t.Fatal(err)
	}
	b, e, err := tx.backlog()
	if err != nil {
		t.Fatal(err)
	}
	it := &Item{Title: "Ours", ID: "T-0011", State: StateBacklog,
		Section: SectionReady, Created: today}
	b.InsertItem(e, SectionReady, 0, it)
	e.SetFM("next_id", "T-0012")
	tx.stage("backlog.md")

	// Somebody else writes the file first.
	time.Sleep(10 * time.Millisecond)
	other := strings.Replace(dirBacklog, "Sample One", "Edited Elsewhere", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = tx.commit(false)
	if !errors.Is(err, ErrConcurrent) {
		t.Fatalf("want ErrConcurrent, got %v", err)
	}
	// The other edit survives untouched.
	if got := readDirFile(t, dir, "backlog.md"); got != other {
		t.Error("the conflicting write was applied over somebody else's edit")
	}
}

// A no-op transaction touches nothing at all: no rewrite, no mtime bump, no diff.
func TestTransactionNoOpWritesNothing(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	p := filepath.Join(dir, "backlog.md")
	fi, _ := os.Stat(p)

	tx, err := s.begin()
	if err != nil {
		t.Fatal(err)
	}
	_, e, err := tx.backlog()
	if err != nil {
		t.Fatal(err)
	}
	e.SetFM("next_id", "T-0011") // the value already there
	tx.stage("backlog.md")
	if !tx.ws.Empty() {
		t.Error("an unchanged file should not be staged")
	}
	if _, err := tx.commit(false); err != nil {
		t.Fatal(err)
	}
	fi2, _ := os.Stat(p)
	if !fi.ModTime().Equal(fi2.ModTime()) {
		t.Error("a no-op transaction bumped the mtime")
	}
}

func TestAddUpdatesTheUpdatedField(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	if _, _, err := s.Add(AddRequest{Title: "x"}, Date{2026, 8, 1}); err != nil {
		t.Fatal(err)
	}
	if out := readDirFile(t, dir, "backlog.md"); !strings.Contains(out, "updated: 2026-08-01") {
		t.Error("updated: should be refreshed in a file being written")
	}
}

// A pre-existing finding must stay recognizable when a splice moves the line it
// names. violationKey drops the finding's own line number; maskLineRefs drops
// the one embedded in the message, which is where I1 puts the OTHER copy's
// location. Found by --migrate: writing a missing project inserts a line into
// the frontmatter, so every line below it moves.
func TestViolationKeyIgnoresLineNumbersInMessages(t *testing.T) {
	before := Violation{Invariant: "I1", At: Location{File: "backlog.md", Line: 13},
		Message: "T-0001 is already defined at backlog.md:11"}
	after := Violation{Invariant: "I1", At: Location{File: "backlog.md", Line: 14},
		Message: "T-0001 is already defined at backlog.md:12"}
	if violationKey(before) != violationKey(after) {
		t.Errorf("a shifted finding looks new:\n%q\n%q",
			violationKey(before), violationKey(after))
	}

	// A different finding is still a different key.
	other := Violation{Invariant: "I1", At: Location{File: "backlog.md", Line: 14},
		Message: "T-0002 is already defined at backlog.md:12"}
	if violationKey(before) == violationKey(other) {
		t.Error("two different duplicates collapsed to one key")
	}

	// A number that is not a line reference is content, and stays.
	counted := Violation{Message: "3 items are missing outcome"}
	if got := maskLineRefs(counted.Message); got != counted.Message {
		t.Errorf("maskLineRefs rewrote content: %q", got)
	}
}
