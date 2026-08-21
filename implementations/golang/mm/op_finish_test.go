package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinishFromTheBacklog(t *testing.T) {
	dir, s := startDir(t)

	// Closing something that was never started is normal.
	it, res, err := testFinishV1(s, "T-0002", FinishRequest{}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if it.State != StateDone {
		t.Errorf("state = %s", it.State)
	}
	if it.Outcome != OutcomeShipped {
		t.Errorf("outcome = %q, want the shipped default", it.Outcome)
	}
	if it.Done != today {
		t.Errorf("done = %s, want today", it.Done)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeMoved {
		t.Errorf("changes = %+v", res.Changes)
	}

	done := readFile(t, dir, "done.md")
	if !strings.Contains(done, "- [x] [T-0002] Second") {
		t.Errorf("the line should be closed in done.md:\n%s", done)
	}
	if !strings.Contains(done, "done:2026-07-29") || !strings.Contains(done, "outcome:shipped") {
		t.Errorf("done.md line is missing required fields:\n%s", done)
	}
	if strings.Contains(readFile(t, dir, "backlog.md"), "[T-0002]") {
		t.Error("the backlog line should be gone")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// Newest first, within the file and within the group.
func TestFinishInsertsAtTheTopOfTheMonthGroup(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := testFinishV1(s, "T-0002", FinishRequest{}, today); err != nil {
		t.Fatal(err)
	}
	group := sectionText(readFile(t, dir, "done.md"), "## 2026-07")
	lines := nonBlank(group)
	if len(lines) != 2 {
		t.Fatalf("want 2 items in the group, got %d:\n%s", len(lines), group)
	}
	if !strings.Contains(lines[0], "T-0002") || !strings.Contains(lines[1], "T-0009") {
		t.Errorf("newest should be first:\n%s", group)
	}
}

// A month with no group yet has to get one, in newest-first position.
func TestFinishCreatesMonthGroupsInOrder(t *testing.T) {
	dir, s := startDir(t)

	// August: newer than the existing 2026-07 group, so it goes above it.
	aug := Date{2026, 8, 3}
	if _, _, err := testFinishV1(s, "T-0001", FinishRequest{Done: aug}, today); err != nil {
		t.Fatalf("finish into a new month: %v", err)
	}
	// June: older, so it goes below.
	jun := Date{2026, 6, 30}
	if _, _, err := testFinishV1(s, "T-0002", FinishRequest{Done: jun, Outcome: OutcomeCancelled}, today); err != nil {
		t.Fatalf("finish into an older month: %v", err)
	}

	done := readFile(t, dir, "done.md")
	order := headings(done)
	want := []string{"## 2026-08", "## 2026-07", "## 2026-06"}
	if len(order) != len(want) {
		t.Fatalf("headings = %v, want %v\n%s", order, want, done)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("headings = %v, want %v\n%s", order, want, done)
		}
	}
	// I6: each item sits under the heading its done: date names.
	if !strings.Contains(sectionText(done, "## 2026-08"), "T-0001") {
		t.Error("T-0001 is not under 2026-08")
	}
	if !strings.Contains(sectionText(done, "## 2026-06"), "T-0002") {
		t.Error("T-0002 is not under 2026-06")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestFinishFromASlot(t *testing.T) {
	dir, s := pauseDir(t, "T-0001", "the fix was a one-liner")

	it, _, err := testFinishV1(s, "T-0001", FinishRequest{}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if it.Started != today {
		t.Errorf("started should be preserved, got %q", it.Started)
	}

	slot := readFile(t, dir, "working.01.md")
	if !strings.Contains(slot, "status: idle") || !strings.Contains(slot, "id: null") {
		t.Errorf("slot was not reset:\n%s", slot)
	}
	if strings.Contains(slot, "owner:") {
		t.Errorf("the item's extra field stayed behind:\n%s", slot)
	}

	// The notes were the last copy of that text.
	if d := readFile(t, dir, "details/T-0001.md"); !strings.Contains(d, "the fix was a one-liner") {
		t.Errorf("notes were lost on finish:\n%s", d)
	}
	// Every field still travels, unregistered ones included.
	if line := readFile(t, dir, "done.md"); !strings.Contains(line, "owner:dana") {
		t.Errorf("done.md lost the unregistered field:\n%s", line)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestFinishNote(t *testing.T) {
	dir, s := startDir(t)

	_, _, err := testFinishV1(s, "T-0002", FinishRequest{
		Outcome: OutcomeObsolete,
		Note:    "superseded by the new pipeline",
	}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	// --note creates the detail file when there is none.
	detail := readFile(t, dir, "details/T-0002.md")
	if !strings.Contains(detail, "superseded by the new pipeline") {
		t.Errorf("closing note missing:\n%s", detail)
	}
	if !strings.Contains(readFile(t, dir, "done.md"), "outcome:obsolete") {
		t.Error("outcome not recorded")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestFinishValidatesItsArguments(t *testing.T) {
	_, s := startDir(t)

	if _, _, err := testFinishV1(s, "T-0001", FinishRequest{Outcome: "abandoned"}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad outcome: want ErrInvalidArgument, got %v", err)
	}
	if _, _, err := testFinishV1(s, "T-0001", FinishRequest{Done: Date{2026, 2, 31}}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("impossible date: want ErrInvalidArgument, got %v", err)
	}
	if _, _, err := testFinishV1(s, "T-0009", FinishRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("already done: want ErrConflict, got %v", err)
	}
	if _, _, err := testFinishV1(s, "T-0099", FinishRequest{}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: want ErrNotFound, got %v", err)
	}
}

func TestFinishDryRun(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "done.md")

	if _, _, err := testFinishV1(s, "T-0002", FinishRequest{DryRun: true, Note: "x"}, today); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if readFile(t, dir, "done.md") != before {
		t.Error("dry run wrote done.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0002.md")); !os.IsNotExist(err) {
		t.Error("dry run created a detail file")
	}
}

// Inserting into an empty section is where the line arithmetic is easiest to
// get wrong: the last element of a split file IS its trailing newline, and
// stepping past it appends after the end of the file.
func TestInsertIntoAnEmptySectionKeepsTheFileWellFormed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	s, _, err := Init(dir, InitRequest{Project: "Spacing"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := testAddV1(s, AddRequest{Title: "Only item"}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testFinishV1(s, "T-0001", FinishRequest{}, today); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"backlog.md", "done.md"} {
		body := readFile(t, dir, name)
		if !strings.HasSuffix(body, "\n") {
			t.Errorf("%s does not end with a newline:\n%q", name, body)
		}
		if strings.HasSuffix(body, "\n\n") {
			t.Errorf("%s ends with a blank line:\n%q", name, body)
		}
		// A heading pressed against the line above it, or an item pressed
		// against a heading, is not what a person would have typed.
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			if i == 0 || !strings.HasPrefix(l, "## ") {
				continue
			}
			if strings.TrimSpace(lines[i-1]) != "" {
				t.Errorf("%s: no blank line before %q:\n%s", name, l, body)
			}
			if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) != "" {
				t.Errorf("%s: no blank line after %q:\n%s", name, l, body)
			}
		}
	}
}

// nonBlank returns the non-empty lines of a block.
func nonBlank(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// headings returns every "## " heading of a file, in order.
func headings(file string) []string {
	var out []string
	for _, l := range strings.Split(file, "\n") {
		if strings.HasPrefix(l, "## ") {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}
