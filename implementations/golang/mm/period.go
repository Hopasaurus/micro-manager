package mm

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Period resolution (spec-tools.md §5.1.11).
//
// ISO 8601 WEEK ARITHMETIC, not "seven days back". Weeks start on Monday and the
// ISO week-year diverges from the calendar year at both ends: 2025-12-29 is in
// week 1 of ISO year 2026, and 2027-01-03 is in week 53 of ISO year 2026. Some
// years have 53 weeks and most have 52, so a week number is not a range check.
// Hand-rolled week maths gets this wrong more often than not, so the calendar
// work here goes through time.Time in UTC, which has a tested ISOWeek.
//
// That is not a contradiction of the rule against time.Time for item fields
// (spec-file-format.md §3.3): the danger there is a timezone shifting a stored
// done: date across a month boundary. Nothing here is stored - a UTC time.Time
// is used for arithmetic and converted straight back to a Date.

// PeriodSource records where a period came from, so a report can say. A report
// whose period is invisible is a report you cannot check.
type PeriodSource string

const (
	PeriodFromSwitch      PeriodSource = "switch"
	PeriodFromEnvironment PeriodSource = "environment"
	PeriodFromConfig      PeriodSource = "config"
	PeriodFromDefault     PeriodSource = "default"
)

// Period is a resolved, inclusive date range.
//
// A zero Since or Until is an open bound, which is how `all` is expressed. The
// range is DATE-GRANULAR because done: is a date with no time; an implementation
// must not invent finer resolution.
type Period struct {
	Since Date
	Until Date

	// Label is the token or range this was resolved from, for display.
	Label string

	// Source is set by whoever resolved it. The library never fills it in: it
	// receives a fully resolved period and has no notion that a default was
	// applied (spec-tools.md §5.1.11 precedence is the wrapper's job).
	Source PeriodSource
}

// Unbounded reports whether the period matches every date.
func (p Period) Unbounded() bool { return p.Since.IsZero() && p.Until.IsZero() }

// Contains reports whether a date falls in the period. A zero date - an item
// with no done: field - is in no period at all.
func (p Period) Contains(d Date) bool {
	if d.IsZero() {
		return false
	}
	if !p.Since.IsZero() && d.Before(p.Since) {
		return false
	}
	if !p.Until.IsZero() && p.Until.Before(d) {
		return false
	}
	return true
}

func (p Period) String() string {
	switch {
	case p.Unbounded():
		return "all"
	case p.Since.IsZero():
		return "up to " + p.Until.String()
	case p.Until.IsZero():
		return "since " + p.Since.String()
	}
	return p.Since.String() + " to " + p.Until.String()
}

// ParsePeriod resolves one period token against a reference date.
//
// The vocabulary is shared by --period and MM_REPORT_PERIOD, so that a token
// that works in one works in the other (spec-tools.md §3.5).
func ParsePeriod(token string, today Date) (Period, error) {
	t := strings.ToLower(strings.TrimSpace(token))
	if t == "" {
		return Period{}, fmt.Errorf("%w: empty period", ErrInvalidArgument)
	}
	if !today.Valid() {
		return Period{}, fmt.Errorf("%w: %q is not a real date to resolve against",
			ErrInvalidArgument, today.String())
	}

	switch t {
	case "all":
		return Period{Label: "all"}, nil

	case "today":
		return Period{Since: today, Until: today, Label: "today"}, nil

	case "yesterday":
		d := today.AddDays(-1)
		return Period{Since: d, Until: d, Label: "yesterday"}, nil

	case "this-week":
		// Monday through TODAY, not through Sunday: a period that has not
		// finished should not claim days that have not happened.
		return Period{Since: today.WeekStart(), Until: today, Label: "this-week"}, nil

	case "last-week":
		// The most recent COMPLETE week. The default is this rather than the
		// current week because a report over a closed period is reproducible and
		// one over an open period is not.
		start := today.WeekStart().AddDays(-7)
		return Period{Since: start, Until: start.AddDays(6), Label: "last-week"}, nil

	case "this-month":
		return Period{
			Since: Date{today.Year, today.Month, 1},
			Until: today,
			Label: "this-month",
		}, nil

	case "last-month":
		y, m := today.Year, today.Month-1
		if m == 0 {
			y, m = y-1, 12
		}
		return Period{
			Since: Date{y, m, 1},
			Until: Date{y, m, DaysInMonth(y, m)},
			Label: "last-month",
		}, nil
	}

	if n, ok := lastNDays(t); ok {
		if n < 1 {
			return Period{}, fmt.Errorf(
				"%w: last-%d-days: N must be at least 1", ErrInvalidArgument, n)
		}
		// Today and the N-1 days before it: last-1-days is today.
		return Period{Since: today.AddDays(-(n - 1)), Until: today, Label: t}, nil
	}
	if p, ok, err := parseWeekToken(t); ok {
		return p, err
	}
	if p, ok, err := parseMonthToken(t); ok {
		return p, err
	}

	return Period{}, fmt.Errorf(
		"%w: %q is not a period; try last-week, this-week, YYYY-Www, last-N-days, "+
			"YYYY-MM, last-month, this-month or all", ErrInvalidArgument, token)
}

