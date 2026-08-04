package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"micromanager/mm"
)

// Refresh-cost measurement (T-0131), the gate between Phase 1 and Phase 2 of
// project/research-app-fllicker.md.
//
// architecture.md §4.5 open question 3 says "if board re-fetches turn out to
// dominate, a per-column event may be worth it. Measure before splitting."
// This file is that measurement, kept rather than thrown away so the Phase 2
// decision can be re-checked on a different corpus or a different machine:
//
//	go test ./internal/web/ -run TestRefreshFetchIsIdenticalWhenNothingChanged -v
//	go test ./internal/web/ -bench BenchmarkRefreshFragment -benchmem -run '^$'
//
// The client half of the measurement — how long idiomorph takes to diff a
// swap in a real browser — cannot be measured from Go and is recorded in
// research-app-fllicker.md §8 instead.

// bigDoneFixture writes a valid micro-manager directory with doneCount closed
// items and readyCount open ones, and returns its path.
//
// Generated rather than checked in: the point is to vary the done column
// across three orders of magnitude, and a corpus fixture per size would be
// three more directories for check.sh to validate forever.
func bigDoneFixture(t testing.TB, parent string, readyCount, doneCount int) string {
	t.Helper()

	dir := filepath.Join(parent, "micro-manager")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	id := func(n int) string { return fmt.Sprintf("T-%04d", n) }
	next := 1

	var ready strings.Builder
	for i := 0; i < readyCount; i++ {
		fmt.Fprintf(&ready, "- [ ] [%s] Ready item %d with a title of realistic length | prio:med | tags:gui,measure | created:2026-08-02\n",
			id(next), i+1)
		next++
	}

	backlog := fmt.Sprintf(`---
doc: backlog
version: 1
project: Refresh Cost %dx%d
next_id: %s
updated: 2026-08-02
---

# Backlog

## Ready

%s
## Blocked

## Someday
`, readyCount, doneCount, id(next+doneCount), ready.String())

	// done.md is newest-month-first, newest-item-first within a month; one
	// month heading keeps the generator honest about ordering without
	// inventing a calendar.
	var done strings.Builder
	done.WriteString("---\ndoc: done\nversion: 1\nupdated: 2026-08-02\n---\n\n# Done\n\n## 2026-08\n\n")
	for i := 0; i < doneCount; i++ {
		fmt.Fprintf(&done, "- [x] [%s] Done item %d with a title of realistic length | prio:med | tags:gui,measure | created:2026-08-01 | done:2026-08-02 | outcome:shipped\n",
			id(next), i+1)
		next++
	}

	working := `---
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

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("backlog.md", backlog)
	write("done.md", done.String())
	write("working.01.md", working)

	// A fixture the validator rejects would make every number below a
	// measurement of the error path.
	store, err := mm.Open(dir)
	if err != nil {
		t.Fatalf("open generated fixture: %v", err)
	}
	violations, err := store.Validate()
	if err != nil {
		t.Fatalf("validate generated fixture: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("generated fixture is invalid: %v", violations[0])
	}
	return dir
}

// serverOver builds a service over one generated directory.
func serverOver(t testing.TB, dir string, mutate func(*Options)) *testServer {
	t.Helper()

	opts := Options{
		ConfigHome: t.TempDir(),
		StartDir:   filepath.Dir(dir),
		Config:     mm.DefaultConfig(),
		Logger:     newTestLogger(t),
	}
	if mutate != nil {
		mutate(&opts)
	}
	opts.Dirs = []string{dir}
	opts.Config.Scan.Roots = []string{opts.StartDir}

	srv, err := New(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return &testServer{Server: srv, t: t, ConfigHome: opts.ConfigHome, Root: opts.StartDir, Dirs: []string{dir}}
}

// TestRefreshFetchIsIdenticalWhenNothingChanged — measurement 2 of T-0131:
// "count how many responses are byte-identical to the current DOM".
//
// On an unchanged directory the answer is not a sample, it is a proof: the
// board fragment is a pure function of the files, so EVERY backstop fetch
// against a quiescent project returns the same bytes the tab already has.
// That is the number the Phase 2 decision turns on — a fetch that cannot
// differ is a fetch worth not making, and it is also a fetch that morph
// renders free.
func TestRefreshFetchIsIdenticalWhenNothingChanged(t *testing.T) {
	dir := bigDoneFixture(t, t.TempDir(), 12, 40)
	ts := serverOver(t, dir, nil)
	id := projectIDOf(t, ts, dir)

	digest := func(path string) string {
		sum := sha256.Sum256([]byte(ts.get(path).expectStatus(200).Body))
		return fmt.Sprintf("%x", sum)
	}

	for _, region := range []struct{ name, path string }{
		{"board", "/p/" + id + "/board?fragment=1"},
		{"status", "/p/" + id + "/status"},
		{"check", "/p/" + id + "/check?fragment=1"},
	} {
		first := digest(region.path)
		identical := 0
		const rounds = 20
		for i := 0; i < rounds; i++ {
			if digest(region.path) == first {
				identical++
			}
		}
		if identical != rounds {
			t.Errorf("%s: %d/%d refetches were byte-identical; a quiescent region must be 100%%",
				region.name, identical, rounds)
		}
		t.Logf("%s: %d/%d refetches byte-identical on a quiescent directory", region.name, identical, rounds)
	}

	// The counterpart: a real write MUST change the bytes, or the backstop
	// would be measuring nothing at all and morph would hide a stale board.
	before := digest("/p/" + id + "/board?fragment=1")
	ts.post("/p/"+id+"/items/T-0001/note", "text=cost probe",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").expectStatus(200)
	if after := digest("/p/" + id + "/board?fragment=1"); after == before {
		t.Error("a note did not change the board fragment: the refresh path cannot see writes")
	}
}

// TestRefreshFetchSizeByDoneColumn — measurement 1's payload half: what the
// backstop actually costs on the wire, and what drives that cost.
//
// The headline finding is the CAP. DoneLimit (mm/config.go, default 20) bounds
// the done column, so the board fragment does not grow as done.md does — the
// "board re-fetches dominate as the project ages" worry behind Phase 2 is
// already answered by a config default. Asserted rather than commented,
// because a change to that default silently changes the Phase 2 calculus.
//
// Sizes are reported raw AND gzipped: gzip is on for every route but the
// event stream (server.go), so the gzipped number is the real wire cost and
// the raw number is what idiomorph has to parse and diff.
func TestRefreshFetchSizeByDoneColumn(t *testing.T) {
	gz := func(s string) int {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		if _, err := w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
		w.Close()
		return buf.Len()
	}

	limit := mm.DefaultConfig().UI.Board.DoneLimit

	t.Run("done column is capped", func(t *testing.T) {
		var sizes []int
		for _, done := range []int{20, 200, 1000} {
			dir := bigDoneFixture(t, t.TempDir(), 12, done)
			ts := serverOver(t, dir, nil)
			id := projectIDOf(t, ts, dir)

			board := ts.get("/p/" + id + "/board?fragment=1").expectStatus(200).Body
			status := ts.get("/p/" + id + "/status").expectStatus(200).Body
			cards := strings.Count(board, `class="mm-item"`)
			sizes = append(sizes, len(board))

			t.Logf("done.md=%4d -> board %6d B raw / %5d B gzip, %2d cards, status %4d B raw / %3d B gzip",
				done, len(board), gz(board), cards, len(status), gz(status))

			if want := 12 + limit; done >= limit && cards != want {
				t.Errorf("done=%d rendered %d cards, want %d (12 ready + DoneLimit %d)",
					done, cards, want, limit)
			}
		}
		for _, s := range sizes[1:] {
			if s != sizes[0] {
				t.Errorf("board fragment size varies with done.md (%v): DoneLimit is not capping it", sizes)
				break
			}
		}
	})

	// Uncapped, the picture reverses: this is what a user who sets
	// ui.board.doneLimit=0 to "see everything" is actually asking the
	// backstop to re-fetch every 30 seconds.
	t.Run("uncapped", func(t *testing.T) {
		for _, done := range []int{20, 200, 1000} {
			dir := bigDoneFixture(t, t.TempDir(), 12, done)
			ts := serverOver(t, dir, func(o *Options) { o.Config.UI.Board.DoneLimit = 0 })
			id := projectIDOf(t, ts, dir)

			board := ts.get("/p/" + id + "/board?fragment=1").expectStatus(200).Body
			t.Logf("doneLimit=0, done.md=%4d -> board %7d B raw / %6d B gzip, %4d cards",
				done, len(board), gz(board), strings.Count(board, `class="mm-item"`))
		}
	})
}

// BenchmarkRefreshFragment — measurement 1's server half: the cost of
// answering one refresh, which is what 120 backstop fetches/hour/region buy.
//
//	go test ./internal/web/ -bench BenchmarkRefreshFragment -benchmem -run '^$'
func BenchmarkRefreshFragment(b *testing.B) {
	for _, done := range []int{20, 200, 1000} {
		dir := bigDoneFixture(b, b.TempDir(), 12, done)
		ts := serverOver(b, dir, nil)
		id := projectIDOfB(b, ts, dir)

		for _, region := range []struct{ name, path string }{
			{"board", "/p/" + id + "/board?fragment=1"},
			{"status", "/p/" + id + "/status"},
		} {
			b.Run(fmt.Sprintf("%s/done=%d", region.name, done), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if r := ts.get(region.path); r.Status != 200 {
						b.Fatalf("status %d", r.Status)
					}
				}
			})
		}
	}
}

// projectIDOfB is projectIDOf for a benchmark: the same resolution, without
// the *testing.T the helper takes.
func projectIDOfB(b *testing.B, ts *testServer, dir string) string {
	b.Helper()
	canonical, err := mm.CanonicalPath(dir)
	if err != nil {
		b.Fatalf("canonical path: %v", err)
	}
	id, err := mm.ProjectID(canonical)
	if err != nil {
		b.Fatalf("project id: %v", err)
	}
	return id
}

// BenchmarkBrokerFingerprintPoll — the other half of the idle cost: the
// broker polls the fingerprint every 5s per OPEN PROJECT whether or not
// anything changed (broker.go). If that poll were expensive, Phase 2's
// per-file stamps (T-0132) would pay for themselves on the server side alone.
func BenchmarkBrokerFingerprintPoll(b *testing.B) {
	for _, done := range []int{20, 1000} {
		dir := bigDoneFixture(b, b.TempDir(), 12, done)
		store, err := mm.Open(dir)
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		b.Run(fmt.Sprintf("done=%d", done), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := store.Fingerprint(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestIdleFetchBudget records the arithmetic the T-0131 go/no-go rests on, so
// the numbers in research-app-fllicker.md §8 have a source that fails loudly
// if the trigger wiring changes underneath them.
//
// T-0139 (D2) changed that wiring deliberately: the unconditional `every 30s`
// clause is gone, so a healthy stream costs ZERO idle fetches. The backstop is
// now mm.js's SSE-down interval, which exists only while the stream is down
// and stops again on reconnect.
func TestIdleFetchBudget(t *testing.T) {
	dir := bigDoneFixture(t, t.TempDir(), 12, 40)
	ts := serverOver(t, dir, nil)
	id := projectIDOf(t, ts, dir)

	board := ts.get("/p/" + id + "/board").expectStatus(200).Body

	// D2: no region carries an unconditional htmx `every` clause. Idle traffic
	// on a healthy stream is zero; the budget line for the stream-down case is
	// the mm.js interval, and the regions it polls are exactly these triggers.
	for _, trigger := range []string{
		`hx-trigger="sse:board from:body"`,
		`hx-trigger="sse:status from:body"`,
	} {
		if !strings.Contains(board, trigger) {
			t.Errorf("board page no longer carries %s: the idle budget below is stale", trigger)
		}
	}
	if strings.Contains(board, "every 30s") || strings.Contains(board, "every 5s") {
		t.Error("a region still carries an htmx `every` clause; D2 moved the backstop to mm.js")
	}

	check := ts.get("/p/" + id + "/check").expectStatus(200).Body
	if !strings.Contains(check, `hx-trigger="sse:check from:body"`) {
		t.Error("check view no longer carries its sse:check trigger: the idle budget below is stale")
	}

	// The numbers research-app-fllicker.md §8 records were the OLD model: 120
	// fetches/hour per region, forever. The D2 model: 0 fetches/hour per tab
	// while the stream is open, and 3 regions x 720/hour = 2160/hour ONLY
	// while it is down (and only until the extension reconnects).
	const downInterval = 5 * time.Second
	perHour := int(time.Hour / downInterval)
	t.Logf("healthy stream: 0 fetches/hour per tab (D2)")
	t.Logf("stream down: 3 regions x %d fetches/hour = %d fetches/hour until reconnect", perHour, 3*perHour)
	t.Logf("broker fingerprint poll: %d/hour per OPEN PROJECT regardless of tabs", int(time.Hour/(5*time.Second)))
}
