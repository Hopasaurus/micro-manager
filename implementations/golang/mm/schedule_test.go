package mm

import (
	"errors"
	"strings"
	"testing"
)

// Schedule grammar: the spike's acceptance table (T-0164) as adopted by
// spec-file-format.md §3.3 (T-0169).
func TestParseScheduleAccepts(t *testing.T) {
	valid := []struct {
		src     string
		oneShot bool
	}{
		{"2026-09-01", true},
		{"2026-09-01@08:00", true},
		{"mon@08:00", false},
		{"first-mon@08:00", false},
		{"last-mon@08:00", false},
		{"15@08:00", false},
		{"last@08:00", false},
	}
	for _, c := range valid {
		sch, err := ParseSchedule(c.src)
		if err != nil {
			t.Errorf("ParseSchedule(%q): %v", c.src, err)
			continue
		}
		if sch.IsOneShot() != c.oneShot {
			t.Errorf("%q: IsOneShot = %v, want %v", c.src, sch.IsOneShot(), c.oneShot)
		}
		if got := sch.String(); got != c.src {
			t.Errorf("%q: String = %q, want the source verbatim", c.src, got)
		}
	}
}

// The schedule time is NOT the format's TIME token: no seconds, no offset,
// and @ not T. A reduced wall-clock form living only inside the expression.
func TestParseScheduleRejects(t *testing.T) {
	bad := []string{
		"", "mon@24:00", "mon@08:60", "mon@8:00", "mon@0800",
		"2026-09-01T08:00", "2026-09-01@25:00", "2026-09-01@",
		"MON@08:00", "Mon@08:00", "monday@08:00",
		"0@08:00", "32@08:00", "5@08:00", "5", "00@08:00",
		"second-last@08:00", "fifth-mon@08:00", "first@08:00",
		"last-last@08:00", "2026-02-31", "2026-13-01", "20260729",
	}
	for _, src := range bad {
		if _, err := ParseSchedule(src); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ParseSchedule(%q): want ErrInvalidArgument, got %v", src, err)
		}
	}
}

func TestScheduleRoundTrips(t *testing.T) {
	for _, src := range []string{
		"2026-09-01", "2026-09-01@00:00", "2026-09-01@08:00", "2026-09-01@23:59",
		"mon", "mon@00:00", "first-mon", "last-mon@08:00",
		"01", "15@08:00", "31", "last", "last@23:59",
	} {
		sch, err := ParseSchedule(src)
		if err != nil {
			t.Errorf("ParseSchedule(%q): %v", src, err)
			continue
		}
		again, err := ParseSchedule(sch.String())
		if err != nil {
			t.Errorf("String() %q of %q does not reparse: %v", sch.String(), src, err)
			continue
		}
		if again.String() != sch.String() {
			t.Errorf("%q -> %q -> %q", src, sch.String(), again.String())
		}
	}
}

func TestScheduleFireDate(t *testing.T) {
	sch, _ := ParseSchedule("2026-09-01@08:00")
	if got := sch.FireDate(); got != (Date{2026, 9, 1}) {
		t.Errorf("FireDate = %s, want 2026-09-01", got)
	}
	rec, _ := ParseSchedule("mon@08:00")
	if got := rec.FireDate(); !got.IsZero() {
		t.Errorf("recurring FireDate = %s, want zero", got)
	}
}

// Calendar edges (spec-file-format.md §2.1): first/last weekday, months that
// lack a 30th or 31st, the last day of a month, and a leap February.
func TestScheduleNext(t *testing.T) {
	cases := []struct {
		name  string
		expr  string
		after Date
		want  Date
	}{
		// Plain weekday: the next occurrence, at most a week away.
		{"plain weekday", "mon", Date{2026, 7, 29}, Date{2026, 8, 3}}, // Wed -> next Mon
		{"same weekday next week", "wed", Date{2026, 7, 29}, Date{2026, 8, 5}},
		{"weekday across month end", "fri", Date{2026, 7, 30}, Date{2026, 7, 31}},

		// first/last weekday: 2026-08-01 is a Saturday, so the first Monday of
		// August is the 3rd; the last Monday is the 31st.
		{"first weekday", "first-mon", Date{2026, 7, 29}, Date{2026, 8, 3}},
		{"last weekday", "last-mon", Date{2026, 7, 29}, Date{2026, 8, 31}},
		{"last weekday same month, still to come", "last-fri", Date{2026, 7, 29}, Date{2026, 7, 31}},
		{"first weekday already past", "first-wed", Date{2026, 8, 10}, Date{2026, 9, 2}},

		// Month days.
		{"monthday", "15", Date{2026, 7, 29}, Date{2026, 8, 15}},
		{"monthday later this month", "31", Date{2026, 7, 1}, Date{2026, 7, 31}},
		// February 2026 has 28 days: the 30th and 31st skip ahead.
		{"30 skips february", "30", Date{2026, 2, 28}, Date{2026, 3, 30}},
		{"31 skips a 31-less month", "31", Date{2026, 9, 1}, Date{2026, 10, 31}}, // Sep has 30
		{"last day", "last", Date{2026, 7, 29}, Date{2026, 7, 31}},
		{"last day next month", "last", Date{2026, 7, 31}, Date{2026, 8, 31}},
		{"leap february last day", "last", Date{2024, 2, 28}, Date{2024, 2, 29}},
		{"non-leap february last day", "last", Date{2025, 2, 28}, Date{2025, 3, 31}},

		// One-shot.
		{"one-shot future", "2026-09-01", Date{2026, 7, 29}, Date{2026, 9, 1}},
		{"one-shot the day before", "2026-09-01", Date{2026, 8, 31}, Date{2026, 9, 1}},
	}
	for _, c := range cases {
		sch, err := ParseSchedule(c.expr)
		if err != nil {
			t.Fatalf("%s: ParseSchedule(%q): %v", c.name, c.expr, err)
		}
		if got := sch.Next(c.after); got != c.want {
			t.Errorf("%s: Next(%s) = %s, want %s", c.name, c.after, got, c.want)
		}
	}
}

