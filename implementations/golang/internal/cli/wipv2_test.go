package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// --wip N --stage SLUG: version 2's per-stage WIP cap (spec-file-format.md
// §5.1.3), generalizing --wip N's version-1 file-count form. T-0230 (M4).

// v2Project inits a fresh version-1 project and migrates it to version 2, so
// tests here start from a real board.md rather than hand-building one.
func v2Project(t *testing.T, args ...string) (runner, string) {
	t.Helper()
	r, dir := newProject(t, args...)
	if got := r.run("--migrate"); got.Code != ExitOK {
		t.Fatalf("migrate: %s", got)
	}
	return r, dir
}

func TestWipCLIStageSetsAndClears(t *testing.T) {
	r, _ := v2Project(t)

	got := r.run("--wip", "2", "--stage", "working")
	if got.Code != ExitOK {
		t.Fatalf("set: %s", got)
	}
	if !strings.Contains(got.Stdout, "wip limit for working is now 2") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}

	again := r.run("--wip", "2", "--stage", "working")
	if again.Code != ExitOK || !strings.Contains(again.Stdout, "already 2") {
		t.Errorf("second run: %s", again)
	}

	cleared := r.run("--wip", "0", "--stage", "working")
	if cleared.Code != ExitOK || !strings.Contains(cleared.Stdout, "working is now uncapped") {
		t.Errorf("clear: %s", cleared)
	}
}

func TestWipCLIStageRejectsUndeclaredStage(t *testing.T) {
	r, _ := v2Project(t)
	got := r.run("--wip", "2", "--stage", "not-a-stage")
	if got.Code == ExitOK {
		t.Fatalf("an undeclared stage should be refused: %s", got)
	}
}

func TestWipCLIStageDryRunWritesNothing(t *testing.T) {
	r, dir := v2Project(t)
	before := readFile(t, dir+"/board.md")

	got := r.run("--wip", "2", "--stage", "working", "--dry-run")
	if got.Code != ExitOK {
		t.Fatalf("dry run: %s", got)
	}
	if !strings.Contains(got.Stdout, "would:") {
		t.Errorf("stdout should be prefixed as a dry run:\n%s", got.Stdout)
	}
	if got := readFile(t, dir+"/board.md"); got != before {
		t.Error("board.md changed during a dry run")
	}
}

func TestWipCLIWithoutStageStillSetsVersion1Limit(t *testing.T) {
	r, _ := newProject(t)
	got := r.run("--wip", "3")
	if got.Code != ExitOK {
		t.Fatalf("wip: %s", got)
	}
	if !strings.Contains(got.Stdout, "wip limit is now 3") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}

func TestStatusCLIV2ShowsStageCounts(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Ready one")
	r.run("--add", "Blocked one", "--stage", "blocked", "--reason", "waiting")

	got := r.run("--status")
	if got.Code != ExitOK {
		t.Fatalf("status: %s", got)
	}
	if strings.Contains(got.Stdout, "ready:     0") {
		t.Errorf("stdout still shows the retired zeroed v1 counts:\n%s", got.Stdout)
	}
	for _, want := range []string{"Someday:", "Ready:     1", "Blocked:   1", "Working:"} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, got.Stdout)
		}
	}
	if strings.Contains(got.Stdout, "working.01.md") {
		t.Error("a version-2 status must not list working.NN.md slots")
	}
}

func TestStatusCLIV2JSONHasStagesArray(t *testing.T) {
	r, _ := v2Project(t)
	got := r.run("--status", "--json")
	if got.Code != ExitOK {
		t.Fatalf("status: %s", got)
	}
	var env struct {
		Result struct {
			Version int
			Stages  []struct {
				Slug  string
				Label string
				Count int
			}
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if env.Result.Version != 2 {
		t.Errorf("version = %d, want 2", env.Result.Version)
	}
	if len(env.Result.Stages) == 0 {
		t.Fatal("stages should be populated for a version-2 directory")
	}
}

func TestAddCLIV2StageAndReason(t *testing.T) {
	r, _ := v2Project(t)

	got := r.run("--add", "Needs eyes", "--stage", "blocked", "--reason", "waiting on design")
	if got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}
	show := r.run("--show", "T-0001", "--json")
	if show.Code != ExitOK {
		t.Fatalf("show: %s", show)
	}
	var env struct {
		Result struct {
			Item struct {
				Stage  string
				Reason string
			}
		}
	}
	if err := json.Unmarshal([]byte(show.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, show.Stdout)
	}
	if env.Result.Item.Stage != "blocked" || env.Result.Item.Reason != "waiting on design" {
		t.Errorf("item = %+v, want stage:blocked reason:%q", env.Result.Item, "waiting on design")
	}
}

func TestMoveCLIV2Stage(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Move me")

	got := r.run("--move", "T-0001", "--stage", "blocked", "--reason", "on hold")
	if got.Code != ExitOK {
		t.Fatalf("move: %s", got)
	}
	if !strings.Contains(got.Stdout, "stage:blocked") || !strings.Contains(got.Stdout, "reason:on hold") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}

func TestPauseCLIV2Stage(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Work this")
	r.run("--start", "T-0001")

	got := r.run("--pause", "T-0001", "--stage", "someday")
	if got.Code != ExitOK {
		t.Fatalf("pause: %s", got)
	}
	if !strings.Contains(got.Stdout, "stage:someday") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}

func TestStartCLIV2SaysStartedNotSlot(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "Pick this up")

	got := r.run("--start", "T-0001")
	if got.Code != ExitOK {
		t.Fatalf("start: %s", got)
	}
	if strings.Contains(got.Stdout, "slot") {
		t.Errorf("a version-2 start should not mention a slot:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "T-0001 started") {
		t.Errorf("stdout:\n%s", got.Stdout)
	}
}
