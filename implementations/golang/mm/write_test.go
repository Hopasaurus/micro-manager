package mm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The acceptance test for T-0008: parse, change nothing, write back, get the
// same bytes. Everything else in this file guards a way that could stop being
// true.
func TestRoundTripUnchanged(t *testing.T) {
	cases := map[string]string{
		"backlog.md":    sampleBacklog,
		"done.md":       sampleDone,
		"working.01.md": idleSlot,
		"working.02.md": busySlot,
	}
	for name, src := range cases {
		var e *fileEdit
		switch {
		case name == "backlog.md":
			b, _ := parseBacklog(name, []byte(src))
			e = b.Edit()
		case name == "done.md":
			d, _ := parseDone(name, []byte(src))
			e = d.Edit()
		default:
			w, _ := parseWorking(name, []byte(src))
			e = w.Edit()
		}
		if e.Dirty() {
			t.Errorf("%s: parsing must not dirty the file", name)
		}
		if got := string(e.Bytes()); got != src {
			t.Errorf("%s: round trip changed bytes\n got %q\nwant %q", name, got, src)
		}
	}
}

// Everything the parser deliberately ignores must survive a write: prose, blank
// lines, HTML comments, YAML comments, lines with no colon, odd spacing.
func TestRoundTripPreservesIgnoredContent(t *testing.T) {
	src := `---
doc: backlog
version: 1
project: Odd   # a trailing comment
   spaced   :   value
a line with no colon
next_id: T-0009
---

# Backlog

Some prose.

<!-- an HTML comment -->

## Ready


- [ ] [T-0001] First | prio:med


## Blocked

## Someday
`
	b, vs := parseBacklog("backlog.md", []byte(src))
	if len(vs) != 0 {
		t.Fatalf("unexpected violations: %v", vs)
	}
	e := b.Edit()
	if got := string(e.Bytes()); got != src {
		t.Errorf("round trip changed bytes:\n%q\n%q", got, src)
	}
}

func TestSetFMRewritesOneLine(t *testing.T) {
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	e.SetFM("next_id", "T-0005")

	out := string(e.Bytes())
	if !strings.Contains(out, "next_id: T-0005") {
		t.Fatal("next_id not updated")
	}
	// Exactly one line differs.
	if n := countDiffLines(sampleBacklog, out); n != 1 {
		t.Errorf("want 1 changed line, got %d", n)
	}
	// The comment-bearing and prose lines around it are untouched.
	if !strings.Contains(out, "project: Sample One") {
		t.Error("neighbouring frontmatter damaged")
	}
}

// A no-op write must produce no diff at all: same value in, nothing dirty.
func TestSetFMNoOpDoesNotDirty(t *testing.T) {
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	e.SetFM("next_id", "T-0004") // the value already there
	if e.Dirty() {
		t.Error("setting a value to itself must not dirty the file")
	}
	if string(e.Bytes()) != sampleBacklog {
		t.Error("no-op changed the bytes")
	}
}

func TestSetFMInsertsNewKey(t *testing.T) {
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	e.SetFM("owner", "dlh")

	out := string(e.Bytes())
	if !strings.Contains(out, "owner: dlh\n---") {
		t.Errorf("new key should go just before the closing delimiter:\n%s", out[:200])
	}
	// Inserting into the frontmatter shifts the body; item line numbers must
	// have moved with it, or the next splice lands in the wrong place.
	ready := b.Section(SectionReady)
	if got := e.lines[ready.Items[0].Source.Line-1]; !strings.Contains(got, "T-0001") {
		t.Errorf("item line number went stale after a frontmatter insert: %q", got)
	}
	if got := e.lines[ready.HeadLine-1]; got != "## Ready" {
		t.Errorf("section heading went stale: %q", got)
	}
}

func TestReplaceItem(t *testing.T) {
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	it := b.Section(SectionReady).Items[0]
	it.Prio = PrioHigh
	e.ReplaceItem(it)

	out := string(e.Bytes())
	if !strings.Contains(out, "- [ ] [T-0001] First | prio:high | tags:example | created:2026-07-29") {
		t.Errorf("item not rewritten:\n%s", out)
	}
	if n := countDiffLines(sampleBacklog, out); n != 1 {
		t.Errorf("want 1 changed line, got %d", n)
	}
}

