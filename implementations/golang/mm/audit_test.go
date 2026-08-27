package mm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T-0253: the audit log. auditFieldChanges/appendAuditEntries are pure
// functions, tested directly; the rest is exercised through Store methods
// against a v2 directory with audit: true.

func TestAuditFieldChangesOnCreation(t *testing.T) {
	c := Change{Kind: ChangeCreated, ID: "T-0001", File: "board.md",
		After: "- [ ] [T-0001] Fix it | stage:ready | prio:high | created:2026-08-27"}
	got := auditFieldChanges(c)
	want := map[string]string{"stage": "ready", "prio": "high", "created": "2026-08-27"}
	if len(got) != len(want) {
		t.Fatalf("got %d fields, want %d: %+v", len(got), len(want), got)
	}
	for _, f := range got {
		if want[f.Field] != f.Value {
			t.Errorf("field %s = %q, want %q", f.Field, f.Value, want[f.Field])
		}
	}
}

func TestAuditFieldChangesOnMove(t *testing.T) {
	c := Change{Kind: ChangeMoved, ID: "T-0001", File: "board.md",
		Before: "- [ ] [T-0001] Fix it | stage:ready | prio:high | created:2026-08-27",
		After:  "- [ ] [T-0001] Fix it | stage:working | prio:high | created:2026-08-27 | started:2026-08-27"}
	got := auditFieldChanges(c)
	if len(got) != 2 {
		t.Fatalf("got %d fields, want 2 (stage, started): %+v", len(got), got)
	}
	var stage, started string
	for _, f := range got {
		switch f.Field {
		case "stage":
			stage = f.Value
		case "started":
			started = f.Value
		}
	}
	if stage != "working" || started != "2026-08-27" {
		t.Errorf("stage=%q started=%q, want working/2026-08-27", stage, started)
	}
}

// A pure reorder changes no field, so it must log nothing.
func TestAuditFieldChangesOnPureReorderIsEmpty(t *testing.T) {
	line := "- [ ] [T-0001] Fix it | stage:ready | prio:high | created:2026-08-27"
	c := Change{Kind: ChangeMoved, ID: "T-0001", File: "board.md", Before: line, After: line}
	if got := auditFieldChanges(c); len(got) != 0 {
		t.Errorf("a pure reorder should log nothing, got %+v", got)
	}
}

func TestAuditFieldChangesOnDeleteAndDetail(t *testing.T) {
	del := Change{Kind: ChangeDeleted, ID: "T-0001", File: "board.md",
		Before: "- [ ] [T-0001] Fix it | stage:ready"}
	if got := auditFieldChanges(del); len(got) != 1 || got[0].Field != "removed" || got[0].Value != "" {
		t.Errorf("delete should log one blank 'removed' entry, got %+v", got)
	}

	detail := Change{Kind: ChangeCreated, ID: "T-0001", File: "details/T-0001.md"}
	if got := auditFieldChanges(detail); len(got) != 1 || got[0].Field != "detail" || got[0].Value != "" {
		t.Errorf("a detail-file touch should log one blank 'detail' entry, got %+v", got)
	}
}

func TestAppendAuditEntriesSkipsChangesWithNoID(t *testing.T) {
	changes := []Change{
		{Kind: ChangeUpdated, File: "board.md", Before: "2", After: "3"}, // e.g. a WIP-limit edit
	}
	if got := appendAuditEntries(nil, time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC), changes); got != nil {
		t.Errorf("a Change with no ID should not be logged, got %q", got)
	}
}

func TestAppendAuditEntriesWritesHeaderOnce(t *testing.T) {
	now := time.Date(2026, 8, 27, 14, 32, 10, 0, time.UTC)
	c := Change{Kind: ChangeCreated, ID: "T-0001", File: "board.md",
		After: "- [ ] [T-0001] Fix it | stage:ready"}

	first := appendAuditEntries(nil, now, []Change{c})
	if !strings.HasPrefix(string(first), auditHeader) {
		t.Errorf("first write should carry the header:\n%s", first)
	}
	want := "2026-08-27T14:32:10Z | id:T-0001 | field:stage | value:ready\n"
	if !strings.HasSuffix(string(first), want) {
		t.Errorf("first write should end with %q, got:\n%s", want, first)
	}

	second := appendAuditEntries(first, now, []Change{c})
	if strings.Count(string(second), auditHeader) != 1 {
		t.Errorf("a second append must not repeat the header:\n%s", second)
	}
	if strings.Count(string(second), want) != 2 {
		t.Errorf("a second append should add one more copy of the line:\n%s", second)
	}
}

// auditDir opens a v2 directory with audit: true already set, and pins the
// clock so a timestamp assertion is exact.
func auditDir(t *testing.T, now time.Time) *Store {
	t.Helper()
	board := strings.Replace(v2Board, "wip.working: 2", "wip.working: 2\naudit: true", 1)
	dir := newV2Dir(t, map[string]string{
		"board.md":             board,
		"done.md":              v2Done,
		"details/T-0001.md":    v2Detail,
		"details/_template.md": "---\ndoc: detail\n---\n\n## Context\n",
	})
	s := mustOpen(t, dir)
	s.clock = func() time.Time { return now }
	return s
}

