package mm

import (
	"errors"
	"strings"
	"testing"
)

// The bulk add of spec-tools.md §5.2.1.
//
// Two properties carry the operation and are what these tests are mostly about:
// the batch is ONE transaction (a bad line writes nothing at all), and the
// input's order survives, including under Top.

const addManyBacklog = `---
doc: backlog
version: 1
project: Bulk Tests
next_id: T-0003
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-0001] One | prio:high | created:2026-07-29
- [ ] [T-0002] Two | created:2026-07-29

## Blocked

## Someday
`

func addManyDir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newDir(t, map[string]string{
		"backlog.md": addManyBacklog,
		"done.md":    "---\ndoc: done\nversion: 1\n---\n\n# Done\n",
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

func TestParseAddLine(t *testing.T) {
	day := Date{2026, 8, 6}

	cases := []struct {
		name  string
		line  string
		check func(t *testing.T, req AddRequest)
	}{
		{"a bare title", "Fix the deploy script", func(t *testing.T, req AddRequest) {
			if req.Title != "Fix the deploy script" {
				t.Fatalf("title = %q", req.Title)
			}
		}},
		{"fields", "Fix it | prio:high | tags:infra,ci", func(t *testing.T, req AddRequest) {
			if req.Prio != PrioHigh {
				t.Fatalf("prio = %q", req.Prio)
			}
			if strings.Join(req.Tags, ",") != "infra,ci" {
				t.Fatalf("tags = %v", req.Tags)
			}
		}},
		// A checklist pasted out of a document is valid input as it stands.
		{"a markdown bullet", "- Fix it | prio:low", func(t *testing.T, req AddRequest) {
			if req.Title != "Fix it" {
				t.Fatalf("title = %q", req.Title)
			}
		}},
		{"a checkbox bullet", "- [ ] Fix it", func(t *testing.T, req AddRequest) {
			if req.Title != "Fix it" {
				t.Fatalf("title = %q", req.Title)
			}
		}},
		{"a star bullet", "* Fix it", func(t *testing.T, req AddRequest) {
			if req.Title != "Fix it" {
				t.Fatalf("title = %q", req.Title)
			}
		}},
		// The extension point does not stop at the boundary of a bulk add.
		{"an unregistered field", "Fix it | owner:dana", func(t *testing.T, req AddRequest) {
			if len(req.Extra) != 1 || req.Extra[0].Key != "owner" || req.Extra[0].Value != "dana" {
				t.Fatalf("extra = %+v", req.Extra)
			}
		}},
		// §5.1.2's implication, applied per line: a reason means Blocked, so
		// one run may write into two sections.
		{"a reason implies the section", "Fix it | blocked:on the vendor", func(t *testing.T, req AddRequest) {
			if req.Section != SectionBlocked || req.Blocked != "on the vendor" {
				t.Fatalf("section = %q blocked = %q", req.Section, req.Blocked)
			}
		}},

		{"a backdated created", "Fix it | created:2026-01-02", func(t *testing.T, req AddRequest) {
			if req.Created.String() != "2026-01-02" {
				t.Fatalf("created = %q", req.Created)
			}
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseAddLine(1, tc.line)
			if err != nil {
				t.Fatalf("ParseAddLine(%q): %v", tc.line, err)
			}
			tc.check(t, req)
			// Whatever it parsed has to be something Add would accept: the
			// grammar's whole claim is that a line is an --add.
			if _, err := buildNewItem(req, day); err != nil {
				t.Fatalf("buildNewItem after ParseAddLine(%q): %v", tc.line, err)
			}
		})
	}
}

func TestParseAddLineRejections(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{"empty", "   ", "empty"},
		{"only a bullet", "- ", "empty"},
		{"no title", " | prio:high", "no title"},
		{"a title that is only a field", "| prio:high", "no title"},
		// The one that matters: pasting existing items back in would hand out
		// ids that are already in use or retired (I2).
		{"a pasted item line", "- [ ] [T-0042] Fix the deploy script", "remove the [T-0042]"},
		{"a pasted done line", "- [x] [T-0042] Fix it | outcome:shipped", "remove the [T-0042]"},
		{"a field with no colon", "Fix it | prio high", "not key:value"},
		{"a repeated field", "Fix it | prio:high | prio:low", "repeats field prio"},
		{"a bad prio", "Fix it | prio:urgent", "not high, med or low"},
		{"a bad date", "Fix it | created:2026-02-31", "not a date"},
		{"a bad schedule", "Fix it | tickler:whenever", "not a schedule"},
		{"a stray pipe in the title", "Fix it |now | prio:high", "may not contain"},
		// detail: cannot be honoured, because the path must match an id this
		// operation has not allocated yet (I8).
		{"detail", "Fix it | detail:details/T-0001.md", "allocated by this operation"},
		{"done", "Fix it | done:2026-08-06", "creates backlog items"},
		{"outcome", "Fix it | outcome:shipped", "creates backlog items"},
		{"started", "Fix it | started:2026-08-06", "creates backlog items"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAddLine(7, tc.line)
			if err == nil {
				t.Fatalf("ParseAddLine(%q) should have failed", tc.line)
			}
			if !errors.Is(err, ErrInvalidArgument) {
				t.Fatalf("error is not InvalidArgument: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
			// Every line error locates itself: a rejected batch is useless if
			// the user cannot find the line to fix.
			if !strings.Contains(err.Error(), "line 7") {
				t.Fatalf("error %q does not name the line", err)
			}
		})
	}
}

// A line's fields mean exactly what they mean to --add (§5.2.1), and that
// includes the one asymmetry: blocked: implies its section, tickler: does not.
// I7 allows a schedule only in Someday, and --add makes the caller say so.
func TestParseAddLineSchedulesNeedTheirSection(t *testing.T) {
	day := Date{2026, 8, 6}
	req, err := ParseAddLine(1, "Renew the domain | tickler:2026-09-01")
	if err != nil {
		t.Fatal(err)
	}
	if req.Tickler != "2026-09-01" {
		t.Fatalf("tickler = %q", req.Tickler)
	}
	if _, err := buildNewItem(req, day); err == nil {
		t.Fatal("a schedule outside Someday should be refused, exactly as --add refuses it")
	}
	req.Section = SectionSomeday
	if _, err := buildNewItem(req, day); err != nil {
		t.Fatalf("with the batch's --section someday it must build: %v", err)
	}
}

// A title that merely starts with a bracket is not a pasted item line, and
// refusing it would make a legitimate title unaddable.
func TestParseAddLineKeepsBracketedTitles(t *testing.T) {
	// Only [LETTERS-DIGITS] reads as an id; everything else is prose someone
	// meant to write down.
	for _, line := range []string{"[WIP] Fix the deploy script", "[a b] Fix it", "[]", "[2026] Plan"} {
		req, err := ParseAddLine(1, line)
		if err != nil {
			t.Fatalf("ParseAddLine(%q): %v", line, err)
		}
		if req.Title != line {
			t.Fatalf("title = %q, want %q", req.Title, line)
		}
	}
}

func TestAddManyAppendsInOrder(t *testing.T) {
	_, s := addManyDir(t)
	day := Date{2026, 8, 6}

	items, res, err := s.AddMany([]AddRequest{
		{Title: "Three"},
		{Title: "Four", Prio: PrioHigh},
		{Title: "Five", Tags: []string{"infra"}},
	}, day)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(items); got != 3 {
		t.Fatalf("added %d items, want 3", got)
	}
	// IDs are allocated in the input's order, from next_id, one per item.
	for i, want := range []ID{"T-0003", "T-0004", "T-0005"} {
		if items[i].ID != want {
			t.Fatalf("item %d has id %s, want %s", i, items[i].ID, want)
		}
	}
	if got := len(res.Changes); got != 3 {
		t.Fatalf("%d changes, want one per item", got)
	}
	// One transaction means one file written, not one per item.
	if len(res.Files) != 1 {
		t.Fatalf("wrote %v, want just backlog.md", res.Files)
	}

	want := []ID{"T-0001", "T-0002", "T-0003", "T-0004", "T-0005"}
	if got := readyIDs(t, s); !equalIDs(got, want) {
		t.Fatalf("ready = %v, want %v", got, want)
	}
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != "T-0006" {
		t.Fatalf("next_id = %s, want T-0006 (one bump per item)", d.NextID)
	}
	if items[0].Created != day {
		t.Fatalf("created = %s, want today", items[0].Created)
	}
}

// §5.2.1: "--top inserts the batch at the top, still in the input's order."
func TestAddManyTopKeepsTheInputOrder(t *testing.T) {
	_, s := addManyDir(t)

	if _, _, err := s.AddMany([]AddRequest{
		{Title: "Three", Top: true},
		{Title: "Four", Top: true},
	}, Date{2026, 8, 6}); err != nil {
		t.Fatal(err)
	}

	want := []ID{"T-0003", "T-0004", "T-0001", "T-0002"}
	if got := readyIDs(t, s); !equalIDs(got, want) {
		t.Fatalf("ready = %v, want %v — a --top that reverses the batch is the bug this catches", got, want)
	}
}

// One run, two sections: a line's own blocked: reason decides where it lands.
func TestAddManySpansSections(t *testing.T) {
	_, s := addManyDir(t)

	items, _, err := s.AddMany([]AddRequest{
		{Title: "Ready one"},
		{Title: "Waiting", Blocked: "on the vendor", Section: SectionBlocked},
		{Title: "Ready two"},
		{Title: "Later", Section: SectionSomeday, Tickler: "2026-09-01"},
	}, Date{2026, 8, 6})
	if err != nil {
		t.Fatal(err)
	}
	if items[1].Section != SectionBlocked || items[3].Section != SectionSomeday {
		t.Fatalf("sections = %q, %q", items[1].Section, items[3].Section)
	}
	if got := readyIDs(t, s); !equalIDs(got, []ID{"T-0001", "T-0002", "T-0003", "T-0005"}) {
		t.Fatalf("ready = %v", got)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("directory should still be clean:\n%s", violationMessages(vs))
	}
}

// The property the operation exists for: a bad request writes NOTHING, not the
// items that preceded it.
func TestAddManyIsAllOrNothing(t *testing.T) {
	dir, s := addManyDir(t)
	before := readFileString(t, dir+"/backlog.md")

	_, _, err := s.AddMany([]AddRequest{
		{Title: "Three"},
		{Title: "Four"},
		{Title: "Bad | title"},
		{Title: "Five"},
	}, Date{2026, 8, 6})
	if err == nil {
		t.Fatal("a pipe in a title should have failed the batch")
	}
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("error is not InvalidArgument: %v", err)
	}
	// The index names which request failed, so a front end can point at the
	// line it came from.
	if !strings.Contains(err.Error(), "item 3") {
		t.Fatalf("error %q does not say which item failed", err)
	}
	if after := readFileString(t, dir+"/backlog.md"); after != before {
		t.Fatalf("backlog.md was written by a failed batch:\n%s", after)
	}
}

func TestAddManyRefusesAnEmptyBatchAndDetailBodies(t *testing.T) {
	_, s := addManyDir(t)
	day := Date{2026, 8, 6}

	if _, _, err := s.AddMany(nil, today); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("an empty batch should be InvalidArgument, got %v", err)
	}
	_, _, err := s.AddMany([]AddRequest{{Title: "Three", DetailBody: "long form"}}, day)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("a detail body should be refused: %v", err)
	}
	if !strings.Contains(err.Error(), "detail files") {
		t.Fatalf("error %q does not say why", err)
	}
}

