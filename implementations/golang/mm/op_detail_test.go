package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testTemplate = `---
doc: detail
id: T-XXXX
title: Copy this file to details/<ID>.md
updated: 2026-07-29
---

# T-XXXX — Title goes here

## Context

Why this exists.

## References
`

// withDetail returns a backlog where T-0001 points at its detail file.
func withDetailLine(src string) string {
	return strings.Replace(src,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | prio:med | tags:example | detail:details/T-0001.md | created:2026-07-29", 1)
}

func TestAttachDetailFromTemplate(t *testing.T) {
	dir := newDir(t, map[string]string{"details/_template.md": testTemplate})
	s := mustOpen(t, dir)

	d, res, err := s.AttachDetail("T-0001", AttachDetailRequest{}, today)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if d.Path != "details/T-0001.md" {
		t.Errorf("path = %q", d.Path)
	}
	if len(res.Changes) != 2 {
		t.Errorf("want 2 changes (item + file), got %+v", res.Changes)
	}

	body := readDirFile(t, dir, "details/T-0001.md")
	// Frontmatter matches the item, which is what I9 checks.
	if !strings.Contains(body, "id: T-0001") || !strings.Contains(body, "title: First") {
		t.Errorf("frontmatter wrong:\n%s", body)
	}
	// The template's structure came through...
	if !strings.Contains(body, "## Context") || !strings.Contains(body, "Why this exists.") {
		t.Errorf("template body not used:\n%s", body)
	}
	// ...but its placeholder id, title and H1 did not.
	if strings.Contains(body, "T-XXXX") || strings.Contains(body, "Copy this file") {
		t.Errorf("template placeholders leaked into the file:\n%s", body)
	}
	if strings.Count(body, "\n# ") != 1 {
		t.Errorf("want exactly one H1, got:\n%s", body)
	}
	// And the item now points at it.
	if !strings.Contains(readDirFile(t, dir, "backlog.md"), "detail:details/T-0001.md") {
		t.Error("the item does not reference the new file")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// With no template in the directory, a built-in shape is used rather than
// failing: a directory that never shipped a template still works.
func TestAttachDetailWithoutTemplate(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	if _, _, err := s.AttachDetail("T-0001", AttachDetailRequest{}, today); err != nil {
		t.Fatalf("attach: %v", err)
	}
	body := readDirFile(t, dir, "details/T-0001.md")
	if !strings.Contains(body, "## Context") || !strings.Contains(body, "## References") {
		t.Errorf("built-in shape missing:\n%s", body)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestAttachDetailWithBody(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	if _, _, err := s.AttachDetail("T-0001",
		AttachDetailRequest{Body: "Just a note."}, today); err != nil {
		t.Fatal(err)
	}
	body := readDirFile(t, dir, "details/T-0001.md")
	if !strings.Contains(body, "Just a note.") {
		t.Errorf("body missing:\n%s", body)
	}
	if strings.Contains(body, "## Context") {
		t.Error("an explicit body should replace the template, not append to it")
	}
}

func TestAttachDetailFromTemplatePlusBody(t *testing.T) {
	dir := newDir(t, map[string]string{"details/_template.md": testTemplate})
	s := mustOpen(t, dir)
	if _, _, err := s.AttachDetail("T-0001",
		AttachDetailRequest{Body: "An extra note.", FromTemplate: true}, today); err != nil {
		t.Fatal(err)
	}
	body := readDirFile(t, dir, "details/T-0001.md")
	if !strings.Contains(body, "## Context") || !strings.Contains(body, "An extra note.") {
		t.Errorf("want both template and body:\n%s", body)
	}
}

func TestAttachDetailRefusals(t *testing.T) {
	// Already attached.
	dir := newDir(t, map[string]string{
		"backlog.md":        withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n",
	})
	s := mustOpen(t, dir)
	if _, _, err := s.AttachDetail("T-0001", AttachDetailRequest{}, today); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("want ErrAlreadyExists, got %v", err)
	}

	// The file exists as an orphan: adopting it is the repair, clobbering it is not.
	dir = newDir(t, map[string]string{
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n\nPrecious text.\n",
	})
	s = mustOpen(t, dir)
	_, _, err := s.AttachDetail("T-0001", AttachDetailRequest{}, today)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("want ErrConflict for an existing orphan, got %v", err)
	}
	if !strings.Contains(readDirFile(t, dir, "details/T-0001.md"), "Precious text.") {
		t.Error("the orphan's contents were destroyed")
	}

	// Unknown item.
	if _, _, err := s.AttachDetail("T-9999", AttachDetailRequest{}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestReadDetail(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n\n" +
			"## Context\n\nSome text.\n",
	})
	s := mustOpen(t, dir)

	d, err := s.Detail("T-0001")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if d.ID != "T-0001" || d.Title != "First" {
		t.Errorf("got %+v", d)
	}
	if !strings.HasPrefix(d.Body, "## Context") || !strings.Contains(d.Body, "Some text.") {
		t.Errorf("body = %q", d.Body)
	}
	if strings.Contains(d.Body, "---") {
		t.Error("the body should not include the frontmatter")
	}

	// An item with no detail file is a distinct, named outcome.
	if _, err := s.Detail("T-0002"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestSetDetailBody(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\nupdated: 2026-07-01\n---\n\n" +
			"Old body.\n",
	})
	s := mustOpen(t, dir)

	if _, err := s.SetDetailBody("T-0001", "## New\n\nBrand new body.", false, today); err != nil {
		t.Fatalf("set: %v", err)
	}
	out := readDirFile(t, dir, "details/T-0001.md")
	if strings.Contains(out, "Old body.") || !strings.Contains(out, "Brand new body.") {
		t.Errorf("body not replaced:\n%s", out)
	}
	// Frontmatter survives, with updated: refreshed.
	if !strings.Contains(out, "id: T-0001") || !strings.Contains(out, "title: First") {
		t.Errorf("frontmatter damaged:\n%s", out)
	}
	if !strings.Contains(out, "updated: 2026-07-29") {
		t.Errorf("updated: not refreshed:\n%s", out)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// The mechanism --pause and --finish use to keep a working file's notes, which
// exist nowhere else.
func TestAppendDetailSection(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n\n" +
			"## Context\n\nExisting context.\n\n## References\n\n- a link\n",
	})
	s := mustOpen(t, dir)

	// Into an existing section: appended at the end of it, not the file.
	if _, err := s.AppendDetailSection("T-0001", "Context", "- 2026-07-29 — a note", false, today); err != nil {
		t.Fatalf("append: %v", err)
	}
	out := readDirFile(t, dir, "details/T-0001.md")
	ctx := strings.Index(out, "## Context")
	note := strings.Index(out, "a note")
	refs := strings.Index(out, "## References")
	if !(ctx < note && note < refs) {
		t.Errorf("the note should land inside ## Context:\n%s", out)
	}
	if !strings.Contains(out, "Existing context.") || !strings.Contains(out, "- a link") {
		t.Errorf("existing content lost:\n%s", out)
	}

	// Into a section that does not exist: created at the end.
	if _, err := s.AppendDetailSection("T-0001", "Notes", "- 2026-07-29 — from the slot", false, today); err != nil {
		t.Fatalf("append new section: %v", err)
	}
	out = readDirFile(t, dir, "details/T-0001.md")
	if !strings.Contains(out, "## Notes") || !strings.Contains(out, "from the slot") {
		t.Errorf("new section missing:\n%s", out)
	}
	if strings.Index(out, "## References") > strings.Index(out, "## Notes") {
		t.Errorf("a new section should go at the end:\n%s", out)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

// Detaching must not leave an invalid directory silently. The caller has to
// choose: delete the file, or accept the orphan explicitly.
func TestDetachDetail(t *testing.T) {
	files := map[string]string{
		"backlog.md":        withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n\nBody.\n",
	}

	// Neither option given: refused, and nothing is written. Clearing the field
	// alone would produce an I9 orphan, and the envelope never writes a directory
	// its own checker rejects.
	dir := newDir(t, files)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")
	_, err := s.DetachDetail("T-0001", DetachDetailRequest{}, today)
	if !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("want ErrPreconditionFailed, got %v", err)
	}
	for _, want := range []string{"Delete", "AllowOrphan", "details/T-0001.md"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the message should name %q: %v", want, err)
		}
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a refused detach wrote to backlog.md")
	}

	// Asking for both is equally ambiguous.
	if _, err := s.DetachDetail("T-0001",
		DetachDetailRequest{Delete: true, AllowOrphan: true}, today); !errors.Is(err, ErrPreconditionFailed) {
		t.Errorf("both options should be refused, got %v", err)
	}

	// Delete: both go, and the directory stays valid.
	dir = newDir(t, files)
	s = mustOpen(t, dir)
	res, err := s.DetachDetail("T-0001", DetachDetailRequest{Delete: true}, today)
	if err != nil {
		t.Fatalf("detach with delete: %v", err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != ChangeDeleted {
		t.Errorf("changes = %+v", res.Changes)
	}
	if strings.Contains(readDirFile(t, dir, "backlog.md"), "detail:") {
		t.Error("the detail: field should have been cleared")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); !os.IsNotExist(err) {
		t.Error("the file should have been removed")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after detach+delete:\n%s", violationMessages(vs))
	}

	// AllowOrphan: the field is cleared, the file survives, and the resulting
	// I9 violation is real and reported by --check afterwards.
	dir = newDir(t, files)
	s = mustOpen(t, dir)
	if _, err := s.DetachDetail("T-0001", DetachDetailRequest{AllowOrphan: true}, today); err != nil {
		t.Fatalf("detach with AllowOrphan: %v", err)
	}
	if strings.Contains(readDirFile(t, dir, "backlog.md"), "detail:") {
		t.Error("the detail: field should have been cleared")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); err != nil {
		t.Error("the file should survive under AllowOrphan")
	}
	if vs, _ := s.Validate(); !hasViolation(vs, "I9", "orphan") {
		t.Errorf("the orphan should be reported by --check:\n%s", violationMessages(vs))
	}
}

// The property that makes detail files worth having: the item moves, the file
// does not, and the link survives every transition.
func TestDetailFileNeverMoves(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": withDetailLine(dirBacklog),
		"details/T-0001.md": "---\ndoc: detail\nid: T-0001\ntitle: First\n---\n\n" +
			"Durable text that must survive every move.\n",
	})
	s := mustOpen(t, dir)
	path := filepath.Join(dir, "details/T-0001.md")
	before := readFileString(t, path)

	// Move the item between sections - the file must not be touched.
	if _, _, err := s.Update("T-0001", UpdateRequest{Prio: priop(PrioHigh)}, today); err != nil {
		t.Fatal(err)
	}
	if readFileString(t, path) != before {
		t.Error("an unrelated edit rewrote the detail file")
	}

	// Retitle: the frontmatter title syncs, the body does not move or change.
	if _, _, err := s.Update("T-0001", UpdateRequest{Title: strp("Renamed")}, today); err != nil {
		t.Fatal(err)
	}
	after := readFileString(t, path)
	if !strings.Contains(after, "Durable text that must survive every move.") {
		t.Errorf("body lost:\n%s", after)
	}
	if !strings.Contains(after, "title: Renamed") {
		t.Errorf("title not synced:\n%s", after)
	}
	// Same path, throughout.
	d, err := s.Detail("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if d.Path != "details/T-0001.md" {
		t.Errorf("path changed to %q", d.Path)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestDetailDryRun(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	before := readDirFile(t, dir, "backlog.md")

	if _, res, err := s.AttachDetail("T-0001", AttachDetailRequest{DryRun: true}, today); err != nil {
		t.Fatal(err)
	} else if !res.DryRun || len(res.Changes) != 2 {
		t.Errorf("res = %+v", res)
	}
	if readDirFile(t, dir, "backlog.md") != before {
		t.Error("a dry run modified backlog.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "details/T-0001.md")); !os.IsNotExist(err) {
		t.Error("a dry run created the file")
	}
}

// A template with no frontmatter is still a usable body rather than an error.
func TestReadTemplateWithoutFrontmatter(t *testing.T) {
	dir := newDir(t, map[string]string{"details/_template.md": "## Notes\n\nplain template\n"})
	s := mustOpen(t, dir)
	if _, _, err := s.AttachDetail("T-0001", AttachDetailRequest{}, today); err != nil {
		t.Fatal(err)
	}
	if body := readDirFile(t, dir, "details/T-0001.md"); !strings.Contains(body, "plain template") {
		t.Errorf("template not used:\n%s", body)
	}
}
