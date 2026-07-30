package mm

import (
	"errors"
	"strings"
	"testing"
)

// backlogWith builds a backlog file with the given Ready lines.
func backlogWith(ready ...string) string {
	return "---\ndoc: backlog\nversion: 1\nproject: Sample One\nnext_id: T-0099\nupdated: 2026-07-30\n---\n\n" +
		"# Backlog\n\n## Ready\n\n" + strings.Join(ready, "\n") + "\n\n## Blocked\n\n## Someday\n"
}

func TestStatusCounts(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": "---\ndoc: backlog\nversion: 1\nproject: Sample One\nnext_id: T-0099\nupdated: 2026-07-30\n---\n\n" +
			"# Backlog\n\n## Ready\n\n" +
			"- [ ] [T-0020] First | prio:high | created:2026-07-10\n" +
			"- [ ] [T-0021] Second | created:2026-07-01\n\n" +
			"## Blocked\n\n" +
			"- [ ] [T-0022] Waiting | blocked:on the vendor | created:2026-07-05\n\n" +
			"## Someday\n\n" +
			"- [ ] [T-0023] Maybe | created:2026-06-01\n",
		"working.02.md": busySlot,
	})

	st, err := mustOpen(t, dir).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Ready != 2 || st.Blocked != 1 || st.Someday != 1 {
		t.Errorf("counts = ready %d, blocked %d, someday %d", st.Ready, st.Blocked, st.Someday)
	}
	if st.Backlog() != 4 {
		t.Errorf("Backlog() = %d, want 4", st.Backlog())
	}
	if st.Done != 3 {
		t.Errorf("Done = %d, want the fixture's 3", st.Done)
	}
	if st.WipUsed() != 1 || st.WipLimit() != 2 {
		t.Errorf("wip = %d/%d, want 1/2", st.WipUsed(), st.WipLimit())
	}
	if st.Directory.Project != "Sample One" {
		t.Errorf("Directory.Project = %q", st.Directory.Project)
	}
	if len(st.Directory.Slots) != 2 {
		t.Errorf("want the slots carried through, got %d", len(st.Directory.Slots))
	}
}

// Next is the TOP of ## Ready - the user's own ordering, never re-sorted by
// priority or date (spec-file-format.md §5.1).
func TestNextIsTheTopOfReady(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": backlogWith(
			"- [ ] [T-0020] Top of the list | prio:low | created:2026-07-30",
			"- [ ] [T-0021] Higher priority but lower down | prio:high | created:2026-01-01",
		),
	})

	it, err := mustOpen(t, dir).Next()
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "T-0020" {
		t.Errorf("Next() = %s, want the top of the list even though T-0021 is high priority", it.ID)
	}

	st, err := mustOpen(t, dir).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Next == nil || st.Next.ID != "T-0020" {
		t.Errorf("Status().Next = %v, want the same item Next() returns", st.Next)
	}
}

// ErrNotFound on an empty section is what lets the CLI exit non-zero and a
// script stop rather than start something arbitrary.
func TestNextOnAnEmptyReadySection(t *testing.T) {
	dir := newDir(t, map[string]string{"backlog.md": backlogWith()})

	if _, err := mustOpen(t, dir).Next(); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	st, err := mustOpen(t, dir).Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.Next != nil || st.OldestReady != nil {
		t.Errorf("an empty Ready section should leave both nil: %v %v", st.Next, st.OldestReady)
	}
	if st.Ready != 0 {
		t.Errorf("Ready = %d", st.Ready)
	}
}

// "Untouched" means never started. An item that was started and paused has been
// looked at; the one that has sat since it was written has not.
func TestOldestUntouchedReadyItem(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  ID
	}{
		{
			name: "the oldest created date wins, wherever it sits",
			lines: []string{
				"- [ ] [T-0020] Newer | created:2026-07-30",
				"- [ ] [T-0021] Oldest | created:2026-01-02",
				"- [ ] [T-0022] Middle | created:2026-04-01",
			},
			want: "T-0021",
		},
		{
			name: "an item that was started and paused is not untouched",
			lines: []string{
				"- [ ] [T-0020] Paused, and older | created:2026-01-01 | started:2026-02-01",
				"- [ ] [T-0021] Never started | created:2026-06-01",
			},
			want: "T-0021",
		},
		{
			name: "an undated item wins only when nothing else qualifies",
			lines: []string{
				"- [ ] [T-0020] Dated | created:2026-06-01",
				"- [ ] [T-0021] Undated",
			},
			want: "T-0020",
		},
		{
			name:  "with only an undated item, it is the answer",
			lines: []string{"- [ ] [T-0021] Undated"},
			want:  "T-0021",
		},
		{
			name: "when everything has been started, the top of the list stands in",
			lines: []string{
				"- [ ] [T-0020] Paused | created:2026-01-01 | started:2026-02-01",
				"- [ ] [T-0021] Also paused | created:2026-01-02 | started:2026-02-02",
			},
			want: "T-0020",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := newDir(t, map[string]string{"backlog.md": backlogWith(c.lines...)})
			st, err := mustOpen(t, dir).Status()
			if err != nil {
				t.Fatal(err)
			}
			if st.OldestReady == nil {
				t.Fatal("OldestReady is nil")
			}
			if st.OldestReady.ID != c.want {
				t.Errorf("OldestReady = %s, want %s", st.OldestReady.ID, c.want)
			}
		})
	}
}

// A directory that already violates its invariants must still report status:
// this is what a person runs to find out what is wrong (spec-tools.md §8).
func TestStatusOfABrokenDirectory(t *testing.T) {
	for _, f := range fixtures(t) {
		if f.Clean() {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			s, err := Open(f.Path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Status(); err != nil {
				t.Errorf("Status on a broken directory failed: %v", err)
			}
		})
	}
}

// Status returns a copy: a caller mutating what it got must not reach into the
// next read.
func TestStatusReturnsCopies(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": backlogWith("- [ ] [T-0020] Real title | created:2026-07-30"),
	})
	s := mustOpen(t, dir)

	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	st.Next.Title = "mutated"

	again, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	if again.Next.Title != "Real title" {
		t.Errorf("Status handed out a shared item: %q", again.Next.Title)
	}
}
