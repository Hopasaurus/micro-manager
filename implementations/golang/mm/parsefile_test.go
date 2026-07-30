package mm

import (
	"strings"
	"testing"
)

const sampleBacklog = `---
doc: backlog
version: 1
project: Sample One
next_id: T-0004
updated: 2026-07-29
---

# Backlog

Prose that must be ignored.

## Ready

- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29
- [ ] [T-0005] Second | prio:high | created:2026-07-29

## Blocked

- [ ] [T-0002] Waiting | prio:low | created:2026-07-29 | blocked:on a thing

## Someday

- [ ] [T-0003] Maybe | prio:low | created:2026-07-29
`

const sampleDone = `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [T-0010] Newest | created:2026-07-01 | done:2026-07-23 | outcome:shipped
- [x] [T-0009] Older | created:2026-06-01 | done:2026-07-02 | outcome:cancelled

## 2026-06

<!-- a comment -->

- [x] [T-0008] Last month | created:2026-05-01 | done:2026-06-30 | outcome:obsolete
`

func TestParseBacklogStructure(t *testing.T) {
	b, vs := parseBacklog("backlog.md", []byte(sampleBacklog))
	if len(vs) != 0 {
		t.Fatalf("clean file produced violations: %v", vs)
	}
	if b.FM.Get("project") != "Sample One" || b.FM.Get("next_id") != "T-0004" {
		t.Error("frontmatter not read")
	}
	if len(b.Sections) != 3 {
		t.Fatalf("want 3 sections, got %d", len(b.Sections))
	}
	for i, want := range Sections() {
		if b.Sections[i].Name != want {
			t.Errorf("section %d = %q, want %q", i, b.Sections[i].Name, want)
		}
	}
	if len(b.Items) != 4 {
		t.Fatalf("want 4 items, got %d", len(b.Items))
	}

	ready := b.Section(SectionReady)
	if len(ready.Items) != 2 {
		t.Fatalf("Ready should hold 2 items, got %d", len(ready.Items))
	}
	// Position is 1-based within the section and is what ordering assertions use.
	if ready.Items[0].ID != "T-0001" || ready.Items[0].Pos != 1 {
		t.Errorf("first Ready item = %s pos %d", ready.Items[0].ID, ready.Items[0].Pos)
	}
	if ready.Items[1].ID != "T-0005" || ready.Items[1].Pos != 2 {
		t.Errorf("second Ready item = %s pos %d", ready.Items[1].ID, ready.Items[1].Pos)
	}
	for _, it := range b.Items {
		if it.State != StateBacklog {
			t.Errorf("%s state = %q", it.ID, it.State)
		}
	}
	if b.Section(SectionBlocked).Items[0].Blocked != "on a thing" {
		t.Error("blocked field lost")
	}
	if b.Section(SectionSomeday).Items[0].ID != "T-0003" {
		t.Error("Someday item wrong")
	}
}

func TestParseDoneStructure(t *testing.T) {
	d, vs := parseDone("done.md", []byte(sampleDone))
	if len(vs) != 0 {
		t.Fatalf("clean file produced violations: %v", vs)
	}
	if len(d.Months) != 2 {
		t.Fatalf("want 2 month groups, got %d", len(d.Months))
	}
	if d.Months[0].Month != "2026-07" || d.Months[1].Month != "2026-06" {
		t.Errorf("months = %q, %q", d.Months[0].Month, d.Months[1].Month)
	}
	if len(d.Months[0].Items) != 2 || len(d.Months[1].Items) != 1 {
		t.Errorf("item counts = %d, %d", len(d.Months[0].Items), len(d.Months[1].Items))
	}
	if d.Month("2026-07").Items[0].ID != "T-0010" {
		t.Error("newest item should come first within a group")
	}
	if m := d.Month("2026-05"); m != nil {
		t.Error("Month() should return nil for an absent group")
	}
	for _, it := range d.Items {
		if it.State != StateDone {
			t.Errorf("%s state = %q", it.ID, it.State)
		}
		if !it.Closed() {
			t.Errorf("%s should render with a checked box", it.ID)
		}
	}
	// An HTML comment between the heading and the items must not break grouping.
	if d.Months[1].Items[0].ID != "T-0008" {
		t.Error("comment line disturbed grouping")
	}
}

