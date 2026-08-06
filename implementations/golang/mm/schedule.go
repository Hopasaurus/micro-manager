package mm

import (
	"fmt"
	"strings"
)

// The tickler schedule, from spec-file-format.md §3.3 (SCHEDULE) and the
// semantics pinned by spec-tools.md §5.3.3.
//
// A schedule is one of three shapes, each with an optional @HH:MM wall time
// (absent time = 00:00):
//
//	one-shot   DATE               -- 2026-09-01, 2026-09-01@08:00
//	weekday    [ord-]weekday     -- mon@08:00, first-mon@08:00, last-mon
//	monthday   DD | last         -- 15@08:00, last@08:00
//
// The checker validates SHAPE only (I7) and never evaluates a schedule: no
// clock enters the format, and a schedule cannot make a directory invalid
// because a clock disagrees (spec-file-format.md §10.1).

// Schedule is an immutable tickler schedule expression.
type Schedule struct {
	kind     scheduleKind
	fireDate Date // one-shot: the date
	weekday  Weekday
	ordinal  ordinal
	monthday int // 1..31, or 0 meaning "the last day of the month"
	hour     int // wall time; hasTime distinguishes an explicit 00:00
	minute   int
	hasTime  bool
}

type scheduleKind int

const (
	schedNone scheduleKind = iota
	schedOneShot
	schedWeekday
	schedMonthday
)

// Weekday is a day of the week, Monday=0 through Sunday=6, matching the
// schedule grammar's lowercase names.
type Weekday int

const (
	Monday Weekday = iota
	Tuesday
	Wednesday
	Thursday
	Friday
	Saturday
	Sunday
)

func parseWeekday(s string) (Weekday, bool) {
	switch s {
	case "mon":
		return Monday, true
	case "tue":
		return Tuesday, true
	case "wed":
		return Wednesday, true
	case "thu":
		return Thursday, true
	case "fri":
		return Friday, true
	case "sat":
		return Saturday, true
	case "sun":
		return Sunday, true
	}
	return 0, false
}

func (w Weekday) String() string {
	switch w {
	case Monday:
		return "mon"
	case Tuesday:
		return "tue"
	case Wednesday:
		return "wed"
	case Thursday:
		return "thu"
	case Friday:
		return "fri"
	case Saturday:
		return "sat"
	case Sunday:
		return "sun"
	}
	return ""
}

// ordinal narrows a weekday to one occurrence per month. Empty means every
// occurrence.
type ordinal string

const (
	ordinalNone   ordinal = ""
	ordinalFirst  ordinal = "first"
	ordinalSecond ordinal = "second"
	ordinalThird  ordinal = "third"
	ordinalFourth ordinal = "fourth"
	ordinalLast   ordinal = "last"
)

func parseOrdinal(s string) (ordinal, bool) {
	switch ordinal(s) {
	case ordinalFirst, ordinalSecond, ordinalThird, ordinalFourth, ordinalLast:
		return ordinal(s), true
	}
	return "", false
}