func TestRemoveItem(t *testing.T) {
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	ready := b.Section(SectionReady)
	e.RemoveItem(ready.Items[0])

	out := string(e.Bytes())
	if strings.Contains(out, "T-0001") {
		t.Error("item still present")
	}
	// The rest of the file is intact and the surviving item is still locatable.
	if !strings.Contains(out, "T-0005") || !strings.Contains(out, "## Someday") {
		t.Errorf("removal damaged the file:\n%s", out)
	}
	if got := e.lines[ready.Items[1].Source.Line-1]; !strings.Contains(got, "T-0005") {
		t.Errorf("surviving item line went stale: %q", got)
	}
}

func TestInsertItemPositions(t *testing.T) {
	mk := func(id ID, title string) *Item {
		return &Item{ID: id, Title: title, Prio: PrioMed, Created: Date{2026, 7, 29}}
	}

	// Top of a populated section.
	b, _ := parseBacklog("backlog.md", []byte(sampleBacklog))
	e := b.Edit()
	b.InsertItem(e, SectionReady, 0, mk("T-0009", "Top"))
	ready := b.Section(SectionReady)
	if ready.Items[0].ID != "T-0009" || ready.Items[1].ID != "T-0001" {
		t.Errorf("insert at 0 wrong: %v", idsOf(ready.Items))
	}
	if ready.Items[0].Pos != 1 || ready.Items[2].Pos != 3 {
		t.Error("positions not renumbered")
	}
	assertLineIs(t, e, ready.Items[0].Source.Line, "T-0009")
	assertLineIs(t, e, ready.Items[1].Source.Line, "T-0001")

	// Bottom of a populated section - the default for --add.
	b, _ = parseBacklog("backlog.md", []byte(sampleBacklog))
	e = b.Edit()
	b.InsertItem(e, SectionReady, 2, mk("T-0009", "Bottom"))
	ready = b.Section(SectionReady)
	if ready.Items[2].ID != "T-0009" {
		t.Errorf("insert at end wrong: %v", idsOf(ready.Items))
	}
	assertLineIs(t, e, ready.Items[2].Source.Line, "T-0009")
	// It must land inside Ready, above the Blocked heading.
	out := strings.Split(string(e.Bytes()), "\n")
	blocked := b.Section(SectionBlocked).HeadLine
	if ready.Items[2].Source.Line >= blocked {
		t.Errorf("item leaked past the section boundary (line %d, ## Blocked at %d)\n%s",
			ready.Items[2].Source.Line, blocked, strings.Join(out, "\n"))
	}

	// Into an empty section.
	src := strings.Replace(sampleBacklog,
		"## Someday\n\n- [ ] [T-0003] Maybe | prio:low | created:2026-07-29\n", "## Someday\n", 1)
	b, _ = parseBacklog("backlog.md", []byte(src))
	e = b.Edit()
	b.InsertItem(e, SectionSomeday, 0, mk("T-0009", "Only"))
	someday := b.Section(SectionSomeday)
	if len(someday.Items) != 1 {
		t.Fatalf("want 1 item, got %d", len(someday.Items))
	}
	assertLineIs(t, e, someday.Items[0].Source.Line, "T-0009")
}

func TestDoneInsertIntoExistingMonth(t *testing.T) {
	d, _ := parseDone("done.md", []byte(sampleDone))
	e := d.Edit()
	it := &Item{ID: "T-0011", Title: "Newest of all", State: StateDone,
		Done: Date{2026, 7, 29}, Outcome: OutcomeShipped}
	d.InsertItem(e, "2026-07", it)

	m := d.Month("2026-07")
	if m.Items[0].ID != "T-0011" {
		t.Errorf("newest should go first: %v", idsOf(m.Items))
	}
	assertLineIs(t, e, m.Items[0].Source.Line, "T-0011")
	assertLineIs(t, e, m.Items[1].Source.Line, "T-0010")
	if !strings.Contains(string(e.Bytes()), "- [x] [T-0011]") {
		t.Error("closed box not rendered")
	}
}

