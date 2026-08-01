package web

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"
)

// Binding and local-only access (spec-gui.md §9.6).
//
// The service has unauthenticated read and write access to the user's files.
// This specification defines no authentication, and it therefore requires the
// service to be unreachable from the network. Everything in this file is that
// requirement; none of it is ceremony.

// checkBindAddress refuses to start on anything but a loopback address unless
// allowRemote is explicitly true (§9.6 rules 2 and 3).
//
// A hostname is resolved and EVERY address it resolves to must be loopback. A
// name that resolves to both 127.0.0.1 and a routable address would otherwise
// pass a check on the first answer and listen on the second.
func checkBindAddress(opts Options) error {
	if opts.Socket != "" {
		// A Unix socket is not on the network at all, which is why §9.6 rule 6
		// calls it the most restrictive option.
		return nil
	}
	// 0 is the ephemeral port: the OS picks one. Everything else must be a real
	// port number.
	if opts.Port < 0 || opts.Port > 65535 {
		return fmt.Errorf("port %d is out of range", opts.Port)
	}

	host := strings.TrimSpace(opts.Bind)
	if host == "" {
		return fmt.Errorf("no bind address")
	}
	// §9.6 rule 3: 0.0.0.0 and :: are refused by the same rule, and named
	// separately because binding every interface is never the accidental
	// outcome of a default.
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		if !opts.AllowRemote {
			return fmt.Errorf(
				"refusing to bind %s: that is every interface on this machine, and this service "+
					"has unauthenticated access to your files. Use 127.0.0.1, or set server.allowRemote "+
					"in the config file if you mean it", host)
		}
		return nil
	}

	addrs, err := resolveBind(host)
	if err != nil {
		return err
	}
	for _, ip := range addrs {
		if !ip.IsLoopback() {
			if !opts.AllowRemote {
				return fmt.Errorf(
					"refusing to bind %s (%s): it is not a loopback address, and this service has "+
						"unauthenticated read and write access to your files. Set server.allowRemote "+
						"in the config file, or pass --allow-remote, if you mean it", host, ip)
			}
			return nil
		}
	}
	return nil
}

func resolveBind(host string) ([]net.IP, error) {
	if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
		return []net.IP{ip}, nil
	}
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve bind address %q: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("bind address %q resolves to nothing", host)
	}
	return ips, nil
}

// contentSecurityPolicy restricts what the page may load and talk to
// (§9.6). connect-src 'self' is the clause the spec names; the rest closes the
// doors it implies.
//
// style-src carries 'unsafe-inline' because §8.7 requires the theme's custom
// properties to be inlined in the served HTML - that is what makes switching
// projects free of a flash of the previous theme. A per-request nonce would be
// stricter and is the obvious upgrade if the style block ever carries anything
// but tokens.
const contentSecurityPolicy = "default-src 'self'; " +
	"connect-src 'self'; " +
	"img-src 'self' data:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; " +
	"font-src 'self' data:; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"

// guardMiddleware is the browser-side half of §9.6.
//
// A loopback bind is not sufficient on its own: any page the user visits can
// issue requests to localhost, and DNS rebinding defeats naive origin checks by
// pointing a hostname the browser already trusts at 127.0.0.1. The Host check is
// what closes that, which is why it is a refusal and not a warning.
func (s *Server) guardMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			res := c.Response()

			res.Header().Set("Content-Security-Policy", contentSecurityPolicy)
			res.Header().Set("X-Content-Type-Options", "nosniff")
			// same-origin, NOT no-referrer — and the difference is load-bearing.
			//
			// Under no-referrer a browser strips the origin from a NAVIGATION
			// too, so a plain <form method="post"> to this very service arrives
			// with `Origin: null`. The origin guard below then correctly refuses
			// its own settings form: this header was defeating the check it sits
			// next to, and every native form POST in the UI 403'd (T-0108).
			//
			// same-origin keeps the property that actually matters here — a
			// cross-origin request still leaks nothing — while letting the
			// browser identify its own requests as its own.
			res.Header().Set("Referrer-Policy", "same-origin")
			// Never Access-Control-Allow-Origin: * and never a reflected origin.
			// The absence of any CORS header is the policy.

			if !s.hostAllowed(req.Host) {
				return echo.NewHTTPError(http.StatusMisdirectedRequest,
					fmt.Sprintf("Host %q is not served here", req.Host))
			}
			if !s.originAllowed(req) {
				return echo.NewHTTPError(http.StatusForbidden,
					fmt.Sprintf("Origin %q may not make state-changing requests here",
						req.Header.Get("Origin")))
			}
			return next(c)
		}
	}
}

// hostAllowed implements the 421 rule: localhost, 127.0.0.1, [::1], or an
// explicitly configured hostname.
func (s *Server) hostAllowed(hostHeader string) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	if host == "" {
		// HTTP/1.0 with no Host. Nothing a browser sends, and not worth serving.
		return false
	}

	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	for _, h := range s.opts.Hosts {
		if strings.EqualFold(strings.Trim(h, "[]"), host) {
			return true
		}
	}
	// Any other loopback literal - 127.0.0.2 and friends - is the same machine
	// and cannot be reached from off it.
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return true
	}
	// With allowRemote the user has said the address is theirs, so the bind
	// address itself is a legitimate Host.
	if s.opts.AllowRemote && strings.EqualFold(host, strings.Trim(s.opts.Bind, "[]")) {
		return true
	}
	return false
}

// originAllowed implements the 403 rule: a state-changing request - anything
// other than GET and HEAD - whose Origin is present and is not this service's
// own origin is refused.
//
// An absent Origin is allowed: curl and the CLI do not send one, and a browser
// always does for the requests this rule is about.
func (s *Server) originAllowed(req *http.Request) bool {
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	origin := req.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// An OPAQUE origin serialises to the literal "null": a sandboxed iframe, a
	// file:// page, a data: URL. That is precisely the caller this rule exists
	// to refuse, so it is rejected by name rather than incidentally through a
	// parse that happens to yield an empty host.
	//
	// A same-origin form post must never arrive this way. If it does, something
	// upstream is stripping the origin — see the Referrer-Policy note above.
	if origin == "null" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	// Same host means same origin here: the service speaks one scheme on one
	// port, so comparing the authority is comparing the origin.
	return strings.EqualFold(u.Host, req.Host)
}
