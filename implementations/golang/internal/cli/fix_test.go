package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0123 — --fix end to end: the merged board repairs, dry-run writes nothing,
// and the second run is a no-op with the same exit code.

// mergedBoardCLI builds a directory containing the T-0101 reproduction: two
// branches both allocated T-0043, one with a detail file on its branch.
func mergedBoardCLI(t *testing.T) (runner, string) {
	t.Helper()
	r, dir := newProject(t)
	backlog, err := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	// Two more items so the board is non-trivial, then the merged duplicate.
	merged := strings.Replace(string(backlog),
		"next_id: T-0001", "next_id: T-0044", 1)
	merged += "- [ ] [T-0043] Alpha | created:2026-07-31\n" +
		"- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md\n"
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	detail := "---\ndoc: detail\nid: T-0043\ntitle: Beta\nupdated: 2026-08-01\n---\n\n# Beta\n"
	if err := os.WriteFile(filepath.Join(dir, "details", "T-0043.md"), []byte(detail), 0o644); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestFixCLIRepairsTheMergedBoard(t *testing.T) {
	r, _ := mergedBoardCLI(t)

	// Dry-run first: it reports the same work and writes nothing.
	dry := r.run("--fix", "--dry-run")
	if dry.Code != ExitOK {
		t.Fatalf("dry-run exit %d:\n%s", dry.Code, dry)
	}
	if !strings.Contains(dry.Stdout, "T-0043 -> T-0044") {
		t.Errorf("dry-run stdout:\n%s", dry.Stdout)
	}

	real := r.run("--fix")
	if real.Code != ExitOK {
		t.Fatalf("exit %d:\n%s", real.Code, real)
	}
	if !strings.Contains(real.Stdout, "T-0043 -> T-0044") ||
		!strings.Contains(real.Stdout, "next_id T-0044 -> T-0045") {
		t.Errorf("stdout:\n%s", real.Stdout)
	}

	// The directory validates after the repair.
	check := r.run("--check")
	if check.Code != ExitOK {
		t.Fatalf("check after fix: %d:\n%s", check.Code, check)
	}

	// A second run is a no-op and still succeeds.
	again := r.run("--fix")
	if again.Code != ExitOK || !strings.Contains(again.Stdout, "nothing to fix") {
		t.Errorf("second run: %d:\n%s", again.Code, again)
	}
}

func TestFixCLIJSONReportsTheRenumbering(t *testing.T) {
	r, _ := mergedBoardCLI(t)

	// The porcelain stream names the renumbering, one record per change. Run
	// it on the still-broken board (dry-run) before the real fix below.
	porc := r.run("--fix", "--porcelain", "--dry-run")
	if porc.Code != ExitOK {
		t.Fatalf("porcelain exit %d:\n%s", porc.Code, porc)
	}
	if !strings.Contains(porc.Stdout, "T-0043\tT-0044\tbacklog.md\tdetails/T-0043.md") {
		t.Errorf("porcelain:\n%s", porc.Stdout)
	}

	got := r.run("--fix", "--json")
	if got.Code != ExitOK {
		t.Fatalf("exit %d:\n%s", got.Code, got)
	}
	var env struct {
		OK        bool `json:"ok"`
		Operation string
		Result    struct {
			Changes []struct {
				OldID string `json:"oldId"`
				NewID string `json:"newId"`
				File  string
			}
			NextID  string `json:"nextId"`
			WasNext string `json:"wasNext"`
		}
		Changes []struct {
			Kind string
			ID   string
			File string
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if !env.OK || env.Operation != "fix" {
		t.Errorf("envelope: ok=%v op=%q", env.OK, env.Operation)
	}
	if len(env.Result.Changes) != 1 {
		t.Fatalf("result changes = %d, want 1", len(env.Result.Changes))
	}
	c := env.Result.Changes[0]
	if c.OldID != "T-0043" || c.NewID != "T-0044" || c.File != "backlog.md" {
		t.Errorf("change = %+v", c)
	}
	if env.Result.WasNext != "T-0044" || env.Result.NextID != "T-0045" {
		t.Errorf("next: %s -> %s", env.Result.WasNext, env.Result.NextID)
	}
	if len(env.Changes) == 0 {
		t.Error("envelope changes empty")
	}
}

// A tie refuses with exit 4 (ErrConflict) and names both items.
func TestFixCLIRefusesATie(t *testing.T) {
	r, dir := mergedBoardCLI(t)
	backlog, err := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	merged := strings.Replace(string(backlog),
		"- [ ] [T-0043] Alpha | created:2026-07-31",
		"- [ ] [T-0043] Alpha | created:2026-08-01", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	// The detail file's title (Beta) no longer matches either tied item, which
	// is itself a blocker; drop the detail file so the tie is the only finding.
	if err := os.Remove(filepath.Join(dir, "details", "T-0043.md")); err != nil {
		t.Fatal(err)
	}
	backlog, err = os.ReadFile(filepath.Join(dir, "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	merged = strings.Replace(string(backlog),
		"- [ ] [T-0043] Beta | created:2026-08-01 | detail:details/T-0043.md",
		"- [ ] [T-0043] Beta | created:2026-08-01", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--fix")
	if got.Code != ExitPrecondition {
		t.Fatalf("exit %d, want %d:\n%s", got.Code, ExitPrecondition, got)
	}
	if !strings.Contains(got.Stderr, "Alpha") || !strings.Contains(got.Stderr, "Beta") {
		t.Errorf("refusal should name both items:\n%s", got.Stderr)
	}
}

// Conflict markers block the repair with the marker finding, exit 1.
func TestFixCLIRefusesOnMarkers(t *testing.T) {
	r, dir := mergedBoardCLI(t)
	backlog, err := os.ReadFile(filepath.Join(dir, "backlog.md"))
	if err != nil {
		t.Fatal(err)
	}
	merged := strings.Replace(string(backlog), "## Ready",
		"## Ready\n\n<<<<<<< HEAD\n=======\n>>>>>>> theirs", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--fix")
	if got.Code != ExitInvariantViolation {
		t.Fatalf("exit %d, want %d:\n%s", got.Code, ExitInvariantViolation, got)
	}
	if !strings.Contains(got.Stderr, "conflict-marker") {
		t.Errorf("stderr should name the marker:\n%s", got.Stderr)
	}
}
