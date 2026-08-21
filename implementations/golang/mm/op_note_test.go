package mm

import (
	"errors"
	"strings"
	"testing"
)

// A working item's notes go in its slot, next to the work — where --pause and
// --finish already know to preserve them.
func TestNoteGoesToTheWorkingSlot(t *testing.T) {
	dir, s := startDir(t)
	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.Note("T-0001", NoteRequest{Text: "the cache key is wrong"}, today); err != nil {
		t.Fatalf("note: %v", err)
	}
	slot := readFile(t, dir, "working.01.md")
	notes := sectionText(slot, "## Notes")
	if !strings.Contains(notes, "the cache key is wrong") {
		t.Errorf("the note is not under ## Notes:\n%s", slot)
	}
	// Dated, because notes accumulate over days and an undated pile of them
	// cannot be read back as a history.
	if !strings.Contains(notes, today.String()) {
		t.Errorf("the note is not dated:\n%s", notes)
	}
	// It must not have gone anywhere else.
	if strings.Contains(sectionText(slot, "## Task"), "cache key") {
		t.Error("the note landed in ## Task")
	}

	// A second note accumulates rather than replacing the first.
	if _, _, err := s.Note("T-0001", NoteRequest{Text: "second thought"}, today); err != nil {
		t.Fatal(err)
	}
	notes = sectionText(readFile(t, dir, "working.01.md"), "## Notes")
	if !strings.Contains(notes, "cache key") || !strings.Contains(notes, "second thought") {
		t.Errorf("notes should accumulate:\n%s", notes)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// An item that is not in a slot has no such place, so the note goes to its
// detail file — created if necessary, because refusing to record something the
// user has already typed is the worse outcome.
func TestNoteGoesToTheDetailFile(t *testing.T) {
	dir, s := startDir(t)

	// T-0002 has no detail file yet.
	it, _, err := s.Note("T-0002", NoteRequest{Text: "thought about this"}, today)
	if err != nil {
		t.Fatalf("note: %v", err)
	}
	if it.Detail != "details/T-0002.md" {
		t.Fatalf("detail = %q, want it created", it.Detail)
	}
	body := readFile(t, dir, "details/T-0002.md")
	if !strings.Contains(body, "thought about this") {
		t.Errorf("detail file:\n%s", body)
	}
	if !strings.Contains(readFile(t, dir, "backlog.md"), "detail:details/T-0002.md") {
		t.Error("the item line does not point at the new detail file")
	}

	// T-0001 already has one: the note is appended, not written over the body.
	if _, _, err := s.Note("T-0001", NoteRequest{Text: "an aside"}, today); err != nil {
		t.Fatal(err)
	}
	body = readFile(t, dir, "details/T-0001.md")
	if !strings.Contains(body, "an aside") || !strings.Contains(body, "Deploys fail on a cold cache.") {
		t.Errorf("the existing body was damaged:\n%s", body)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// A note on a closed item still records, because that is when a postscript is
// most often written.
func TestNoteOnADoneItem(t *testing.T) {
	dir, s := startDir(t)
	if _, _, err := s.Note("T-0009", NoteRequest{Text: "came back to bite us"}, today); err != nil {
		t.Fatalf("note: %v", err)
	}
	if !strings.Contains(readFile(t, dir, "details/T-0009.md"), "came back to bite us") {
		t.Error("the note was not recorded")
	}
	if !strings.Contains(readFile(t, dir, "done.md"), "detail:details/T-0009.md") {
		t.Error("the done line does not point at the detail file")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestNoteValidatesAndDryRuns(t *testing.T) {
	dir, s := startDir(t)

	if _, _, err := s.Note("T-0001", NoteRequest{Text: ""}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty text: want ErrInvalidArgument, got %v", err)
	}
	if _, _, err := s.Note("T-9999", NoteRequest{Text: "x"}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: want ErrNotFound, got %v", err)
	}

	before := readFile(t, dir, "details/T-0001.md")
	if _, _, err := s.Note("T-0001", NoteRequest{Text: "x", DryRun: true}, today); err != nil {
		t.Fatal(err)
	}
	if readFile(t, dir, "details/T-0001.md") != before {
		t.Error("the dry run wrote to disk")
	}

	// Undated writes the text as given, for a caller that formats its own.
	if _, _, err := s.Note("T-0001", NoteRequest{Text: "raw", Undated: true}, today); err != nil {
		t.Fatal(err)
	}
	body := readFile(t, dir, "details/T-0001.md")
	if !strings.Contains(body, "raw") {
		t.Error("the undated note is missing")
	}
}

// A note written into a slot survives the pause that empties it: both use the
// same preservation path, so the two cannot drift apart.
func TestNoteSurvivesPause(t *testing.T) {
	dir, s := startDir(t)
	if _, _, err := testStartV1(s, "T-0002", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Note("T-0002", NoteRequest{Text: "halfway through"}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := testPauseV1(s, "T-0002", PauseRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, dir, "details/T-0002.md"), "halfway through") {
		t.Error("the note did not survive the pause")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}
