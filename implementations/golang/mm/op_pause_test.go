package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pauseDir starts an item, writes notes and subtasks into its slot by hand -
// the way a person would - and hands back the directory ready to pause.
func pauseDir(t *testing.T, id ID, notes string) (string, *Store) {
	t.Helper()
	dir, s := startDir(t)
	if _, _, err := s.Start(id, StartRequest{}, today); err != nil {
		t.Fatalf("start: %v", err)
	}
	if notes != "" {
		p := filepath.Join(dir, "working.01.md")
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		updated := strings.Replace(string(body), "## Notes\n", "## Notes\n\n"+notes+"\n", 1)
		updated = strings.Replace(updated, "## Plan\n", "## Plan\n\n- [x] a subtask\n- [ ] another\n", 1)
		if err := os.WriteFile(p, []byte(updated), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, s
}

func TestPauseReturnsTheItemToTheTopOfReady(t *testing.T) {
	dir, s := pauseDir(t, "T-0001", "")

	it, _, err := s.Pause("T-0001", PauseRequest{}, today)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if it.State != StateBacklog || it.Section != SectionReady {
		t.Errorf("state/section = %s/%s", it.State, it.Section)
	}
	if it.Slot != 0 {
		t.Errorf("slot = %d, want 0", it.Slot)
	}
	// started: survives. It records when the work began, not when it was last
	// picked up.
	if it.Started != today {
		t.Errorf("started = %q, want it preserved", it.Started)
	}

	ready, _ := s.List(Filter{Section: SectionReady})
	if got := idsOfValues(ready); len(got) == 0 || got[0] != "T-0001" {
		t.Errorf("ready = %v, want T-0001 first", got)
	}

	line := readFile(t, dir, "backlog.md")
	for _, want := range []string{
		"prio:high", "tags:infra,ci", "detail:details/T-0001.md",
		"created:2026-07-20", "started:2026-07-29", "owner:dana",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("backlog line lost %q:\n%s", want, sectionText(line, "## Ready"))
		}
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// The slot must come back to a state a new item can be started into, with no
// residue from the item that just left.
func TestPauseResetsTheSlotCompletely(t *testing.T) {
	dir, s := pauseDir(t, "T-0001", "some notes")

	if _, _, err := s.Pause("T-0001", PauseRequest{}, today); err != nil {
		t.Fatalf("pause: %v", err)
	}
	got := readFile(t, dir, "working.01.md")
	for _, want := range []string{
		"status: idle", "id: null", "title: null", "prio: null",
		"tags: null", "detail: null", "created: null", "started: null",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("slot is missing %q:\n%s", want, got)
		}
	}
	// The unregistered field left with the item; leaving it behind would attach
	// dana to whatever is started next.
	if strings.Contains(got, "owner:") {
		t.Errorf("owner survived in the idle slot:\n%s", got)
	}
	// Subtasks are scratch by design.
	if strings.Contains(got, "a subtask") {
		t.Errorf("## Plan should have been cleared:\n%s", got)
	}
	for _, h := range []string{"## Task", "## Plan", "## Notes", "## Blockers"} {
		if !strings.Contains(got, h) {
			t.Errorf("section %s went missing:\n%s", h, got)
		}
		if body := sectionText(got, h); strings.TrimSpace(body) != "" {
			t.Errorf("%s should be empty, got %q", h, body)
		}
	}

	// And the proof: the slot is usable again.
	if _, _, err := s.Start("T-0002", StartRequest{Slot: 1}, today); err != nil {
		t.Errorf("slot not reusable after pause: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestPausePreservesNotesIntoAnExistingDetailFile(t *testing.T) {
	dir, s := pauseDir(t, "T-0001", "the cache key is wrong")

	if _, _, err := s.Pause("T-0001", PauseRequest{}, today); err != nil {
		t.Fatalf("pause: %v", err)
	}
	detail := readFile(t, dir, "details/T-0001.md")
	if !strings.Contains(detail, "the cache key is wrong") {
		t.Errorf("notes were not preserved:\n%s", detail)
	}
	// Appended under ## Notes, not dumped at the top over the existing body.
	if !strings.Contains(detail, "Deploys fail on a cold cache.") {
		t.Errorf("existing detail body was damaged:\n%s", detail)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// The harder half: an item with no detail file still must not lose its notes.
func TestPauseCreatesADetailFileForNotes(t *testing.T) {
	dir, s := pauseDir(t, "T-0002", "half done, see the branch")

	it, res, err := s.Pause("T-0002", PauseRequest{}, today)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if it.Detail != "details/T-0002.md" {
		t.Fatalf("detail = %q, want details/T-0002.md", it.Detail)
	}
	created := false
	for _, c := range res.Changes {
		if c.Kind == ChangeCreated && c.File == "details/T-0002.md" {
			created = true
		}
	}
	if !created {
		t.Errorf("the created detail file was not reported: %+v", res.Changes)
	}
	detail := readFile(t, dir, "details/T-0002.md")
	if !strings.Contains(detail, "half done, see the branch") {
		t.Errorf("notes missing:\n%s", detail)
	}
	// I9: the new file's frontmatter must match the item exactly.
	if !strings.Contains(detail, "id: T-0002") || !strings.Contains(detail, "title: Second") {
		t.Errorf("frontmatter does not match the item:\n%s", detail)
	}
	if !strings.Contains(readFile(t, dir, "backlog.md"), "detail:details/T-0002.md") {
		t.Error("the item line does not point at the new detail file")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestPauseDiscardNotes(t *testing.T) {
	dir, s := pauseDir(t, "T-0002", "throwaway")

	if _, _, err := s.Pause("T-0002", PauseRequest{DiscardNotes: true}, today); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0002.md")); !os.IsNotExist(err) {
		t.Error("DiscardNotes should not create a detail file")
	}
	if strings.Contains(readFile(t, dir, "backlog.md"), "detail:details/T-0002.md") {
		t.Error("no detail: field should have been set")
	}
}

func TestPauseDestinations(t *testing.T) {
	// End appends instead of the default top.
	dir, s := pauseDir(t, "T-0001", "")
	if _, _, err := s.Pause("T-0001", PauseRequest{End: true}, today); err != nil {
		t.Fatalf("pause --end: %v", err)
	}
	ready, _ := s.List(Filter{Section: SectionReady})
	got := idsOfValues(ready)
	if len(got) == 0 || got[len(got)-1] != "T-0001" {
		t.Errorf("ready = %v, want T-0001 last", got)
	}
	_ = dir

	// Into Someday.
	_, s2 := pauseDir(t, "T-0001", "")
	if _, _, err := s2.Pause("T-0001", PauseRequest{Section: SectionSomeday}, today); err != nil {
		t.Fatalf("pause --section someday: %v", err)
	}
	someday, _ := s2.List(Filter{Section: SectionSomeday})
	if got := idsOfValues(someday); len(got) == 0 || got[0] != "T-0001" {
		t.Errorf("someday = %v", got)
	}

	// Into Blocked, which I5 says needs a reason.
	_, s3 := pauseDir(t, "T-0001", "")
	if _, _, err := s3.Pause("T-0001", PauseRequest{Section: SectionBlocked}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("blocked without a reason: want ErrInvalidArgument, got %v", err)
	}
	it, _, err := s3.Pause("T-0001", PauseRequest{Section: SectionBlocked, Blocked: "waiting on ops"}, today)
	if err != nil {
		t.Fatalf("pause into blocked: %v", err)
	}
	if it.Blocked != "waiting on ops" {
		t.Errorf("blocked = %q", it.Blocked)
	}
	if vs, _ := s3.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestPauseConflicts(t *testing.T) {
	_, s := startDir(t)

	if _, _, err := s.Pause("T-0001", PauseRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("backlog item: want ErrConflict, got %v", err)
	}
	if _, _, err := s.Pause("T-0009", PauseRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("done item: want ErrConflict, got %v", err)
	}
	if _, _, err := s.Pause("T-0099", PauseRequest{}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: want ErrNotFound, got %v", err)
	}
}

func TestPauseDryRun(t *testing.T) {
	dir, s := pauseDir(t, "T-0002", "notes that must not be written yet")
	before := readFile(t, dir, "working.01.md")

	if _, _, err := s.Pause("T-0002", PauseRequest{DryRun: true}, today); err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if readFile(t, dir, "working.01.md") != before {
		t.Error("dry run wrote to the slot")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0002.md")); !os.IsNotExist(err) {
		t.Error("dry run created a detail file")
	}
}

// Start then pause then start again: every field has to arrive back where it
// began, which is the whole point of giving both places the same lexical form.
func TestStartPauseRoundTrip(t *testing.T) {
	dir, s := startDir(t)
	before := readFile(t, dir, "backlog.md")

	if _, _, err := s.Start("T-0001", StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Pause("T-0001", PauseRequest{}, today); err != nil {
		t.Fatal(err)
	}
	it, err := s.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	want := "- [ ] [T-0001] Fix the deploy script | prio:high | tags:infra,ci | " +
		"detail:details/T-0001.md | created:2026-07-20 | started:2026-07-29 | owner:dana"
	if got := RenderItemLine(&it); got != want {
		t.Errorf("round trip changed the line:\n got %s\nwant %s", got, want)
	}
	// It comes back at the top of Ready rather than where it was, which is the
	// documented behaviour, so the file is expected to differ.
	if before == readFile(t, dir, "backlog.md") {
		t.Log("backlog unchanged; the item happened to already be first")
	}
}
