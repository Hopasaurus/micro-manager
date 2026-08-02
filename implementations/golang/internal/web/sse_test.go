package web

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"micromanager/mm"
)

// SSE tests (project/architecture.md §7).
//
// These need a real HTTP server: the harness's recorder cannot stream, and the
// point of the test is that a stream stays open and delivers events. The poll
// interval is shortened so a mutation is observed within the test's lifetime;
// the mechanism is identical at the configured 5s.

// sseCfg is the configuration the SSE tests run on: fast polling, everything
// else default.
func sseCfg() mm.Config {
	cfg := mm.DefaultConfig()
	cfg.UI.PollIntervalMs = 50
	return cfg
}

// connectSSE opens the event stream for a project and returns the response,
// which stays open until the test closes it.
func connectSSE(t *testing.T, ts *testServer, projectID string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(ts.Handler())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/api/v1/events?project=" + projectID)
	if err != nil {
		t.Fatalf("connect to events stream: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	if res.StatusCode != 200 {
		t.Fatalf("events stream status = %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("events Content-Type = %q, want text/event-stream", ct)
	}
	return res
}

// nextEvents reads event names from an open stream until all of want have been
// seen or the deadline passes.
func nextEvents(t *testing.T, res *http.Response, want ...string) []string {
	t.Helper()
	rd := bufio.NewReader(res.Body)
	deadline := time.After(10 * time.Second)

	var got []string
	wantSet := map[string]bool{}
	for _, w := range want {
		wantSet[w] = true
	}
	seen := map[string]bool{}

	for len(seen) < len(wantSet) {
		// Read the stream in a goroutine so the deadline can break it: a stream
		// that delivers nothing would otherwise block the test forever.
		type lineResult struct {
			line string
			err  error
		}
		ch := make(chan lineResult, 1)
		go func() {
			line, err := rd.ReadString('\n')
			ch <- lineResult{line, err}
		}()

		select {
		case <-deadline:
			t.Fatalf("timed out waiting for events %v; got %v so far", want, got)
		case lr := <-ch:
			if lr.err != nil && lr.line == "" {
				t.Fatalf("stream closed while waiting for %v (got %v): %v", want, got, lr.err)
			}
			line := strings.TrimSpace(lr.line)
			if strings.HasPrefix(line, "event:") {
				name := strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				got = append(got, name)
				seen[name] = true
			}
		}
	}
	return got
}

func TestSSEEventsOnMutation(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	res := connectSSE(t, ts, id)

	// The stream is open but silent until something changes.
	time.Sleep(120 * time.Millisecond)

	// A mutation through the API changes the fingerprint; the poller publishes
	// the data events.
	ts.post("/api/v1/projects/"+id+"/items",
		`{"title":"SSE probe"}`, apiHeaders()...).expectStatus(201)

	events := nextEvents(t, res, "board", "status", "check")
	for _, name := range events {
		switch name {
		case "board", "status", "check":
		default:
			t.Fatalf("unexpected event name %q", name)
		}
	}
}

func TestSSEThemeEvent(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	res := connectSSE(t, ts, id)
	time.Sleep(120 * time.Millisecond)

	// Writing theme.json must reach the stream even though the data fingerprint
	// deliberately excludes it (mm/fingerprint.go).
	path := filepath.Join(ts.Dirs[0], "theme.json")
	if err := os.WriteFile(path, []byte(`{"name":"SSE Theme","color":{"bg.base":"#ffffff"}}`), 0o644); err != nil {
		t.Fatalf("write theme.json: %v", err)
	}

	events := nextEvents(t, res, "theme")
	if events[0] != "theme" {
		t.Fatalf("first event = %q, want theme", events[0])
	}
}

// Deleting theme.json drops the project back to the system theme, which is as
// much a re-theme as writing one. The poller reported the file's mtime as a
// bare time.Time, so "gone" and "could not stat" were the same zero value and
// both were ignored: an open tab kept rendering a theme whose file no longer
// existed (review 006 F5).
func TestSSEThemeEventOnDelete(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// The file has to exist BEFORE the stream opens, so the poller's baseline
	// records it and the deletion is a change away from a known stamp.
	path := filepath.Join(ts.Dirs[0], "theme.json")
	if err := os.WriteFile(path, []byte(`{"name":"Doomed","color":{"bg.base":"#ffffff"}}`), 0o644); err != nil {
		t.Fatalf("write theme.json: %v", err)
	}

	res := connectSSE(t, ts, id)
	time.Sleep(120 * time.Millisecond)

	if err := os.Remove(path); err != nil {
		t.Fatalf("remove theme.json: %v", err)
	}

	events := nextEvents(t, res, "theme")
	if events[0] != "theme" {
		t.Fatalf("first event = %q, want theme", events[0])
	}
}

func TestSSEStreamStaysOpenAndUnsubscribes(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	res := connectSSE(t, ts, id)
	time.Sleep(120 * time.Millisecond)

	// Close the client side; the server's request context cancels and the
	// subscriber is removed. There is no direct assertion for the goroutine
	// going away, so the cheap proxy is that the test finishes — a leaked
	// poller would keep the test process alive, and -race would complain.
	res.Body.Close()
}

// Shutdown must not race a stream that is still arriving.
//
// Subscribe calls wg.Add under the broker lock; close called wg.Wait without
// it. A subscription landing after the last poller exited - counter at zero,
// Wait already blocked - panics the runtime with "WaitGroup misuse: Add called
// concurrently with Wait", killing the process as it shuts down. Narrow, and
// the sort of thing that only ever happens in front of a user (T-0099).
//
// This is a stress test rather than a proof: it hammers the window under -race
// so a regression has a real chance of being caught, and asserts the contract
// that makes the window impossible.
func TestBrokerCloseDoesNotRaceSubscribe(t *testing.T) {
	ts := newTestServerCfg(t, sseCfg(), "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])
	b := ts.Server.broker

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				// Subscribing straight through the close is the point: some of
				// these land before it and some after.
				_, unsubscribe := b.Subscribe(id)
				unsubscribe()
			}
		}()
	}

	time.Sleep(30 * time.Millisecond)
	b.close() // must not panic
	b.close() // and must stay safe to repeat
	close(stop)
	wg.Wait()

	// After close a subscription is refused with a channel that is already
	// closed, so an SSE handler holding one exits rather than blocking forever.
	ch, unsubscribe := b.Subscribe(id)
	defer unsubscribe()
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("a post-close subscription delivered an event")
		}
	case <-time.After(2 * time.Second):
		t.Error("a post-close subscription blocked instead of returning a closed channel")
	}
}

