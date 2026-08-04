package mm

import (
	"fmt"
	"sort"
	"time"
)

// Stats: throughput, cycle time, WIP over time and tag distribution
// (spec-tools.md §5.3).
//
// Everything here is derived from three date fields — created, started, done —
// because they are all the format records (spec-file-format.md §10.1). That
// bounds what can honestly be computed, and the bounds are stated rather than
// papered over:
//
//   - **Cycle time is whole days**, done − started. Two items closed on the same
//     day have no order, and an item started and finished the same day has a
//     cycle time of 0. That is the format being date-granular, not a rounding
//     choice made here.
//   - **WIP over time is items IN FLIGHT, not slots occupied.** An item counts
//     from its started date to its done date inclusive, because that is what the
//     files say. A pause leaves no trace (§5.2: the slot is reset), so an item
//     paused for a month still reads as in flight for that month. The WIP LIMIT
//     is not recorded historically either (§10.7) — done.md shows what was
//     finished, never how many slots were open at the time — so this is a
//     measure of work in progress, not of pressure against the limit.
//   - **An item with no started date has no cycle time and no WIP contribution.**
//     It is counted separately rather than assumed to have started when it was
//     created: a guess dressed as a measurement is worse than a gap.
//
// The period arrives resolved, as it does for Report: precedence between
// switches and defaults is the front end's job.

// BucketSize is the resolution of the throughput and WIP series.
type BucketSize string

const (
	BucketDay   BucketSize = "day"
	BucketWeek  BucketSize = "week"
	BucketMonth BucketSize = "month"
)

// ParseBucket reads a bucket size. Empty means the default, a week: the format's
// natural reporting unit, and what --report is built around.
func ParseBucket(s string) (BucketSize, error) {
	switch BucketSize(s) {
	case BucketDay, BucketWeek, BucketMonth:
		return BucketSize(s), nil
	case "":
		return BucketWeek, nil
	}
	return "", fmt.Errorf("%w: unknown bucket %q (want day, week or month)",
		ErrInvalidArgument, s)
}

// StatsOptions controls how the series are cut.
type StatsOptions struct {
	Bucket BucketSize

	// IncludeArchives reads the done-YYYY.md files as well, exactly as
	// ReportOptions does (§5.1.11). Stats over a long history usually wants
	// them; without them an archived period reads as a period in which nothing
	// was closed, which is what the warning says.
	IncludeArchives bool
}

// StatsBucket is one interval of the series: what closed in it, and how much
// was in flight during it.
type StatsBucket struct {
	Label string
	Since Date
	Until Date

	// Closed is throughput: items whose done date falls in this bucket.
	Closed int

	// WipPeak and WipMean summarise the days IN this bucket. Mean is over the
	// bucket's days, so a quiet week reads as a low mean rather than as a gap.
	WipPeak int
	WipMean float64
}

// CycleTime summarises done − started, in whole days.
type CycleTime struct {
	// N is how many closed items had both dates and could be measured;
	// Unknown is how many could not, which is a fact about the data.
	N       int
	Unknown int

	Mean   float64
	Median float64
	P90    float64
	Min    int
	Max    int
}

// TagCount is one row of the tag distribution.
type TagCount struct {
	Tag   string
	Count int
}

// StatsResult is what a directory did over a period.
type StatsResult struct {
	Period  Period
	Project string
	Path    string
	Bucket  BucketSize

	// Closed is the total in the period; ByOutcome splits it, in the fixed
	// order shipped, cancelled, obsolete, so two runs read the same way.
	Closed    int
	ByOutcome []TagCount

	Buckets []StatsBucket

	Cycle CycleTime

	// WipPeak is the highest count on any single day in the period, and
	// WipPeakOn the first day it reached it. WipMean is over every day.
	WipPeak   int
	WipPeakOn Date
	WipMean   float64

	// Tags distributes the closed items over their tags, commonest first, then
	// alphabetically. An item with several tags counts once per tag, so the
	// counts do not sum to Closed; Untagged is the items with none.
	Tags     []TagCount
	Untagged int

	Warnings []string
}

