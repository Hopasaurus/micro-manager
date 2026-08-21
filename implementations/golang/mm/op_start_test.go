package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const startBacklog = `---
doc: backlog
version: 1
project: Start Tests
next_id: T-0011
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-0001] Fix the deploy script | prio:high | tags:infra,ci | detail:details/T-0001.md | created:2026-07-20 | owner:dana
- [ ] [T-0002] Second | prio:med | created:2026-07-21
- [ ] [T-0004] Reserved field | created:2026-07-23 | status:draft

## Blocked

- [ ] [T-0003] Waiting | prio:low | created:2026-07-22 | blocked:on the vendor

## Someday
`

const startDone = `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0009] Already closed | created:2026-07-01 | done:2026-07-10 | outcome:shipped
`

const startDetail = `---
doc: detail
id: T-0001
title: Fix the deploy script
updated: 2026-07-20
---

# T-0001 — Fix the deploy script

## Context

Deploys fail on a cold cache.
`

// startDir has TWO slots, so the difference between "lowest idle slot" and "any
// idle slot" is observable and the WIP limit can be reached without a fixture
// that is degenerate at one file.
func startDir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newDir(t, map[string]string{
		"backlog.md":           startBacklog,
		"done.md":              startDone,
		"working.01.md":        idleSlot,
		"working.02.md":        idleSlot,
		"details/T-0001.md":    startDetail,
		"details/_template.md": "---\ndoc: detail\n---\n\n## Context\n",
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestStartCopiesEveryFieldIntoTheSlot(t *testing.T) {
	dir, s := startDir(t)

	it, res, err := testStartV1(s, "T-0001", StartRequest{}, today)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if it.State != StateWorking || it.Slot != 1 {
		t.Errorf("state/slot = %s/%d, want working/1", it.State, it.Slot)
	}
	if it.Started != today {
		t.Errorf("started = %s, want %s", it.Started, today)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeMoved {
		t.Errorf("changes = %+v", res.Changes)
	}

	got := readFile(t, dir, "working.01.md")
	for _, want := range []string{
		"status: working",
		"id: T-0001",
		"title: Fix the deploy script",
		"prio: high",
		"tags: infra,ci", // a TAGLIST here exactly as on the line, not a YAML list
		"detail: details/T-0001.md",
		"created: 2026-07-20", // carried over, NOT reset to today
		"started: 2026-07-29",
		"owner: dana", // unregistered, and it must survive the move
	} {
		if !strings.Contains(got, want) {
			t.Errorf("working.01.md is missing %q:\n%s", want, got)
		}
	}

	// The task section is seeded; the other three stay empty.
	task := sectionText(got, "## Task")
	if !strings.Contains(task, "Fix the deploy script") {
		t.Errorf("## Task not seeded with the title:\n%s", task)
	}
	if !strings.Contains(task, "details/T-0001.md") {
		t.Errorf("## Task has no link to the detail file:\n%s", task)
	}
	if plan := sectionText(got, "## Plan"); strings.TrimSpace(plan) != "" {
		t.Errorf("## Plan should be left empty, got:\n%s", plan)
	}

	if b := readFile(t, dir, "backlog.md"); strings.Contains(b, "[T-0001]") {
		t.Error("the backlog line should be gone")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after start:\n%s", violationMessages(vs))
	}

	// Read back through the parser: the unregistered field is an Item field
	// again, which is what makes --pause able to put it back on the line.
	back, err := s.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Extra) != 1 || back.Extra[0] != (Field{"owner", "dana"}) {
		t.Errorf("extra = %+v, want owner:dana", back.Extra)
	}
	if back.Prio != PrioHigh || FormatTags(back.Tags) != "infra,ci" {
		t.Errorf("prio/tags = %q/%v", back.Prio, back.Tags)
	}
}

func TestStartUsesLowestIdleSlot(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStartV1(s, "T-0002", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, dir, "working.01.md"); !strings.Contains(got, "id: T-0001") {
		t.Error("first start should take slot 01")
	}
	if got := readFile(t, dir, "working.02.md"); !strings.Contains(got, "id: T-0002") {
		t.Error("second start should take slot 02")
	}
}

func TestStartExplicitSlot(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := testStartV1(s, "T-0001", StartRequest{Slot: 2}, today); err != nil {
		t.Fatalf("start into slot 2: %v", err)
	}
	if got := readFile(t, dir, "working.02.md"); !strings.Contains(got, "id: T-0001") {
		t.Error("item should be in slot 02")
	}
	if got := readFile(t, dir, "working.01.md"); !strings.Contains(got, "status: idle") {
		t.Error("slot 01 should still be idle")
	}

	// Occupied, and nonexistent, are both guard failures rather than conflicts:
	// the caller asked for something specific and did not get it.
	_, _, err := testStartV1(s, "T-0002", StartRequest{Slot: 2}, today)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("occupied slot: want ErrPreconditionFailed, got %v", err)
	}
	_, _, err = testStartV1(s, "T-0002", StartRequest{Slot: 7}, today)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("missing slot: want ErrPreconditionFailed, got %v", err)
	}
}

