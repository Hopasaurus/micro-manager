package mm

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// ID
// ---------------------------------------------------------------------------

// ID identifies an item permanently: the directory's declared prefix, a
// hyphen, and exactly its declared number of zero-padded digits - by default
// "T-" plus four (spec-file-format.md §3.3.2). IDs are never reused and never
// renumbered.
type ID string

// ParseID parses s as an ID in the default grammar: "T-" plus four digits.
func ParseID(s string) (ID, error) { return DefaultIDGrammar().ParseID(s) }

// ParseID parses s as an ID in the declared grammar: the prefix, a hyphen, and
// exactly g.Width zero-padded digits. Matching is exact - a case deviation or a
// wrong width is rejected, not repaired, because an ID that does not match the
// directory's declared grammar belongs to a different grammar and a different
// counter (§3.3.2 rules 1-2).
//
// A bare number ("42", "0042") is still accepted and padded, because typing
// the prefix is friction the format imposes for machine reasons (spec-tools.md
// §3.3 rule 4).
func (g IDGrammar) ParseID(s string) (ID, error) {
	s = strings.TrimSpace(s)
	bad := func() (ID, error) {
		return "", fmt.Errorf("%w: %q is not a %s id", ErrInvalidArgument, s, g)
	}
	if s == "" {
		return bad()
	}
	if isDigits(s) { // a bare number, resolved to the declared grammar
		if len(s) > g.Width {
			return bad() // a number that cannot fit the width
		}
		n, _ := strconv.Atoi(s)
		return g.NewID(n), nil
	}
	dash := strings.Index(s, "-")
	if dash != len(g.Prefix) || s[:dash] != g.Prefix {
		return bad()
	}
	digits := s[dash+1:]
	if len(digits) != g.Width || !isDigits(digits) {
		return bad()
	}
	return ID(s), nil
}

// NewID formats n as an ID in the default grammar. n is not range checked
// here; the caller allocating from next_id is responsible for staying below
// the counter cap.
func NewID(n int) ID { return DefaultIDGrammar().NewID(n) }

// Num returns the numeric part of the ID in the default grammar, or -1 if it
// is not well formed.
func (id ID) Num() int { return DefaultIDGrammar().Num(string(id)) }

// Valid reports whether the ID matches the default grammar exactly: the
// prefix, a hyphen, and exactly four digits.
func (id ID) Valid() bool { return DefaultIDGrammar().ValidID(string(id)) }

func (id ID) String() string { return string(id) }

// ---------------------------------------------------------------------------
// Date
// ---------------------------------------------------------------------------

// Date is a calendar date with no time and no timezone, matching the format's
// DATE token (spec-file-format.md §3.3).
//
// This is deliberately not time.Time. The format is date granular; carrying a
// timezone invites an offset bug that shifts a done: date across a month
// boundary and files the item under the wrong ## YYYY-MM heading.
type Date struct {
	Year  int
	Month int
	Day   int
}

// ParseDate accepts an ISO 8601 calendar date in extended format: YYYY-MM-DD.
//
// Both halves are enforced (spec-file-format.md §3.3.1): the lexical form, and
// calendar validity. 2026-02-31 and 2027-02-29 are rejected. A regex cannot
// express the month-length rule, so it is checked here.
//
// The basic format (20260729) is deliberately NOT accepted. ISO 8601 permits it;
// this format does not, because the files are hand-edited and the hyphens are
// what make a mistyped field obvious.
func ParseDate(s string) (Date, error) {
	bad := func() (Date, error) {
		return Date{}, fmt.Errorf("%w: %q is not an ISO 8601 date (YYYY-MM-DD)",
			ErrInvalidArgument, s)
	}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return bad()
	}
	for i, r := range s {
		if i == 4 || i == 7 {
			continue
		}
		if r < '0' || r > '9' {
			return bad()
		}
	}
	y, _ := strconv.Atoi(s[0:4])
	m, _ := strconv.Atoi(s[5:7])
	d, _ := strconv.Atoi(s[8:10])
	if m < 1 || m > 12 || d < 1 || d > DaysInMonth(y, m) {
		return bad()
	}
	return Date{Year: y, Month: m, Day: d}, nil
}