// Stats computes the four measures over a period.
//
// An unbounded period ("all") is resolved against the data: from the earliest
// date the directory holds to today. Without that the series would have no
// beginning to cut buckets from.
func (s *Store) Stats(p Period, opts StatsOptions, today Date) (StatsResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bucket, err := ParseBucket(string(opts.Bucket))
	if err != nil {
		return StatsResult{}, err
	}
	m, err := s.load()
	if err != nil {
		return StatsResult{}, err
	}

	res := StatsResult{Period: p, Path: s.path, Bucket: bucket}
	if m.backlog != nil {
		res.Project = m.backlog.FM.Get("project")
	}

	// Every item the directory can see, including the ones still open: WIP is
	// about what was in flight, not only about what was finished.
	all := make([]Item, 0, 64)
	for _, it := range m.items() {
		all = append(all, *it)
	}
	archives := 0
	if opts.IncludeArchives {
		archived, n, err := s.archivedItems(m)
		if err != nil {
			return StatsResult{}, err
		}
		archives = n
		all = append(all, archived...)
	}
	if archives == 0 {
		res.Warnings = append(res.Warnings, archiveWarning(m, p)...)
	}

	// Closed items in the period drive throughput, cycle time and tags.
	var closed []Item
	for _, it := range all {
		if it.Done.IsZero() {
			continue
		}
		if p.Unbounded() || p.Contains(it.Done) {
			closed = append(closed, it)
		}
	}
	res.Closed = len(closed)
	res.ByOutcome = outcomeCounts(closed)
	res.Cycle = cycleTime(closed)
	res.Tags, res.Untagged = tagCounts(closed)

	// The series needs a bounded range; an unbounded period takes one from the
	// data rather than inventing a start.
	since, until := seriesRange(p, all, today)
	if since.IsZero() || until.Before(since) {
		return res, nil // nothing dated at all: no series to cut
	}

	daily := wipByDay(all, since, until)
	res.WipPeak, res.WipPeakOn, res.WipMean = summarise(daily, since, until)
	res.Buckets = buildBuckets(bucket, since, until, closed, daily)
	return res, nil
}

// outcomeCounts splits the closed items by outcome, in a fixed order: a
// distribution whose rows move between runs cannot be compared by eye.
func outcomeCounts(items []Item) []TagCount {
	counts := map[Outcome]int{}
	for _, it := range items {
		counts[it.Outcome]++
	}
	var out []TagCount
	for _, o := range []Outcome{OutcomeShipped, OutcomeCancelled, OutcomeObsolete} {
		if n := counts[o]; n > 0 {
			out = append(out, TagCount{Tag: string(o), Count: n})
		}
		delete(counts, o)
	}
	// Anything else - an item with no outcome at all, which I6 reports - is
	// still counted, or the parts would not sum to the whole.
	var rest []string
	for o := range counts {
		rest = append(rest, string(o))
	}
	sort.Strings(rest)
	for _, o := range rest {
		out = append(out, TagCount{Tag: o, Count: counts[Outcome(o)]})
	}
	return out
}

// cycleTime measures done − started over the closed items that carry both.
func cycleTime(items []Item) CycleTime {
	var days []int
	var ct CycleTime
	for _, it := range items {
		if it.Started.IsZero() {
			ct.Unknown++
			continue
		}
		d := daysBetween(it.Started, it.Done)
		if d < 0 {
			// done before started: the dates are wrong, and averaging them in
			// would hide it. I7 does not catch this - neither date is invalid
			// on its own - so it is counted as unmeasurable rather than used.
			ct.Unknown++
			continue
		}
		days = append(days, d)
	}
	ct.N = len(days)
	if ct.N == 0 {
		return ct
	}
	sort.Ints(days)
	total := 0
	for _, d := range days {
		total += d
	}
	ct.Mean = float64(total) / float64(ct.N)
	ct.Median = quantile(days, 0.5)
	ct.P90 = quantile(days, 0.9)
	ct.Min, ct.Max = days[0], days[len(days)-1]
	return ct
}

// quantile is the linear-interpolation percentile over a sorted slice.
func quantile(sorted []int, q float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return float64(sorted[0])
	}
	pos := q * float64(len(sorted)-1)
	lo := int(pos)
	hi := lo + 1
	if hi >= len(sorted) {
		return float64(sorted[len(sorted)-1])
	}
	frac := pos - float64(lo)
	return float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo])
}

