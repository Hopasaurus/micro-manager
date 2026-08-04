package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0044 — --migrate, over the three legacy shapes spec-tools.md §5.3 names.
//
// The fixtures are deliberately written the OLD way: a backlog with no project,
// tags as YAML flow sequences, and a pre-slot working.md. Every one of them is
// a directory the current validator rejects, which is the point — the assertion
// that matters most is that a migrated directory validates.

const legacyBacklog = `---
doc: backlog
version: 1
next_id: T-0003
---

# Backlog

## Ready

- [ ] [T-0001] Old style item | prio:high | tags:[infra, ci] | created:2026-01-05

## Blocked

## Someday
`

const legacyDone = `---
doc: done
version: 1
---

# Done

## 2026-01

- [x] [T-0002] Done thing | tags:[ops] | created:2026-01-01 | done:2026-01-06 | outcome:shipped
`

const legacyWorking = `---
doc: working
version: 1
status: idle
id: null
title: null
prio: null
tags: null
detail: null
created: null
started: null
---

# Working

## Task

## Plan

## Notes

## Blockers
`

// legacyDir is a directory as an earlier revision of the format wrote it.
// files overrides or adds to the three defaults.
func legacyDir(t *testing.T, files map[string]string) (string, *Store) {
	t.Helper()
	all := map[string]string{
		"backlog.md": legacyBacklog,
		"done.md":    legacyDone,
		"working.md": legacyWorking,
	}
	for k, v := range files {
		all[k] = v
	}
	dir := newDir(t, all)
	// newDir writes a current working.01.md; a legacy directory has none,
	// unless the caller asked for one.
	if _, given := files["working.01.md"]; !given {
		if err := os.Remove(filepath.Join(dir, "working.01.md")); err != nil {
			t.Fatal(err)
		}
	}
	return dir, mustOpen(t, dir)
}

// projectOf reads the directory's project name through the public surface.
func projectOf(t *testing.T, s *Store) string {
	t.Helper()
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	return d.Project
}

func changeKinds(res MigrateResult) string {
	var out []string
	for _, c := range res.Changes {
		out = append(out, string(c.Kind))
	}
	return strings.Join(out, ",")
}

// The whole point, in one test: a directory the checker rejects goes in, and a
// directory it accepts comes out.
func TestMigrateBringsALegacyDirectoryUpToDate(t *testing.T) {
	dir, s := legacyDir(t, nil)
	before, _ := s.Validate()
	if len(before) == 0 {
		t.Fatal("the fixture should start broken, or this proves nothing")
	}

	res, tx, err := s.Migrate(MigrateRequest{}, today)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("the migrated directory should validate:\n%s", violationMessages(vs))
	}

	// working.md is gone, working.01.md holds what it held.
	if exists(t, dir, "working.md") {
		t.Error("working.md survived the rename")
	}
	if got := readFile(t, dir, "working.01.md"); !strings.Contains(got, "status: idle") {
		t.Errorf("working.01.md:\n%s", got)
	}

	// The project name is the parent directory's, and it is reported.
	project := projectOf(t, s)
	if project == "" || project == "null" {
		t.Error("backlog.md still has no project")
	}

	// Both flow sequences became TAGLISTs, in both files.
	if got := readFile(t, dir, "backlog.md"); !strings.Contains(got, "tags:infra,ci") {
		t.Errorf("backlog.md tags were not converted:\n%s", got)
	}
	if got := readFile(t, dir, "done.md"); !strings.Contains(got, "tags:ops") {
		t.Errorf("done.md tags were not converted:\n%s", got)
	}

	// Every change is reported (§5.3), one per repair.
	if got := changeKinds(res); got != "renamed,tags,tags,project" {
		t.Errorf("changes = %s, want one per repair: %+v", got, res.Changes)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("nothing here is unconvertible: %q", res.Warnings)
	}
	if len(tx.Files) != 4 {
		t.Errorf("files = %v, want backlog, done, the new slot and the old one", tx.Files)
	}

	// And the item the conversion touched parses back with its tags intact.
	items, err := s.List(Filter{State: StateBacklog})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || strings.Join(items[0].Tags, ",") != "infra,ci" {
		t.Errorf("the migrated item does not read back: %+v", items)
	}
}

