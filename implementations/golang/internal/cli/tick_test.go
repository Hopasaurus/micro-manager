package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// T-0173 — --tick end to end (spec-tools.md §5.3.3).
//
// The library's own tests own the splice and the due test; what is tested here
// is the wrapper's half: both kinds of fire reaching the human, JSON and
// porcelain outputs, dry-run writing nothing, idempotence across two runs, and
// --add --tickler (the §5.1.2 spelling the CLI side of the same feature needs).

func tickProject(t *testing.T, args ...string) (runner, string) {
	t.Helper()
	r, dir := v2Project(t)
	if got := r.run(append([]string{"--add", "One shot", "--stage", "someday",
		"--tickler", "2026-07-01", "--created", "2026-07-01"}, args...)...); got.Code != ExitOK {
		t.Fatalf("add one-shot failed: %s", got)
	}
	// A recurring prototype whose created date anchors a due fire: created
	// 2026-07-01, so next(created) is on or before testDay 2026-07-30.
	if got := r.run("--add", "Recurring", "--stage", "someday",
		"--tickler", "15@08:00", "--created", "2026-07-01"); got.Code != ExitOK {
		t.Fatalf("add recurring failed: %s", got)
	}
	return r, dir
}

func TestTickFiresBothKinds(t *testing.T) {
	r, _ := tickProject(t)

	got := r.run("--tick")
	if got.Code != ExitOK {
		t.Fatalf("tick failed: %s", got)
	}
	// Both kinds, named. The one-shot moved, the recurring spawned.
	if !strings.Contains(got.Stdout, "T-0001: moved to Ready (tickled 2026-07-30)") {
		t.Errorf("one-shot fire not reported:\n%s", got.Stdout)
	}
	if !strings.Contains(got.Stdout, "T-0002: spawned T-0003 into Ready (tickled 2026-07-30)") {
		t.Errorf("recurring fire not reported:\n%s", got.Stdout)
	}

	// The one-shot consumed its schedule; the prototype kept it and was stamped.
	show := r.run("--show", "T-0001", "--detail")
	if !strings.Contains(show.Stdout, "tickled") {
		t.Errorf("one-shot should carry tickled:\n%s", show.Stdout)
	}
	show = r.run("--show", "T-0002")
	if !strings.Contains(show.Stdout, "tickler   15@08:00") {
		t.Errorf("prototype should keep its tickler:\n%s", show.Stdout)
	}
	if !strings.Contains(show.Stdout, "tickled   2026-07-30") {
		t.Errorf("prototype should be stamped tickled:\n%s", show.Stdout)
	}

	// Idempotent within a day: a second run finds nothing due.
	again := r.run("--tick")
	if !strings.Contains(again.Stdout, "nothing due") {
		t.Errorf("second run should be a no-op:\n%s", again.Stdout)
	}
}

func TestTickDryRunWritesNothing(t *testing.T) {
	r, dir := tickProject(t)

	before := readFile(t, dir+"/board.md")
	got := r.run("--tick", "--dry-run")
	if got.Code != ExitOK {
		t.Fatalf("dry run failed: %s", got)
	}
	if !strings.HasPrefix(got.Stdout, "would: ") {
		t.Errorf("dry run output must be prefixed:\n%s", got.Stdout)
	}
	if after := readFile(t, dir+"/board.md"); after != before {
		t.Errorf("dry run wrote the file")
	}

	// The real run right after fires the same set.
	real := r.run("--tick")
	if !strings.Contains(real.Stdout, "T-0001: moved to Ready") {
		t.Errorf("real run should fire what the dry run previewed:\n%s", real.Stdout)
	}
}

func TestTickJSONAndPorcelain(t *testing.T) {
	r, _ := tickProject(t)

	got := r.run("--tick", "--json")
	if got.Code != ExitOK {
		t.Fatalf("tick --json failed: %s", got)
	}
	var env struct {
		OK        bool   `json:"ok"`
		Operation string `json:"operation"`
		Result    struct {
			Fired []struct {
				ID      string `json:"id"`
				Kind    string `json:"kind"`
				Spawned string `json:"spawned"`
				Tickled string `json:"tickled"`
			} `json:"fired"`
			Errors []map[string]any `json:"errors"`
		} `json:"result"`
		Changes []struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			File string `json:"file"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("bad json: %v\n%s", err, got.Stdout)
	}
	if env.Operation != "tick" || !env.OK {
		t.Errorf("envelope: ok=%v op=%s", env.OK, env.Operation)
	}
	if len(env.Result.Fired) != 2 {
		t.Fatalf("want 2 fired, got %d:\n%s", len(env.Result.Fired), got.Stdout)
	}
	if f := env.Result.Fired[0]; f.ID != "T-0001" || f.Kind != "move" || f.Tickled != "2026-07-30" {
		t.Errorf("one-shot json: %+v", f)
	}
	if f := env.Result.Fired[1]; f.ID != "T-0002" || f.Kind != "spawn" || f.Spawned != "T-0003" {
		t.Errorf("spawn json: %+v", f)
	}
	if len(env.Changes) != 3 {
		t.Errorf("want 3 synthesized changes (move + update + create), got %d", len(env.Changes))
	}

	// A second run is a no-op, so the porcelain half needs its own board.
	pr, _ := tickProject(t)
	porc := pr.run("--tick", "--porcelain")
	if porc.Code != ExitOK {
		t.Fatalf("porcelain run failed: %s", porc)
	}
	lines := strings.Split(strings.TrimRight(porc.Stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 porcelain rows, got %d:\n%s", len(lines), porc.Stdout)
	}
	cols := strings.Split(lines[0], "\t")
	if cols[0] != "T-0001" || cols[1] != "move" || cols[3] != "2026-07-30" {
		t.Errorf("move row: %q", cols)
	}
	cols = strings.Split(lines[1], "\t")
	if cols[0] != "T-0002" || cols[1] != "spawn" || cols[2] != "T-0003" {
		t.Errorf("spawn row: %q", cols)
	}
}

func TestTickNothingDue(t *testing.T) {
	r, _ := v2Project(t)
	if got := r.run("--add", "Later", "--stage", "someday", "--tickler", "2026-09-01"); got.Code != ExitOK {
		t.Fatalf("add failed: %s", got)
	}

	got := r.run("--tick")
	if got.Code != ExitOK {
		t.Fatalf("tick failed: %s", got)
	}
	if !strings.Contains(got.Stdout, "nothing due") {
		t.Errorf("want the affirmative no-op message:\n%s", got.Stdout)
	}

	// The JSON half reports two empty lists, never null.
	j := r.run("--tick", "--json")
	var env struct {
		Result struct {
			Fired  []any `json:"fired"`
			Errors []any `json:"errors"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(j.Stdout), &env); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if env.Result.Fired == nil || env.Result.Errors == nil {
		t.Error("fired/errors must be [] not null")
	}
}

func TestTickAddRequiresSomeday(t *testing.T) {
	r, _ := v2Project(t)
	// --tickler without --stage naming a tickler_stages source is the
	// library's placement rule (generalized from version 1's fixed Someday);
	// the CLI passes the value through and the library refuses it. The
	// default stage (ready) is not a source in DefaultStageConfig.
	got := r.run("--add", "Wrong place", "--tickler", "mon@08:00")
	if got.Code != ExitUsage {
		t.Fatalf("expected a refusal, got %d:\n%s", got.Code, got)
	}
	if !strings.Contains(got.Stderr, "tickler_stages") && !strings.Contains(got.Stdout, "tickler_stages") {
		t.Errorf("refusal should name the tickler_stages rule:\n%s", got)
	}
}
