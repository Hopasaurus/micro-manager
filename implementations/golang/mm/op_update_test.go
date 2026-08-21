package mm

import (
	"errors"
	"strings"
	"testing"
)

func strp(s string) *string { return &s }
func priop(p Prio) *Prio    { return &p }

func TestUpdateFields(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)

	it, res, err := testUpdateV1(s, "T-0001", UpdateRequest{
		Title: strp("Renamed"),
		Prio:  priop(PrioHigh),
	}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if it.Title != "Renamed" || it.Prio != PrioHigh {
		t.Errorf("got %+v", it)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeUpdated {
		t.Errorf("changes = %+v", res.Changes)
	}
	// The Before/After pair is what makes a dry run readable.
	if !strings.Contains(res.Changes[0].Before, "First") ||
		!strings.Contains(res.Changes[0].After, "Renamed") {
		t.Errorf("change should carry both forms: %+v", res.Changes[0])
	}
	out := readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out, "- [ ] [T-0001] Renamed | prio:high | tags:example | created:2026-07-29") {
		t.Errorf("line not rewritten as expected:\n%s", out)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// A nil field means "leave alone"; without pointers this would be
// indistinguishable from "clear it".
func TestUpdateLeavesUnsetFieldsAlone(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	if _, _, err := testUpdateV1(s, "T-0001", UpdateRequest{Prio: priop(PrioLow)}, today); err != nil {
		t.Fatal(err)
	}
	it, _ := s.Get("T-0001")
	if it.Title != "First" {
		t.Errorf("title should be untouched, got %q", it.Title)
	}
	if FormatTags(it.Tags) != "example" {
		t.Errorf("tags should be untouched, got %v", it.Tags)
	}
	if it.Created.String() != "2026-07-29" {
		t.Errorf("created should be untouched, got %q", it.Created)
	}
	// Only the one item line changed. updated: was already today, so SetFM
	// correctly left it alone rather than rewriting it to the same value.
	if n := countDiffLines(before, readDirFile(t, dir, "backlog.md")); n != 1 {
		t.Errorf("want 1 changed line, got %d", n)
	}
}

func TestUpdateTagOperations(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))

	// Incremental add.
	it, _, err := testUpdateV1(s, "T-0001", UpdateRequest{AddTags: []string{"infra", "ci"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if FormatTags(it.Tags) != "example,infra,ci" {
		t.Errorf("add: got %v", it.Tags)
	}
	// Adding a tag twice must not duplicate it.
	it, _, err = testUpdateV1(s, "T-0001", UpdateRequest{AddTags: []string{"infra"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if FormatTags(it.Tags) != "example,infra,ci" {
		t.Errorf("re-add should be idempotent: %v", it.Tags)
	}
	// Incremental remove.
	it, _, err = testUpdateV1(s, "T-0001", UpdateRequest{RemoveTags: []string{"example", "ci"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if FormatTags(it.Tags) != "infra" {
		t.Errorf("remove: got %v", it.Tags)
	}
	// Wholesale replace.
	it, _, err = testUpdateV1(s, "T-0001", UpdateRequest{SetTags: true, Tags: []string{"a", "b"}}, today)
	if err != nil {
		t.Fatal(err)
	}
	if FormatTags(it.Tags) != "a,b" {
		t.Errorf("replace: got %v", it.Tags)
	}
	// Replace with empty clears the field entirely.
	it, _, err = testUpdateV1(s, "T-0001", UpdateRequest{SetTags: true, Tags: nil}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Tags) != 0 || strings.Contains(RenderItemLine(&it), "tags:") {
		t.Errorf("cleared tags should not render: %q", RenderItemLine(&it))
	}
	// The two forms are exclusive.
	_, _, err = testUpdateV1(s, "T-0001", UpdateRequest{
		SetTags: true, Tags: []string{"a"}, AddTags: []string{"b"}}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// The extension point: --set must reach a key this implementation has never
// heard of, and --unset must remove it.
func TestUpdateSetUnsetUnregisteredFields(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)

	if _, _, err := testUpdateV1(s, "T-0001", UpdateRequest{
		Set: []Field{{"owner", "dlh"}, {"estimate", "3d"}}}, today); err != nil {
		t.Fatal(err)
	}
	out := readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out, "| owner:dlh | estimate:3d") {
		t.Errorf("unregistered fields not written:\n%s", out)
	}

	// Setting an existing unknown key updates it in place, preserving order.
	if _, _, err := testUpdateV1(s, "T-0001", UpdateRequest{
		Set: []Field{{"owner", "someone-else"}}}, today); err != nil {
		t.Fatal(err)
	}
	out = readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out, "| owner:someone-else | estimate:3d") {
		t.Errorf("in-place update changed field order:\n%s", out)
	}

	if _, _, err := testUpdateV1(s, "T-0001", UpdateRequest{Unset: []string{"owner"}}, today); err != nil {
		t.Fatal(err)
	}
	out = readDirFile(t, dir, "backlog.md")
	if strings.Contains(out, "owner:") || !strings.Contains(out, "estimate:3d") {
		t.Errorf("unset removed the wrong field:\n%s", out)
	}
}

// An edit of one field must not drop unregistered fields it never touched.
func TestUpdatePreservesUnregisteredFieldsAcrossAnEdit(t *testing.T) {
	src := strings.Replace(dirBacklog,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29 | owner:dlh | estimate:3d", 1)
	dir := newDir(t, map[string]string{"backlog.md": src})
	s := mustOpen(t, dir)

	if _, _, err := testUpdateV1(s, "T-0001", UpdateRequest{Prio: priop(PrioLow)}, today); err != nil {
		t.Fatal(err)
	}
	out := readDirFile(t, dir, "backlog.md")
	if !strings.Contains(out, "prio:low") {
		t.Error("the edit did not apply")
	}
	if !strings.Contains(out, "owner:dlh") || !strings.Contains(out, "estimate:3d") {
		t.Errorf("editing prio destroyed unrelated fields:\n%s", out)
	}
}

func TestUpdateWorkingSlotItem(t *testing.T) {
	// busySlot's T-0042 sits above dirBacklog's next_id and points at a detail
	// file that does not exist, so it needs an ID and fields that fit the rest
	// of the fixture set.
	slot := strings.NewReplacer(
		"id: T-0042", "id: T-0006",
		"detail: details/T-0042.md", "detail: null",
	).Replace(busySlot)
	dir := newDir(t, map[string]string{"working.02.md": slot})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}

	it, res, err := testUpdateV1(s, "T-0006", UpdateRequest{
		Prio: priop(PrioLow), AddTags: []string{"urgent"}}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if it.Prio != PrioLow {
		t.Errorf("prio = %q", it.Prio)
	}
	if res.Changes[0].File != "working.02.md" {
		t.Errorf("change should name the slot file, got %q", res.Changes[0].File)
	}
	out := readDirFile(t, dir, "working.02.md")
	if !strings.Contains(out, "prio: low") {
		t.Errorf("frontmatter not updated:\n%s", out)
	}
	// tags stays a TAGLIST here, not a YAML list.
	if !strings.Contains(out, "tags: infra,ci,urgent") {
		t.Errorf("tags should stay a TAGLIST:\n%s", out)
	}
	// The body survives - only frontmatter lines changed.
	if !strings.Contains(out, "a subtask, which has no ID") {
		t.Error("the body was damaged")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestUpdateDoneItem(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	if _, _, err := testUpdateV1(s, "T-0010", UpdateRequest{AddTags: []string{"shipped-late"}}, today); err != nil {
		t.Fatalf("editing a closed item should work: %v", err)
	}
	out := readDirFile(t, dir, "done.md")
	if !strings.Contains(out, "tags:shipped-late") {
		t.Errorf("done.md not updated:\n%s", out)
	}
	// Still closed, still dated, still filed under its month.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// The sharpest coupling in the operation set: retitling an item has to drag its
// detail file along, in the same transaction, or I9 breaks silently.
func TestUpdateTitleSyncsDetailFile(t *testing.T) {
	withDetail := strings.Replace(dirBacklog,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | prio:med | tags:example | detail:details/T-0001.md | created:2026-07-29", 1)
	dir := newDir(t, map[string]string{
		"backlog.md":        withDetail,
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\nupdated: 2026-07-01\n---\n\n# T-0001 — First\n\nBody text.\n",
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}

	_, res, err := testUpdateV1(s, "T-0001", UpdateRequest{Title: strp("Renamed properly")}, today)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(res.Changes) != 2 {
		t.Errorf("want 2 changes (item + detail), got %+v", res.Changes)
	}

	detail := readDirFile(t, dir, "details/T-0001.md")
	if !strings.Contains(detail, "title: Renamed properly") {
		t.Errorf("detail title not synced:\n%s", detail)
	}
	if !strings.Contains(detail, "updated: 2026-07-29") {
		t.Error("detail updated: not refreshed")
	}
	// Only the frontmatter changed; the body is untouched.
	if !strings.Contains(detail, "Body text.") || !strings.Contains(detail, "# T-0001 — First") {
		t.Errorf("the detail body was rewritten:\n%s", detail)
	}
	// And I9 still holds.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("retitle broke an invariant:\n%s", violationMessages(vs))
	}
}

func TestUpdateValidation(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	cases := []struct {
		name string
		id   ID
		req  UpdateRequest
		want error
	}{
		{"unknown item", "T-9999", UpdateRequest{Prio: priop(PrioLow)}, ErrNotFound},
		{"empty title", "T-0001", UpdateRequest{Title: strp("  ")}, ErrInvalidArgument},
		{"pipe in title", "T-0001", UpdateRequest{Title: strp("a | b")}, ErrInvalidArgument},
		{"bad prio", "T-0001", UpdateRequest{Prio: priop("URGENT")}, ErrInvalidArgument},
		{"bad tag", "T-0001", UpdateRequest{AddTags: []string{"a b"}}, ErrInvalidArgument},
		{"bad date via set", "T-0001", UpdateRequest{Set: []Field{{"created", "2026-02-31"}}}, ErrInvalidArgument},
		{"pipe via set", "T-0001", UpdateRequest{Set: []Field{{"note", "a|b"}}}, ErrInvalidArgument},
		{"wrong detail path", "T-0001", UpdateRequest{Set: []Field{{"detail", "details/other.md"}}}, ErrInvalidArgument},
		{"unset title", "T-0001", UpdateRequest{Unset: []string{"title"}}, ErrInvalidArgument},
		{"reason outside blocked", "T-0001", UpdateRequest{Blocked: strp("why")}, ErrConflict},
		{"clear required reason", "T-0002", UpdateRequest{Blocked: strp("")}, ErrConflict},
		{"unset required reason", "T-0002", UpdateRequest{Unset: []string{"blocked"}}, ErrConflict},
		{"unset done on closed", "T-0010", UpdateRequest{Unset: []string{"done"}}, ErrConflict},
		{"unset outcome on closed", "T-0010", UpdateRequest{Unset: []string{"outcome"}}, ErrConflict},
	}
	for _, c := range cases {
		_, _, err := testUpdateV1(s, c.id, c.req, today)
		if err == nil {
			t.Errorf("%s: should fail", c.name)
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
}

// I6 requires done: and outcome: on every closed item, so an --unset that would
// remove one is refused rather than written and then reported.
func TestUpdateCannotBreakDoneInvariants(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "done.md")
	if _, _, err := testUpdateV1(s, "T-0010", UpdateRequest{Unset: []string{"outcome"}}, today); err == nil {
		t.Fatal("should be refused")
	}
	if readDirFile(t, dir, "done.md") != before {
		t.Error("a refused update wrote to the file")
	}
}

func TestUpdateDryRun(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	it, res, err := testUpdateV1(s, "T-0001", UpdateRequest{
		Title: strp("Would be renamed"), DryRun: true}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || it.Title != "Would be renamed" {
		t.Errorf("dry run should report the result: %+v", it)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a dry run wrote to the file")
	}
}

// An update that changes nothing writes nothing.
func TestUpdateNoOp(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	if _, res, err := testUpdateV1(s, "T-0001", UpdateRequest{Prio: priop(PrioMed)}, today); err != nil {
		t.Fatal(err)
	} else if len(res.Files) != 0 {
		t.Errorf("a no-op should touch no files, got %v", res.Files)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a no-op update rewrote the file")
	}
}