func TestDoneCreatesNewestMonthGroup(t *testing.T) {
	d, _ := parseDone("done.md", []byte(sampleDone))
	e := d.Edit()
	it := &Item{ID: "T-0011", Title: "Next month", State: StateDone,
		Done: Date{2026, 8, 3}, Outcome: OutcomeShipped}
	d.InsertItem(e, "2026-08", it)

	out := string(e.Bytes())
	// Groups descend: the new one goes above 2026-07.
	i8, i7, i6 := strings.Index(out, "## 2026-08"), strings.Index(out, "## 2026-07"), strings.Index(out, "## 2026-06")
	if i8 < 0 || !(i8 < i7 && i7 < i6) {
		t.Errorf("month groups out of order (8:%d 7:%d 6:%d)\n%s", i8, i7, i6, out)
	}
	if d.Months[0].Month != "2026-08" {
		t.Errorf("model order wrong: %v", monthsOf(d))
	}
	assertLineIs(t, e, d.Month("2026-08").Items[0].Source.Line, "T-0011")
	assertLineIs(t, e, d.Month("2026-07").Items[0].Source.Line, "T-0010")
}

func TestDoneCreatesOldestMonthGroup(t *testing.T) {
	d, _ := parseDone("done.md", []byte(sampleDone))
	e := d.Edit()
	it := &Item{ID: "T-0007", Title: "Ancient", State: StateDone,
		Done: Date{2026, 5, 4}, Outcome: OutcomeShipped}
	d.InsertItem(e, "2026-05", it)

	out := string(e.Bytes())
	if strings.Index(out, "## 2026-06") > strings.Index(out, "## 2026-05") {
		t.Errorf("oldest group should go last:\n%s", out)
	}
	assertLineIs(t, e, d.Month("2026-05").Items[0].Source.Line, "T-0007")
}

func TestDoneCreatesFirstMonthGroup(t *testing.T) {
	src := "---\ndoc: done\nversion: 1\n---\n\n# Done\n\nSome prose.\n"
	d, _ := parseDone("done.md", []byte(src))
	e := d.Edit()
	it := &Item{ID: "T-0001", Title: "First ever", State: StateDone,
		Done: Date{2026, 7, 29}, Outcome: OutcomeShipped}
	d.InsertItem(e, "2026-07", it)

	out := string(e.Bytes())
	if !strings.Contains(out, "## 2026-07") || !strings.Contains(out, "- [x] [T-0001]") {
		t.Errorf("group or item missing:\n%s", out)
	}
	if !strings.Contains(out, "Some prose.") {
		t.Error("existing content lost")
	}
	assertLineIs(t, e, d.Month("2026-07").Items[0].Source.Line, "T-0001")
}

// The whole repository, again: every real file must survive a parse-and-write
// with identical bytes. This is the cheapest guard against a regression in any
// of the above, because it uses data nobody wrote for a test.
func TestRoundTripRealRepositoryFiles(t *testing.T) {
	dirs := []string{
		"../../../sample1/micro-manager", "../../../sample2/micro-manager",
		"../../../hidden/.micro-manager", "../../../symbol/µmanager",
		"../../../symbol-hidden/.µmanager", "../micro-manager",
	}
	n := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Skipf("repository fixtures not present: %v", err)
		}
		for _, ent := range entries {
			name := ent.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("%s/%s: %v", dir, name, err)
			}
			var e *fileEdit
			switch {
			case name == "backlog.md":
				b, _ := parseBacklog(name, data)
				e = b.Edit()
			case name == "done.md":
				d, _ := parseDone(name, data)
				e = d.Edit()
			case strings.HasPrefix(name, "working."):
				w, _ := parseWorking(name, data)
				e = w.Edit()
			default:
				continue // structure.md and friends are not parsed
			}
			if got := string(e.Bytes()); got != string(data) {
				t.Errorf("%s/%s: round trip is not byte-exact", dir, name)
			}
			n++
		}
	}
	t.Logf("round-tripped %d real files byte-for-byte", n)
}

// ---------------------------------------------------------------------------

func assertLineIs(t *testing.T, e *fileEdit, line int, want string) {
	t.Helper()
	if line < 1 || line > len(e.lines) {
		t.Fatalf("line %d out of range (file has %d lines)", line, len(e.lines))
	}
	if !strings.Contains(e.lines[line-1], want) {
		t.Errorf("line %d = %q, want it to contain %q", line, e.lines[line-1], want)
	}
}

func countDiffLines(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	n := 0
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] != bl[i] {
			n++
		}
	}
	return n + abs(len(al)-len(bl))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func idsOf(items []*Item) []ID {
	out := make([]ID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func monthsOf(d *doneFile) []string {
	out := make([]string, len(d.Months))
	for i, m := range d.Months {
		out[i] = m.Month
	}
	return out
}
