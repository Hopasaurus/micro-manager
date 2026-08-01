package mm

import (
	"strings"
	"testing"
)

const idleSlot = `---
doc: working
version: 1
status: idle
id: null
title: null
prio: null
tags: null
detail: null
created: null
started: null
---

# Working

## Task

## Plan

## Notes

## Blockers
`

const busySlot = `---
doc: working
version: 1
status: working
id: T-0042
title: Fix the deploy script
prio: high
tags: infra,ci
detail: details/T-0042.md
created: 2026-07-29
started: 2026-07-30
---

# Working

## Task

See [details/T-0042.md](details/T-0042.md).

## Plan

- [ ] a subtask, which has no ID
- [x] a finished subtask

## Notes

## Blockers
`

func TestIsWorkingFileName(t *testing.T) {
	cases := []struct {
		name       string
		num, width int
		ok         bool
	}{
		{"working.01.md", 1, 2, true},
		{"working.1.md", 1, 1, true},
		{"working.003.md", 3, 3, true},
		{"working.12.md", 12, 2, true},
		{"working.md", 0, 0, false},
		{"working..md", 0, 0, false},
		{"working.0a.md", 0, 0, false},
		{"backlog.md", 0, 0, false},
		{"working.01.txt", 0, 0, false},
	}
	for _, c := range cases {
		num, width, ok := isWorkingFileName(c.name)
		if ok != c.ok || (ok && (num != c.num || width != c.width)) {
			t.Errorf("isWorkingFileName(%q) = %d,%d,%v want %d,%d,%v",
				c.name, num, width, ok, c.num, c.width, c.ok)
		}
	}
	if workingFileName(1, 2) != "working.01.md" || workingFileName(12, 3) != "working.012.md" {
		t.Error("workingFileName is wrong")
	}
}

func TestDiscoverWorkingFilesClean(t *testing.T) {
	names, nums, widths, vs := discoverWorkingFiles(
		[]string{"backlog.md", "working.02.md", "done.md", "working.01.md", "working.03.md"}, "dir")
	if len(vs) != 0 {
		t.Fatalf("clean set produced violations: %v", vs)
	}
	// Sorted by slot number regardless of listing order.
	if strings.Join(names, ",") != "working.01.md,working.02.md,working.03.md" {
		t.Errorf("names = %v", names)
	}
	if len(nums) != 3 || nums[0] != 1 || nums[2] != 3 {
		t.Errorf("nums = %v", nums)
	}
	if widths[0] != 2 {
		t.Errorf("widths = %v", widths)
	}
}

// I10, and the reason it matters: working.1.md and working.01.md would both
// claim slot 1, so the file count would stop being the number of slots.
func TestDiscoverWorkingFilesMixedWidths(t *testing.T) {
	_, _, _, vs := discoverWorkingFiles([]string{"working.1.md", "working.02.md"}, "dir")
	if !containsMessage(vs, "mix digit widths") {
		t.Errorf("want a mixed-width violation, got %v", vs)
	}
}

func TestDiscoverWorkingFilesGap(t *testing.T) {
	_, _, _, vs := discoverWorkingFiles([]string{"working.01.md", "working.03.md"}, "dir")
	if !containsMessage(vs, "not numbered 1..2 (missing: 2)") {
		t.Errorf("want a numbering violation, got %v", vs)
	}
}

func TestDiscoverWorkingFilesNoneAndLegacy(t *testing.T) {
	_, _, _, vs := discoverWorkingFiles([]string{"backlog.md", "done.md"}, "dir")
	if !containsMessage(vs, "no working.NN.md file") {
		t.Errorf("want a missing-file violation, got %v", vs)
	}
	_, _, _, vs = discoverWorkingFiles([]string{"working.md", "working.01.md"}, "dir")
	if !containsMessage(vs, "legacy name") {
		t.Errorf("want a legacy-name violation, got %v", vs)
	}
}