// The property spec-tools.md §8 demands: a broken file still parses, and its
// problems come back as findings rather than as a refusal to read.
func TestParsingIsPermissive(t *testing.T) {
	src := `---
doc: backlog
version: 1
project: Broken
next_id: T-0004
---

## Ready

- [ ] [T-0001] Fine | prio:med
- [ ] T-0002 missing brackets
- [ ] [T-0003] Bad prio | prio:URGENT

## Invented Heading

- [ ] [T-0007] Under an unknown heading
`
	b, vs := parseBacklog("backlog.md", []byte(src))
	if len(vs) != 3 {
		t.Fatalf("want 3 violations, got %d: %v", len(vs), vs)
	}
	// Parsing continued past every one of them.
	if len(b.Items) != 2 {
		t.Fatalf("readable items should survive, got %d", len(b.Items))
	}
	if b.Items[0].ID != "T-0001" || b.Items[1].ID != "T-0007" {
		t.Errorf("wrong items survived: %v", b.Items)
	}
	// An item under an unrecognised heading is still found, with no section.
	if b.Items[1].Section != SectionNone {
		t.Errorf("section should be none, got %q", b.Items[1].Section)
	}
	msgs := violationMessages(vs)
	for _, want := range []string{"malformed item line", "prio:URGENT", "unknown section heading"} {
		if !strings.Contains(msgs, want) {
			t.Errorf("missing %q in:\n%s", want, msgs)
		}
	}
}

func TestParseBacklogMissingFrontmatter(t *testing.T) {
	src := "# Backlog\n\n## Ready\n\n- [ ] [T-0001] Still found\n"
	b, vs := parseBacklog("backlog.md", []byte(src))
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "must open with YAML frontmatter") {
		t.Fatalf("want one frontmatter violation, got %v", vs)
	}
	// Reported, then treated as all body - the item is still located.
	if len(b.Items) != 1 || b.Items[0].ID != "T-0001" {
		t.Errorf("item should still be found, got %v", b.Items)
	}
}

func TestParseBacklogUnterminatedFrontmatter(t *testing.T) {
	src := "---\ndoc: backlog\n\n## Ready\n\n- [ ] [T-0001] Unreachable\n"
	b, vs := parseBacklog("backlog.md", []byte(src))
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "not terminated") {
		t.Fatalf("want one termination violation, got %v", vs)
	}
	// There is no body to walk: everything is inside an unclosed header.
	if len(b.Items) != 0 {
		t.Errorf("no body should be parsed, got %v", b.Items)
	}
}

func TestParseDoneBadHeading(t *testing.T) {
	src := `---
doc: done
version: 1
---

# Done

## July 2026

- [x] [T-0001] Under a bad heading | done:2026-07-01 | outcome:shipped
`
	d, vs := parseDone("done.md", []byte(src))
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "heading must be YYYY-MM") {
		t.Fatalf("want one heading violation, got %v", vs)
	}
	if len(d.Months) != 1 || d.Months[0].Month != "" {
		t.Error("an invalid heading should still open a group, with no month")
	}
	if len(d.Items) != 1 {
		t.Error("the item should still be found")
	}
}

func TestIsMonth(t *testing.T) {
	for _, ok := range []string{"2026-07", "0000-00", "9999-99"} {
		if !isMonth(ok) {
			t.Errorf("%q should be a month", ok)
		}
	}
	for _, bad := range []string{"", "2026-7", "26-07", "2026-07-01", "2026/07", "July", "2026-0x"} {
		if isMonth(bad) {
			t.Errorf("%q should not be a month", bad)
		}
	}
}

// Lines are retained verbatim so an untouched file writes back byte for byte.
// T-0008 depends on this; asserting it here catches a regression at its source.
func TestLinesRoundTrip(t *testing.T) {
	for name, src := range map[string]string{"backlog.md": sampleBacklog, "done.md": sampleDone} {
		var lines []string
		if name == "backlog.md" {
			b, _ := parseBacklog(name, []byte(src))
			lines = b.Lines
		} else {
			d, _ := parseDone(name, []byte(src))
			lines = d.Lines
		}
		if got := strings.Join(lines, "\n"); got != src {
			t.Errorf("%s: lines do not reproduce the file", name)
		}
	}
}

// A "- [ ]" line in a working file is a subtask, not an item. The backlog and
// done parsers are the only callers of parseItemLine, which is what keeps
// subtasks from being read as phantom items.
func TestSubtaskLinesAreNotItems(t *testing.T) {
	src := "---\ndoc: backlog\n---\n\n## Ready\n\n- [ ] a subtask-looking line with no id\n"
	b, vs := parseBacklog("backlog.md", []byte(src))
	if len(b.Items) != 0 {
		t.Error("a line with no ID must not become an item")
	}
	// It looks like an item line, so it is reported rather than ignored: in
	// backlog.md that shape is always a mistake.
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "malformed item line") {
		t.Errorf("want a malformed-line violation, got %v", vs)
	}
}

func violationMessages(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		b.WriteString(v.String())
		b.WriteString("\n")
	}
	return b.String()
}
