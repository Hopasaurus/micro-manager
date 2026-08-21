package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// --include-stage SLUG (spec-tools.md §5.1.11): version 2 only, repeatable,
// appends what currently sits on that stage; --include-wip is kept as a
// shorthand for --include-stage working. T-0230 (M4).

func TestReportCLIIncludeStage(t *testing.T) {
	r, _ := v2Project(t)
	if got := r.run("--add", "Fix the deploy script", "--stage", "ready"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}

	got := r.run("--report", "--period", "all", "--include-stage", "ready")
	if got.Code != ExitOK {
		t.Fatalf("report: %s", got)
	}
	if !strings.Contains(got.Stdout, "## Ready") || !strings.Contains(got.Stdout, "Fix the deploy script") {
		t.Errorf("--include-stage ready should list what is on Ready:\n%s", got.Stdout)
	}
}

// --include-wip is a shorthand for --include-stage working, on a version-2
// directory — not version 1's working-slot meaning, which stays unchanged.
func TestReportCLIIncludeWipIsStageWorkingShorthandOnV2(t *testing.T) {
	r, _ := v2Project(t)
	if got := r.run("--add", "Rewrite the deploy docs"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}
	if got := r.run("--start", "T-0001"); got.Code != ExitOK {
		t.Fatalf("start: %s", got)
	}

	wip := r.run("--report", "--period", "all", "--include-wip")
	if wip.Code != ExitOK {
		t.Fatalf("report --include-wip: %s", wip)
	}
	stage := r.run("--report", "--period", "all", "--include-stage", "working")
	if stage.Code != ExitOK {
		t.Fatalf("report --include-stage working: %s", stage)
	}
	if wip.Stdout != stage.Stdout {
		t.Errorf("--include-wip and --include-stage working should agree on v2:\nwip:\n%s\nstage:\n%s", wip.Stdout, stage.Stdout)
	}
	if !strings.Contains(wip.Stdout, "Rewrite the deploy docs") {
		t.Errorf("the started item should appear:\n%s", wip.Stdout)
	}
}

func TestReportCLIIncludeStageRejectsUndeclaredStage(t *testing.T) {
	r, _ := v2Project(t)
	got := r.run("--report", "--period", "all", "--include-stage", "not-a-stage")
	if got.Code == ExitOK {
		t.Fatalf("an undeclared stage should be refused: %s", got)
	}
}

// A version-1 directory has no stages: declaration to validate a slug
// against, so --include-stage refuses there instead of silently doing
// nothing.
func TestReportCLIIncludeStageRefusedOnV1(t *testing.T) {
	r, _ := newProject(t)
	got := r.run("--report", "--period", "all", "--include-stage", "ready")
	if got.Code == ExitOK {
		t.Fatalf("--include-stage on a version-1 directory should be refused: %s", got)
	}
}

func TestReportCLIIncludeStageJSON(t *testing.T) {
	r, _ := v2Project(t)
	if got := r.run("--add", "Second thing", "--stage", "blocked", "--reason", "waiting"); got.Code != ExitOK {
		t.Fatalf("add: %s", got)
	}

	got := r.run("--report", "--period", "all", "--include-stage", "blocked", "--json")
	if got.Code != ExitOK {
		t.Fatalf("report: %s", got)
	}
	var parsed struct {
		Result struct {
			StageIncluded []struct {
				Stage string `json:"stage"`
				Label string `json:"label"`
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			} `json:"stageIncluded"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, got.Stdout)
	}
	if len(parsed.Result.StageIncluded) != 1 {
		t.Fatalf("stageIncluded = %+v", parsed.Result.StageIncluded)
	}
	g := parsed.Result.StageIncluded[0]
	if g.Stage != "blocked" || g.Label != "Blocked" || len(g.Items) != 1 || g.Items[0].ID != "T-0001" {
		t.Errorf("group = %+v", g)
	}
}