func TestSSEGzipSkipped(t *testing.T) {
	ts := newTestServer(t, "clean-full")

	// The event route must NEVER be gzipped: compression buffers, and buffering
	// is the one thing a stream cannot tolerate.
	srv := httptest.NewServer(ts.Handler())
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/events?project=000000000000", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	resp.Body.Close()
	if ce := resp.Header.Get("Content-Encoding"); ce != "" {
		t.Fatalf("events stream Content-Encoding = %q, want empty (gzip must skip this route)", ce)
	}

	// A normal route IS gzipped under the same header, so the skipper is doing
	// the work rather than gzip being absent.
	req, _ = http.NewRequest("GET", srv.URL+"/api/v1/health", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()
	if ce := resp.Header.Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("health Content-Encoding = %q, want gzip", ce)
	}
}

func TestSSEStreamAttributesInShell(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// The app shell wires the stream: sse-connect on the app root, the
	// 30s polling backstop on the regions (§2.3, architecture.md §4.5).
	body := ts.get("/p/" + id + "/board").expectStatus(200).Body
	if !strings.Contains(body, `sse-connect="/api/v1/events?project=`+id+`"`) {
		t.Fatal("app root has no sse-connect")
	}
	if !strings.Contains(body, `hx-trigger="sse:board from:body, every 30s"`) {
		t.Fatal("board has no sse:board trigger with the polling backstop")
	}
	if !strings.Contains(body, `hx-trigger="sse:status from:body, every 30s"`) {
		t.Fatal("status bar has no sse:status trigger with the polling backstop")
	}
	if !strings.Contains(body, `hx-trigger="sse:theme from:body"`) {
		t.Fatal("app root has no sse:theme trigger")
	}

	// The status route serves the same footer the page renders.
	status := ts.get("/p/" + id + "/status").expectStatus(200).Body
	if !strings.Contains(status, `data-testid="app-status"`) {
		t.Fatal("status route did not render the app-status footer")
	}

	// The shell route carries the theme style so a theme event re-themes.
	shell := ts.get("/p/" + id + "/shell").expectStatus(200).Body
	if !strings.Contains(shell, "<style>") || !strings.Contains(shell, `data-testid="app"`) {
		t.Fatal("shell route did not render the app element with its theme style")
	}

	// No hx-target on the APP ROOT itself (T-0084): hx-target is inherited by
	// every descendant trigger, so one here would redirect the board/status
	// refreshes to the shell and abort with htmx:targetError when it matched
	// nothing. Each refresh trigger relies on the default self-target instead.
	// (Item cards inside the shell carry their own hx-targets, which resolve.)
	appTag := body[strings.Index(body, `<div data-testid="app"`):]
	appTag = appTag[:strings.Index(appTag, ">")+1]
	if strings.Contains(appTag, "hx-target") {
		t.Fatalf("app root carries hx-target=%q: descendant SSE refreshes would inherit it and abort",
			regexp.MustCompile(`hx-target="[^"]*"`).FindString(appTag))
	}

	// The shell's own theme swap is outerHTML, so it MUST be fenced off with
	// hx-disinherit="*": hx-swap is inherited like hx-target, and an app
	// root leaking outerHTML turns every descendant swap into a container-
	// REPLACING swap (the confirm dialog destroyed dialog-root this way).
	if !strings.Contains(appTag, `hx-disinherit="*"`) {
		t.Fatal("app root hx-swap=outerHTML is not fenced with hx-disinherit=*: descendant swaps would inherit outerHTML")
	}

	// No project open: no stream at all.
	home := ts.get("/").expectStatus(200).Body
	if strings.Contains(home, "sse-connect") {
		t.Fatal("home without a project should not open an event stream")
	}
}