// §5.3: dry-runnable. And §7 rule 6: a second run writes nothing at all.
func TestMigrateDryRunAndIdempotence(t *testing.T) {
	dir, s := legacyDir(t, nil)
	before := readFile(t, dir, "backlog.md")

	dry, tx, err := s.Migrate(MigrateRequest{DryRun: true}, today)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !tx.DryRun || len(dry.Changes) != 4 {
		t.Errorf("a dry run must report what a real run would do: %+v", dry.Changes)
	}
	if readFile(t, dir, "backlog.md") != before {
		t.Error("the dry run wrote backlog.md")
	}
	if exists(t, dir, "working.01.md") || !exists(t, dir, "working.md") {
		t.Error("the dry run performed the rename")
	}

	if _, _, err := s.Migrate(MigrateRequest{}, today); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	after := readFile(t, dir, "backlog.md")

	res, tx2, err := s.Migrate(MigrateRequest{}, today)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(res.Changes) != 0 || len(tx2.Files) != 0 {
		t.Errorf("the second run was not a no-op: %+v %v", res.Changes, tx2.Files)
	}
	if readFile(t, dir, "backlog.md") != after {
		t.Error("the second run rewrote backlog.md")
	}
}

// The project name: given, or derived from the parent directory and reported
// either way. It is free text (§10.8), so a guess is a starting point — but a
// silent one would be a decision.
func TestMigrateProjectName(t *testing.T) {
	t.Run("given", func(t *testing.T) {
		dir, s := legacyDir(t, nil)
		res, _, err := s.Migrate(MigrateRequest{Project: "Acme Rewrite"}, today)
		if err != nil {
			t.Fatal(err)
		}
		if got := projectOf(t, s); got != "Acme Rewrite" {
			t.Errorf("project = %q", got)
		}
		if !strings.Contains(readFile(t, dir, "backlog.md"), "project: Acme Rewrite") {
			t.Error("the name is not in the frontmatter")
		}
		var named bool
		for _, c := range res.Changes {
			if c.Kind == MigrateProject && c.After == "Acme Rewrite" {
				named = true
			}
		}
		if !named {
			t.Errorf("the change does not report the name it wrote: %+v", res.Changes)
		}
	})

	t.Run("derived from the parent directory", func(t *testing.T) {
		_, s := legacyDir(t, nil)
		if _, _, err := s.Migrate(MigrateRequest{}, today); err != nil {
			t.Fatal(err)
		}
		// The board directory IS the test's temp dir, so the derived name is
		// that directory's parent — whatever the runner made it.
		if got := projectOf(t, s); got == "" || got == "null" {
			t.Errorf("project = %q, want the parent directory's name", got)
		}
	})

	t.Run("an existing name is never overwritten", func(t *testing.T) {
		_, s := legacyDir(t, map[string]string{
			"backlog.md": strings.Replace(legacyBacklog,
				"next_id: T-0003", "next_id: T-0003\nproject: Already Named", 1),
		})
		res, _, err := s.Migrate(MigrateRequest{Project: "Something Else"}, today)
		if err != nil {
			t.Fatal(err)
		}
		if got := projectOf(t, s); got != "Already Named" {
			t.Errorf("project = %q; migration must not rename a named directory", got)
		}
		if strings.Contains(changeKinds(res), "project") {
			t.Errorf("a project change was reported for a directory that had one: %+v", res.Changes)
		}
	})
}

// The rename picks the lowest free slot at the width already in use, so a
// directory that has both shapes does not lose either file.
func TestMigrateRenamesIntoTheLowestFreeSlot(t *testing.T) {
	t.Run("beside existing slots", func(t *testing.T) {
		dir, s := legacyDir(t, map[string]string{
			"working.01.md": legacyWorking,
			"working.02.md": legacyWorking,
		})
		if _, _, err := s.Migrate(MigrateRequest{}, today); err != nil {
			t.Fatal(err)
		}
		if !exists(t, dir, "working.03.md") {
			t.Error("the legacy file did not become the next free slot")
		}
		if exists(t, dir, "working.md") {
			t.Error("working.md survived")
		}
		if vs, _ := s.Validate(); len(vs) != 0 {
			t.Errorf("violations:\n%s", violationMessages(vs))
		}
	})

	t.Run("at the directory's own width", func(t *testing.T) {
		dir, s := legacyDir(t, map[string]string{"working.001.md": legacyWorking})
		if _, _, err := s.Migrate(MigrateRequest{}, today); err != nil {
			t.Fatal(err)
		}
		if !exists(t, dir, "working.002.md") {
			t.Error("the rename ignored the directory's digit width (I10)")
		}
		if vs, _ := s.Validate(); len(vs) != 0 {
			t.Errorf("violations:\n%s", violationMessages(vs))
		}
	})
}

