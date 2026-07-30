package mm

import (
	"strings"
	"testing"
)

func validateDir(t *testing.T, files map[string]string) []Violation {
	t.Helper()
	vs, err := mustOpen(t, newDir(t, files)).Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return vs
}

func TestValidateCleanDirectory(t *testing.T) {
	if vs := validateDir(t, nil); len(vs) != 0 {
		t.Errorf("a clean directory should have no findings, got:\n%s", violationMessages(vs))
	}
}

// I1: an ID has exactly one home. A duplicate is what a crash mid-transaction
// leaves behind, which is why the write ordering prefers it to a deletion.
func TestValidateI1DuplicateID(t *testing.T) {
	dup := strings.Replace(sampleDone,
		"- [x] [T-0010] Newest", "- [x] [T-0001] Newest", 1)
	vs := validateDir(t, map[string]string{"done.md": dup})
	if !hasViolation(vs, "I1", "T-0001 is already defined at backlog.md") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI2NextID(t *testing.T) {
	// An ID at or above next_id.
	vs := validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "next_id: T-0011", "next_id: T-0002", 1)})
	if !hasViolation(vs, "I2", "is at or above next_id (T-0002)") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// A missing next_id.
	vs = validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "next_id: T-0011\n", "", 1)})
	if !hasViolation(vs, "I2", "frontmatter has no next_id") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// A malformed next_id.
	vs = validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "next_id: T-0011", "next_id: 42", 1)})
	if !hasViolation(vs, "I2", "next_id is not a T-NNNN id") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