func readAudit(t *testing.T, s *Store) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.path, "audit.md"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAuditDisabledByDefaultWritesNothing(t *testing.T) {
	_, s := v2Dir(t)
	if _, _, err := s.Add(AddRequest{Title: "New thing"}, today); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.path, "audit.md")); !os.IsNotExist(err) {
		t.Errorf("audit.md should not exist when audit is not enabled, stat err = %v", err)
	}
}

func TestAuditEnabledLogsAddAndStart(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	s := auditDir(t, now)

	it, _, err := s.Add(AddRequest{Title: "New thing", Stage: "ready"}, today)
	if err != nil {
		t.Fatal(err)
	}
	got := readAudit(t, s)
	if !strings.Contains(got, "id:"+string(it.ID)+" | field:stage | value:ready") {
		t.Errorf("audit.md should log the new item's stage:\n%s", got)
	}
	if !strings.HasPrefix(got, auditHeader) {
		t.Errorf("audit.md should start with its header:\n%s", got)
	}

	if _, _, err := s.Start(it.ID, StartRequest{}, today); err != nil {
		t.Fatal(err)
	}
	got = readAudit(t, s)
	if !strings.Contains(got, "id:"+string(it.ID)+" | field:stage | value:working") {
		t.Errorf("audit.md should log the stage change from --start:\n%s", got)
	}
	if !strings.Contains(got, "id:"+string(it.ID)+" | field:started | value:"+today.String()) {
		t.Errorf("audit.md should log the started: stamp from --start:\n%s", got)
	}
}

// Regression: moveV2 used to mutate started:/reason:/tickler: on the item
// BEFORE taking its "before" snapshot for the Change record, which hid
// those fields from the audit differ - only the stage change (applied via
// InsertItem, after the snapshot) ever showed up. --move --stage blocked
// --reason must log both stage and reason.
func TestAuditLogsMoveReasonAndStarted(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	s := auditDir(t, now)

	if _, _, err := s.Move("T-0002", MoveRequest{Stage: "blocked", Reason: "waiting on design"}, today); err != nil {
		t.Fatal(err)
	}
	got := readAudit(t, s)
	if !strings.Contains(got, "id:T-0002 | field:stage | value:blocked") {
		t.Errorf("audit.md should log the stage change from --move:\n%s", got)
	}
	if !strings.Contains(got, "id:T-0002 | field:reason | value:waiting on design") {
		t.Errorf("audit.md should log the reason set by --move --reason:\n%s", got)
	}

	// --move --stage working must log started: too, the same as --start.
	if _, _, err := s.Move("T-0005", MoveRequest{Stage: "working"}, today); err != nil {
		t.Fatal(err)
	}
	got = readAudit(t, s)
	if !strings.Contains(got, "id:T-0005 | field:started | value:"+today.String()) {
		t.Errorf("audit.md should log started: from --move --stage working:\n%s", got)
	}
}

// --note on a v2 item always lands in its detail file (op_note.go's
// preserveNotes path - v2 has no working-file slot to write into). T-0002
// has no detail: yet, so this also touches board.md to attach one - two
// audit lines from one operation, each accurate to its own file.
func TestAuditLogsNoteAsBlankDetailValue(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	s := auditDir(t, now)

	if _, _, err := s.Note("T-0002", NoteRequest{Text: "checked with design"}, today); err != nil {
		t.Fatal(err)
	}
	got := readAudit(t, s)
	if !strings.Contains(got, "id:T-0002 | field:detail | value:\n") {
		t.Errorf("the detail-file touch should log a blank value:\n%s", got)
	}
	if !strings.Contains(got, "id:T-0002 | field:detail | value:details/T-0002.md") {
		t.Errorf("attaching detail: to the board.md line should log its actual value:\n%s", got)
	}
}

func TestAuditDryRunWritesNothing(t *testing.T) {
	now := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	s := auditDir(t, now)
	if _, _, err := s.Add(AddRequest{Title: "New thing", DryRun: true}, today); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.path, "audit.md")); !os.IsNotExist(err) {
		t.Error("a dry run must not write audit.md")
	}
}

func TestSetAuditTogglesTheBoardKey(t *testing.T) {
	_, s := v2Dir(t)
	dir, _, err := s.SetAudit(true, false)
	if err != nil {
		t.Fatal(err)
	}
	if !dir.StageCfg.AuditEnabled {
		t.Error("SetAudit(true) should enable it")
	}

	// A no-op enable writes nothing further.
	before := readFile(t, s.path, "board.md")
	if _, _, err := s.SetAudit(true, false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, s.path, "board.md"); got != before {
		t.Error("re-enabling an already-enabled audit should write nothing")
	}

	dir, _, err = s.SetAudit(false, false)
	if err != nil {
		t.Fatal(err)
	}
	if dir.StageCfg.AuditEnabled {
		t.Error("SetAudit(false) should disable it")
	}
}

func TestSetAuditRefusedOnVersion1(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": sampleBacklog,
		"done.md":    sampleDone,
	})
	s := mustOpen(t, dir)
	if _, _, err := s.SetAudit(true, false); err == nil {
		t.Error("SetAudit on a version-1 directory should be refused")
	}
}
