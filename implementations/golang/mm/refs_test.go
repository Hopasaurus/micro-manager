package mm

import (
	"strings"
	"testing"
)

// T-0126 — the board slug (§5.1) and the refs LINKLIST (§6) are
// shape-checked, never resolved. A link to a board that does not exist
// anywhere validates clean: resolution is a front end's job (spec §9).

func TestParseRefs(t *testing.T) {
	ok := []string{
		"py:T-0012",
		"py:T-0012,go:T-0003",
		"python-impl:AB-0001",
		"a:B-1",                   // width-1 target grammar
		"z9:ABCD-123456789012345", // 15-digit target
	}
	for _, s := range ok {
		refs, err := ParseRefs(s)
		if err != nil {
			t.Errorf("ParseRefs(%q) failed: %v", s, err)
			continue
		}
		if FormatRefs(refs) != s {
			t.Errorf("FormatRefs(ParseRefs(%q)) = %q", s, FormatRefs(refs))
		}
	}

	bad := []string{
		"py:",                         // no ID
		":T-0012",                     // no slug
		"PY:T-0012",                   // uppercase slug
		"py: T-0012",                  // space
		"py:twelve",                   // non-ID element
		"py:T-0012,",                  // trailing comma, empty element
		",py:T-0012",                  // leading comma
		"py:-T-0012",                  // slug cannot start with a hyphen
		"-py:T-0012",                  // slug cannot start with a hyphen
		"py:T-0012 go:T-0003",         // space instead of comma
		"toolongslug-aaaaaaaa:T-0012", // 17 characters
		"py:T-0012:extra",             // a second colon is not an ID
		"py:0012",                     // no hyphen
	}
	for _, s := range bad {
		if _, err := ParseRefs(s); err == nil {
			t.Errorf("ParseRefs(%q) should fail", s)
		}
	}
}

func TestParseRefsEmptyIsAbsent(t *testing.T) {
	refs, err := ParseRefs("")
	if err != nil || refs != nil {
		t.Errorf("ParseRefs(\"\") = %v, %v; want nil, nil", refs, err)
	}
}

// The core of the advisory rule: a refs value naming a board and item that do
// not exist anywhere in the tree validates clean. Shape is this directory's
// business; resolution is not (§9).
func TestRefsAreAdvisoryNotLoadBearing(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog,
			"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
			"- [ ] [T-0001] First | prio:med | tags:example | refs:nowhere:T-9999 | created:2026-07-29", 1),
	})
	s := mustOpen(t, dir)
	if vs, err := s.Validate(); err != nil || len(vs) != 0 {
		t.Errorf("a dangling ref is not a violation: %v %s", err, violationMessages(vs))
	}
}

// A malformed refs value is a parse-time format violation naming the line.
func TestMalformedRefsIsAFormatViolation(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog,
			"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
			"- [ ] [T-0001] First | refs:py: T-0012 | created:2026-07-29", 1),
	})
	s := mustOpen(t, dir)
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || vs[0].Invariant != invFormat || !strings.Contains(vs[0].Message, "malformed refs") {
		t.Fatalf("violations = %s, want one malformed-refs format finding", violationMessages(vs))
	}
	if vs[0].At.Line != 0 {
		// The finding names the item line, so a human can find it.
		if !strings.Contains(vs[0].At.String(), "backlog.md") {
			t.Errorf("finding location = %s", vs[0].At)
		}
	}
}

// A bad board slug in backlog.md frontmatter is a format violation; a good
// one validates clean.
func TestBoardSlugShape(t *testing.T) {
	good := newDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "project: Sample One",
			"project: Sample One\nboard: py", 1),
	})
	s := mustOpen(t, good)
	if vs, err := s.Validate(); err != nil || len(vs) != 0 {
		t.Errorf("board: py must validate clean: %s", violationMessages(vs))
	}

	bad := newDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "project: Sample One",
			"project: Sample One\nboard: Py", 1),
	})
	s = mustOpen(t, bad)
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "board must be a slug") {
		t.Fatalf("violations = %s, want one bad-board-slug finding", violationMessages(vs))
	}
}

// refs survive a full start/finish round trip: they ride into the slot's
// frontmatter and back onto the item line, in canonical position.
func TestRefsSurviveStartAndFinish(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog,
			"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
			"- [ ] [T-0001] First | tags:example | refs:py:T-0012,go:T-0003 | created:2026-07-29", 1),
	})
	s := mustOpen(t, dir)

	started, _, err := s.Start("T-0001", StartRequest{}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Refs) != 2 || started.Refs[0] != (Ref{"py", "T-0012"}) {
		t.Fatalf("started refs = %+v", started.Refs)
	}
	// The slot's frontmatter carries refs (spec-file-format.md §5.2.2).
	dir2, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	var fmRefs string
	for _, sl := range dir2.Slots {
		if sl.Item != nil && sl.Item.ID == "T-0001" {
			fmRefs = FormatRefs(sl.Item.Refs)
		}
	}
	if fmRefs != "py:T-0012,go:T-0003" {
		t.Errorf("slot refs = %q", fmRefs)
	}

	back, _, err := s.Pause("T-0001", PauseRequest{}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Refs) != 2 {
		t.Fatalf("paused refs = %+v", back.Refs)
	}
	// The line still carries refs in canonical order after tags.
	line := RenderItemLine(&back)
	if !strings.Contains(line, "tags:example | refs:py:T-0012,go:T-0003 | created:") {
		t.Errorf("item line = %s", line)
	}

	if vs, err := s.Validate(); err != nil || len(vs) != 0 {
		t.Errorf("violations after round trip: %s", violationMessages(vs))
	}
}

// --edit --set refs:... reaches the registered field, validates it, and a
// malformed value refuses without writing.
func TestUpdateSetRefs(t *testing.T) {
	dir := newDir(t, map[string]string{})
	s := mustOpen(t, dir)

	it, _, err := s.Update("T-0001", UpdateRequest{Set: []Field{{"refs", "py:T-0012"}}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Refs) != 1 || it.Refs[0].Slug != "py" {
		t.Fatalf("refs after set = %+v", it.Refs)
	}

	_, _, err = s.Update("T-0001", UpdateRequest{Set: []Field{{"refs", "py: nope"}}}, today)
	if err == nil {
		t.Fatal("malformed refs must refuse")
	}

	it, _, err = s.Update("T-0001", UpdateRequest{Unset: []string{"refs"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Refs) != 0 {
		t.Errorf("refs after unset = %+v", it.Refs)
	}
	if vs, err := s.Validate(); err != nil || len(vs) != 0 {
		t.Errorf("violations: %s", violationMessages(vs))
	}
}