// A working.md whose CONTENT is not a working file is refused, not renamed:
// moving it would put a file the checker rejects at a name the checker reads.
func TestMigrateRefusesAnUnreadableWorkingFile(t *testing.T) {
	dir, s := legacyDir(t, map[string]string{
		"working.md": strings.Replace(legacyWorking, "status: idle", "status: busy", 1),
	})

	_, _, err := s.Migrate(MigrateRequest{}, today)
	var ie *InvariantError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want the violations under the new name", err)
	}
	if len(ie.Violations) == 0 || !strings.Contains(ie.Violations[0].At.File, "working.01.md") {
		t.Errorf("the violations should name the file it would have become: %+v", ie.Violations)
	}
	// Nothing at all was written: a refusal is not a partial migration.
	if exists(t, dir, "working.01.md") {
		t.Error("the refused rename wrote the new name anyway")
	}
	if !strings.Contains(readFile(t, dir, "backlog.md"), "tags:[infra, ci]") {
		t.Error("the refusal still converted tags; a refused migration writes nothing")
	}
}

// The tag converter, over the shapes a hand-written YAML file actually holds.
func TestMigrateTagsLineShapes(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		changed bool
		warns   bool
	}{
		{`- [ ] [T-0001] A | tags:[infra, ci] | created:2026-01-05`,
			`- [ ] [T-0001] A | tags:infra,ci | created:2026-01-05`, true, false},
		{`- [ ] [T-0001] A | tags:[ "infra", 'ci' ]`,
			`- [ ] [T-0001] A | tags:infra,ci`, true, false},
		{`- [ ] [T-0001] A | prio:high | tags:[] | created:2026-01-05`,
			`- [ ] [T-0001] A | prio:high | created:2026-01-05`, true, false},
		// Already current: left exactly alone.
		{`- [ ] [T-0001] A | tags:infra,ci`, `- [ ] [T-0001] A | tags:infra,ci`, false, false},
		// A space inside a tag is not something to guess at.
		{`- [ ] [T-0001] A | tags:[hello world]`, `- [ ] [T-0001] A | tags:[hello world]`, false, true},
		// Frontmatter, the other place the format puts a TAGLIST.
		{`tags: [infra, ci]`, `tags: infra,ci`, true, false},
		{`tags: []`, `tags: null`, true, false},
		{`tags: null`, `tags: null`, false, false},
		{`tags: infra,ci`, `tags: infra,ci`, false, false},
		// Not tags, and not touched: a title mentioning a list, an indented key.
		{`- [ ] [T-0001] Fix tags: [infra, ci] in the parser`,
			`- [ ] [T-0001] Fix tags: [infra, ci] in the parser`, false, false},
		{`  tags: [infra, ci]`, `  tags: [infra, ci]`, false, false},
	}
	for _, c := range cases {
		got, changed, warn := migrateTagsLine(c.in)
		if got != c.want || changed != c.changed || (warn != "") != c.warns {
			t.Errorf("migrateTagsLine(%q)\n = %q, changed=%v, warn=%q\nwant %q, changed=%v, warns=%v",
				c.in, got, changed, warn, c.want, c.changed, c.warns)
		}
	}
}

// An unconvertible tag list is reported and left exactly as written — and the
// rest of the migration still happens.
func TestMigrateReportsWhatItCannotConvert(t *testing.T) {
	dir, s := legacyDir(t, map[string]string{
		"backlog.md": strings.Replace(legacyBacklog, "tags:[infra, ci]", "tags:[hello world]", 1),
	})

	res, _, err := s.Migrate(MigrateRequest{}, today)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "backlog.md:11") {
		t.Errorf("warnings = %q, want the line it could not convert", res.Warnings)
	}
	if !strings.Contains(readFile(t, dir, "backlog.md"), "tags:[hello world]") {
		t.Error("the unconvertible value was rewritten anyway")
	}
	// The other two repairs still landed, and the value it left is still
	// reported by the validator.
	if !strings.Contains(changeKinds(res), "renamed") || !strings.Contains(changeKinds(res), "project") {
		t.Errorf("one bad value stopped the rest of the migration: %+v", res.Changes)
	}
	vs, _ := s.Validate()
	if len(vs) != 1 || !strings.Contains(vs[0].Message, "malformed tags") {
		t.Errorf("want exactly the one finding it could not fix:\n%s", violationMessages(vs))
	}
}