// Next is strictly after: the date itself never counts as the next fire, which
// is what lets the due test use it without double-firing.
func TestScheduleNextIsStrictlyAfter(t *testing.T) {
	sch, _ := ParseSchedule("2026-09-01")
	if got := sch.Next(Date{2026, 9, 1}); !got.IsZero() {
		t.Errorf("one-shot Next on its own date = %s, want zero", got)
	}
	if got := sch.Next(Date{2026, 9, 2}); !got.IsZero() {
		t.Errorf("one-shot Next after its date = %s, want zero", got)
	}
	mon, _ := ParseSchedule("mon")
	if got := mon.Next(Date{2026, 8, 3}); got != (Date{2026, 8, 10}) {
		t.Errorf("mon Next on a Monday = %s, want the following Monday", got)
	}
}

// The due test of spec-tools.md §5.3.3. One-shot: now >= fire date and, when
// it has fired, the fire date is after the last fire (the at-most-once guard).
// Recurring: next(last ?? created) <= now, so a never-fired schedule anchors
// its first fire on created and a missed run catches up.
func TestScheduleDue(t *testing.T) {
	oneShot, _ := ParseSchedule("2026-09-01")
	mon, _ := ParseSchedule("mon@08:00")

	cases := []struct {
		name    string
		sch     Schedule
		now     Date
		last    Date
		created Date
		want    bool
	}{
		{"one-shot not yet", oneShot, Date{2026, 7, 29}, Date{}, Date{2026, 7, 1}, false},
		{"one-shot on the day", oneShot, Date{2026, 9, 1}, Date{}, Date{2026, 7, 1}, true},
		{"one-shot backdated fires now", oneShot, Date{2026, 9, 2}, Date{}, Date{2026, 7, 1}, true},
		{"one-shot already fired", oneShot, Date{2026, 9, 2}, Date{2026, 9, 1}, Date{2026, 7, 1}, false},
		{"one-shot fired same day", oneShot, Date{2026, 9, 1}, Date{2026, 9, 1}, Date{2026, 7, 1}, false},

		// A Monday schedule written on Wednesday: created anchors the first
		// fire to the following Monday, never the Monday that already passed.
		{"recurring not due", mon, Date{2026, 7, 29}, Date{}, Date{2026, 7, 29}, false},
		{"recurring due on fire day", mon, Date{2026, 8, 3}, Date{}, Date{2026, 7, 27}, true},
		{"recurring fired this week", mon, Date{2026, 7, 29}, Date{2026, 7, 27}, Date{2026, 7, 1}, false},
		{"recurring missed a run", mon, Date{2026, 8, 5}, Date{2026, 7, 27}, Date{2026, 7, 1}, true},
	}
	for _, c := range cases {
		if got := c.sch.Due(c.now, c.last, c.created); got != c.want {
			t.Errorf("%s: Due = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestScheduleTime(t *testing.T) {
	with, _ := ParseSchedule("mon@08:00")
	h, m, ok := with.Time()
	if !ok || h != 8 || m != 0 {
		t.Errorf("mon@08:00 time = %d:%d has=%v, want 8:0 true", h, m, ok)
	}
	explicitMidnight, _ := ParseSchedule("mon@00:00")
	if _, _, ok := explicitMidnight.Time(); !ok {
		t.Error("an explicit @00:00 must report hasTime")
	}
	none, _ := ParseSchedule("mon")
	if _, _, ok := none.Time(); ok {
		t.Error("a bare weekday must report no time")
	}
}

// The canonical form zero-pads month days: "05@08:00", never "5@08:00"
// (spec-gui.md §5.6). The parser refuses the unpadded form as input.
func TestScheduleMonthdayPadding(t *testing.T) {
	if _, err := ParseSchedule("5@08:00"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("5@08:00 must be rejected, got %v", err)
	}
	sch, err := ParseSchedule("05@08:00")
	if err != nil {
		t.Fatal(err)
	}
	if got := sch.String(); got != "05@08:00" {
		t.Errorf("String = %q, want 05@08:00", got)
	}
	// The time is not part of the due arithmetic: 05@08:00 and 05@23:59 are
	// the same date for a date-passing caller (spec-tools.md §5.3.3).
	early, _ := ParseSchedule("05@08:00")
	late, _ := ParseSchedule("05@23:59")
	if early.Next(Date{2026, 7, 29}) != late.Next(Date{2026, 7, 29}) {
		t.Error("the @HH:MM must not change Next")
	}
}

// Schedule.String must render every shape somewhere, or the round-trip tests
// above could pass for the wrong reason.
func TestScheduleStringCoversEveryShape(t *testing.T) {
	for _, shape := range []string{
		"2026-09-01@08:00", "mon", "first-mon", "last-mon",
		"15@08:00", "last", "last@08:00", "01",
	} {
		sch, err := ParseSchedule(shape)
		if err != nil {
			t.Fatalf("ParseSchedule(%q): %v", shape, err)
		}
		if !strings.Contains(sch.String(), strings.TrimSuffix(shape, "@08:00")) &&
			!strings.Contains(sch.String(), "last") {
			t.Errorf("%q renders as %q", shape, sch.String())
		}
	}
}
