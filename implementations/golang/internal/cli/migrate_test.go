package cli

import (
	"encoding/json"
	"os"
	"os/exec"
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
//
// Scoped with --to 1 so only the legacy repair runs: a bare --migrate would
// also carry a now-valid version-1 directory on to version 2 (§5.3.4), which
// is exactly right in general but would replace backlog.md with board.md
// before this test gets to read it — a different behavior, covered by
// TestMigrateCLIAlsoRunsTheVersionChain, not this one.
func TestMigrateCLIProjectName(t *testing.T) {
	r, dir := legacyProject(t)

	got := r.run("--migrate", "--to", "1")
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
	// The version chain cannot proceed past a directory the legacy repair
	// left broken; that has to be visible even under --quiet, the same way
	// the value it could not convert is.
	if !strings.Contains(got.Stderr, "version not migrated") {
		t.Errorf("the blocked version bump must be reported:\n%s", got.Stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "board.md")); !os.IsNotExist(err) {
		t.Error("board.md should not exist: the version bump must not have run")
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

// A directory that needs no legacy repair says so and exits 0 — a script
// gating on "is this old?" needs an answer, not a silence. Scoped to --to 1
// for the same reason TestMigrateCLIProjectName is: see
// TestMigrateCLIAlsoRunsTheVersionChain for the bare, default-to-latest case.
func TestMigrateCLIOnACurrentDirectory(t *testing.T) {
	r, _ := newProject(t)

	got := r.run("--migrate", "--to", "1")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if !strings.Contains(got.Stdout, "nothing to migrate") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}

// A bare --migrate, with no --to, also runs the versioned chain
// (spec-tools.md §5.3.4): a healthy version-1 directory - --init still
// creates version 1, T-0233's own scope stopping short of that - becomes
// version 2 in the same invocation the legacy repair runs in, and a second
// run then reports nothing left to do at either phase.
func TestMigrateCLIAlsoRunsTheVersionChain(t *testing.T) {
	r, dir := newProject(t)

	got := r.run("--migrate")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if !strings.Contains(got.Stdout, "migrated version 1 -> 2") {
		t.Errorf("stdout should report the version bump:\n%s", got.Stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, "board.md")); err != nil {
		t.Errorf("board.md should exist after migrating to version 2: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "backlog.md")); !os.IsNotExist(err) {
		t.Error("backlog.md should be gone after migrating to version 2")
	}

	if check := r.run("--check"); check.Code != ExitOK {
		t.Fatalf("check after migrate: %s", check)
	}

	again := r.run("--migrate")
	if again.Code != ExitOK || !strings.Contains(again.Stdout, "nothing to migrate") {
		t.Errorf("second run: %s", again)
	}
}

// --to targets a specific version rather than the latest this build
// implements, and refuses a target this build cannot reach.
func TestMigrateCLITo(t *testing.T) {
	r, dir := newProject(t)

	got := r.run("--migrate", "--to", "99")
	if got.Code == ExitOK {
		t.Fatalf("--to 99 should fail, this build only knows version 2: %s", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "board.md")); !os.IsNotExist(err) {
		t.Error("a rejected --to must not have migrated anything")
	}

	bad := r.run("--migrate", "--to", "not-a-number")
	if bad.Code != ExitUsage {
		t.Errorf("--to not-a-number should be a usage error, got %s", bad)
	}
}

// The git-uncommitted-changes warning (spec-tools.md §5.3.4) belongs to the
// wrapper, not the library (TestLibraryImports forbids os/exec in package
// mm) - this is that wiring, proved against a real git tree.
func TestMigrateCLIWarnsOnDirtyGitTree(t *testing.T) {
	r, dir := newProject(t)
	repoRoot := filepath.Dir(dir) // newProject's cwd; micro-manager is a subdirectory
	if out, err := exec.Command("git", "-C", repoRoot, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git not available in this environment: %v: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--migrate", "--to", "1")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	if !strings.Contains(got.Stderr, "uncommitted git changes") {
		t.Errorf("stderr should warn about the dirty tree:\n%s", got.Stderr)
	}
}

// The chain's JSON output is a separate "steps" field from the legacy
// repair's "changes", so an existing caller parsing the old shape is
// unaffected by the new one appearing alongside it.
func TestMigrateCLIJSONStepsField(t *testing.T) {
	r, dir := newProject(t)

	got := r.run("--migrate", "--json")
	if got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	var env struct {
		Result struct {
			Steps []struct {
				From    int
				To      int
				Changes []struct {
					Kind string
					File string
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if len(env.Result.Steps) != 1 || env.Result.Steps[0].From != 1 || env.Result.Steps[0].To != 2 {
		t.Fatalf("steps = %+v, want one From:1 To:2 step", env.Result.Steps)
	}
	if _, err := os.Stat(filepath.Join(dir, "board.md")); err != nil {
		t.Errorf("board.md should exist: %v", err)
	}
}