// DaysInMonth returns the length of a month in the proleptic Gregorian calendar
// that ISO 8601 specifies.
func DaysInMonth(year, month int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		if IsLeapYear(year) {
			return 29
		}
		return 28
	}
	return 31
}

// IsLeapYear applies the ISO 8601 leap-year rule: divisible by 4, except
// centuries not divisible by 400.
func IsLeapYear(year int) bool {
	return (year%4 == 0 && year%100 != 0) || year%400 == 0
}

// IsZero reports whether the date is unset, which serialises as an absent field.
func (d Date) IsZero() bool { return d == Date{} }

// Valid reports whether the date is a real calendar date. A Date built by hand
// rather than by ParseDate has not been through that check, and 2026-02-31 in a
// done: field would file the item under a month that cannot exist.
func (d Date) Valid() bool {
	return d.Year >= 0 && d.Month >= 1 && d.Month <= 12 &&
		d.Day >= 1 && d.Day <= DaysInMonth(d.Year, d.Month)
}

func (d Date) String() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

// Month7 returns the YYYY-MM prefix used as a done.md group heading.
func (d Date) Month7() string {
	if d.IsZero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d", d.Year, d.Month)
}

// Before reports whether d sorts before e.
func (d Date) Before(e Date) bool {
	if d.Year != e.Year {
		return d.Year < e.Year
	}
	if d.Month != e.Month {
		return d.Month < e.Month
	}
	return d.Day < e.Day
}

// ---------------------------------------------------------------------------
// Enumerations
// ---------------------------------------------------------------------------

// Prio is an item's priority. The empty value means the field is absent, which
// the format reads as med but which must serialise back as absent.
type Prio string

const (
	PrioNone Prio = ""
	PrioHigh Prio = "high"
	PrioMed  Prio = "med"
	PrioLow  Prio = "low"
)

func ParsePrio(s string) (Prio, error) {
	switch Prio(s) {
	case PrioHigh, PrioMed, PrioLow:
		return Prio(s), nil
	}
	return "", fmt.Errorf("%w: prio:%s (want high, med or low)", ErrInvalidArgument, s)
}

// Effective returns the priority the format assigns when the field is absent.
func (p Prio) Effective() Prio {
	if p == PrioNone {
		return PrioMed
	}
	return p
}

// Outcome records how a closed item ended.
type Outcome string

const (
	OutcomeNone      Outcome = ""
	OutcomeShipped   Outcome = "shipped"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeObsolete  Outcome = "obsolete"
)

func ParseOutcome(s string) (Outcome, error) {
	switch Outcome(s) {
	case OutcomeShipped, OutcomeCancelled, OutcomeObsolete:
		return Outcome(s), nil
	}
	return "", fmt.Errorf("%w: outcome:%s (want shipped, cancelled or obsolete)",
		ErrInvalidArgument, s)
}

// State is which of the three files an item currently lives in.
type State string

const (
	StateBacklog State = "backlog"
	StateWorking State = "working"
	StateDone    State = "done"
)

// Section is a backlog.md section. Only backlog items have one.
type Section string

const (
	SectionNone    Section = ""
	SectionReady   Section = "Ready"
	SectionBlocked Section = "Blocked"
	SectionSomeday Section = "Someday"
)

func ParseSection(s string) (Section, error) {
	switch strings.ToLower(s) {
	case "ready":
		return SectionReady, nil
	case "blocked":
		return SectionBlocked, nil
	case "someday":
		return SectionSomeday, nil
	}
	return "", fmt.Errorf("%w: unknown section %q (want ready, blocked or someday)",
		ErrInvalidArgument, s)
}

// Sections lists the three backlog sections in their required file order.
func Sections() []Section { return []Section{SectionReady, SectionBlocked, SectionSomeday} }

// ---------------------------------------------------------------------------
// Tags
// ---------------------------------------------------------------------------