// Unlike Fix, Migrate does not refuse over a violation it does not own — if it
// did, a legacy directory that also has a duplicate ID could be repaired by
// neither tool, since Fix blocks on I10 and this is what clears I10.
func TestMigrateToleratesUnrelatedViolations(t *testing.T) {
	// A duplicate ID, the thing a git merge manufactures, on a directory that
	// is also legacy. Both lines parse, so the finding exists before and after
	// — and the migration inserts a project: line above them, which moves the
	// line number the I1 message names. That shift must not read as a NEW
	// violation (violationKey/maskLineRefs), or this refuses for no reason.
	merged := strings.Replace(legacyBacklog, "next_id: T-0003", "next_id: T-0010", 1)
	merged = strings.Replace(merged, "## Blocked",
		"- [ ] [T-0005] A merge duplicate | created:2026-01-05\n"+
			"- [ ] [T-0005] The other branch's | created:2026-01-06\n\n## Blocked", 1)
	_, s := legacyDir(t, map[string]string{"backlog.md": merged})
	before, _ := s.Validate()

	res, _, err := s.Migrate(MigrateRequest{}, today)
	if err != nil {
		t.Fatalf("an unrelated violation must not block the migration: %v", err)
	}
	if !strings.Contains(changeKinds(res), "renamed") {
		t.Errorf("the migration did not run: %+v", res.Changes)
	}

	// The duplicate is still there, untouched and still reported — Fix's job,
	// and Fix can now run, because I10 is clear.
	after, _ := s.Validate()
	if len(after) >= len(before) {
		t.Errorf("findings went from %d to %d; the migration fixed nothing:\n%s",
			len(before), len(after), violationMessages(after))
	}
	var dup bool
	for _, v := range after {
		if v.Invariant == "I1" {
			dup = true
		}
	}
	if !dup {
		t.Errorf("the duplicate ID was silently repaired or lost:\n%s", violationMessages(after))
	}
	if _, _, err := s.Fix(FixRequest{DryRun: true}); err != nil {
		t.Errorf("--fix should be able to run after a migration: %v", err)
	}
}

// The one case where tolerance runs out: a line that could not be parsed at all
// was not counted against I1, and converting it REVEALS a duplicate. The
// transaction refuses, because the finding really is introduced by this write —
// and refusing names the duplicate, which is what a person needs to resolve it.
func TestMigrateRefusesWhenARepairWouldRevealADuplicate(t *testing.T) {
	dir, s := legacyDir(t, map[string]string{
		"backlog.md": strings.Replace(legacyBacklog,
			"## Blocked", "- [ ] [T-0001] Parses fine | created:2026-01-05\n\n## Blocked", 1),
	})

	_, _, err := s.Migrate(MigrateRequest{}, today)
	var ie *InvariantError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v, want the duplicate the conversion would reveal", err)
	}
	if len(ie.Violations) == 0 || ie.Violations[0].Invariant != "I1" {
		t.Errorf("violations = %+v, want I1", ie.Violations)
	}
	if !strings.Contains(readFile(t, dir, "backlog.md"), "tags:[infra, ci]") {
		t.Error("a refused migration wrote part of itself")
	}
	if exists(t, dir, "working.01.md") {
		t.Error("a refused migration renamed the working file")
	}
}

// A directory that is already current is not touched at all: no rewrite, no
// mtime bump, no diff (§7 rule 6).
func TestMigrateOnACurrentDirectoryWritesNothing(t *testing.T) {
	dir := newDir(t, map[string]string{})
	s := mustOpen(t, dir)
	before := readFile(t, dir, "backlog.md")

	res, tx, err := s.Migrate(MigrateRequest{}, today)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(res.Changes) != 0 || len(tx.Files) != 0 {
		t.Errorf("a current directory was migrated: %+v %v", res.Changes, tx.Files)
	}
	if readFile(t, dir, "backlog.md") != before {
		t.Error("backlog.md was rewritten")
	}
}