func TestDiscoverWorkingFilesUnrecognised(t *testing.T) {
	_, _, _, vs := discoverWorkingFiles([]string{"working.01.md", "working.draft.md"}, "dir")
	if !containsMessage(vs, "not a working file") {
		t.Errorf("want an unrecognised-name violation, got %v", vs)
	}
}

func TestParseWorkingIdle(t *testing.T) {
	w, vs := parseWorking("working.01.md", []byte(idleSlot))
	if len(vs) != 0 {
		t.Fatalf("clean idle slot produced violations: %v", vs)
	}
	if !w.Idle() || w.Item != nil {
		t.Error("slot should be idle")
	}
	if w.Number != 1 || w.Width != 2 {
		t.Errorf("number/width = %d/%d", w.Number, w.Width)
	}
}

func TestParseWorkingBusy(t *testing.T) {
	w, vs := parseWorking("working.01.md", []byte(busySlot))
	if len(vs) != 0 {
		t.Fatalf("clean busy slot produced violations: %v", vs)
	}
	it := w.Item
	if it == nil {
		t.Fatal("slot should hold an item")
	}
	if it.ID != "T-0042" || it.Title != "Fix the deploy script" {
		t.Errorf("got %s %q", it.ID, it.Title)
	}
	if it.Prio != PrioHigh || FormatTags(it.Tags) != "infra,ci" {
		t.Errorf("prio/tags = %q %v", it.Prio, it.Tags)
	}
	if it.Detail != "details/T-0042.md" {
		t.Errorf("detail = %q", it.Detail)
	}
	if it.Created.String() != "2026-07-29" || it.Started.String() != "2026-07-30" {
		t.Errorf("dates = %q %q", it.Created, it.Started)
	}
	if it.State != StateWorking || it.Slot != 1 {
		t.Errorf("state/slot = %q/%d", it.State, it.Slot)
	}
}

// The rule that keeps phantom items out of the model.
func TestSubtasksAreNotItems(t *testing.T) {
	w, _ := parseWorking("working.01.md", []byte(busySlot))
	// The body holds two "- [" lines. Exactly one item exists: the one in the
	// frontmatter.
	if w.Item == nil || w.Item.ID != "T-0042" {
		t.Fatal("frontmatter item missing")
	}
	if !strings.Contains(strings.Join(w.Lines, "\n"), "- [ ] a subtask") {
		t.Fatal("fixture should contain subtask lines")
	}
	// Nothing in the parser reads them, so there is nothing else to assert
	// beyond: the parse produced no extra items and no violations about them.
	if _, vs := parseWorking("working.01.md", []byte(busySlot)); len(vs) != 0 {
		t.Errorf("subtask lines should be ignored entirely, got %v", vs)
	}
}

func TestParseWorkingIdleWithLeftovers(t *testing.T) {
	src := strings.Replace(idleSlot, "id: null", "id: T-0002", 1)
	src = strings.Replace(src, "started: null", "started: 2026-07-02", 1)
	_, vs := parseWorking("working.01.md", []byte(src))
	if len(vs) != 2 {
		t.Fatalf("want 2 violations, got %d: %v", len(vs), vs)
	}
	if !containsMessage(vs, "status is idle but id is T-0002") ||
		!containsMessage(vs, "status is idle but started is 2026-07-02") {
		t.Errorf("got %v", vs)
	}
	// Each violation points at the offending line, not at the file.
	for _, v := range vs {
		if v.At.Line == 0 {
			t.Errorf("violation should be located: %v", v)
		}
	}
}

