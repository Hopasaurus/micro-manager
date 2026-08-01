package mm

import (
	"errors"
	"testing"
)

func TestParseID(t *testing.T) {
	cases := []struct {
		in   string
		want ID
		ok   bool
	}{
		{"T-0042", "T-0042", true},
		{"T-0001", "T-0001", true},
		{"T-9999", "T-9999", true},
		{"42", "T-0042", true},   // bare number is resolved
		{"0042", "T-0042", true}, // already padded
		{" T-0042 ", "T-0042", true},
		{"", "", false},
		{"T-", "", false},
		{"t-0042", "", false},   // case deviation is rejected, not repaired (§3.3.2 rule 2)
		{"T-42", "", false},     // width mismatch (§3.3.2 rule 3)
		{"T-00042", "", false},  // too wide
		{"X-0042", "", false},   // a prefix the default grammar does not declare
		{"T-004x", "", false},
		{"00042", "", false}, // a bare number that cannot fit the width
		{"nope", "", false},
	}
	for _, c := range cases {
		got, err := ParseID(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseID(%q) failed: %v", c.in, err)
			} else if got != c.want {
				t.Errorf("ParseID(%q) = %q, want %q", c.in, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseID(%q) = %q, want error", c.in, got)
		} else if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ParseID(%q) error is %v, want ErrInvalidArgument", c.in, err)
		}
	}
}

// The same parsing rules against a declared grammar: the prefix and width come
// from the directory, matching is exact, and the bare number resolves into the
// declared shape.
func TestParseIDAgainstDeclaredGrammar(t *testing.T) {
	g := IDGrammar{Prefix: "MM", Width: 3}
	cases := []struct {
		in   string
		want ID
		ok   bool
	}{
		{"MM-001", "MM-001", true},
		{"MM-999", "MM-999", true},
		{"7", "MM-007", true},
		{"42", "MM-042", true},
		{"mm-001", "", false}, // case deviation
		{"M-001", "", false},  // wrong prefix
		{"MMM-001", "", false},
		{"MM-1", "", false},  // wrong width
		{"MM-0001", "", false},
		{"T-001", "", false}, // an ID from the default grammar does not belong here
	}
	for _, c := range cases {
		got, err := g.ParseID(c.in)
		if c.ok {
			if err != nil {
				t.Errorf("ParseID(%q) failed: %v", c.in, err)
			} else if got != c.want {
				t.Errorf("ParseID(%q) = %q, want %q", c.in, got, c.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseID(%q) = %q, want error", c.in, got)
		}
	}
}

func TestIDGrammarShapes(t *testing.T) {
	// ValidID, Num and NewID follow the declared grammar.
	if !ID("T-0042").Valid() || ID("T-0042").Num() != 42 {
		t.Error("T-0042 should be valid with num 42")
	}
	for _, bad := range []ID{"", "T-42", "t-0042", "X-0042", "T-004a", "T-00042"} {
		if bad.Valid() {
			t.Errorf("%q should be invalid", bad)
		}
		if bad.Num() != -1 {
			t.Errorf("%q.Num() should be -1", bad)
		}
	}
	if NewID(7) != "T-0007" {
		t.Errorf("NewID(7) = %q", NewID(7))
	}

	g := IDGrammar{Prefix: "MM", Width: 3}
	if !g.ValidID("MM-007") || g.ValidID("MM-07") || g.ValidID("mm-007") || g.ValidID("T-007") {
		t.Error("MM/3 grammar not honoured by ValidID")
	}
	if g.Num("MM-007") != 7 || g.Num("MM-07") != -1 {
		t.Errorf("Num = %d, %d", g.Num("MM-007"), g.Num("MM-07"))
	}
	if g.NewID(7) != "MM-007" {
		t.Errorf("NewID = %q", g.NewID(7))
	}
}

func TestIDGrammarCap(t *testing.T) {
	if c := DefaultIDGrammar().Cap(); c != 9999 {
		t.Errorf("default cap = %d, want 9999", c)
	}
	for width, want := range map[int]int{1: 9, 2: 99, 3: 999, 4: 9999, 5: 99999} {
		if c := (IDGrammar{Prefix: "T", Width: width}).Cap(); c != want {
			t.Errorf("width %d cap = %d, want %d", width, c, want)
		}
	}
}

func TestParseIDGrammar(t *testing.T) {
	fm := NewFrontmatter()
	g, vs, warns := ParseIDGrammar(fm)
	if g != DefaultIDGrammar() {
		t.Errorf("absent keys must mean the defaults, got %v", g)
	}
	if len(vs) != 0 || len(warns) != 0 {
		t.Errorf("an empty frontmatter produced %d violations %d warnings", len(vs), len(warns))
	}

	// Either key alone takes its own default (rule 2).
	fm = NewFrontmatter()
	fm.Set("id_prefix", "MM")
	g, vs, warns = ParseIDGrammar(fm)
	if g != (IDGrammar{Prefix: "MM", Width: 4}) || len(vs) != 0 || len(warns) != 0 {
		t.Errorf("prefix alone: %v, %v, %v", g, vs, warns)
	}

	fm = NewFrontmatter()
	fm.Set("id_width", "3")
	g, vs, warns = ParseIDGrammar(fm)
	if g != (IDGrammar{Prefix: "T", Width: 3}) || len(vs) != 0 || len(warns) != 0 {
		t.Errorf("width alone: %v, %v, %v", g, vs, warns)
	}

	// A width outside the RECOMMENDED 3-6 range warns but never fails.
	for _, w := range []string{"1", "2", "7", "12"} {
		fm = NewFrontmatter()
		fm.Set("id_width", w)
		g, vs, warns = ParseIDGrammar(fm)
		if len(vs) != 0 || len(warns) != 1 {
			t.Errorf("width %s: %d violations %d warnings, want just one warning", w, len(vs), len(warns))
		}
	}

	// Invalid declarations are violations, with the default standing in.
	for _, p := range []string{"t", "Tt", "ABCDE", ""} {
		fm = NewFrontmatter()
		fm.Set("id_prefix", p)
		g, vs, warns = ParseIDGrammar(fm)
		if len(vs) != 1 || g.Prefix != "T" || len(warns) != 0 {
			t.Errorf("prefix %q: grammar %v, %d violations, %d warnings", p, g, len(vs), len(warns))
		}
	}
	for _, w := range []string{"abc", "0", "-2", ""} {
		fm = NewFrontmatter()
		fm.Set("id_width", w)
		g, vs, warns = ParseIDGrammar(fm)
		if len(vs) != 1 || g.Width != 4 || len(warns) != 0 {
			t.Errorf("width %q: grammar %v, %d violations, %d warnings", w, g, len(vs), len(warns))
		}
	}
}

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2026-07-29")
	if err != nil {
		t.Fatalf("valid date rejected: %v", err)
	}
	if d.Year != 2026 || d.Month != 7 || d.Day != 29 {
		t.Errorf("got %+v", d)
	}
	if d.String() != "2026-07-29" {
		t.Errorf("String() = %q", d.String())
	}
	if d.Month7() != "2026-07" {
		t.Errorf("Month7() = %q", d.Month7())
	}

	for _, bad := range []string{"", "2026-7-29", "26-07-29", "2026/07/29",
		"2026-13-01", "2026-00-01", "2026-07-32", "2026-07-00", "2026-07-2x",
		"2026-07-291"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) should fail", bad)
		} else if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ParseDate(%q) error is %v, want ErrInvalidArgument", bad, err)
		}
	}
}

