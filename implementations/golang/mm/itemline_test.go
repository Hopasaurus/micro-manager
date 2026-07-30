package mm

import (
	"errors"
	"strings"
	"testing"
)

func mustParseLine(t *testing.T, line string) *Item {
	t.Helper()
	it, err := parseItemLine("backlog.md", 19, line)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	return it
}

func TestParseItemLineFull(t *testing.T) {
	it := mustParseLine(t,
		"- [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29")

	if it.ID != "T-0042" {
		t.Errorf("ID = %q", it.ID)
	}
	if it.Title != "Fix the deploy script" {
		t.Errorf("Title = %q", it.Title)
	}
	if it.Prio != PrioHigh {
		t.Errorf("Prio = %q", it.Prio)
	}
	if FormatTags(it.Tags) != "infra,ci" {
		t.Errorf("Tags = %v", it.Tags)
	}
	if it.Created.String() != "2026-07-29" {
		t.Errorf("Created = %q", it.Created)
	}
	if it.Source.File != "backlog.md" || it.Source.Line != 19 {
		t.Errorf("Source = %v", it.Source)
	}
}

func TestParseItemLineClosed(t *testing.T) {
	it := mustParseLine(t,
		"- [x] [T-0007] Ship it | prio:med | created:2026-06-01 | done:2026-07-23 | outcome:shipped")
	if it.Done.String() != "2026-07-23" || it.Outcome != OutcomeShipped {
		t.Errorf("got done=%q outcome=%q", it.Done, it.Outcome)
	}
	// The box is a function of which file the item lives in, so a bare parse
	// does not set State; Closed() follows State, not the box character.
	if it.State != "" {
		t.Errorf("parse should not infer State, got %q", it.State)
	}
}

func TestParseItemLineMinimal(t *testing.T) {
	it := mustParseLine(t, "- [ ] [T-0001] Just a title")
	if it.Title != "Just a title" || it.Prio != PrioNone || len(it.Tags) != 0 {
		t.Errorf("got %+v", it)
	}
	// Absent prio reads as med but must not be materialised onto the item.
	if it.Prio.Effective() != PrioMed {
		t.Error("absent prio should read as med")
	}
}

func TestIsItemLineRejects(t *testing.T) {
	bad := []string{
		"",
		"- [ ] T-0002 missing brackets",
		"- [ ] [T-002] short id",
		"- [ ] [T-00002] long id",
		"- [ ] [X-0002] wrong prefix",
		"- [?] [T-0002] bad box",
		"- [ ] [T-0002]",          // no title
		"- [ ] [T-0002] ",         // empty title
		"-[ ] [T-0002] no space",  // malformed prefix
		"  - [ ] [T-0002] indent", // must start at column 0
		"* [ ] [T-0002] wrong bullet",
	}
	for _, line := range bad {
		if isItemLine(line) {
			t.Errorf("isItemLine(%q) should be false", line)
		}
	}
	for _, line := range []string{
		"- [ ] [T-0002] ok",
		"- [x] [T-9999] ok",
	} {
		if !isItemLine(line) {
			t.Errorf("isItemLine(%q) should be true", line)
		}
	}
}

func TestParseItemLineErrors(t *testing.T) {
	cases := []struct{ line, want string }{
		{"- [ ] T-0002 missing brackets", "malformed item line"},
		{"- [ ] [T-0002] title | nocolon", "field is not key:value"},
		{"- [ ] [T-0002] title | prio:high | prio:low", "repeats field prio"},
		{"- [ ] [T-0002] title | prio:URGENT", "prio:URGENT"},
		{"- [ ] [T-0002] title | created:29-07-2026", "created:29-07-2026"},
		{"- [ ] [T-0002] title | outcome:done", "outcome:done"},
		{"- [ ] [T-0002] title | tags:a, b", "malformed tags"},
	}
	for _, c := range cases {
		_, err := parseItemLine("backlog.md", 19, c.line)
		if err == nil {
			t.Errorf("parse(%q) should fail", c.line)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("parse(%q) error %q should mention %q", c.line, err, c.want)
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("parse(%q) should unwrap to ErrInvalidArgument", c.line)
		}
		if !strings.HasPrefix(err.Error(), "backlog.md:19:") {
			t.Errorf("error should be located: %q", err)
		}
	}
}

// The extension point. An unknown key is legal and must survive verbatim,
// in order, or a field belonging to another tool is destroyed on the next move.
func TestUnregisteredFieldsPreserved(t *testing.T) {
	line := "- [ ] [T-0042] Title | prio:high | owner:dlh | estimate:3d | created:2026-07-29"
	it := mustParseLine(t, line)

	if len(it.Extra) != 2 {
		t.Fatalf("Extra = %v, want 2 entries", it.Extra)
	}
	if it.Extra[0] != (Field{"owner", "dlh"}) || it.Extra[1] != (Field{"estimate", "3d"}) {
		t.Errorf("Extra order or content wrong: %v", it.Extra)
	}
	// Round-trip: registered fields in canonical order, then the unknown ones.
	want := "- [ ] [T-0042] Title | prio:high | created:2026-07-29 | owner:dlh | estimate:3d"
	if got := RenderItemLine(it); got != want {
		t.Errorf("render:\n got %q\nwant %q", got, want)
	}
}