func TestParseWorkingRequiredFields(t *testing.T) {
	cases := []struct{ replace, with, want string }{
		{"id: T-0042", "id: null", "status is working but id is null"},
		{"id: T-0042", "id: T-42", "id is not a T-#### id"},
		{"title: Fix the deploy script", "title: null", "status is working but title is null"},
		{"started: 2026-07-30", "started: null", "started:null"},
		{"started: 2026-07-30", "started: soon", "started:soon"},
		{"prio: high", "prio: URGENT", "prio:URGENT"},
		{"tags: infra,ci", "tags: [infra, ci]", "malformed tags"},
		{"created: 2026-07-29", "created: 29-07-2026", "created:29-07-2026"},
	}
	for _, c := range cases {
		src := strings.Replace(busySlot, c.replace, c.with, 1)
		_, vs := parseWorking("working.01.md", []byte(src))
		if !containsMessage(vs, c.want) {
			t.Errorf("replacing %q with %q: want %q, got %v", c.replace, c.with, c.want, vs)
		}
	}
}

func TestParseWorkingBadStatus(t *testing.T) {
	src := strings.Replace(idleSlot, "status: idle", "status: paused", 1)
	_, vs := parseWorking("working.01.md", []byte(src))
	if len(vs) != 1 || !containsMessage(vs, "status must be working or idle, got: paused") {
		t.Errorf("got %v", vs)
	}
}

// Moving an item between files must be a copy, never a conversion: the working
// frontmatter uses the same lexical forms as an item line.
func TestRenderWorkingItemUsesItemLineForms(t *testing.T) {
	it := &Item{
		ID: "T-0042", Title: "Fix the deploy script", Prio: PrioHigh,
		Tags: []string{"infra", "ci"}, Detail: "details/T-0042.md",
		Created: Date{2026, 7, 29}, Started: Date{2026, 7, 30},
	}
	fm := NewFrontmatter()
	renderWorkingItem(fm, it)

	if got := fm.Get("tags"); got != "infra,ci" {
		t.Errorf("tags must be a TAGLIST, not a YAML list; got %q", got)
	}
	if got := fm.Get("prio"); got != "high" {
		t.Errorf("prio = %q", got)
	}
	if got := fm.Get("started"); got != "2026-07-30" {
		t.Errorf("started = %q", got)
	}

	// Round-trip through the parser: what we wrote is what comes back.
	src := fm.Render() + "\n# Working\n"
	w, vs := parseWorking("working.01.md", []byte(src))
	if len(vs) != 0 {
		t.Fatalf("rendered slot did not parse cleanly: %v", vs)
	}
	got := w.Item
	if got.ID != it.ID || got.Title != it.Title || got.Prio != it.Prio ||
		FormatTags(got.Tags) != FormatTags(it.Tags) || got.Detail != it.Detail ||
		got.Created != it.Created || got.Started != it.Started {
		t.Errorf("round trip lost data:\n got %+v\nwant %+v", got, it)
	}
}

// An absent field becomes null, which is what an absent field means on an item
// line - not an empty string, which would fail validation on the way back in.
func TestRenderWorkingItemNullsAbsentFields(t *testing.T) {
	fm := NewFrontmatter()
	renderWorkingItem(fm, &Item{ID: "T-0001", Title: "Bare", Started: Date{2026, 7, 29}})
	for _, key := range []string{"prio", "tags", "detail", "created"} {
		if fm.Get(key) != "null" {
			t.Errorf("%s = %q, want null", key, fm.Get(key))
		}
	}
	_, vs := parseWorking("working.01.md", []byte(fm.Render()))
	if len(vs) != 0 {
		t.Errorf("a minimal item should render to a valid slot, got %v", vs)
	}
}

func TestRenderWorkingIdle(t *testing.T) {
	fm := NewFrontmatter()
	fm.Set("doc", "working")
	fm.Set("version", "1")
	renderWorkingIdle(fm)
	w, vs := parseWorking("working.01.md", []byte(fm.Render()))
	if len(vs) != 0 || !w.Idle() {
		t.Errorf("rendered idle slot should parse clean and idle, got %v", vs)
	}
}

func containsMessage(vs []Violation, want string) bool {
	for _, v := range vs {
		if strings.Contains(v.Message, want) {
			return true
		}
	}
	return false
}