// I3: the box character must match the file. This is the check that needs the
// raw box, since Closed() is derived from which file the item is in.
func TestValidateI3BoxMatchesFile(t *testing.T) {
	vs := validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, "- [ ] [T-0001]", "- [x] [T-0001]", 1)})
	if !hasViolation(vs, "I3", "T-0001 is closed but sits in backlog.md") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	vs = validateDir(t, map[string]string{
		"done.md": strings.Replace(sampleDone, "- [x] [T-0010]", "- [ ] [T-0010]", 1)})
	if !hasViolation(vs, "I3", "T-0010 is open but sits in done.md") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI4WorkingCoherence(t *testing.T) {
	vs := validateDir(t, map[string]string{
		"working.01.md": strings.Replace(busySlot, "started: 2026-07-30", "started: null", 1)})
	if !hasViolation(vs, "I4", "started:null") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
	vs = validateDir(t, map[string]string{
		"working.01.md": strings.Replace(idleSlot, "id: null", "id: T-0009", 1)})
	if !hasViolation(vs, "I4", "status is idle but id is T-0009") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI5Blocked(t *testing.T) {
	// Under Blocked with no reason.
	vs := validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog, " | blocked:on a thing", "", 1)})
	if !hasViolation(vs, "I5", "T-0002 is under Blocked with no blocked: field") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// A reason outside Blocked.
	vs = validateDir(t, map[string]string{
		"backlog.md": strings.Replace(dirBacklog,
			"- [ ] [T-0003] Maybe | prio:low | created:2026-07-29",
			"- [ ] [T-0003] Maybe | prio:low | created:2026-07-29 | blocked:why", 1)})
	if !hasViolation(vs, "I5", "T-0003 has a blocked: field but is under Someday") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI6DoneFields(t *testing.T) {
	// Wrong month group.
	vs := validateDir(t, map[string]string{
		"done.md": strings.Replace(sampleDone, "done:2026-07-23", "done:2026-03-02", 1)})
	if !hasViolation(vs, "I6", "T-0010 has done:2026-03-02 under heading 2026-07") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// Missing outcome.
	vs = validateDir(t, map[string]string{
		"done.md": strings.Replace(sampleDone, " | outcome:shipped", "", 1)})
	if !hasViolation(vs, "I6", "T-0010 has no outcome: field") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI7Project(t *testing.T) {
	for _, mut := range []string{"project: Sample One\n", "project: Sample One"} {
		repl := ""
		if !strings.HasSuffix(mut, "\n") {
			repl = "project: null"
		}
		vs := validateDir(t, map[string]string{
			"backlog.md": strings.Replace(dirBacklog, mut, repl, 1)})
		if !hasViolation(vs, "I7", "frontmatter has no project name") {
			t.Errorf("mutation %q: got:\n%s", mut, violationMessages(vs))
		}
	}
}

func TestValidateI8DetailPaths(t *testing.T) {
	withDetail := strings.Replace(dirBacklog,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | prio:med | tags:example | detail:details/T-0001.md | created:2026-07-29", 1)

	// Referenced but absent.
	vs := validateDir(t, map[string]string{"backlog.md": withDetail})
	if !hasViolation(vs, "I8", "detail file does not exist: details/T-0001.md") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// Pointing at the wrong name.
	wrong := strings.Replace(withDetail, "detail:details/T-0001.md", "detail:details/notes.md", 1)
	vs = validateDir(t, map[string]string{"backlog.md": wrong})
	if !hasViolation(vs, "I8", "T-0001 points at details/notes.md (expected details/T-0001.md)") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateI9DetailDrift(t *testing.T) {
	withDetail := strings.Replace(dirBacklog,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | prio:med | tags:example | detail:details/T-0001.md | created:2026-07-29", 1)

	// Title drift - the whole reason the duplication exists.
	vs := validateDir(t, map[string]string{
		"backlog.md":        withDetail,
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: Renamed elsewhere\n---\n",
	})
	if !hasViolation(vs, "I9", `frontmatter title is "Renamed elsewhere", expected "First"`) {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// Orphan.
	vs = validateDir(t, map[string]string{
		"details/T-0099.md": "---\ndoc: detail\nid: T-0099\ntitle: Nobody\n---\n"})
	if !hasViolation(vs, "I9", "orphan") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// A template is exempt: it belongs to no item by design.
	vs = validateDir(t, map[string]string{
		"details/_template.md": "---\ndoc: detail\nid: T-XXXX\ntitle: Template\n---\n"})
	if len(vs) != 0 {
		t.Errorf("a _-prefixed template must be exempt, got:\n%s", violationMessages(vs))
	}
}

func TestValidateI10WorkingFileSet(t *testing.T) {
	vs := validateDir(t, map[string]string{"working.03.md": idleSlot})
	if !hasViolation(vs, "I10", "not numbered 1..2 (missing: 2)") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
	vs = validateDir(t, map[string]string{"working.2.md": idleSlot})
	if !hasViolation(vs, "I10", "mix digit widths") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

func TestValidateMissingSections(t *testing.T) {
	src := strings.Replace(dirBacklog, "## Someday\n\n- [ ] [T-0003] Maybe | prio:low | created:2026-07-29\n", "", 1)
	vs := validateDir(t, map[string]string{"backlog.md": src})
	if !hasViolation(vs, invFormat, "no ## Someday heading") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

// Findings are ordered by file then NUMERIC line: line 10 must not sort before
// line 5.
func TestViolationsSortNumerically(t *testing.T) {
	vs := []Violation{
		{At: Location{File: "b.md", Line: 2}},
		{At: Location{File: "a.md", Line: 10}},
		{At: Location{File: "a.md", Line: 5}},
		{At: Location{File: "a.md", Line: 0}},
	}
	sortViolations(vs)
	want := []string{"a.md", "a.md:5", "a.md:10", "b.md:2"}
	for i, w := range want {
		if got := vs[i].At.String(); got != w {
			t.Errorf("position %d = %q, want %q", i, got, w)
		}
	}
}

func hasViolation(vs []Violation, invariant, substr string) bool {
	for _, v := range vs {
		if v.Invariant == invariant && strings.Contains(v.Message, substr) {
			return true
		}
	}
	return false
}
