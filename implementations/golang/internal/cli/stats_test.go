package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0045 — --stats end to end. The library owns the arithmetic (mm's own
// op_stats_test.go checks every number by hand); what is tested here is the
// wrapper: the switches, the period default, the three output modes, and the
// labels that keep the numbers from being read as something they are not.

const statsDoneCLI = `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [T-0004] Obsolete one | tags:ci | created:2026-07-05 | started:2026-07-06 | done:2026-07-13 | outcome:obsolete
- [x] [T-0003] Never started | created:2026-07-09 | done:2026-07-10 | outcome:shipped
- [x] [T-0002] Same day | tags:infra | created:2026-07-02 | started:2026-07-02 | done:2026-07-02 | outcome:cancelled
- [x] [T-0001] Two days | tags:infra,ci | created:2026-06-30 | started:2026-07-01 | done:2026-07-03 | outcome:shipped
`

func statsProject(t *testing.T) (runner, string) {
	t.Helper()
	r, dir := newProject(t)
	if err := os.WriteFile(filepath.Join(dir, "done.md"), []byte(statsDoneCLI), 0o644); err != nil {
		t.Fatal(err)
	}
	backlog := readFile(t, filepath.Join(dir, "backlog.md"))
	backlog = strings.Replace(backlog, "next_id: T-0001", "next_id: T-0010", 1)
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := r.run("--check"); got.Code != ExitOK {
		t.Fatalf("fixture should start clean: %s", got)
	}
	return r, dir
}

func TestStatsCLIReportsTheFourMeasures(t *testing.T) {
	r, _ := statsProject(t)

	got := r.run("--stats", "--bucket", "month")
	if got.Code != ExitOK {
		t.Fatalf("stats: %s", got)
	}
	for _, want := range []string{
		"closed 4",
		"shipped 2, cancelled 1, obsolete 1",
		"cycle time, started to done: mean 3.0d, median 2.0d",
		"1 closed item(s) had no usable started date",
		"in flight: peak 2 on 2026-07-02",
		"2026-07",
		"tags: ci 2, infra 2, untagged 1",
	} {
		if !strings.Contains(got.Stdout, want) {
			t.Errorf("stdout is missing %q:\n%s", want, got.Stdout)
		}
	}
}

// The numbers are labelled for what they measure. "in flight" rather than
// "WIP", because the format cannot see slots — an item counts from started to
// done whatever happened in between (§10.1, §10.7).
func TestStatsCLICallsItInFlightNotWip(t *testing.T) {
	r, _ := statsProject(t)

	got := r.run("--stats")
	if got.Code != ExitOK {
		t.Fatalf("stats: %s", got)
	}
	if !strings.Contains(got.Stdout, "in flight") {
		t.Errorf("the WIP series must say what it measures:\n%s", got.Stdout)
	}
	if strings.Contains(got.Stdout, "WIP") {
		t.Errorf("calling it WIP claims something the format cannot check:\n%s", got.Stdout)
	}
}

// The period defaults to all of history, and MM_REPORT_PERIOD — documented as
// the default --report period — must not narrow it.
func TestStatsCLIPeriodDefaultsToAll(t *testing.T) {
	r, _ := statsProject(t)

	got := r.run("--stats")
	if got.Code != ExitOK {
		t.Fatalf("stats: %s", got)
	}
	if !strings.Contains(got.Stdout, "— all,") || !strings.Contains(got.Stdout, "closed 4") {
		t.Errorf("the default period should be all of history:\n%s", got.Stdout)
	}

	withEnv := runner{cwd: r.cwd, period: "2026-08"}
	env := withEnv.run("--stats")
	if env.Code != ExitOK {
		t.Fatalf("stats: %s", env)
	}
	if !strings.Contains(env.Stdout, "closed 4") {
		t.Errorf("MM_REPORT_PERIOD must not narrow --stats:\n%s", env.Stdout)
	}

	// An explicit switch does narrow it.
	narrow := r.run("--stats", "--since", "2026-07-11", "--until", "2026-07-31")
	if narrow.Code != ExitOK {
		t.Fatalf("stats: %s", narrow)
	}
	if !strings.Contains(narrow.Stdout, "closed 1") {
		t.Errorf("--since/--until should narrow it:\n%s", narrow.Stdout)
	}
}

func TestStatsCLIJSONAndPorcelain(t *testing.T) {
	r, _ := statsProject(t)

	porc := r.run("--stats", "--porcelain", "--bucket", "week")
	if porc.Code != ExitOK {
		t.Fatalf("porcelain: %s", porc)
	}
	// bucket since until closed wipPeak wipMean — one record per bucket.
	if !strings.Contains(porc.Stdout, "2026-W27\t2026-06-29\t2026-07-05\t2\t2\t") {
		t.Errorf("porcelain:\n%s", porc.Stdout)
	}

	got := r.run("--stats", "--json", "--bucket", "month")
	if got.Code != ExitOK {
		t.Fatalf("json: %s", got)
	}
	var env struct {
		OK        bool
		Operation string
		Result    struct {
			Period    string
			Bucket    string
			Closed    int
			ByOutcome []struct {
				Name  string
				Count int
			}
			Buckets []struct {
				Label   string
				Closed  int
				WipPeak int `json:"wipPeak"`
			}
			CycleTime struct {
				N       int
				Unknown int
				Mean    float64
				Median  float64
				Max     int
			} `json:"cycleTime"`
			Wip struct {
				Peak   int
				PeakOn string
				Mean   float64
			}
			Tags []struct {
				Name  string
				Count int
			}
			Untagged int
		}
	}
	if err := json.Unmarshal([]byte(got.Stdout), &env); err != nil {
		t.Fatalf("json: %v\n%s", err, got.Stdout)
	}
	if !env.OK || env.Operation != "stats" {
		t.Errorf("envelope: ok=%v operation=%q", env.OK, env.Operation)
	}
	if env.Result.Closed != 4 || env.Result.Bucket != "month" {
		t.Errorf("result = %+v", env.Result)
	}
	if env.Result.CycleTime.N != 3 || env.Result.CycleTime.Unknown != 1 ||
		env.Result.CycleTime.Mean != 3 || env.Result.CycleTime.Max != 7 {
		t.Errorf("cycleTime = %+v", env.Result.CycleTime)
	}
	if env.Result.Wip.Peak != 2 || env.Result.Wip.PeakOn != "2026-07-02" {
		t.Errorf("wip = %+v", env.Result.Wip)
	}
	// The full tag list is here even though the human output truncates it.
	if len(env.Result.Tags) != 2 || env.Result.Untagged != 1 {
		t.Errorf("tags = %+v untagged=%d", env.Result.Tags, env.Result.Untagged)
	}
}

func TestStatsCLIRejectsABadBucket(t *testing.T) {
	r, _ := statsProject(t)

	if got := r.run("--stats", "--bucket", "fortnight"); got.Code != ExitUsage {
		t.Errorf("--bucket fortnight = exit %d, want %d:\n%s", got.Code, ExitUsage, got)
	}
	if got := r.run("--stats", "--since", "not-a-date"); got.Code != ExitUsage {
		t.Errorf("--since not-a-date = exit %d, want %d", got.Code, ExitUsage)
	}
}

// An archived period reads as one in which nothing closed, so it says so.
func TestStatsCLIWarnsAboutArchives(t *testing.T) {
	r, dir := statsProject(t)
	archive := "---\ndoc: done\nversion: 1\n---\n\n# Done 2025\n\n## 2025-12\n\n" +
		"- [x] [T-0009] Ancient | tags:infra | created:2025-12-01 | started:2025-12-01" +
		" | done:2025-12-24 | outcome:shipped\n"
	if err := os.WriteFile(filepath.Join(dir, "done-2025.md"), []byte(archive), 0o644); err != nil {
		t.Fatal(err)
	}

	got := r.run("--stats", "--since", "2025-12-01", "--bucket", "month")
	if got.Code != ExitOK {
		t.Fatalf("stats: %s", got)
	}
	if !strings.Contains(got.Stderr, "archived") {
		t.Errorf("a period reaching past done.md must warn:\n%s", got.Stderr)
	}
	if !strings.Contains(got.Stdout, "closed 4") {
		t.Errorf("the archive must not be read unasked:\n%s", got.Stdout)
	}

	with := r.run("--stats", "--since", "2025-12-01", "--bucket", "month", "--include-archives")
	if with.Code != ExitOK {
		t.Fatalf("stats: %s", with)
	}
	if !strings.Contains(with.Stdout, "closed 5") {
		t.Errorf("--include-archives should find the archived item:\n%s", with.Stdout)
	}
	if strings.Contains(with.Stderr, "archived") {
		t.Errorf("nothing was hidden, so nothing to warn about:\n%s", with.Stderr)
	}
}
