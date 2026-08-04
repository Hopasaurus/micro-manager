package mm

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// T-0045 — --stats. Every number in the fixture is small enough to check by
// hand, which is the point: a statistic nobody can verify is a statistic nobody
// should trust.
//
// The four closed items, and what each contributes:
//
//	T-0001  started 07-01  done 07-03  infra,ci  shipped    cycle 2, in flight 01-03
//	T-0002  started 07-02  done 07-02  infra     cancelled  cycle 0, in flight 02
//	T-0003  no started     done 07-10  no tags   shipped    cycle unknown, no flight
//	T-0004  started 07-06  done 07-13  ci        obsolete   cycle 7, in flight 06-13
const statsDone = `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0004] Obsolete one | tags:ci | created:2026-07-05 | started:2026-07-06 | done:2026-07-13 | outcome:obsolete
- [x] [T-0003] Never started | created:2026-07-09 | done:2026-07-10 | outcome:shipped
- [x] [T-0002] Same day | tags:infra | created:2026-07-02 | started:2026-07-02 | done:2026-07-02 | outcome:cancelled
- [x] [T-0001] Two days | tags:infra,ci | created:2026-06-30 | started:2026-07-01 | done:2026-07-03 | outcome:shipped
`

const statsBacklog = `---
doc: backlog
version: 1
project: Stats Fixture
next_id: T-0010
---

# Backlog

## Ready

## Blocked

## Someday
`

