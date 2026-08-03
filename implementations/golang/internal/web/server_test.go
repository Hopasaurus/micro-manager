package web

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
)

func TestHealth(t *testing.T) {
	ts := newTestServer(t)

	var body healthResponse
	ts.get("/api/v1/health").expectStatus(http.StatusOK).json(&body)

	if !body.OK || body.Service != "micro-manager" || body.Version == "" {
		t.Errorf("health = %+v", body)
	}
}

// The versions /about reports must be the ones the specs declare, or the view
// tells the user something that is not true (spec-gui.md §4.1).
func TestSpecVersionsMatchTheSpecs(t *testing.T) {
	cases := map[string]string{
		"../../../../project/spec-gui.md":         SpecUIVersion,
		"../../../../project/spec-tools.md":       SpecToolsVersion,
		"../../../../project/spec-file-format.md": SpecFormatVersion,
	}
	for path, want := range cases {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("the specs are not readable from here: %v", err)
		}
		// Each spec opens with an indented "Spec version: N" block.
		var got string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "Spec version:") {
				got = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
				break
			}
		}
		if got == "" {
			t.Errorf("%s declares no spec version", path)
			continue
		}
		if got != want {
			t.Errorf("%s is version %s, the service reports %s", filepath.Base(path), got, want)
		}
	}
}

// Assets are embedded, so the binary is a single file and the service works with
// no network at all (project/architecture.md §8).
func TestStaticAssetsAreEmbedded(t *testing.T) {
	ts := newTestServer(t)

	for _, name := range []string{"mm.css", "mm.js"} {
		r := ts.get("/static/" + name)
		if r.Status != http.StatusOK {
			t.Errorf("/static/%s returned %d", name, r.Status)
		}
		if r.Body == "" {
			t.Errorf("/static/%s is empty", name)
		}
	}
	if got := ts.get("/static/nothing-here.js").Status; got != http.StatusNotFound {
		t.Errorf("a missing asset returned %d, want 404", got)
	}
}

// A route that does not exist is a 404, not a panic and not a redirect.
func TestUnknownRoute(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/api/v1/nothing").expectStatus(http.StatusNotFound)
}

// Recover middleware is installed: a handler that panics must not take the
// process down, because the service is long-lived and the next request should
// still work.
func TestPanicBecomesAnError(t *testing.T) {
	ts := newTestServer(t)
	ts.echo.GET("/x-panic", func(*echo.Context) error { panic("boom") })

	if got := ts.get("/x-panic").Status; got != http.StatusInternalServerError {
		t.Errorf("a panicking handler returned %d, want 500", got)
	}
	// The service is still serving.
	ts.get("/api/v1/health").expectStatus(http.StatusOK)
}

// The harness must serve fixture directories, which is what every later item's
// tests are built on.
func TestHarnessServesFixtures(t *testing.T) {
	ts := newTestServer(t, "clean-full", "clean-minimal")

	if len(ts.Dirs) != 2 {
		t.Fatalf("want 2 fixture directories, got %d", len(ts.Dirs))
	}
	for _, dir := range ts.Dirs {
		if _, err := os.Stat(filepath.Join(dir, "backlog.md")); err != nil {
			t.Errorf("fixture %s is not a micro-manager directory: %v", dir, err)
		}
		// A copy, not the corpus: these tests write.
		if strings.Contains(dir, "testdata") {
			t.Errorf("fixture %s points into the read-only corpus", dir)
		}
	}
}

// Start binds, serves and shuts down on context cancellation. This is the one
// test that opens a real socket, because binding is what it is testing.
func TestStartAndShutdown(t *testing.T) {
	srv, err := New(Options{Bind: "127.0.0.1", Port: 0, Logger: newTestLogger(t)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	addr := waitForAddr(t, srv)
	res, err := http.Get("http://" + addr.String() + "/api/v1/health")
	if err != nil {
		t.Fatalf("the service did not answer: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("health returned %d", res.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shutdown returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the service did not shut down")
	}
}

// A stream must end when the service stops, not when echo's graceful-shutdown
// timeout forces it (T-0151). Echo's shutdown waits for active connections to
// go idle; the events handler watches the service's shutdown channel, so the
// stream closes within milliseconds and Start returns long before the 5s
// GracefulTimeout, which used to expire and log "failed to shut down server
// within given timeout".
func TestShutdownClosesEventStreams(t *testing.T) {
	srv, err := New(Options{Bind: "127.0.0.1", Port: 0, Logger: newTestLogger(t)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	addr := waitForAddr(t, srv)

	// A stream for a project the service has not opened stays open and silent
	// — from the shutdown's point of view it is exactly a browser tab with a
	// project open: an active connection that must go idle.
	res, err := http.Get("http://" + addr.String() + "/api/v1/events?project=nonexistent")
	if err != nil {
		t.Fatalf("open event stream: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("events stream status = %d", res.StatusCode)
	}

	// Let the stream register as an active connection.
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("shutdown returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown held the event stream open past the graceful window (T-0151)")
	}

	// And the stream itself is closed by the server, not left dangling until
	// the client notices.
	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(res.Body)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Errorf("stream read after shutdown = %v, want EOF", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("the stream stayed open after shutdown")
	}
}

// spec-gui.md §9.6 rule 6: a Unix domain socket is the most restrictive option
// and SHOULD be offered.
func TestUnixSocket(t *testing.T) {
	// macOS caps a socket path near 104 bytes, and a long TempDir blows past it.
	sock := filepath.Join(t.TempDir(), "s")
	srv, err := New(Options{Socket: sock, Logger: newTestLogger(t)})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	var conn net.Conn
	for time.Now().Before(deadline) {
		if conn, err = net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if conn == nil {
		t.Fatalf("nothing listening on %s: %v", sock, err)
	}
	conn.Close()

	fi, err := os.Stat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("socket mode is %o, want 600 — the socket is the access control", perm)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the service did not shut down")
	}
}

// A socket file left by a crash must not make every later start fail.
func TestStaleSocketIsReplaced(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	srv, err := New(Options{Socket: sock, Logger: newTestLogger(t)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("a stale socket file blocked startup: %v", err)
	}
	t.Fatal("the service never came up over the stale socket path")
}

func waitForAddr(t *testing.T, srv *Server) net.Addr {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if addr := srv.Addr(); addr != nil {
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the service never reported a listening address")
	return nil
}