// ParseTags splits a TAGLIST: one or more tags of [A-Za-z0-9._-], comma
// separated with no spaces. The same lexical form is used on an item line and
// in working file frontmatter (spec-file-format.md §5.2.2).
func ParseTags(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || !validTag(p) {
			return nil, fmt.Errorf("%w: malformed tags: %s", ErrInvalidArgument, s)
		}
		out = append(out, p)
	}
	return out, nil
}

// FormatTags renders tags in their canonical form. No spaces: a space would
// make the value unparseable as a TAGLIST.
func FormatTags(tags []string) string { return strings.Join(tags, ",") }

func validTag(t string) bool {
	for _, r := range t {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return t != ""
}

// ---------------------------------------------------------------------------
// Cross-board links
// ---------------------------------------------------------------------------

// Ref is one cross-board link: a target board's slug and the target item's ID
// (spec-file-format.md §6, `refs`). Resolution — whether the target exists — is
// deliberately never this directory's business (§9); shape is.
//
// The slug is the target board's declared `board` value (§5.1), the one board
// handle that is content rather than location. The ID half uses the generic
// form every declared grammar shares, so a link can be checked locally without
// knowing the target directory's grammar (plan-board-links.md decision 3).
type Ref struct {
	Slug string
	ID   ID
}

func (r Ref) String() string { return r.Slug + ":" + string(r.ID) }

// ParseRefs splits a LINKLIST: one or more SLUG:ID elements, comma separated
// with no spaces — the same lexical rule as a TAGLIST (spec-file-format.md
// §3.3).
func ParseRefs(s string) ([]Ref, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]Ref, 0, len(parts))
	for _, p := range parts {
		slug, id, ok := strings.Cut(p, ":")
		if !ok || !validSlug(slug) || !validGenericID(id) {
			return nil, fmt.Errorf("%w: malformed refs: %s", ErrInvalidArgument, s)
		}
		out = append(out, Ref{Slug: slug, ID: ID(id)})
	}
	return out, nil
}

// FormatRefs renders refs in their canonical form. No spaces: a space would
// make the value unparseable as a LINKLIST.
func FormatRefs(refs []Ref) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = r.String()
	}
	return strings.Join(parts, ",")
}

