package web

import (
	"net/http"
	"strings"
	"testing"

	"micromanager/mm"
)

// spec-gui.md §9.6 rules 2 and 3. The service has unauthenticated read and write
// access to the user's files, so this is the protection that stands in for the
// authentication the specification does not define.
func TestBindGuard(t *testing.T) {
	cases := []struct {
		name        string
		bind        string
		port        int
		socket      string
		allowRemote bool
		wantErr     bool
		// wantIn is what the refusal must name. A refusal a user cannot act on
		// is a refusal they will work around by turning the guard off.
		wantIn string
	}{
		{name: "the default", bind: "127.0.0.1", port: 7717},
		{name: "any loopback literal", bind: "127.0.0.2", port: 7717},
		{name: "ipv6 loopback", bind: "::1", port: 7717},
		{name: "bracketed ipv6 loopback", bind: "[::1]", port: 7717},
		{name: "localhost resolves to loopback", bind: "localhost", port: 7717},

		{name: "every interface, v4", bind: "0.0.0.0", port: 7717, wantErr: true, wantIn: "0.0.0.0"},
		{name: "every interface, v6", bind: "::", port: 7717, wantErr: true, wantIn: "::"},
		{name: "bracketed every interface", bind: "[::]", port: 7717, wantErr: true, wantIn: "[::]"},
		{name: "a routable address", bind: "192.168.1.10", port: 7717, wantErr: true, wantIn: "192.168.1.10"},
		{name: "a public address", bind: "8.8.8.8", port: 7717, wantErr: true, wantIn: "8.8.8.8"},

		{name: "allowRemote permits every interface", bind: "0.0.0.0", port: 7717, allowRemote: true},
		{name: "allowRemote permits a routable address", bind: "192.168.1.10", port: 7717, allowRemote: true},

		{name: "a socket is not on the network", socket: "/tmp/mm-ui.sock"},
		{name: "port zero asks the OS to choose", bind: "127.0.0.1", port: 0},
		{name: "a negative port is not a port", bind: "127.0.0.1", port: -1, wantErr: true, wantIn: "-1"},
		{name: "port out of range", bind: "127.0.0.1", port: 70000, wantErr: true, wantIn: "70000"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(Options{
				Bind: c.bind, Port: c.port, Socket: c.socket,
				AllowRemote: c.allowRemote, Logger: newTestLogger(t),
			})
			if c.wantErr && err == nil {
				t.Fatalf("binding %s:%d was allowed", c.bind, c.port)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("binding %s:%d was refused: %v", c.bind, c.port, err)
			}
			if c.wantErr && err != nil && !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("the refusal must name %q so the user can act on it; got %q", c.wantIn, err)
			}
		})
	}
}

