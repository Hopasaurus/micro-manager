package mm

import (
	"errors"
	"strings"
	"testing"
)

const reportDone = `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0008] Ship the parser | prio:high | tags:library,parse | created:2026-07-01 | done:2026-07-29 | outcome:shipped
- [x] [T-0007] Drop the old API | tags:library | created:2026-07-01 | done:2026-07-24 | outcome:obsolete
- [x] [T-0006] Fix the flaky test | tags:test | created:2026-07-01 | done:2026-07-24 | outcome:shipped
- [x] [T-0005] Rewrite the docs | created:2026-07-01 | done:2026-07-22 | outcome:cancelled
- [x] [T-0004] Add the walker | tags:library,discover | created:2026-06-01 | done:2026-07-20 | outcome:shipped

## 2026-06

- [x] [T-0003] Older work | tags:test | created:2026-05-01 | done:2026-06-30 | outcome:shipped
`

const reportBacklog = `---
doc: backlog
version: 1
project: Report Tests
next_id: T-0011
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-0009] Next thing | prio:high | created:2026-07-29
- [ ] [T-0010] Thing after that | created:2026-07-29

## Blocked

## Someday
`

func reportDir(t *testing.T) *Store {
	t.Helper()
	dir := newDir(t, map[string]string{
		"backlog.md":    reportBacklog,
		"done.md":       reportDone,
		"working.01.md": idleSlot,
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return s
}

func reportIDs(items []Item) []ID {
	out := make([]ID, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestReportSelectsByPeriodNewestFirst(t *testing.T) {
	s := reportDir(t)
	// 2026-W31 is Mon 2026-07-27 to Sun 2026-08-02.
	p, err := ParsePeriod("2026-W31", Date{2026, 7, 29})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := reportIDs(rep.Done); !sameIDs(got, []ID{"T-0008"}) {
		t.Errorf("week 31 = %v, want just T-0008", got)
	}
	if rep.Project != "Report Tests" {
		t.Errorf("project = %q", rep.Project)
	}
	if rep.Period.Label != "2026-W31" {
		t.Errorf("the report must carry its period, got %q", rep.Period.Label)
	}

	// The previous week, which is where most of the fixture sits.
	p, _ = ParsePeriod("2026-W30", Date{2026, 7, 29})
	rep, err = s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Newest first, and file order preserved among items closed the same day.
	if got := reportIDs(rep.Done); !sameIDs(got, []ID{"T-0007", "T-0006", "T-0005", "T-0004"}) {
		t.Errorf("week 30 = %v", got)
	}
}

// All outcomes are included: a week's cancellations are part of the week.
func TestReportIncludesEveryOutcome(t *testing.T) {
	s := reportDir(t)
	p, _ := ParsePeriod("2026-07", Date{2026, 7, 29})
	rep, err := s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Done) != 5 {
		t.Fatalf("got %d items, want all 5 of July: %v", len(rep.Done), reportIDs(rep.Done))
	}
	seen := map[Outcome]bool{}
	for _, it := range rep.Done {
		seen[it.Outcome] = true
	}
	for _, o := range []Outcome{OutcomeShipped, OutcomeCancelled, OutcomeObsolete} {
		if !seen[o] {
			t.Errorf("%s is missing from the report", o)
		}
	}
}

func TestReportAll(t *testing.T) {
	s := reportDir(t)
	p, _ := ParsePeriod("all", Date{2026, 7, 29})
	rep, err := s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Done) != 6 {
		t.Errorf("all = %v, want every item", reportIDs(rep.Done))
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("an unbounded period has nothing to warn about: %v", rep.Warnings)
	}
}

func TestReportGroupBy(t *testing.T) {
	s := reportDir(t)
	p, _ := ParsePeriod("2026-07", Date{2026, 7, 29})

	// Outcome: fixed order, shipped first, empty groups dropped.
	rep, err := s.Report(p, ReportOptions{GroupBy: GroupByOutcome})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	total := 0
	for _, g := range rep.Groups {
		keys = append(keys, g.Key)
		total += len(g.Items)
	}
	if strings.Join(keys, ",") != "shipped,cancelled,obsolete" {
		t.Errorf("outcome groups = %v", keys)
	}
	if total != len(rep.Done) {
		t.Errorf("grouping lost items: %d grouped, %d total", total, len(rep.Done))
	}

	// Tag: an item repeats under each of its tags, untagged last.
	rep, err = s.Report(p, ReportOptions{GroupBy: GroupByTag})
	if err != nil {
		t.Fatal(err)
	}
	keys = nil
	for _, g := range rep.Groups {
		keys = append(keys, g.Key)
	}
	if strings.Join(keys, ",") != "discover,library,parse,test,(untagged)" {
		t.Errorf("tag groups = %v", keys)
	}
	for _, g := range rep.Groups {
		if g.Key == "library" && len(g.Items) != 3 {
			t.Errorf("library group has %d items, want 3", len(g.Items))
		}
	}

	// Day: newest first, one group per date.
	rep, err = s.Report(p, ReportOptions{GroupBy: GroupByDay})
	if err != nil {
		t.Fatal(err)
	}
	keys = nil
	for _, g := range rep.Groups {
		keys = append(keys, g.Key)
	}
	if strings.Join(keys, ",") != "2026-07-29,2026-07-24,2026-07-22,2026-07-20" {
		t.Errorf("day groups = %v", keys)
	}

	if _, err := s.Report(p, ReportOptions{GroupBy: "sideways"}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

func TestReportIncludeWipAndBacklog(t *testing.T) {
	s := reportDir(t)
	if _, _, err := s.Start("T-0009", StartRequest{}, Date{2026, 7, 29}); err != nil {
		t.Fatal(err)
	}
	p, _ := ParsePeriod("2026-07", Date{2026, 7, 29})

	rep, err := s.Report(p, ReportOptions{IncludeWip: true, IncludeBacklog: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := reportIDs(rep.Wip); !sameIDs(got, []ID{"T-0009"}) {
		t.Errorf("wip = %v", got)
	}
	if got := reportIDs(rep.Next); !sameIDs(got, []ID{"T-0010"}) {
		t.Errorf("next = %v", got)
	}

	// Without the options, neither list is populated.
	rep, _ = s.Report(p, ReportOptions{})
	if len(rep.Wip) != 0 || len(rep.Next) != 0 {
		t.Error("wip and backlog must be opt-in")
	}
}

// A period reaching past what done.md holds returns nothing and looks exactly
// like a quiet week. It must say so instead.
func TestReportWarnsAboutArchivedPeriods(t *testing.T) {
	s := reportDir(t)

	p, _ := ParsePeriod("2026-03", Date{2026, 7, 29})
	rep, err := s.Report(p, ReportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Done) != 0 {
		t.Fatalf("expected no items, got %v", reportIDs(rep.Done))
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "2026-06") {
		t.Errorf("want a warning naming the oldest month present, got %v", rep.Warnings)
	}

	// A period inside what the file holds warns about nothing.
	p, _ = ParsePeriod("2026-06", Date{2026, 7, 29})
	rep, _ = s.Report(p, ReportOptions{})
	if len(rep.Warnings) != 0 {
		t.Errorf("unexpected warning: %v", rep.Warnings)
	}
}
