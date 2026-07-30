package mm

import (
	"errors"
	"testing"
)

// Expected values here were computed with an independent implementation
// (Python's datetime.isocalendar), not with the code under test.
func TestParsePeriodTokens(t *testing.T) {
	// 2026-07-29 is a Wednesday, in ISO week 2026-W31.
	ref := Date{2026, 7, 29}

	cases := []struct {
		token string
		since string
		until string
	}{
		{"all", "", ""},
		{"today", "2026-07-29", "2026-07-29"},
		{"yesterday", "2026-07-28", "2026-07-28"},
		// Monday through today, not through Sunday: a period that has not
		// finished must not claim days that have not happened.
		{"this-week", "2026-07-27", "2026-07-29"},
		{"last-week", "2026-07-20", "2026-07-26"},
		{"last-1-days", "2026-07-29", "2026-07-29"},
		{"last-7-days", "2026-07-23", "2026-07-29"},
		{"this-month", "2026-07-01", "2026-07-29"},
		{"last-month", "2026-06-01", "2026-06-30"},
		{"2026-07", "2026-07-01", "2026-07-31"},
		{"2026-02", "2026-02-01", "2026-02-28"}, // not a leap year
		{"2028-02", "2028-02-01", "2028-02-29"}, // leap year
		{"2026-W31", "2026-07-27", "2026-08-02"},
		// The week-year boundary in both directions.
		{"2026-W01", "2025-12-29", "2026-01-04"},
		{"2026-W53", "2026-12-28", "2027-01-03"},
		{"2020-W53", "2020-12-28", "2021-01-03"},
		// Case-insensitive, because a shell profile is written by a person.
		{"LAST-WEEK", "2026-07-20", "2026-07-26"},
		{"2026-w31", "2026-07-27", "2026-08-02"},
	}
	for _, c := range cases {
		p, err := ParsePeriod(c.token, ref)
		if err != nil {
			t.Errorf("%s: %v", c.token, err)
			continue
		}
		if p.Since.String() != c.since || p.Until.String() != c.until {
			t.Errorf("%s = %s..%s, want %s..%s",
				c.token, p.Since, p.Until, c.since, c.until)
		}
		if p.Label == "" {
			t.Errorf("%s: no label; every output mode must state the period", c.token)
		}
	}
}

// A week number is not a range check: week 53 exists only in long ISO years.
func TestParsePeriodRejectsWeeksThatDoNotExist(t *testing.T) {
	ref := Date{2026, 7, 29}
	for _, token := range []string{
		"2025-W53", // 2025 has 52 weeks
		"2027-W53", // so does 2027
		"2026-W54",
		"2026-W00",
		"2026-Wxx",
		"2026-13",     // month 13
		"last-0-days", // "today and the -1 days before it" is not a period
		"last--3-days",
		"next-week",
		"7 days",
		"",
	} {
		if _, err := ParsePeriod(token, ref); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%q: want ErrInvalidArgument, got %v", token, err)
		}
	}
	// 2026 IS a long year, so this one must be accepted.
	if _, err := ParsePeriod("2026-W53", ref); err != nil {
		t.Errorf("2026-W53 is a real week: %v", err)
	}
}

func TestPeriodContains(t *testing.T) {
	p, err := ParsePeriod("2026-W31", Date{2026, 7, 29})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		date string
		want bool
	}{
		{"2026-07-26", false}, // the Sunday before
		{"2026-07-27", true},  // Monday, inclusive
		{"2026-07-30", true},
		{"2026-08-02", true}, // Sunday, inclusive
		{"2026-08-03", false},
	}
	for _, c := range cases {
		d, err := ParseDate(c.date)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Contains(d); got != c.want {
			t.Errorf("Contains(%s) = %v, want %v", c.date, got, c.want)
		}
	}
	// An item with no done: field is in no period, including the unbounded one.
	if p.Contains(Date{}) {
		t.Error("a zero date should not be contained")
	}
	all, _ := ParsePeriod("all", Date{2026, 7, 29})
	if !all.Unbounded() || all.Contains(Date{}) {
		t.Error("all should be unbounded and still exclude a zero date")
	}
}

func TestPeriodBetween(t *testing.T) {
	p, err := PeriodBetween(Date{2026, 7, 1}, Date{2026, 7, 31})
	if err != nil {
		t.Fatal(err)
	}
	if !p.Contains(Date{2026, 7, 15}) || p.Contains(Date{2026, 8, 1}) {
		t.Errorf("range %s does not bound correctly", p)
	}
	// Reversed bounds are a usage error, not an empty report.
	if _, err := PeriodBetween(Date{2026, 7, 31}, Date{2026, 7, 1}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
	if _, err := PeriodBetween(Date{2026, 2, 31}, Date{}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("impossible date: want ErrInvalidArgument, got %v", err)
	}
	// One open bound is legal: --since with no --until.
	open, err := PeriodBetween(Date{2026, 7, 1}, Date{})
	if err != nil {
		t.Fatal(err)
	}
	if !open.Contains(Date{2030, 1, 1}) {
		t.Error("an open upper bound should match a later date")
	}
}

func TestISOWeekArithmetic(t *testing.T) {
	cases := []struct {
		date string
		year int
		week int
	}{
		{"2026-07-29", 2026, 31},
		{"2026-12-28", 2026, 53}, // 2026 is a long year
		{"2027-01-03", 2026, 53}, // still last year's week
		{"2027-01-04", 2027, 1},
		{"2025-12-29", 2026, 1}, // week 1 starts before the calendar year
	}
	for _, c := range cases {
		d, err := ParseDate(c.date)
		if err != nil {
			t.Fatal(err)
		}
		y, w := d.ISOWeek()
		if y != c.year || w != c.week {
			t.Errorf("%s: ISOWeek = %d-W%02d, want %d-W%02d", c.date, y, w, c.year, c.week)
		}
	}

	// WeekStart is Monday, including from a Sunday, which Go numbers 0.
	for _, c := range []struct{ from, want string }{
		{"2026-07-27", "2026-07-27"}, // Monday
		{"2026-07-29", "2026-07-27"}, // Wednesday
		{"2026-08-02", "2026-07-27"}, // Sunday
	} {
		d, _ := ParseDate(c.from)
		if got := d.WeekStart().String(); got != c.want {
			t.Errorf("WeekStart(%s) = %s, want %s", c.from, got, c.want)
		}
	}

	// AddDays crosses months and years, and handles leap day.
	for _, c := range []struct {
		from string
		n    int
		want string
	}{
		{"2026-07-31", 1, "2026-08-01"},
		{"2026-01-01", -1, "2025-12-31"},
		{"2028-02-28", 1, "2028-02-29"},
		{"2026-02-28", 1, "2026-03-01"},
	} {
		d, _ := ParseDate(c.from)
		if got := d.AddDays(c.n).String(); got != c.want {
			t.Errorf("AddDays(%s, %d) = %s, want %s", c.from, c.n, got, c.want)
		}
	}
}
