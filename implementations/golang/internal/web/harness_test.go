package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"micromanager/mm"
)

// The harness every test in this package uses.
//
// It drives the service through http.Handler rather than a real socket: an
// httptest server needs no port, cannot collide with a service the user is
// already running, and leaves nothing behind when a test fails. The bind guard
// is the one thing that has to be tested without it, since binding is the
// behaviour under test.

// testServer is a service wired to fixture directories.
type testServer struct {
	*Server
	t          *testing.T
	ConfigHome string
	Root       string // the configured scan root; every fixture sits under it
	Dirs       []string
}

// newTestServer builds a service over copies of the named fixtures.
//
// Fixtures are COPIED: several tests here mutate a directory, and the corpus
// under testdata/ is read-only by convention.
func newTestServer(t *testing.T, fixtures ...string) *testServer {
	t.Helper()
	return newTestServerCfg(t, mm.DefaultConfig(), fixtures...)
}

// newTestServerCfg is newTestServer with the merged configuration supplied by
// the caller, for tests that need a non-default setting - the SSE tests use a
// fast poll interval so a mutation is observed within the test's lifetime.
func newTestServerCfg(t *testing.T, cfg mm.Config, fixtures ...string) *testServer {
	t.Helper()
	return newTestServerWith(t, func(o *Options) { o.Config = cfg }, fixtures...)
}

// newTestServerWith is newTestServer with a caller-supplied mutation of the
// options, applied after the harness defaults. The test-mode tests pass
// TestMode: true this way.
func newTestServerWith(t *testing.T, mutate func(*Options), fixtures ...string) *testServer {
	t.Helper()

	opts := Options{
		ConfigHome: t.TempDir(),
		StartDir:   t.TempDir(),
		Config:     mm.DefaultConfig(),
		Logger:     newTestLogger(t),
	}
	mutate(&opts)

	var dirs []string
	for _, name := range fixtures {
		dirs = append(dirs, copyFixtureTo(t, name, filepath.Join(opts.StartDir, name)))
	}

	opts.Dirs = dirs
	opts.Config.Scan.Roots = []string{opts.StartDir}

	srv, err := New(opts)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return &testServer{Server: srv, t: t, ConfigHome: opts.ConfigHome, Root: opts.StartDir, Dirs: dirs}
}

// samePath reports whether two paths name the same directory.
//
// A fixture path comes from t.TempDir(); a path that came back from the service
// came through discovery, which RESOLVES symlinks. On macOS t.TempDir() hands
// out /var/folders/... while discovery reports /private/var/folders/..., so a
// raw == between the two is false for every fixture on darwin and true on
// Linux - a test that compares them directly passes in CI and fails on the
// machine it was written on. registry.go's under() documents the same hazard
// for the production path; this is that rule, for tests.
//
// Compare fixture paths against service output with this, never with ==.
func samePath(a, b string) bool {
	ca, err := mm.CanonicalPath(a)
	if err != nil {
		return false
	}
	cb, err := mm.CanonicalPath(b)
	if err != nil {
		return false
	}
	return ca == cb
}

// underRoot reports whether path sits at or below root, canonicalising both
// sides for the reason samePath does. The separator check is what keeps
// /tmp/aaa-sibling from counting as being under /tmp/aaa.
func underRoot(path, root string) bool {
	cp, err := mm.CanonicalPath(path)
	if err != nil {
		return false
	}
	cr, err := mm.CanonicalPath(root)
	if err != nil {
		return false
	}
	return cp == cr || strings.HasPrefix(cp, cr+string(filepath.Separator))
}

// response is one round trip, kept as bytes so a test may read it twice.
type response struct {
	t      *testing.T
	Status int
	Header http.Header
	Body   string
}

// do issues a request against the handler. The Host header is set to a value the
// guard accepts, since a test that has to remember to do that would eventually
// forget and fail for the wrong reason.
func (ts *testServer) do(method, target string, body io.Reader, headers ...string) *response {
	ts.t.Helper()
	if len(headers)%2 != 0 {
		ts.t.Fatalf("headers must be name/value pairs, got %d values", len(headers))
	}

	req := httptest.NewRequest(method, target, body)
	req.Host = "localhost:7717"
	for i := 0; i < len(headers); i += 2 {
		if strings.EqualFold(headers[i], "Host") {
			req.Host = headers[i+1]
			continue
		}
		req.Header.Set(headers[i], headers[i+1])
	}

	rec := httptest.NewRecorder()
	ts.Handler().ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()

	data, err := io.ReadAll(res.Body)
	if err != nil {
		ts.t.Fatalf("read body: %v", err)
	}
	return &response{t: ts.t, Status: res.StatusCode, Header: res.Header, Body: string(data)}
}

func (ts *testServer) get(target string, headers ...string) *response {
	ts.t.Helper()
	return ts.do(http.MethodGet, target, nil, headers...)
}

func (ts *testServer) post(target, body string, headers ...string) *response {
	ts.t.Helper()
	return ts.do(http.MethodPost, target, strings.NewReader(body), headers...)
}

// expectStatus fails with the body attached: a bare "want 200, got 500" sends
// the reader back to the terminal to reproduce it by hand.
func (r *response) expectStatus(want int) *response {
	r.t.Helper()
	if r.Status != want {
		r.t.Fatalf("status = %d, want %d\nbody: %s", r.Status, want, r.Body)
	}
	return r
}

// json decodes the body into v, failing the test on anything but valid JSON.
func (r *response) json(v any) *response {
	r.t.Helper()
	if err := json.Unmarshal([]byte(r.Body), v); err != nil {
		r.t.Fatalf("body is not JSON: %v\nbody: %s", err, r.Body)
	}
	return r
}

// expectHeader asserts a header contains a substring, which is what the header
// assertions in this package actually need - CSP is one long value and the
// tests care about single directives in it.
func (r *response) expectHeader(name, contains string) *response {
	r.t.Helper()
	got := r.Header.Get(name)
	if !strings.Contains(got, contains) {
		r.t.Errorf("header %s = %q, want it to contain %q", name, got, contains)
	}
	return r
}

func (r *response) expectNoHeader(name string) *response {
	r.t.Helper()
	if got := r.Header.Get(name); got != "" {
		r.t.Errorf("header %s should be absent, got %q", name, got)
	}
	return r
}

// copyFixtureTo copies a fixture from the corpus into parent, and returns the
// path of the micro-manager directory it created.
//
// The fixture lands in parent/micro-manager rather than in parent itself,
// because discovery matches on DIRECTORY NAME (spec-file-format.md Appendix B):
// a directory called clean-full is not a micro-manager directory however
// well-formed its contents are, and a walk would never find it.
func copyFixtureTo(t *testing.T, name, parent string) string {
	t.Helper()
	src := filepath.Join("../../testdata", name)
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	dst := filepath.Join(parent, "micro-manager")
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}