// TestStartAtTheWipLimit is the point of the operation: the limit is the file
// count, so the only way to honour it is to refuse.
func TestStartAtTheWipLimit(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStartV1(s, "T-0002", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	beforeBacklog := readFile(t, dir, "backlog.md")

	_, _, err := testStartV1(s, "T-0003", StartRequest{}, today)
	if !errors.Is(err, ErrWipLimitReached) {
		t.Fatalf("want ErrWipLimitReached, got %v", err)
	}
	we, ok := AsWipLimitError(err)
	if !ok {
		t.Fatal("error should carry the occupant list")
	}
	if we.Limit != 2 {
		t.Errorf("limit = %d, want 2", we.Limit)
	}
	occupied := 0
	for _, sl := range we.Occupants {
		if sl.Occupied() {
			occupied++
		}
	}
	if occupied != 2 {
		t.Errorf("occupants = %d, want 2", occupied)
	}

	// No third file, and nothing else touched. Creating a slot to make room
	// would raise the WIP limit, which is the one thing it exists to prevent.
	if _, err := os.Stat(filepath.Join(dir, "working.03.md")); !os.IsNotExist(err) {
		t.Error("a new working file was created to make room")
	}
	if readFile(t, dir, "backlog.md") != beforeBacklog {
		t.Error("backlog.md was modified by a failed start")
	}
}

func TestStartFromBlockedMovesTheReasonToBlockers(t *testing.T) {
	dir, s := startDir(t)

	it, _, err := testStartV1(s, "T-0003", StartRequest{}, today)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if it.Blocked != "" {
		t.Errorf("blocked should not travel into a slot, got %q", it.Blocked)
	}
	got := readFile(t, dir, "working.01.md")
	if strings.Contains(got, "blocked:") || strings.Contains(got, "blocked: ") {
		t.Errorf("blocked must not appear in slot frontmatter:\n%s", got)
	}
	if b := sectionText(got, "## Blockers"); !strings.Contains(b, "on the vendor") {
		t.Errorf("the reason should be preserved under ## Blockers, got:\n%s", b)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestStartConflicts(t *testing.T) {
	_, s := startDir(t)

	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("already working: want ErrConflict, got %v", err)
	}
	if _, _, err := testStartV1(s, "T-0009", StartRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("done item: want ErrConflict, got %v", err)
	}
	if _, _, err := testStartV1(s, "T-0099", StartRequest{}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: want ErrNotFound, got %v", err)
	}
}

// A reserved key cannot be represented in a slot's frontmatter, and silently
// dropping it would destroy the field this path exists to preserve.
func TestStartRefusesAReservedExtraField(t *testing.T) {
	_, s := startDir(t)
	_, _, err := testStartV1(s, "T-0004", StartRequest{}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

func TestStartDryRun(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "backlog.md")
	beforeSlot := readFile(t, dir, "working.01.md")

	_, res, err := testStartV1(s, "T-0001", StartRequest{DryRun: true}, today)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(res.Files) != 2 {
		t.Errorf("dry run should report both files, got %v", res.Files)
	}
	if readFile(t, dir, "backlog.md") != before ||
		readFile(t, dir, "working.01.md") != beforeSlot {
		t.Error("dry run wrote to disk")
	}
}

// sectionText returns the body of a "## Heading" section of a markdown file.
func sectionText(file, heading string) string {
	lines := strings.Split(file, "\n")
	out := []string{}
	in := false
	for _, l := range lines {
		if strings.HasPrefix(l, "## ") {
			if in {
				break
			}
			in = strings.TrimSpace(l) == heading
			continue
		}
		if in {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