// tagCounts distributes items over their tags, commonest first then
// alphabetically, and counts the untagged separately.
func tagCounts(items []Item) ([]TagCount, int) {
	counts := map[string]int{}
	untagged := 0
	for _, it := range items {
		if len(it.Tags) == 0 {
			untagged++
			continue
		}
		for _, tag := range it.Tags {
			counts[tag]++
		}
	}
	out := make([]TagCount, 0, len(counts))
	for tag, n := range counts {
		out = append(out, TagCount{Tag: tag, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out, untagged
}

// seriesRange bounds the series. A period with both ends set is taken as
// given; an open or unbounded one is closed against the data and today.
func seriesRange(p Period, items []Item, today Date) (Date, Date) {
	since, until := p.Since, p.Until
	if since.IsZero() {
		for _, it := range items {
			for _, d := range []Date{it.Started, it.Done, it.Created} {
				if d.IsZero() {
					continue
				}
				if since.IsZero() || d.Before(since) {
					since = d
				}
			}
		}
	}
	if until.IsZero() {
		until = today
	}
	return since, until
}

// wipByDay counts, for each day in the range, the items in flight on it: started
// on or before it, and not yet done — or done on it, since an item finished on a
// day was in flight that day.
func wipByDay(items []Item, since, until Date) []int {
	n := daysBetween(since, until) + 1
	if n <= 0 {
		return nil
	}
	out := make([]int, n)
	for _, it := range items {
		if it.Started.IsZero() {
			continue
		}
		from := daysBetween(since, it.Started)
		to := n - 1
		if !it.Done.IsZero() {
			to = daysBetween(since, it.Done)
		}
		if to < 0 || from >= n {
			continue // entirely outside the range
		}
		if from < 0 {
			from = 0
		}
		if to >= n {
			to = n - 1
		}
		for i := from; i <= to; i++ {
			out[i]++
		}
	}
	return out
}

// summarise reduces the daily series to peak, the first day it peaked, and mean.
func summarise(daily []int, since, until Date) (int, Date, float64) {
	if len(daily) == 0 {
		return 0, Date{}, 0
	}
	peak, at, total := 0, Date{}, 0
	for i, n := range daily {
		total += n
		if n > peak {
			peak, at = n, since.AddDays(i)
		}
	}
	return peak, at, float64(total) / float64(len(daily))
}

// buildBuckets cuts the range into intervals and fills each with its throughput
// and its WIP summary.
func buildBuckets(size BucketSize, since, until Date, closed []Item, daily []int) []StatsBucket {
	var out []StatsBucket
	for start := bucketStart(size, since); !until.Before(start); start = bucketNext(size, start) {
		end := bucketNext(size, start).AddDays(-1)
		b := StatsBucket{Label: bucketLabel(size, start), Since: start, Until: end}

		// Clipped to the range, so the first and last buckets do not average
		// over days the period never covered.
		from, to := start, end
		if from.Before(since) {
			from = since
		}
		if until.Before(to) {
			to = until
		}
		for _, it := range closed {
			if !it.Done.Before(from) && !to.Before(it.Done) {
				b.Closed++
			}
		}
		lo, hi := daysBetween(since, from), daysBetween(since, to)
		if lo >= 0 && hi < len(daily) && hi >= lo {
			total := 0
			for i := lo; i <= hi; i++ {
				total += daily[i]
				if daily[i] > b.WipPeak {
					b.WipPeak = daily[i]
				}
			}
			b.WipMean = float64(total) / float64(hi-lo+1)
		}
		out = append(out, b)
	}
	return out
}

// bucketStart snaps a date back to the start of its bucket: the day itself, the
// Monday of its ISO week, or the first of its month.
func bucketStart(size BucketSize, d Date) Date {
	switch size {
	case BucketWeek:
		return d.WeekStart()
	case BucketMonth:
		return Date{Year: d.Year, Month: d.Month, Day: 1}
	}
	return d
}

func bucketNext(size BucketSize, start Date) Date {
	switch size {
	case BucketWeek:
		return start.AddDays(7)
	case BucketMonth:
		return dateOf(start.time().AddDate(0, 1, 0))
	}
	return start.AddDays(1)
}

func bucketLabel(size BucketSize, start Date) string {
	switch size {
	case BucketWeek:
		year, week := start.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	case BucketMonth:
		return start.Month7()
	}
	return start.String()
}

// daysBetween is b − a in whole days. Both are calendar dates, so this is
// exact: the underlying times are midnight UTC (period.go).
func daysBetween(a, b Date) int {
	return int(b.time().Sub(a.time()) / (24 * time.Hour))
}
