// Package web is the GUI service of spec-gui.md: an HTTP front end with the
// library compiled in.
//
// It MUST NOT invoke the mm CLI, spawn it as a subprocess, or parse its output
// (spec-gui.md §2.1). Every operation goes through the same mm package functions
// the CLI calls, which is what keeps the two front ends from drifting.
//
// Nothing in this package may contain a domain rule. The test is mechanical: if
// a function here would also be needed by the TUI, it is in the wrong package.
package web

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"micromanager/mm"
)

// DefaultPort is spec-gui.md §9.6 rule 1. A user who bookmarks :7717 should find
// the same thing there tomorrow.
const DefaultPort = 7717

// DefaultBind is the only address the service starts on without being told
// otherwise (spec-gui.md §9.6 rule 1).
const DefaultBind = "127.0.0.1"

// Options is everything the service needs that it may not work out for itself.
//
// The environment is read by cmd/mm-ui and arrives here as values: this package
// is a guest in the process for the same reason the library is, and a service
// that inherits the environment of whoever started it is the coupling
// spec-tools.md §3.5 exists to prevent.
type Options struct {
	// Bind, Port and Socket come from the config file, then the command line.
	// Socket wins over an address when set (spec-gui.md §9.6 rule 6).
	Bind   string
	Port   int
	Socket string

	// AllowRemote is settable ONLY from the config file or --allow-remote
	// (spec-gui.md §9.6 rule 4). No route and no template may change it.
	AllowRemote bool

	// Hosts are extra hostnames the Host header may carry, beyond localhost,
	// 127.0.0.1 and [::1].
	Hosts []string

	// ConfigHome is $XDG_CONFIG_HOME resolved by mm.ConfigHome.
	ConfigHome string

	// StartDir is the directory the service was started in. It becomes the
	// single scan root when scan.roots is empty (spec-gui.md §9.5).
	StartDir string

	// Dirs are directories named on the command line, already absolute. They are
	// opened whether or not a scan root reaches them.
	Dirs []string

	// Config is the merged configuration and Warnings is what merging reported.
	// The service starts with warnings and surfaces them; it does not refuse.
	Config   mm.Config
	Warnings []mm.ConfigWarning

	// TestMode is MM_UI_TEST=1: no animations, no auto-dismissing toasts,
	// data-test-mode="true" on the app root (spec-gui.md §4.4). It changes
	// nothing else - not layout, not locators, not which operations are allowed.
	TestMode bool

	// Dev re-reads templates from disk on every render, for editing them without
	// rebuilding. Never on in a released binary.
	Dev bool

	// Logger receives the service's own diagnostics. Nil means the default.
	Logger *slog.Logger
}

// Server is the running service.
type Server struct {
	echo     *echo.Echo
	opts     Options
	log      *slog.Logger
	renderer *renderer
	registry *registry

	// addr is filled once a listener exists, which is the only point at which
	// port 0 in a test resolves to a real port.
	addr net.Addr
}

// New builds the service. It does not listen; Start does.
//
// An invalid bind address is refused HERE rather than at Start, so a
// misconfiguration is a startup error and never a service that came up on an
// address the user did not intend.
func New(opts Options) (*Server, error) {
	if opts.Bind == "" {
		opts.Bind = DefaultBind
	}
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	if err := checkBindAddress(opts); err != nil {
		return nil, err
	}

	r, err := newRenderer(templatesFS(), templateFuncs(), opts.Dev)
	if err != nil {
		// A broken template is a startup failure, not a 500 the first time
		// somebody opens the view it belongs to.
		return nil, err
	}

	s := &Server{
		echo:     echo.New(),
		opts:     opts,
		log:      opts.Logger,
		renderer: r,
		registry: newRegistry(opts),
	}
	s.echo.HTTPErrorHandler = s.errorHandler
	s.echo.Use(middleware.Recover())
	s.echo.Use(s.guardMiddleware())

	s.routes()
	return s, nil
}

// Handler exposes the service as a plain http.Handler, which is what an
// httptest server needs and what keeps the tests from opening real sockets.
func (s *Server) Handler() http.Handler { return s.echo }

// Address is the TCP address the service will listen on, or the socket path.
func (s *Server) Address() string {
	if s.opts.Socket != "" {
		return s.opts.Socket
	}
	return net.JoinHostPort(s.opts.Bind, fmt.Sprint(s.opts.Port))
}

// Addr is the address actually listened on, available after Start has bound.
func (s *Server) Addr() net.Addr { return s.addr }

// URL is where a browser should be pointed.
func (s *Server) URL() string {
	if s.opts.Socket != "" {
		return "unix://" + s.opts.Socket
	}
	host := s.opts.Bind
	if host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s", net.JoinHostPort(host, fmt.Sprint(s.opts.Port)))
}

// Start listens and serves until the context is cancelled.
//
// A port already in use is a clear failure, never a quiet move to another port
// (spec-gui.md §9.6 rule 7).
func (s *Server) Start(ctx context.Context) error {
	if s.opts.AllowRemote && s.opts.Socket == "" {
		// §9.6 rule 5: a prominent warning naming the address and port.
		s.log.Warn("REMOTE ACCESS ENABLED: this service has unauthenticated read and write access to your files",
			"address", s.opts.Bind, "port", s.opts.Port)
	}
	for _, w := range s.opts.Warnings {
		s.log.Warn("configuration", "file", w.File, "key", w.Key, "message", w.Message)
	}

	sc := echo.StartConfig{
		Address:          s.Address(),
		HideBanner:       true,
		HidePort:         true,
		GracefulTimeout:  5 * time.Second,
		ListenerAddrFunc: func(addr net.Addr) { s.addr = addr },
		BeforeServeFunc: func(srv *http.Server) error {
			// WriteTimeout MUST be zero. The event stream of §2.3 is a
			// long-lived response, and any write deadline eventually kills it -
			// a failure that looks like a flaky client rather than a setting.
			srv.WriteTimeout = 0
			srv.ReadHeaderTimeout = 10 * time.Second
			return nil
		},
	}

	if s.opts.Socket != "" {
		ln, err := s.listenUnix()
		if err != nil {
			return err
		}
		sc.Listener = ln
	}

	err := sc.Start(ctx, s.echo)
	if err != nil && !isServerClosed(err) {
		return fmt.Errorf("%s: %w", s.Address(), err)
	}
	return nil
}

// listenUnix binds a Unix domain socket, the most restrictive option
// (spec-gui.md §9.6 rule 6).
func (s *Server) listenUnix() (net.Listener, error) {
	path := s.opts.Socket
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("socket directory %s: %w", filepath.Dir(path), err)
	}
	// A socket left behind by a crash would otherwise make every start fail with
	// "address already in use" for a service that is not running. Connect first:
	// if something answers, the address really is taken.
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return nil, fmt.Errorf("%s: already in use by a running service", path)
	} else if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("stale socket %s: %w", path, err)
		}
	}

	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Owner only: the socket is the access control, since there is no
	// authentication anywhere in this specification.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, fmt.Errorf("socket permissions %s: %w", path, err)
	}
	return ln, nil
}

func isServerClosed(err error) bool {
	return err == http.ErrServerClosed || err == context.Canceled
}
