package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0044 — --migrate end to end, the second §5.3 optional operation to reach
// the CLI. The library owns the repairs; what is tested here is the wrapper's
// half: the switch, --project, the three output modes, and the exit code a
// legacy directory produces before and after.

const legacyBacklogCLI = `---
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

const legacyWorkingCLI = `---
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

// legacyProject is a runner over a directory in the old shape: no project, a
// flow-sequence tags value, and a pre-slot working.md.
func legacyProject(t *testing.T) (runner, string) {
	t.Helper()
	r, dir := newProject(t)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(legacyBacklogCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "working.md"), []byte(legacyWorkingCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "working.01.md")); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--check"); got.Code != ExitInvariantViolation {
		t.Fatalf("the fixture should start broken: %s", got)
	}
	return r, dir
}

func TestMigrateCLIBringsALegacyDirectoryUpToDate(t *testing.T) {
	r, dir := legacyProject(t)

	// Dry run: the same report, nothing on disk.
	dry := r.run("--migrate", "--dry-run")
	if dry.Code != ExitOK {
		t.Fatalf("dry run: %s", dry)
	}
	if !strings.Contains(dry.Stdout, "would: migrated") ||
		!strings.Contains(dry.Stdout, "renamed working.md -> working.01.md") {
		t.Errorf("dry run stdout:\n%s", dry.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "working.01.md")); err == nil {
		t.Error("the dry run performed the rename")
	}

	got := r.run("--migrate", "--project", "Acme Rewrite")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	for _, want := range []string{
		"renamed working.md -> working.01.md",
		"backlog.md: project: Acme Rewrite",
		"tags:infra,ci",
	} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, got.Stdout)
		}
	}

	// The directory now validates — the assertion the whole operation is for.
	if check := r.run("--check"); check.Code != ExitOK {
		t.Fatalf("check after migrate: %s", check)
	}

	// And a second run has nothing to do, without failing.
	again := r.run("--migrate")
	if again.Code != ExitOK || !strings.Contains(again.Stdout, "nothing to migrate") {
		t.Errorf("second run: %s", again)
	}
}

// --project is the same switch --init uses, and means the same thing. Without
// it the name is derived and still reported: a guessed name is a starting
// point, but a silent one would be a decision.
func TestMigrateCLIProjectName(t *testing.T) {
	r, dir := legacyProject(t)

	got := r.run("--migrate")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if !strings.Contains(got.Stdout, "project: ") {
		t.Errorf("the derived name must be reported:\n%s", got.Stdout)
	}
	backlog := readFile(t, filepath.Join(dir, "backlog.md"))
	if !strings.Contains(backlog, "project: ") {
		t.Errorf("backlog.md has no project:\n%s", backlog)
	}
}

func TestMigrateCLIJSONAndPorcelain(t *testing.T) {
	r, _ := legacyProject(t)

	porc := r.run("--migrate", "--porcelain", "--dry-run")
	if porc.Code != ExitOK {
		t.Fatalf("porcelain: %s", porc)
	}
	// kind, file, line, after — one record per repair.
	if !strings.Contains(porc.Stdout, "renamed\tworking.md\t0\tworking.01.md") {
		t.Errorf("porcelain:\n%s", porc.Stdout)
	}
	if !strings.Contains(porc.Stdout, "tags\tbacklog.md\t11\t") {
		t.Errorf("porcelain is missing the tags record:\n%s", porc.Stdout)
	}

	got := r.run("--migrate", "--json")
	if got.Code != ExitOK {
		t.Fatalf("json: %s", got)
	}
	var env struct {
		OK        bool
		Operation string
		Result    struct {
			Changes []struct {
				Kind  string
				File  string
				Line  int
				After string
			}
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if !env.OK || env.Operation != "migrate" {
		t.Errorf("envelope: ok=%v operation=%q", env.OK, env.Operation)
	}
	if len(env.Result.Changes) != 3 {
		t.Errorf("changes = %+v, want the rename, the tags and the project", env.Result.Changes)
	}
	var kinds []string
	for _, c := range env.Result.Changes {
		kinds = append(kinds, c.Kind)
	}
	if strings.Join(kinds, ",") != "renamed,tags,project" {
		t.Errorf("kinds = %v", kinds)
	}
}

// A value it recognizes and cannot convert is named on stderr, survives
// --quiet, and leaves the finding for --check to go on reporting.
func TestMigrateCLIWarnsAboutWhatItCannotConvert(t *testing.T) {
	r, dir := legacyProject(t)
	backlog := readFile(t, filepath.Join(dir, "backlog.md"))
	backlog = strings.Replace(backlog, "tags:[infra, ci]", "tags:[hello world]", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--migrate", "--quiet")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if got.Stdout != "" {
		t.Errorf("--quiet should print no commentary:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stderr, "backlog.md:11") {
		t.Errorf("the unconvertible value must be named:\n%s", got.Stderr)
	}

	// The rest of the migration happened, and the one finding it could not fix
	// is still reported.
	check := r.run("--check")
	if check.Code != ExitInvariantViolation {
		t.Fatalf("check = %d, want the finding it left: %s", check.Code, check)
	}
	if !strings.Contains(check.Stdout, "malformed tags") {
		t.Errorf("check:\n%s", check.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "working.01.md")); err != nil {
		t.Error("one bad value stopped the rename")
	}
}

// A directory that is already current says so and exits 0 — a script gating on
// "is this old?" needs an answer, not a silence.
func TestMigrateCLIOnACurrentDirectory(t *testing.T) {
	r, _ := newProject(t)

	got := r.run("--migrate")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if !strings.Contains(got.Stdout, "nothing to migrate") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}