// Dates are ISO 8601 calendar dates: the lexical form AND a real day of a real
// month (spec-file-format.md §3.3.1). A well-formed but impossible date is
// rejected, and the reference checker rejects the same set.
func TestParseDateIsCalendarValidated(t *testing.T) {
	for _, s := range []string{
		"2026-02-29", // 2026 is not a leap year
		"2026-02-30", "2026-02-31",
		"2027-02-29", // nor is 2027
		"2026-04-31", "2026-06-31", "2026-09-31", "2026-11-31",
		"2100-02-29", // a century that is not divisible by 400
	} {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("ParseDate(%q) should be rejected as an impossible date", s)
		}
	}
	for _, s := range []string{
		"2026-01-31", "2026-02-28", "2026-04-30", "2026-12-31",
		"2028-02-29", // a leap year
		"2000-02-29", // a century divisible by 400
	} {
		if _, err := ParseDate(s); err != nil {
			t.Errorf("ParseDate(%q) should be accepted: %v", s, err)
		}
	}
}

// The basic format is valid ISO 8601 but deliberately not accepted here: the
// files are hand-edited, and the separators are what make a typo visible.
func TestParseDateRejectsBasicFormat(t *testing.T) {
	for _, s := range []string{"20260729", "2026-0729", "202607-29"} {
		if _, err := ParseDate(s); err == nil {
			t.Errorf("ParseDate(%q) should be rejected; extended format only", s)
		}
	}
}