func statsDir(t *testing.T, files map[string]string) (string, *Store) {
	t.Helper()
	all := map[string]string{"backlog.md": statsBacklog, "done.md": statsDone}
	for k, v := range files {
		all[k] = v
	}
	dir := newDir(t, all)
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

// statsPeriod is the fixture's own window: the first closed day to the last.
func statsPeriod(t *testing.T) Period {
	t.Helper()
	p, err := PeriodBetween(Date{2026, 7, 1}, Date{2026, 7, 13})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStatsThroughputAndOutcomes(t *testing.T) {
	_, s := statsDir(t, nil)

	res, err := s.Stats(statsPeriod(t), StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if res.Closed != 4 {
		t.Errorf("closed = %d, want the four in the period", res.Closed)
	}
	if got := countsString(res.ByOutcome); got != "shipped=2,cancelled=1,obsolete=1" {
		t.Errorf("outcomes = %s, want them in the fixed order", got)
	}

	// Weekly buckets over 07-01..07-13: W27 holds the 2nd and the 3rd, W28 the
	// 10th, W29 the 13th.
	want := []struct {
		label  string
		closed int
	}{{"2026-W27", 2}, {"2026-W28", 1}, {"2026-W29", 1}}
	if len(res.Buckets) != len(want) {
		t.Fatalf("buckets = %+v, want three weeks", res.Buckets)
	}
	for i, w := range want {
		if res.Buckets[i].Label != w.label || res.Buckets[i].Closed != w.closed {
			t.Errorf("bucket %d = %+v, want %s closed=%d", i, res.Buckets[i], w.label, w.closed)
		}
	}
}

// Cycle time is done − started in whole days, and an item that cannot be
// measured is COUNTED, not assumed.
func TestStatsCycleTime(t *testing.T) {
	_, s := statsDir(t, nil)

	res, err := s.Stats(statsPeriod(t), StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	c := res.Cycle
	// Measurable: 0, 2, 7 days.
	if c.N != 3 || c.Unknown != 1 {
		t.Errorf("N=%d unknown=%d, want 3 measured and 1 without a started date", c.N, c.Unknown)
	}
	if c.Mean != 3 || c.Median != 2 || c.Min != 0 || c.Max != 7 {
		t.Errorf("cycle = %+v, want mean 3, median 2, min 0, max 7", c)
	}
	// p90 over [0 2 7]: position 1.8, so 2 + 0.8*(7-2).
	if c.P90 != 6 {
		t.Errorf("p90 = %v, want 6", c.P90)
	}
}

// done before started is not a negative cycle time. Neither date is invalid on
// its own, so I7 says nothing; averaging it in would hide it.
func TestStatsCycleTimeRefusesBackwardsDates(t *testing.T) {
	_, s := statsDir(t, map[string]string{
		"done.md": strings.Replace(statsDone,
			"started:2026-07-06 | done:2026-07-13", "started:2026-07-13 | done:2026-07-06", 1),
	})

	res, err := s.Stats(statsPeriod(t), StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cycle.N != 2 || res.Cycle.Unknown != 2 {
		t.Errorf("cycle = %+v, want the backwards item counted as unmeasurable", res.Cycle)
	}
	if res.Cycle.Max != 2 {
		t.Errorf("max = %d, want the backwards item excluded entirely", res.Cycle.Max)
	}
}

// WIP counts items IN FLIGHT per day: started on or before the day, not
// finished before it. An item started and finished on one day is in flight that
// day — the format cannot say otherwise (§10.1).
func TestStatsWipOverTime(t *testing.T) {
	_, s := statsDir(t, nil)

	res, err := s.Stats(statsPeriod(t), StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	// 07-01:1  07-02:2  07-03:1  07-04:0  07-05:0  07-06..07-13:1 each
	if res.WipPeak != 2 || res.WipPeakOn.String() != "2026-07-02" {
		t.Errorf("peak = %d on %s, want 2 on 2026-07-02", res.WipPeak, res.WipPeakOn)
	}
	// 12 item-days over 13 days.
	if fmt.Sprintf("%.3f", res.WipMean) != "0.923" {
		t.Errorf("mean = %v, want 12/13", res.WipMean)
	}

	// Per bucket: W27 covers 07-01..07-05 here, W28 the seven days to 07-12.
	if b := res.Buckets[0]; b.WipPeak != 2 || fmt.Sprintf("%.1f", b.WipMean) != "0.8" {
		t.Errorf("W27 = %+v, want peak 2 mean 0.8", b)
	}
	if b := res.Buckets[1]; b.WipPeak != 1 || b.WipMean != 1 {
		t.Errorf("W28 = %+v, want peak 1 mean 1", b)
	}
}

// An item still open contributes to WIP right up to today, which is the whole
// point of measuring flight rather than completion.
func TestStatsCountsOpenWorkAsInFlight(t *testing.T) {
	slot := strings.Replace(idleSlot, "status: idle", "status: working", 1)
	slot = strings.Replace(slot, "id: null", "id: T-0007", 1)
	slot = strings.Replace(slot, "title: null", "title: Still going", 1)
	slot = strings.Replace(slot, "started: null", "started: 2026-07-11", 1)
	_, s := statsDir(t, map[string]string{"working.01.md": slot})

	p, err := PeriodBetween(Date{2026, 7, 1}, Date{2026, 7, 15})
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Stats(p, StatsOptions{Bucket: BucketDay}, Date{2026, 7, 15})
	if err != nil {
		t.Fatal(err)
	}
	// Everything closed by 07-13, so a day after it can only be the open item.
	byDay := map[string]StatsBucket{}
	for _, b := range res.Buckets {
		byDay[b.Label] = b
	}
	for _, day := range []string{"2026-07-14", "2026-07-15"} {
		if got := byDay[day].WipPeak; got != 1 {
			t.Errorf("%s wip = %d, want the still-open item counted", day, got)
		}
	}
	// And on 07-11 it overlaps T-0004, the only two-item day after the 2nd.
	if got := byDay["2026-07-11"].WipPeak; got != 2 {
		t.Errorf("2026-07-11 wip = %d, want both in flight", got)
	}
	// It closed nothing, so throughput is untouched.
	if res.Closed != 4 {
		t.Errorf("closed = %d; an open item is not throughput", res.Closed)
	}
}

// Tags distribute over the closed items; an item with two tags counts twice, so
// the counts do not sum to Closed, and the untagged are counted separately.
func TestStatsTagDistribution(t *testing.T) {
	_, s := statsDir(t, nil)

	res, err := s.Stats(statsPeriod(t), StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if got := countsString(res.Tags); got != "ci=2,infra=2" {
		t.Errorf("tags = %s, want ci and infra at 2 each, alphabetical on a tie", got)
	}
	if res.Untagged != 1 {
		t.Errorf("untagged = %d, want the one item with no tags", res.Untagged)
	}
}

func TestStatsBucketSizes(t *testing.T) {
	_, s := statsDir(t, nil)
	p := statsPeriod(t)

	day, err := s.Stats(p, StatsOptions{Bucket: BucketDay}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(day.Buckets) != 13 {
		t.Errorf("day buckets = %d, want one per day in 07-01..07-13", len(day.Buckets))
	}
	if day.Buckets[1].Label != "2026-07-02" || day.Buckets[1].Closed != 1 {
		t.Errorf("second day = %+v", day.Buckets[1])
	}

	month, err := s.Stats(p, StatsOptions{Bucket: BucketMonth}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(month.Buckets) != 1 || month.Buckets[0].Label != "2026-07" || month.Buckets[0].Closed != 4 {
		t.Errorf("month buckets = %+v, want one July holding all four", month.Buckets)
	}

	if _, err := s.Stats(p, StatsOptions{Bucket: "fortnight"}, Date{2026, 8, 4}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bucket 'fortnight' = %v, want ErrInvalidArgument", err)
	}
}

// An unbounded period has no start to cut buckets from, so it takes one from
// the data: the earliest date the directory holds, through today.
func TestStatsUnboundedPeriodTakesItsRangeFromTheData(t *testing.T) {
	_, s := statsDir(t, nil)

	res, err := s.Stats(Period{}, StatsOptions{Bucket: BucketMonth}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if res.Closed != 4 {
		t.Errorf("closed = %d, want everything", res.Closed)
	}
	// The earliest date in the fixture is T-0001's created, 2026-06-30, so the
	// series opens in June and runs to today's month.
	if len(res.Buckets) != 3 {
		t.Fatalf("buckets = %+v, want June, July and August", res.Buckets)
	}
	if res.Buckets[0].Label != "2026-06" || res.Buckets[0].Closed != 0 {
		t.Errorf("first bucket = %+v, want an empty June", res.Buckets[0])
	}
	if res.Buckets[2].Label != "2026-08" {
		t.Errorf("last bucket = %+v, want today's month", res.Buckets[2])
	}
}

// A directory with nothing dated produces no series rather than a panic or a
// series starting at year zero.
func TestStatsOnAnEmptyDirectory(t *testing.T) {
	dir := newDir(t, map[string]string{"backlog.md": statsBacklog, "done.md": `---
doc: done
version: 1
---

# Done
`})
	s := mustOpen(t, dir)

	res, err := s.Stats(Period{}, StatsOptions{}, Date{2026, 8, 4})
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if res.Closed != 0 || len(res.Buckets) != 0 || res.WipPeak != 0 {
		t.Errorf("an empty directory produced %+v", res)
	}
	if res.Cycle.N != 0 || res.Cycle.Mean != 0 {
		t.Errorf("cycle = %+v, want zero", res.Cycle)
	}
}

// Archived months are outside the checked set, so they are outside the stats
// too unless asked for — and the warning says so, exactly as it does for
// --report (§5.1.11).
func TestStatsAndArchives(t *testing.T) {
	archive := `---
doc: done
version: 1
---

# Done 2025

## 2025-12

- [x] [T-0009] Ancient | tags:infra | created:2025-12-01 | started:2025-12-01 | done:2025-12-24 | outcome:shipped
`
	_, s := statsDir(t, map[string]string{"done-2025.md": archive})
	p, err := PeriodBetween(Date{2025, 12, 1}, Date{2026, 7, 13})
	if err != nil {
		t.Fatal(err)
	}

	without, err := s.Stats(p, StatsOptions{Bucket: BucketMonth}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if without.Closed != 4 {
		t.Errorf("closed = %d; the archive must not be read unasked", without.Closed)
	}
	if len(without.Warnings) == 0 {
		t.Error("a period reaching past done.md must warn that work is hidden")
	}

	with, err := s.Stats(p, StatsOptions{Bucket: BucketMonth, IncludeArchives: true}, Date{2026, 8, 4})
	if err != nil {
		t.Fatal(err)
	}
	if with.Closed != 5 {
		t.Errorf("closed = %d, want the archived item too", with.Closed)
	}
	if with.Cycle.Max != 23 {
		t.Errorf("max cycle = %d, want the archived item's 23 days", with.Cycle.Max)
	}
	if len(with.Warnings) != 0 {
		t.Errorf("nothing was hidden, so nothing to warn about: %q", with.Warnings)
	}
}

// countsString renders a distribution compactly, for comparing in one line.
func countsString(counts []TagCount) string {
	var out []string
	for _, c := range counts {
		out = append(out, fmt.Sprintf("%s=%d", c.Tag, c.Count))
	}
	return strings.Join(out, ",")
}