// ParseSchedule parses a SCHEDULE expression (spec-file-format.md §3.3).
//
// The forms are exact: a bare number like "5" is NOT a month day (the grammar
// zero-pads to two digits), an uppercase weekday is not a weekday, and a
// time of 24:00 does not exist. The library is the single place the grammar
// lives: the GUI composes values, the checker validates shape, and a hand
// edit that slips through both is rejected here.
func ParseSchedule(s string) (Schedule, error) {
	bad := func() (Schedule, error) {
		return Schedule{}, fmt.Errorf(
			"%w: %q is not a SCHEDULE (a date, a weekday, or a month day, each with an optional @HH:MM time)",
			ErrInvalidArgument, s)
	}

	base := s
	var sch Schedule
	if at := strings.Index(s, "@"); at >= 0 {
		timePart := s[at+1:]
		if len(timePart) != 5 || timePart[2] != ':' {
			return bad()
		}
		h := int(timePart[0]-'0')*10 + int(timePart[1]-'0')
		m := int(timePart[3]-'0')*10 + int(timePart[4]-'0')
		for _, r := range timePart {
			if r == ':' {
				continue
			}
			if r < '0' || r > '9' {
				return bad()
			}
		}
		if h > 23 || m > 59 {
			return bad()
		}
		sch.hour, sch.minute, sch.hasTime = h, m, true
		base = s[:at]
	}

	// A calendar date. A date-like string is a date or it is nothing: an
	// ordinal-weekday base like "first-mon" never looks like YYYY-MM-DD, so a
	// ten-character base with hyphens at 4 and 7 that is not a real date is
	// rejected rather than tried against the other shapes.
	if len(base) == 10 && base[4] == '-' && base[7] == '-' {
		d, err := ParseDate(base)
		if err != nil {
			return bad()
		}
		sch.kind = schedOneShot
		sch.fireDate = d
		return sch, nil
	}

	// ordinal-weekday: "first-mon", "last-mon".
	if i := strings.Index(base, "-"); i > 0 {
		ord, ok := parseOrdinal(base[:i])
		if !ok {
			return bad()
		}
		wd, ok := parseWeekday(base[i+1:])
		if !ok {
			return bad()
		}
		sch.kind = schedWeekday
		sch.ordinal = ord
		sch.weekday = wd
		return sch, nil
	}

	// Bare weekday.
	if wd, ok := parseWeekday(base); ok {
		sch.kind = schedWeekday
		sch.weekday = wd
		return sch, nil
	}

	// The last day of every month.
	if base == "last" {
		sch.kind = schedMonthday
		sch.monthday = 0
		return sch, nil
	}

	// A numbered month day, zero-padded to two digits.
	if len(base) == 2 && base[0] >= '0' && base[0] <= '9' && base[1] >= '0' && base[1] <= '9' {
		n := int(base[0]-'0')*10 + int(base[1]-'0')
		if n >= 1 && n <= 31 {
			sch.kind = schedMonthday
			sch.monthday = n
			return sch, nil
		}
	}
	return bad()
}

// String renders the canonical form, exactly as ParseSchedule read it: a
// value that round-trips through String parses back to an identical schedule.
func (s Schedule) String() string {
	var b strings.Builder
	switch s.kind {
	case schedOneShot:
		b.WriteString(s.fireDate.String())
	case schedWeekday:
		if s.ordinal != ordinalNone {
			b.WriteString(string(s.ordinal))
			b.WriteString("-")
		}
		b.WriteString(s.weekday.String())
	case schedMonthday:
		if s.monthday == 0 {
			b.WriteString("last")
		} else {
			fmt.Fprintf(&b, "%02d", s.monthday)
		}
	}
	if s.hasTime {
		fmt.Fprintf(&b, "@%02d:%02d", s.hour, s.minute)
	}
	return b.String()
}

// IsOneShot reports whether the schedule is a bare date: it fires once and is
// then consumed (spec-tools.md §5.3.3). The weekday and monthday shapes
// recur, making their item a prototype.
func (s Schedule) IsOneShot() bool { return s.kind == schedOneShot }

// FireDate returns a one-shot's date, or the zero Date when the schedule
// recurs.
func (s Schedule) FireDate() Date {
	if s.kind == schedOneShot {
		return s.fireDate
	}
	return Date{}
}

// Time returns the schedule's wall time. hasTime distinguishes an explicit
// 00:00 from no time at all.
func (s Schedule) Time() (hour, minute int, hasTime bool) { return s.hour, s.minute, s.hasTime }