// TestRefreshRegionsSwapWithMorph — T-0129. The 30s backstop, the SSE echo of
// a tab's own mutation, and the mutation swap itself all tear down and
// rebuild the region under outerHTML; morph diffs instead, so all three
// become invisible. Every region that self-refreshes on sse:board/status/check
// must swap with morph, and so must every element that swaps the board as a
// mutation response (the second, identical swap around a write is the one
// morph makes a no-op).
func TestRefreshRegionsSwapWithMorph(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// The opening tag of the first element with this exact data-testid.
	openTag := func(t *testing.T, body, testid string) string {
		t.Helper()
		idx := strings.Index(body, `data-testid="`+testid+`"`)
		if idx < 0 {
			t.Fatalf("no data-testid=%q in body", testid)
		}
		end := strings.Index(body[idx:], ">")
		if end < 0 {
			t.Fatalf("unterminated tag for data-testid=%q", testid)
		}
		return body[idx : idx+end+1]
	}

	board := ts.get("/p/" + id + "/board").expectStatus(200).Body

	boardTag := openTag(t, board, "board")
	if !strings.Contains(boardTag, `hx-trigger="sse:board from:body, every 30s"`) ||
		!strings.Contains(boardTag, `hx-swap="morph"`) {
		t.Errorf("board region does not morph on its sse:board/backstop refresh:\n%s", boardTag)
	}

	statusTag := openTag(t, board, "app-status")
	if !strings.Contains(statusTag, `hx-trigger="sse:status from:body, every 30s"`) ||
		!strings.Contains(statusTag, `hx-swap="morph"`) {
		t.Errorf("status footer does not morph on its sse:status/backstop refresh:\n%s", statusTag)
	}

	check := ts.get("/p/" + id + "/check").expectStatus(200).Body
	checkTag := openTag(t, check, "check")
	if !strings.Contains(checkTag, `hx-trigger="sse:check from:body, every 30s"`) ||
		!strings.Contains(checkTag, `hx-swap="morph"`) {
		t.Errorf("check region does not morph on its sse:check/backstop refresh:\n%s", checkTag)
	}

	// board.html's app-status-oob is a SEPARATE template from layout.html's
	// own self-refresh above, rendered only on a mutation response — a note
	// is the least destructive mutation to trigger it with.
	mutated := ts.post("/p/"+id+"/items/T-0001/note", "text=morph+check",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").expectStatus(200).Body
	oobStatusTag := openTag(t, mutated, "app-status")
	if !strings.Contains(oobStatusTag, `hx-swap="morph"`) {
		t.Errorf("app-status-oob does not morph on its own refresh trigger:\n%s", oobStatusTag)
	}

	// The mutation response IS the board, re-rendered with the same "board"
	// template checked above, so its own trigger already proves the point.
	// What is left to check is the ELEMENT THAT ISSUED the mutation: the
	// item-panel's note form targets the board with its own hx-swap, and it
	// must say morph too, so the identical SSE re-fetch that follows the
	// write is a no-op diff rather than a teardown.
	panel := ts.get("/p/" + id + "/item/T-0001").expectStatus(200).Body
	noteForm := panel[strings.Index(panel, `class="mm-note-form"`):]
	noteForm = noteForm[:strings.Index(noteForm, "</form>")]
	if !strings.Contains(noteForm, `hx-target="[data-testid='board']" hx-swap="morph"`) {
		t.Errorf("item-panel's note form does not morph its board target:\n%s", noteForm)
	}
}

// TestMorphSwapsDeclareOwnExtension — T-0129. In the vendored htmx build, a
// native form submit (and an htmx.ajax() call with no explicit source) does
// not reliably resolve the "morph" extension from an ancestor's hx-ext, even
// a near one: the request silently falls back to htmx's default swap style
// (innerHTML) and nests a full copy of the response INSIDE the existing
// target instead of replacing it — verified empirically against this exact
// vendored htmx.min.js, and reproducible with hx-ext declared only on the
// app root despite the general rule (layout.html's app-root comment) that
// hx-ext ordinarily walks ancestors directly. The one combination proven
// reliable is declaring hx-ext="morph" on the SAME element as hx-swap="morph".
// This test enforces that pairing everywhere in the rendered HTML so a future
// morph swap added without it fails loudly here instead of nesting silently
// in a browser no test here can drive.
func TestMorphSwapsDeclareOwnExtension(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	pages := map[string]string{
		"board":                 ts.get("/p/" + id + "/board").expectStatus(200).Body,
		"item detail":           ts.get("/p/" + id + "/item/T-0001").expectStatus(200).Body,
		"new item":              ts.get("/p/" + id + "/new").expectStatus(200).Body,
		"check":                 ts.get("/p/" + id + "/check").expectStatus(200).Body,
		"dialog confirm-remove": ts.get("/p/" + id + "/dialog/confirm-remove?item=T-0001").expectStatus(200).Body,
		"dialog block":          ts.get("/p/" + id + "/dialog/block?item=T-0001").expectStatus(200).Body,
		"dialog finish":         ts.get("/p/" + id + "/dialog/finish?item=T-0001").expectStatus(200).Body,
		"note mutation response": ts.post("/p/"+id+"/items/T-0001/note", "text=ext+audit",
			"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").expectStatus(200).Body,
	}

	tagRe := regexp.MustCompile(`<[a-zA-Z][^>]*>`)
	extRe := regexp.MustCompile(`hx-ext="([^"]*)"`)
	for name, body := range pages {
		for _, tag := range tagRe.FindAllString(body, -1) {
			if !strings.Contains(tag, `hx-swap="morph"`) {
				continue
			}
			m := extRe.FindStringSubmatch(tag)
			if m == nil {
				t.Errorf("%s: element declares hx-swap=\"morph\" with no hx-ext of its own:\n%s", name, tag)
				continue
			}
			ok := false
			for _, ext := range strings.Split(m[1], ",") {
				if strings.TrimSpace(ext) == "morph" {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s: element's hx-ext=%q does not include morph:\n%s", name, m[1], tag)
			}
		}
	}
}