func TestLeapYearRule(t *testing.T) {
	cases := map[int]bool{
		2024: true, 2026: false, 2027: false, 2028: true,
		1900: false, 2000: true, 2100: false, 2400: true,
	}
	for year, want := range cases {
		if got := IsLeapYear(year); got != want {
			t.Errorf("IsLeapYear(%d) = %v, want %v", year, got, want)
		}
	}
	if DaysInMonth(2026, 2) != 28 || DaysInMonth(2028, 2) != 29 {
		t.Error("February length wrong")
	}
	if DaysInMonth(2026, 4) != 30 || DaysInMonth(2026, 1) != 31 {
		t.Error("month length wrong")
	}
}

func TestDateZeroAndOrdering(t *testing.T) {
	var zero Date
	if !zero.IsZero() || zero.String() != "" || zero.Month7() != "" {
		t.Error("zero Date should render empty")
	}
	a := Date{2026, 7, 1}
	b := Date{2026, 7, 2}
	c := Date{2026, 8, 1}
	d := Date{2027, 1, 1}
	if !a.Before(b) || !b.Before(c) || !c.Before(d) {
		t.Error("Before is wrong")
	}
	if b.Before(a) || a.Before(a) {
		t.Error("Before should be strict")
	}
}

func TestParsePrio(t *testing.T) {
	for _, s := range []string{"high", "med", "low"} {
		if p, err := ParsePrio(s); err != nil || string(p) != s {
			t.Errorf("ParsePrio(%q) = %q, %v", s, p, err)
		}
	}
	for _, s := range []string{"", "HIGH", "urgent", "medium"} {
		if _, err := ParsePrio(s); err == nil {
			t.Errorf("ParsePrio(%q) should fail", s)
		}
	}
	// Absent means med on read, but must stay absent on write.
	if PrioNone.Effective() != PrioMed {
		t.Error("absent prio should read as med")
	}
	if PrioHigh.Effective() != PrioHigh {
		t.Error("Effective should not alter a set prio")
	}
}

func TestParseOutcome(t *testing.T) {
	for _, s := range []string{"shipped", "cancelled", "obsolete"} {
		if o, err := ParseOutcome(s); err != nil || string(o) != s {
			t.Errorf("ParseOutcome(%q) = %q, %v", s, o, err)
		}
	}
	for _, s := range []string{"", "done", "canceled", "SHIPPED"} {
		if _, err := ParseOutcome(s); err == nil {
			t.Errorf("ParseOutcome(%q) should fail", s)
		}
	}
}

func TestParseSection(t *testing.T) {
	cases := map[string]Section{
		"ready": SectionReady, "Ready": SectionReady, "READY": SectionReady,
		"blocked": SectionBlocked, "someday": SectionSomeday,
	}
	for in, want := range cases {
		if got, err := ParseSection(in); err != nil || got != want {
			t.Errorf("ParseSection(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseSection("later"); err == nil {
		t.Error("unknown section should fail")
	}
}

func TestParseTags(t *testing.T) {
	got, err := ParseTags("infra,ci,build-2.1_x")
	if err != nil {
		t.Fatalf("valid tags rejected: %v", err)
	}
	want := []string{"infra", "ci", "build-2.1_x"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	if FormatTags(want) != "infra,ci,build-2.1_x" {
		t.Errorf("FormatTags round-trip failed: %q", FormatTags(want))
	}
	if tags, err := ParseTags(""); err != nil || tags != nil {
		t.Error("empty tag list should be nil, no error")
	}

	// A space is the give-away for the YAML flow-sequence form the format
	// deliberately does not use (spec-file-format.md §5.2.2).
	for _, bad := range []string{"infra, ci", "[infra,ci]", "infra,,ci", "a,", ",a", "in fra"} {
		if _, err := ParseTags(bad); err == nil {
			t.Errorf("ParseTags(%q) should fail", bad)
		}
	}
}

func TestItemDetailPath(t *testing.T) {
	it := Item{ID: "T-0042"}
	if it.DetailPath() != "details/T-0042.md" {
		t.Errorf("DetailPath() = %q", it.DetailPath())
	}
}

func TestLocationString(t *testing.T) {
	if got := (Location{File: "backlog.md", Line: 19}).String(); got != "backlog.md:19" {
		t.Errorf("got %q", got)
	}
	if got := (Location{File: "backlog.md"}).String(); got != "backlog.md" {
		t.Errorf("file-level location should omit the line, got %q", got)
	}
}