// PeriodBetween builds an explicit inclusive range.
func PeriodBetween(since, until Date) (Period, error) {
	if !since.IsZero() && !since.Valid() {
		return Period{}, fmt.Errorf("%w: since:%s is not a real date", ErrInvalidArgument, since)
	}
	if !until.IsZero() && !until.Valid() {
		return Period{}, fmt.Errorf("%w: until:%s is not a real date", ErrInvalidArgument, until)
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return Period{}, fmt.Errorf(
			"%w: since:%s is after until:%s", ErrInvalidArgument, since, until)
	}
	p := Period{Since: since, Until: until}
	p.Label = p.String()
	return p, nil
}

// lastNDays matches "last-N-days".
func lastNDays(t string) (int, bool) {
	if !strings.HasPrefix(t, "last-") || !strings.HasSuffix(t, "-days") {
		return 0, false
	}
	mid := t[len("last-") : len(t)-len("-days")]
	if mid == "" {
		return 0, false
	}
	n, err := strconv.Atoi(mid)
	if err != nil {
		return 0, false
	}
	return n, true
}

// parseWeekToken matches YYYY-Www, an ISO 8601 week date.
func parseWeekToken(t string) (Period, bool, error) {
	if len(t) != 8 || t[4] != '-' || t[5] != 'w' {
		return Period{}, false, nil
	}
	year, err1 := strconv.Atoi(t[0:4])
	week, err2 := strconv.Atoi(t[6:8])
	if err1 != nil || err2 != nil {
		return Period{}, true, fmt.Errorf("%w: %q is not an ISO week (YYYY-Www)",
			ErrInvalidArgument, t)
	}
	start, ok := isoWeekStart(year, week)
	if !ok {
		// Week 53 exists only in long ISO years, so this is not just a range
		// check - 2026-W53 names a week that does not exist.
		return Period{}, true, fmt.Errorf(
			"%w: %04d-W%02d is not a week of ISO year %d", ErrInvalidArgument, year, week, year)
	}
	label := fmt.Sprintf("%04d-W%02d", year, week)
	return Period{Since: start, Until: start.AddDays(6), Label: label}, true, nil
}

// parseMonthToken matches YYYY-MM.
func parseMonthToken(t string) (Period, bool, error) {
	if len(t) != 7 || t[4] != '-' {
		return Period{}, false, nil
	}
	year, err1 := strconv.Atoi(t[0:4])
	month, err2 := strconv.Atoi(t[5:7])
	if err1 != nil || err2 != nil {
		return Period{}, true, fmt.Errorf("%w: %q is not a month (YYYY-MM)", ErrInvalidArgument, t)
	}
	if month < 1 || month > 12 {
		return Period{}, true, fmt.Errorf("%w: %q is not a month (YYYY-MM)", ErrInvalidArgument, t)
	}
	return Period{
		Since: Date{year, month, 1},
		Until: Date{year, month, DaysInMonth(year, month)},
		Label: fmt.Sprintf("%04d-%02d", year, month),
	}, true, nil
}

// ---------------------------------------------------------------------------
// Calendar arithmetic
// ---------------------------------------------------------------------------

func (d Date) time() time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC)
}

func dateOf(t time.Time) Date {
	y, m, day := t.Date()
	return Date{Year: y, Month: int(m), Day: day}
}

// AddDays returns the date n days later, or earlier when n is negative.
func (d Date) AddDays(n int) Date { return dateOf(d.time().AddDate(0, 0, n)) }

// Weekday returns the day of the week.
func (d Date) Weekday() time.Weekday { return d.time().Weekday() }

// ISOWeek returns the ISO 8601 week-numbering year and week. The year is NOT
// always the calendar year: 2026-12-28 is week 1 of 2027.
func (d Date) ISOWeek() (year, week int) { return d.time().ISOWeek() }

// WeekStart returns the Monday of the date's ISO week.
func (d Date) WeekStart() Date {
	// Go numbers Sunday 0; ISO weeks start on Monday, so Sunday is 6 days in.
	offset := int(d.Weekday()) - int(time.Monday)
	if offset < 0 {
		offset += 7
	}
	return d.AddDays(-offset)
}

// isoWeekStart returns the Monday of an ISO week, and whether that week exists.
//
// January 4th is in week 1 of its ISO year by definition, which is the anchor
// the whole calculation hangs off. The result is verified by asking for its own
// week back: a year with 52 weeks answers a request for week 53 with a date in
// week 1 of the next year, and that mismatch is how the week is rejected.
func isoWeekStart(year, week int) (Date, bool) {
	if week < 1 || week > 53 {
		return Date{}, false
	}
	jan4 := Date{Year: year, Month: 1, Day: 4}
	start := jan4.WeekStart().AddDays((week - 1) * 7)
	gotYear, gotWeek := start.ISOWeek()
	if gotYear != year || gotWeek != week {
		return Date{}, false
	}
	return start, true
}