// Next returns the smallest fire DATE strictly after after, or the zero Date
// when the schedule will never fire again — a one-shot past its date.
//
// The tickler is date-granular like every operation: a caller passes a date
// (a daily cron passes today; a minute-clock UI passes its zone's today), and
// the @HH:MM inside the expression never gates the answer (spec-tools.md
// §5.3.3: "the caller passes today"). The time exists for the canonical form
// and the human "next Mon 08:00" badge, which reads it from the expression.
func (s Schedule) Next(after Date) Date {
	switch s.kind {
	case schedOneShot:
		if s.fireDate.After(after) {
			return s.fireDate
		}
		return Date{}
	case schedWeekday:
		if s.ordinal == ordinalNone {
			return nextPlainWeekday(after, s.weekday)
		}
		y, m := after.Year, after.Month
		for {
			if d := s.ordinalOccurrence(y, m); !d.IsZero() && d.After(after) {
				return d
			}
			y, m = nextMonth(y, m)
		}
	case schedMonthday:
		y, m := after.Year, after.Month
		for {
			var cand Date
			if s.monthday == 0 {
				cand = Date{Year: y, Month: m, Day: DaysInMonth(y, m)} // the last day
			} else if s.monthday <= DaysInMonth(y, m) {
				cand = Date{Year: y, Month: m, Day: s.monthday}
			} else {
				// A 30th or 31st that this month does not have: skip the whole
				// month. The next fire lands in the next month that does
				// (spec-file-format.md §2.1's "30/31 and last-day" edge).
				y, m = nextMonth(y, m)
				continue
			}
			if cand.After(after) {
				return cand
			}
			y, m = nextMonth(y, m)
		}
	}
	return Date{}
}

// Due reports whether the schedule is due on or before now, given the last
// fire date (tickled) and the item's created date — the due test of
// spec-tools.md §5.3.3.
//
// One-shot: due once its date has arrived, and — when it has fired before —
// only if that date is after the last fire (the at-most-once guard). A
// backdated one-shot fires on the next run: "overdue, fire now" is the least
// surprising reading.
//
// Recurring: due iff next(last ?? created) <= now, where next is strictly
// after. A mon@08:00 prototype written on a Tuesday fires the following
// Monday, never the Monday that already passed — created anchors the first
// fire — and a runner that missed a week still catches up.
func (s Schedule) Due(now, last, created Date) bool {
	if s.kind == schedOneShot {
		if now.Before(s.fireDate) {
			return false
		}
		return last.IsZero() || s.fireDate.After(last)
	}
	after := last
	if after.IsZero() {
		after = created
	}
	nxt := s.Next(after)
	return !nxt.IsZero() && !now.Before(nxt)
}

// ordinalOccurrence returns the schedule's one weekday occurrence in a month:
// first = the weekday in days 1-7, last = the one in the final seven days
// (spec-file-format.md §2.1's definitions).
func (s Schedule) ordinalOccurrence(y, m int) Date {
	switch s.ordinal {
	case ordinalFirst:
		for d := 1; d <= 7; d++ {
			if weekdayOfDate(y, m, d) == s.weekday {
				return Date{Year: y, Month: m, Day: d}
			}
		}
	case ordinalLast:
		dim := DaysInMonth(y, m)
		for d := dim; d > dim-7; d-- {
			if weekdayOfDate(y, m, d) == s.weekday {
				return Date{Year: y, Month: m, Day: d}
			}
		}
	}
	return Date{}
}

// nextPlainWeekday returns the smallest date strictly after after whose
// weekday is wd — at most a week away.
func nextPlainWeekday(after Date, wd Weekday) Date {
	d := after
	for i := 0; i < 7; i++ {
		d = addDay(d)
		if weekdayOfDate(d.Year, d.Month, d.Day) == wd {
			return d
		}
	}
	return Date{} // unreachable: every seven-day window has every weekday
}

func nextMonth(y, m int) (int, int) {
	m++
	if m > 12 {
		return y + 1, 1
	}
	return y, m
}

func addDay(d Date) Date {
	if d.Day < DaysInMonth(d.Year, d.Month) {
		return Date{Year: d.Year, Month: d.Month, Day: d.Day + 1}
	}
	y, m := nextMonth(d.Year, d.Month)
	return Date{Year: y, Month: m, Day: 1}
}

// weekdayOfDate returns the weekday of a calendar date (0=Monday..6=Sunday)
// by pure arithmetic — Sakamoto's algorithm, no time package, no timezone.
func weekdayOfDate(y, m, d int) Weekday {
	// Sakamoto's table yields 0=Sunday..6=Saturday; shift to Monday-first.
	var t = [12]int{0, 3, 2, 5, 0, 3, 5, 1, 4, 6, 2, 4}
	yy := y
	if m < 3 {
		yy--
	}
	sundayFirst := (yy + yy/4 - yy/100 + yy/400 + t[m-1] + d) % 7
	return Weekday((sundayFirst + 6) % 7)
}