// validSlug reports whether s is a board slug (spec-file-format.md §5.1):
// lowercase, one to sixteen characters, starting with a letter. Lowercase
// keeps the slug namespace disjoint from the uppercase ID-prefix namespace.
func validSlug(s string) bool {
	if len(s) == 0 || len(s) > 16 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// validGenericID reports whether s has the ID form every declared grammar
// shares: an uppercase prefix of one to four letters, a hyphen, one to fifteen
// digits (spec-file-format.md §3.3.2). The target board's exact grammar is
// unknowable from here; the generic form is the intersection of them all.
func validGenericID(s string) bool {
	i := 0
	for i < len(s) && s[i] >= 'A' && s[i] <= 'Z' {
		i++
	}
	if i < 1 || i > 4 || i >= len(s) || s[i] != '-' {
		return false
	}
	i++
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	return i == len(s) && digits >= 1 && digits <= 15
}

// ---------------------------------------------------------------------------
// Item
// ---------------------------------------------------------------------------

// Location points at where something was parsed from, for diagnostics.
type Location struct {
	File string // path relative to the micro-manager directory
	Line int    // 1-based; 0 when the finding is file level
}

func (l Location) String() string {
	if l.Line == 0 {
		return l.File
	}
	return fmt.Sprintf("%s:%d", l.File, l.Line)
}

// Item is one unit of tracked work.
type Item struct {
	ID      ID
	Title   string
	State   State
	Section Section // backlog only
	Slot    int     // working only; 0 means not in a slot
	Pos     int     // 1-based index within its section; 0 when not applicable

	Prio    Prio
	Tags    []string
	Refs    []Ref
	Detail  string // "details/T-0042.md", or empty
	Created Date
	Started Date
	Done    Date
	Outcome Outcome
	Blocked string

	// Extra holds fields this implementation does not recognise, in the order
	// they appeared. Unregistered keys are the format's extension point
	// (spec-file-format.md §9) and MUST survive every move; dropping them
	// silently destroys data written by another tool.
	Extra []Field

	Source Location

	// rawBox is the box character exactly as it appeared on disk. Closed() is
	// derived from State, so without this I3 could not tell a "- [x]" line
	// sitting in backlog.md from a correctly open one.
	rawBox byte
}

// Field is one key:value pair from an item line.
type Field struct {
	Key   string
	Value string
}

// DetailPath returns the only path this item's detail file may occupy.
func (it Item) DetailPath() string { return "details/" + string(it.ID) + ".md" }

// Closed reports whether the item's box is checked, which is a function of the
// file it lives in rather than a field.
func (it Item) Closed() bool { return it.State == StateDone }

// ---------------------------------------------------------------------------
// Slots and directories
// ---------------------------------------------------------------------------

// Slot is one working file: one WIP slot.
type Slot struct {
	Number int    // 1-based
	Width  int    // digits in the filename, uniform across a directory
	File   string // "working.01.md"
	Item   *Item  // nil when idle
}

// Occupied reports whether the slot currently holds an item.
func (s Slot) Occupied() bool { return s.Item != nil }

// Directory summarises an open micro-manager directory.
type Directory struct {
	Path      string // absolute, as opened
	ProjectID string // spec-gui.md §3.1; how a URI addresses this directory
	Project   string // the project frontmatter value
	NextID    ID

	// The declared ID grammar (spec-file-format.md §3.3.2). Absent keys mean
	// the defaults, exactly as if they were written.
	IDPrefix string // id_prefix; "T" when absent
	IDWidth  int    // id_width; 4 when absent

	Slots    []Slot
	WipLimit int // == len(Slots)
	WipUsed  int
}

// ---------------------------------------------------------------------------
// Changes and violations
// ---------------------------------------------------------------------------

// ChangeKind describes what happened to an item in a transaction.
type ChangeKind string

const (
	ChangeCreated ChangeKind = "created"
	ChangeUpdated ChangeKind = "updated"
	ChangeMoved   ChangeKind = "moved"
	ChangeDeleted ChangeKind = "deleted"
)

// Change is one effect of an operation. Every mutation returns its full change
// set, so a dry run can report exactly what a real run would do.
type Change struct {
	Kind   ChangeKind
	ID     ID
	File   string // file the change lands in
	Before string // rendered previous form, empty when created
	After  string // rendered new form, empty when deleted
}

// Violation is one invariant failure.
type Violation struct {
	Invariant string // "I1".."I10"
	At        Location
	Message   string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s", v.At, v.Message)
}

// ---------------------------------------------------------------------------
// Discovery
// ---------------------------------------------------------------------------

// DiscoveryOptions controls the walk that finds micro-manager directories
// (spec-tools.md §6.1).
type DiscoveryOptions struct {
	Roots          []string // absolute; the caller expands ~ and env vars
	MaxDepth       int      // levels below each root; 0 means unlimited
	FollowSymlinks bool
	IncludeHidden  bool     // default true: two conventional names start with a dot
	Excludes       []string // directory names pruned during the walk
	MaxResults     int
	TimeoutMS      int
}

// DefaultDiscoveryOptions returns the defaults from spec-gui.md §9.5.
func DefaultDiscoveryOptions(roots ...string) DiscoveryOptions {
	return DiscoveryOptions{
		Roots:         roots,
		MaxDepth:      6,
		IncludeHidden: true,
		Excludes: []string{".git", ".hg", ".svn", ".claude", "node_modules",
			"vendor", "target", "dist", "build", ".venv", "venv"},
		MaxResults: 500,
		TimeoutMS:  5000,
	}
}

// DiscoveryResult is what a walk found.
type DiscoveryResult struct {
	Directories []Directory
	Partial     bool // a limit or timeout was hit; results are incomplete
	Skipped     int  // unreadable directories
}