// §9.6 rule 7: a port already in use fails clearly and MUST NOT be silently
// moved. Nothing in the service may pick a different port.
func TestNoPortFallback(t *testing.T) {
	s, err := New(Options{Bind: "127.0.0.1", Port: 7717, Logger: newTestLogger(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Address(); got != "127.0.0.1:7717" {
		t.Errorf("Address() = %q, want the configured address unchanged", got)
	}
	if got := s.URL(); got != "http://127.0.0.1:7717" {
		t.Errorf("URL() = %q", got)
	}
}

// §9.6: reject any request whose Host header is not localhost, 127.0.0.1, [::1],
// or an explicitly configured hostname, with 421.
//
// This is DNS-rebinding defence. A loopback bind is not enough on its own,
// because any page the user visits can issue requests to localhost.
func TestHostGuard(t *testing.T) {
	ts := newTestServer(t)

	allowed := []string{
		"localhost:7717", "localhost", "127.0.0.1:7717", "127.0.0.1",
		"[::1]:7717", "127.0.0.2:7717",
	}
	for _, host := range allowed {
		if got := ts.get("/api/v1/health", "Host", host).Status; got != http.StatusOK {
			t.Errorf("Host %q returned %d, want 200", host, got)
		}
	}

	refused := []string{
		"evil.example.com", "evil.example.com:7717",
		"micro-manager.internal", "10.0.0.5:7717", "",
	}
	for _, host := range refused {
		if got := ts.get("/api/v1/health", "Host", host).Status; got != http.StatusMisdirectedRequest {
			t.Errorf("Host %q returned %d, want 421", host, got)
		}
	}
}

// An explicitly configured hostname is the documented escape hatch.
func TestConfiguredHostIsAccepted(t *testing.T) {
	srv, err := New(Options{
		Bind: "127.0.0.1", Port: 7717,
		Hosts:  []string{"mm.local"},
		Logger: newTestLogger(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := &testServer{Server: srv, t: t}

	if got := ts.get("/api/v1/health", "Host", "mm.local:7717").Status; got != http.StatusOK {
		t.Errorf("a configured host returned %d, want 200", got)
	}
	if got := ts.get("/api/v1/health", "Host", "other.local").Status; got != http.StatusMisdirectedRequest {
		t.Errorf("an unconfigured host returned %d, want 421", got)
	}
}

// The Referrer-Policy must not defeat the origin guard next to it.
//
// Under `no-referrer` a browser strips the origin from NAVIGATIONS as well, so
// a plain <form method="post"> aimed at this very service arrives carrying
// `Origin: null` — indistinguishable from a sandboxed iframe, and refused as
// one. Every native form POST in the UI 403'd with
// `Origin "null" may not make state-changing requests here`, and saving
// settings was impossible (T-0108).
//
// This cannot be caught by a Go test issuing its own headers, because the
// header the browser sends is the whole bug. So the guard is on the policy
// itself: any value that suppresses the origin on a same-origin navigation is
// wrong here, however hardened it looks.
func TestReferrerPolicyDoesNotSuppressOrigin(t *testing.T) {
	ts := newTestServer(t)

	got := ts.get("/api/v1/health").Header.Get("Referrer-Policy")
	if got == "no-referrer" {
		t.Fatal("Referrer-Policy: no-referrer makes browsers send Origin: null on " +
			"same-origin form posts, which the origin guard then refuses (T-0108)")
	}
	if got != "same-origin" {
		t.Errorf("Referrer-Policy = %q, want same-origin", got)
	}
}

// §9.6: reject any state-changing request - anything other than GET and HEAD -
// whose Origin is present and not the service's own origin, with 403.
func TestOriginGuard(t *testing.T) {
	ts := newTestServer(t)

	// A GET is not state-changing, so a foreign Origin is not this rule's
	// business - the Host check is what protects reads.
	if got := ts.get("/api/v1/health", "Origin", "https://evil.example.com").Status; got != http.StatusOK {
		t.Errorf("a GET with a foreign Origin returned %d, want 200", got)
	}

	cases := []struct {
		origin string
		want   int
	}{
		{"", http.StatusNotFound},                          // absent: curl and the CLI send none
		{"http://localhost:7717", http.StatusNotFound},     // our own
		{"https://evil.example.com", http.StatusForbidden}, // someone else's
		{"http://127.0.0.1:9999", http.StatusForbidden},    // right host, wrong port
		{"null", http.StatusForbidden},                     // a sandboxed iframe
	}
	for _, c := range cases {
		// The route does not exist yet, so a request that passes the guard is a
		// 404 and one that does not is a 403. That distinction is the assertion.
		got := ts.post("/api/v1/projects/x/items/T-0001/start", "", "Origin", c.origin).Status
		if got != c.want {
			t.Errorf("Origin %q returned %d, want %d", c.origin, got, c.want)
		}
	}
}

// §9.6: set Content-Security-Policy restricting connect-src to 'self', never
// send Access-Control-Allow-Origin: *, and never reflect an arbitrary origin.
func TestSecurityHeaders(t *testing.T) {
	ts := newTestServer(t)

	r := ts.get("/api/v1/health").expectStatus(http.StatusOK)
	r.expectHeader("Content-Security-Policy", "connect-src 'self'")
	r.expectHeader("Content-Security-Policy", "default-src 'self'")
	r.expectHeader("Content-Security-Policy", "frame-ancestors 'none'")
	r.expectHeader("X-Content-Type-Options", "nosniff")
	r.expectNoHeader("Access-Control-Allow-Origin")

	// Present on a refusal too: a 421 that leaked CORS headers would be a hole
	// with a tidy status code.
	refused := ts.get("/api/v1/health", "Host", "evil.example.com")
	refused.expectStatus(http.StatusMisdirectedRequest)
	refused.expectNoHeader("Access-Control-Allow-Origin")
	refused.expectHeader("Content-Security-Policy", "connect-src 'self'")

	// A reflected origin is the failure mode a naive CORS implementation has.
	withOrigin := ts.get("/api/v1/health", "Origin", "https://evil.example.com")
	withOrigin.expectNoHeader("Access-Control-Allow-Origin")
	for name, values := range withOrigin.Header {
		for _, v := range values {
			if strings.Contains(v, "evil.example.com") {
				t.Errorf("header %s reflects the request origin: %q", name, v)
			}
		}
	}
}

// §9.6 rule 4: allowRemote MUST NOT be settable from the web UI. The service
// exposes no route that could reach it, and the library refuses the key.
func TestAllowRemoteIsNotReachableFromTheUI(t *testing.T) {
	for _, key := range []string{"server", "server.allowRemote", "server.bind", "server.port"} {
		if mm.SettableFromUI(key) {
			t.Errorf("%s is settable from a settings screen", key)
		}
	}
}