func TestRenderOmitsEmptyFields(t *testing.T) {
	it := &Item{ID: "T-0001", Title: "Bare"}
	if got := RenderItemLine(it); got != "- [ ] [T-0001] Bare" {
		t.Errorf("got %q", got)
	}
	// An absent prio must not materialise as prio:med, or a no-op write
	// would change the file.
	if strings.Contains(RenderItemLine(it), "prio") {
		t.Error("absent prio must not be rendered")
	}
}

func TestRenderCanonicalFieldOrder(t *testing.T) {
	// Parsed in a scrambled order; must come out canonical.
	it := mustParseLine(t,
		"- [x] [T-0042] T | outcome:shipped | created:2026-01-01 | tags:a | done:2026-02-02 | prio:low")
	it.State = StateDone
	want := "- [x] [T-0042] T | prio:low | tags:a | created:2026-01-01 | done:2026-02-02 | outcome:shipped"
	if got := RenderItemLine(it); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
}

func TestParseRenderRoundTrip(t *testing.T) {
	lines := []string{
		"- [ ] [T-0001] Just a title",
		"- [ ] [T-0002] Blocked one | prio:low | tags:example | created:2026-07-29 | blocked:waiting on a thing",
		"- [x] [T-0003] Closed | prio:high | tags:a,b | detail:details/T-0003.md | created:2026-01-01 | started:2026-01-02 | done:2026-01-03 | outcome:cancelled",
	}
	for _, line := range lines {
		it := mustParseLine(t, line)
		if strings.HasPrefix(line, "- [x]") {
			it.State = StateDone
		}
		if got := RenderItemLine(it); got != line {
			t.Errorf("round trip changed the line:\n got %q\nwant %q", got, line)
		}
	}
}

// spec-file-format.md §10.2, verified rather than assumed: a pipe in a title is
// caught when the fragment has no colon, and silently mis-parses when it does.
// The parser must NOT try to be clever here - the reference checker behaves the
// same way, and diverging would be worse than the limitation.
func TestPipeInTitleIsADocumentedLimitation(t *testing.T) {
	// Caught: "b" is not key:value.
	if _, err := parseItemLine("backlog.md", 1, "- [ ] [T-0002] Fix a | b"); err == nil {
		t.Error("pipe without a colon should be reported")
	}
	// Not caught: parses as a truncated title plus an unregistered field.
	it, err := parseItemLine("backlog.md", 1, "- [ ] [T-0002] Fix a | b: c")
	if err != nil {
		t.Fatalf("§10.2 says this is accepted, got %v", err)
	}
	if it.Title != "Fix a" {
		t.Errorf("title should be truncated at the pipe, got %q", it.Title)
	}
	if len(it.Extra) != 1 || it.Extra[0] != (Field{"b", "c"}) {
		t.Errorf("should yield one unregistered field, got %v", it.Extra)
	}
}

// The separator is " | " with spaces, so "a|b" does not split into two fields.
// It stays one part, which then fails the no-pipe-in-value rule - reported as a
// pipe in the "blocked" value, not as a stray field named "b".
func TestFieldSeparatorRequiresSpaces(t *testing.T) {
	_, err := parseItemLine("backlog.md", 19, "- [ ] [T-0002] Title | blocked:waiting on a|b")
	if err == nil {
		t.Fatal("a pipe in a value must be rejected")
	}
	if !strings.Contains(err.Error(), "field blocked contains a pipe") {
		t.Errorf("the value should not have been split; got %v", err)
	}
}

func TestPipeInValueRejected(t *testing.T) {
	_, err := parseItemLine("backlog.md", 1, "- [ ] [T-0002] Title | blocked:a|b")
	if err == nil || !strings.Contains(err.Error(), "contains a pipe") {
		t.Errorf("want a pipe error, got %v", err)
	}
}

func TestLooksLikeItemLine(t *testing.T) {
	if !looksLikeItemLine("- [ ] a subtask in a working file") {
		t.Error("should flag a candidate for the malformed-line check")
	}
	if looksLikeItemLine("- an ordinary bullet") || looksLikeItemLine("prose") {
		t.Error("ordinary prose should not be flagged")
	}
}

// Titles are UTF-8 and the byte offsets must not corrupt them.
func TestUnicodeTitle(t *testing.T) {
	it := mustParseLine(t, "- [ ] [T-0042] Rename µmanager → .µmanager | prio:low")
	if it.Title != "Rename µmanager → .µmanager" {
		t.Errorf("Title = %q", it.Title)
	}
	if got := RenderItemLine(it); !strings.Contains(got, "µmanager → .µmanager") {
		t.Errorf("render mangled unicode: %q", got)
	}
}
