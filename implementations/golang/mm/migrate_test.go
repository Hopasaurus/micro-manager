package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 1->2 migration step (spec-tools.md §5.3.4, spec-file-format.md
// Appendix C), proved against a captured version-1 directory carrying real
// working-file notes, subtasks and a blocker - the content Appendix C says
// must land in details/<ID>.md, not be dropped on the floor.

const migV1Backlog = `---
doc: backlog
version: 1
project: Migrate Me
board: mig
next_id: T-0007
updated: 2026-08-01
owner: dana
---

# Backlog

## Ready

- [ ] [T-0001] Ready item | prio:med | created:2026-07-01

## Blocked

- [ ] [T-0002] Blocked item | prio:low | created:2026-07-02 | blocked:waiting on vendor

## Someday

- [ ] [T-0003] Someday item | created:2026-07-03 | tickler:mon@08:00
`

const migV1Done = `---
doc: done
version: 1
updated: 2026-08-01
---

# Done

## 2026-07

- [x] [T-0006] Already shipped | created:2026-07-10 | done:2026-07-20 | outcome:shipped
`

// working.01.md: occupied, no detail file yet. Every body section carries
// real content, so the fold-in has something in all four to move.
const migV1Working01 = `---
doc: working
version: 1
status: working
id: T-0004
title: In progress, no detail yet
prio: high
tags: infra
detail: null
created: 2026-07-05
started: 2026-07-06
---

# Working

## Task

Ship the deploy fix.

## Plan

- [ ] write the migration
- [x] design the format

## Notes

Talked to ops, they want a canary first.

## Blockers

Waiting on a review from dana.
`

// working.02.md: occupied, WITH an existing detail file - the append path,
// not the create path.
const migV1Working02 = `---
doc: working
version: 1
status: working
id: T-0005
title: Second in-progress item
prio: null
tags: null
detail: details/T-0005.md
created: 2026-07-07
started: 2026-07-08
---

# Working

## Task

See [details/T-0005.md](details/T-0005.md).

## Plan

## Notes

Second item's fresh notes from this work session.

## Blockers
`

const migV1Detail0005 = `---
doc: detail
id: T-0005
title: Second in-progress item
updated: 2026-07-05
---

# T-0005 — Second in-progress item

## Context

Some pre-existing long-form notes that predate this working session.
`

// working.03.md: idle. Counts toward wip.working but contributes no item and
// nothing to fold - the boilerplate empty headings must not manufacture a
// detail file.
const migV1Working03 = `---
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

// migV1Dir builds a version-1 directory on disk with real working-file
// content to migrate, and returns its path.
func migV1Dir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"backlog.md":        migV1Backlog,
		"done.md":           migV1Done,
		"working.01.md":     migV1Working01,
		"working.02.md":     migV1Working02,
		"working.03.md":     migV1Working03,
		"details/T-0005.md": migV1Detail0005,
	}
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestMigrateOneToTwoProducesAValidBoard(t *testing.T) {
	dir := migV1Dir(t)
	s := mustOpen(t, dir)

	results, err := s.MigrateVersion(MigrateVersionRequest{}, today)
	if err != nil {
		t.Fatalf("MigrateVersion: %v", err)
	}
	if len(results) != 1 || results[0].From != 1 || results[0].To != 2 {
		t.Fatalf("results = %+v, want one From:1 To:2 step", results)
	}

	// The old shape is gone.
	for _, name := range []string{"backlog.md", "working.01.md", "working.02.md", "working.03.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should be gone after migration, stat err = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "board.md")); err != nil {
		t.Errorf("board.md should exist: %v", err)
	}

	// The migrated directory validates clean.
	vs, err := s.Validate()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(vs) != 0 {
		t.Fatalf("migrated directory should be clean:\n%s", violationMessages(vs))
	}

	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != 2 {
		t.Errorf("Version = %d, want 2", d.Version)
	}
	if d.Project != "Migrate Me" {
		t.Errorf("Project = %q; unregistered/registered backlog.md keys must carry through", d.Project)
	}
	if d.StageCfg.WipLimits["working"] != 3 {
		t.Errorf("wip.working = %d, want 3 (the working-file count)", d.StageCfg.WipLimits["working"])
	}

	board, err := os.ReadFile(filepath.Join(dir, "board.md"))
	if err != nil {
		t.Fatal(err)
	}
	boardText := string(board)
	if !strings.Contains(boardText, "owner: dana") {
		t.Error("board.md should carry the unregistered owner: key through from backlog.md")
	}

	ready, err := s.Get("T-0001")
	if err != nil {
		t.Fatal(err)
	}
	if ready.Stage != "ready" {
		t.Errorf("T-0001 stage = %q, want ready", ready.Stage)
	}

	blocked, err := s.Get("T-0002")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Stage != "blocked" || blocked.Reason != "waiting on vendor" || blocked.Blocked != "" {
		t.Errorf("T-0002 = stage:%q reason:%q blocked:%q, want blocked/waiting on vendor/empty",
			blocked.Stage, blocked.Reason, blocked.Blocked)
	}

	someday, err := s.Get("T-0003")
	if err != nil {
		t.Fatal(err)
	}
	if someday.Stage != "someday" || someday.Tickler != "mon@08:00" {
		t.Errorf("T-0003 = stage:%q tickler:%q, want someday/mon@08:00", someday.Stage, someday.Tickler)
	}

	working1, err := s.Get("T-0004")
	if err != nil {
		t.Fatal(err)
	}
	if working1.Stage != "working" || working1.Started.String() != "2026-07-06" {
		t.Errorf("T-0004 = stage:%q started:%q, want working/2026-07-06", working1.Stage, working1.Started)
	}
	if working1.Detail == "" {
		t.Fatal("T-0004 should have gained a detail file from its working-file body")
	}

	detail1, err := s.Detail("T-0004")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Task", "Ship the deploy fix.",
		"## Plan", "- [ ] write the migration", "- [x] design the format",
		"## Notes", "Talked to ops, they want a canary first.",
		"## Blockers", "Waiting on a review from dana.",
	} {
		if !strings.Contains(detail1.Body, want) {
			t.Errorf("T-0004's detail file missing %q\ngot:\n%s", want, detail1.Body)
		}
	}

	// T-0005 already had a detail file; migration must APPEND to it, not
	// replace it, and the pre-existing content must survive untouched.
	detail2, err := s.Detail("T-0005")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail2.Body, "Some pre-existing long-form notes") {
		t.Error("T-0005's pre-existing ## Context content was lost")
	}
	if !strings.Contains(detail2.Body, "Second item's fresh notes from this work session.") {
		t.Error("T-0005's working-file ## Notes were not folded in")
	}

	// done.md: version bumped, the one shipped item otherwise untouched.
	doneBytes, err := os.ReadFile(filepath.Join(dir, "done.md"))
	if err != nil {
		t.Fatal(err)
	}
	doneText := string(doneBytes)
	if !strings.Contains(doneText, "version: 2") {
		t.Error("done.md should read version: 2 after migration")
	}
	if !strings.Contains(doneText, "- [x] [T-0006] Already shipped | created:2026-07-10 | done:2026-07-20 | outcome:shipped") {
		t.Error("done.md's item line should be untouched by the version bump")
	}

	// Migrating an already-version-2 directory is a no-op error, not silent
	// success or a second write.
	if _, err := s.MigrateVersion(MigrateVersionRequest{}, today); !errors.Is(err, ErrConflict) {
		t.Errorf("re-migrating should report ErrConflict, got %v", err)
	}
}

func TestMigrateOneToTwoDryRunWritesNothing(t *testing.T) {
	dir := migV1Dir(t)
	s := mustOpen(t, dir)

	before := map[string][]byte{}
	for _, name := range []string{"backlog.md", "done.md", "working.01.md", "working.02.md", "working.03.md"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		before[name] = b
	}

	results, err := s.MigrateVersion(MigrateVersionRequest{DryRun: true}, today)
	if err != nil {
		t.Fatalf("MigrateVersion (dry run): %v", err)
	}
	if len(results) != 1 || len(results[0].Changes) == 0 {
		t.Fatalf("a dry run should still report what it would change: %+v", results)
	}

	if _, err := os.Stat(filepath.Join(dir, "board.md")); !os.IsNotExist(err) {
		t.Errorf("board.md must not exist after a dry run, stat err = %v", err)
	}
	for name, want := range before {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s vanished during a dry run: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s changed during a dry run", name)
		}
	}
}

// A line that fails to parse at all is invisible to m.backlog.Items - the
// permissive parser reports it as a Violation and moves on - so it must
// refuse rather than silently rebuild board.md without the item.
func TestMigrateOneToTwoRefusesRatherThanDroppingAnUnparseableItem(t *testing.T) {
	dir := migV1Dir(t)
	backlog := readMigFile(t, dir, "backlog.md")
	// A space inside a tag is not a TAGLIST the parser can read; the whole
	// line fails, unlike a flow sequence op_migrate.go's legacy repair can
	// convert.
	backlog = strings.Replace(backlog,
		"- [ ] [T-0001] Ready item | prio:med | created:2026-07-01",
		"- [ ] [T-0001] Ready item | prio:med | tags:not a tag | created:2026-07-01", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	s := mustOpen(t, dir)

	var invErr *InvariantError
	if _, err := s.MigrateVersion(MigrateVersionRequest{}, today); !errors.As(err, &invErr) {
		t.Fatalf("err = %v, want *InvariantError (unreadable lines must block, not silently drop the item)", err)
	}
	// Nothing written: board.md must not exist, backlog.md must be untouched.
	if _, err := os.Stat(filepath.Join(dir, "board.md")); !os.IsNotExist(err) {
		t.Error("board.md should not exist after a refused migration")
	}
	if got := readMigFile(t, dir, "backlog.md"); got != backlog {
		t.Error("backlog.md should be untouched after a refused migration")
	}
}

func readMigFile(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMigrateVersionErrors(t *testing.T) {
	dir := migV1Dir(t)
	s := mustOpen(t, dir)

	if _, err := s.MigrateVersion(MigrateVersionRequest{To: 99}, today); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("--to a version this build does not implement should be ErrInvalidArgument, got %v", err)
	}

	// A directory with neither board.md nor backlog.md has nothing to migrate.
	emptyDir := t.TempDir()
	es := mustOpen(t, emptyDir)
	if _, err := es.MigrateVersion(MigrateVersionRequest{}, today); !errors.Is(err, ErrNotFound) {
		t.Errorf("an empty directory should report ErrNotFound, got %v", err)
	}
}
