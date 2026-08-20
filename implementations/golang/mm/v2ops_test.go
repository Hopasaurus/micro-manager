package mm

import (
	"strings"
	"testing"
)

// Version-2 correctness for the operations T-0232 covers: List/Status/Next,
// Remove, Update (via the shared writeItemLine helper), AttachDetail/
// DetachDetail (same helper), and confirmation that Search/Stats/Report/
// Archive already handle a board.md directory (op_stats.go and report.go
// needed a small Project fix along the way; op_archive.go and search.go
// needed none).

func TestListV2DefaultsToOpenBoardItems(t *testing.T) {
	_, s := v2Dir(t)
	items, err := s.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOfValues(items)
	want := []ID{"T-0001", "T-0002", "T-0003", "T-0005", "T-0006", "T-0007"}
	if !sameIDs(got, want) {
		t.Errorf("List(Filter{}) = %v, want %v (a bare filter must not silently exclude every v2 item)", got, want)
	}
}

func TestListV2StageFilter(t *testing.T) {
	_, s := v2Dir(t)
	items, err := s.List(Filter{Stage: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOfValues(items); !sameIDs(got, []ID{"T-0001", "T-0002"}) {
		t.Errorf("List(Stage:ready) = %v, want [T-0001 T-0002]", got)
	}
}

func TestListV2BlockedFilterMatchesReason(t *testing.T) {
	_, s := v2Dir(t)
	items, err := s.List(Filter{Blocked: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOfValues(items); !sameIDs(got, []ID{"T-0003"}) {
		t.Errorf("List(Blocked:true) = %v, want [T-0003] (a v2 reason: field must count)", got)
	}
}

func TestListV2StateAllIncludesDone(t *testing.T) {
	_, s := v2Dir(t)
	items, err := s.List(Filter{State: StateAll})
	if err != nil {
		t.Fatal(err)
	}
	got := idsOfValues(items)
	want := []ID{"T-0001", "T-0002", "T-0003", "T-0005", "T-0006", "T-0007", "T-0009"}
	if !sameIDs(got, want) {
		t.Errorf("List(StateAll) = %v, want %v", got, want)
	}
}

func TestStatusV2StageCounts(t *testing.T) {
	_, s := v2Dir(t)
	st, err := s.Status()
	if err != nil {
		t.Fatal(err)
	}
	want := map[Stage]int{"someday": 1, "ready": 2, "blocked": 1, "working": 1, "review": 1}
	for stage, n := range want {
		if st.StageCounts[stage] != n {
			t.Errorf("StageCounts[%s] = %d, want %d", stage, st.StageCounts[stage], n)
		}
	}
	if st.Done != 1 {
		t.Errorf("Done = %d, want 1", st.Done)
	}
	if st.Next == nil || st.Next.ID != "T-0001" {
		t.Errorf("Next = %v, want T-0001 (the first item on the ready stage)", st.Next)
	}
	if st.OldestReady == nil || st.OldestReady.ID != "T-0001" {
		t.Errorf("OldestReady = %v, want T-0001", st.OldestReady)
	}
}

func TestNextV2(t *testing.T) {
	_, s := v2Dir(t)
	it, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "T-0001" {
		t.Errorf("Next() = %s, want T-0001", it.ID)
	}
}

func TestNextV2NoReadyStageDeclared(t *testing.T) {
	dir := newV2Dir(t, map[string]string{
		"board.md": strings.Replace(v2Board, "stages: someday,ready,blocked,working,review",
			"stages: someday,blocked,working,review", 1),
		"done.md": v2Done,
	})
	s := mustOpen(t, dir)
	if _, err := s.Next(); err == nil {
		t.Error("Next() should fail when the directory declares no \"ready\" stage")
	}
}

func TestRemoveV2(t *testing.T) {
	_, s := v2Dir(t)
	// T-0002 has no detail file: the plain removal path.
	out, _, err := s.Remove("T-0002", RemoveRequest{Force: true}, today)
	if err != nil {
		t.Fatalf("remove T-0002: %v", err)
	}
	if out.DetailOrphan != "" || out.DetailDeleted != "" {
		t.Errorf("T-0002 has no detail file, got orphan=%q deleted=%q", out.DetailOrphan, out.DetailDeleted)
	}
	if _, err := s.Get("T-0002"); err == nil {
		t.Error("T-0002 should be gone")
	}

	// T-0001 has a detail file: removing WithDetail must take it along.
	if _, _, err := s.Remove("T-0001", RemoveRequest{Force: true, WithDetail: true}, today); err != nil {
		t.Fatalf("remove T-0001: %v", err)
	}
	if _, err := s.Detail("T-0001"); err == nil {
		t.Error("T-0001's detail file should be gone")
	}
}

func TestUpdateV2(t *testing.T) {
	_, s := v2Dir(t)
	title := "Second, retitled"
	it, _, err := s.Update("T-0002", UpdateRequest{Title: &title}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if it.Title != title {
		t.Errorf("Title = %q, want %q", it.Title, title)
	}
	// Re-read to prove the write actually landed in board.md, not just the
	// in-memory return value.
	reread, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if reread.Title != title {
		t.Errorf("re-read Title = %q, want %q", reread.Title, title)
	}
}

func TestUpdateV2BlockedRoutesToReason(t *testing.T) {
	_, s := v2Dir(t)
	reason := "still waiting on legal"
	it, _, err := s.Update("T-0003", UpdateRequest{Blocked: &reason}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if it.Reason != reason {
		t.Errorf("Reason = %q, want %q", it.Reason, reason)
	}
	if it.Blocked != "" {
		t.Errorf("Blocked = %q, want empty; v2 must never write a blocked: field", it.Blocked)
	}

	reread, err := s.Get("T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if reread.Reason != reason || reread.Blocked != "" {
		t.Errorf("re-read reason=%q blocked=%q, want %q/empty", reread.Reason, reread.Blocked, reason)
	}
}

func TestUpdateV2SetReasonGeneric(t *testing.T) {
	_, s := v2Dir(t)
	// The --set escape hatch (spec-file-format.md §9) must still reach reason:
	// even without a dedicated request field.
	it, _, err := s.Update("T-0003", UpdateRequest{Set: []Field{{Key: "reason", Value: "escalated"}}}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if it.Reason != "escalated" {
		t.Errorf("Reason = %q, want escalated", it.Reason)
	}
}

func TestAttachDetailV2(t *testing.T) {
	_, s := v2Dir(t)
	d, _, err := s.AttachDetail("T-0002", AttachDetailRequest{Body: "some long-form notes"}, today)
	if err != nil {
		t.Fatalf("AttachDetail: %v", err)
	}
	if d.Path != "details/T-0002.md" {
		t.Errorf("Path = %q", d.Path)
	}
	it, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if it.Detail != "details/T-0002.md" {
		t.Errorf("item Detail = %q, want details/T-0002.md", it.Detail)
	}
}

func TestDetachDetailV2(t *testing.T) {
	_, s := v2Dir(t)
	if _, err := s.DetachDetail("T-0001", DetachDetailRequest{AllowOrphan: true}, today); err != nil {
		t.Fatalf("DetachDetail: %v", err)
	}
	it, err := s.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if it.Detail != "" {
		t.Errorf("Detail = %q, want empty", it.Detail)
	}
	if _, err := s.Detail("T-0001"); err == nil {
		t.Error("Detail() should fail once the item no longer references a file")
	}
}

func TestSearchV2(t *testing.T) {
	_, s := v2Dir(t)
	hits, err := s.Search(SearchRequest{Query: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range hits {
		if h.Item.ID == "T-0001" {
			found = true
		}
	}
	if !found {
		t.Errorf("search for \"deploy\" should find T-0001, got %+v", hits)
	}
}

func TestStatsV2ProjectField(t *testing.T) {
	_, s := v2Dir(t)
	p, err := ParsePeriod("all", today)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Stats(p, StatsOptions{}, today)
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "V2 Tests" {
		t.Errorf("Project = %q, want %q", res.Project, "V2 Tests")
	}
}

func TestReportV2ProjectField(t *testing.T) {
	_, s := v2Dir(t)
	p, err := ParsePeriod("all", today)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Project != "V2 Tests" {
		t.Errorf("Project = %q, want %q", rep.Project, "V2 Tests")
	}
	if len(rep.Done) != 1 || rep.Done[0].ID != "T-0009" {
		t.Errorf("Done = %+v, want just T-0009", rep.Done)
	}
}

const v2DoneWithOldMonth = `---
doc: done
version: 2
---

# Done

## 2026-07

- [x] [T-0009] Already closed | created:2026-07-01 | done:2026-07-10 | outcome:shipped

## 2025-01

- [x] [T-0008] Old one | created:2025-01-01 | done:2025-01-15 | outcome:shipped
`

func TestArchiveV2(t *testing.T) {
	dir := newV2Dir(t, map[string]string{
		"board.md": v2Board,
		"done.md":  v2DoneWithOldMonth,
	})
	s := mustOpen(t, dir)

	res, _, err := s.Archive(ArchiveRequest{Before: Date{2026, 2, 1}}, today)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if len(res.Months) != 1 || res.Months[0] != "2025-01" {
		t.Errorf("Months = %v, want [2025-01]", res.Months)
	}

	items, err := s.List(Filter{State: StateAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.ID == "T-0008" {
			t.Error("T-0008 should have left done.md")
		}
	}
}