func TestAddManyDryRunWritesNothing(t *testing.T) {
	dir, s := addManyDir(t)
	before := readFileString(t, dir+"/backlog.md")

	items, res, err := s.AddMany([]AddRequest{
		{Title: "Three", DryRun: true},
		{Title: "Four", DryRun: true},
	}, Date{2026, 8, 6})
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Changes) != 2 {
		t.Fatalf("dry run reported %+v", res)
	}
	// The IDs a real run would allocate are part of what a dry run reports.
	if items[0].ID != "T-0003" || items[1].ID != "T-0004" {
		t.Fatalf("ids = %s, %s", items[0].ID, items[1].ID)
	}
	if after := readFileString(t, dir+"/backlog.md"); after != before {
		t.Fatalf("a dry run wrote to backlog.md:\n%s", after)
	}
}

// next_id exhaustion inside a batch: the whole batch fails, and nothing is
// written — the failure is not "some of them fitted".
func TestAddManyStopsAtTheIDCap(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": `---
doc: backlog
version: 1
project: Nearly Full
next_id: T-9998
id_width: 4
updated: 2026-07-29
---

# Backlog

## Ready

## Blocked

## Someday
`,
		"done.md": "---\ndoc: done\nversion: 1\n---\n\n# Done\n",
	})
	s := mustOpen(t, dir)
	before := readFileString(t, dir+"/backlog.md")

	_, _, err := s.AddMany([]AddRequest{
		{Title: "One"},
		{Title: "Two"},
		{Title: "Three"},
	}, Date{2026, 8, 6})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("exhausting next_id should be a Conflict, got %v", err)
	}
	if after := readFileString(t, dir+"/backlog.md"); after != before {
		t.Fatalf("a failed batch wrote to backlog.md:\n%s", after)
	}
}

func equalIDs(got, want []ID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
