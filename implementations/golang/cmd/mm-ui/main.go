// Command mm-ui is the micro-manager GUI service (spec-gui.md).
//
// It links the library in and serves HTTP on loopback. It does not shell out to
// mm, and mm does not know it exists.
//
// This file is the only place in the service that reads the environment, argv or
// the working directory. Everything below it receives values (spec-tools.md
// §2.2, §3.5): a service outlives the shell that started it, so inheriting that
// shell's environment is a coupling, not a convenience.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Hopasaurus/micro-manager/internal/web"
	"github.com/Hopasaurus/micro-manager/mm"
)

const usage = `mm-ui — the micro-manager web interface

usage: mm-ui [options]

Serves the GUI of spec-gui.md on http://127.0.0.1:7717 by default. The service
reads and writes your files with no authentication, so it refuses to bind
anything but a loopback address unless told otherwise in the config file.

Options:
  --bind ADDRESS       address to listen on (default 127.0.0.1)
  --port N             port to listen on (default 7717)
  --socket PATH        listen on a Unix domain socket instead of TCP
  --allow-remote       permit a non-loopback bind; the only way to set it
                       outside the config file
  --host NAME          additional hostname the Host header may carry
  --dir PATH           a micro-manager directory to open; repeatable
  --config-home PATH   override $XDG_CONFIG_HOME for this run
  --version            print the version and exit
  --help               print this message

Configuration is $XDG_CONFIG_HOME/micro-manager/config.json; command line
options override it. There is no UI control for binding, by design.

Environment:
  XDG_CONFIG_HOME      where config.json, theme.json, recent and favorites live
  HOME                 fallback for the above, as $HOME/.config
  MM_UI_TEST=1         test mode: no animations, no auto-dismissing toasts
`

// Exit codes. Deliberately fewer than the CLI's seven (spec-tools.md §10): those
// describe the outcome of one operation on one directory, which is not what a
// long-running service does.
const (
	exitOK     = 0
	exitFailed = 1
	exitUsage  = 2
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	var (
		bind        string
		port        int
		socket      string
		allowRemote bool
		configHome  string
		showVersion bool
		hosts       stringList
		dirs        stringList
	)

	fs := flag.NewFlagSet("mm-ui", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	fs.StringVar(&bind, "bind", "", "address to listen on")
	fs.IntVar(&port, "port", 0, "port to listen on")
	fs.StringVar(&socket, "socket", "", "unix domain socket to listen on")
	fs.BoolVar(&allowRemote, "allow-remote", false, "permit a non-loopback bind")
	fs.StringVar(&configHome, "config-home", "", "override XDG_CONFIG_HOME")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")
	fs.Var(&hosts, "host", "additional permitted Host header")
	fs.Var(&dirs, "dir", "a micro-manager directory to open")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if showVersion {
		fmt.Printf("mm-ui %s (ui spec %s, tools spec %s, format spec %s)\n",
			web.Version, web.SpecUIVersion, web.SpecToolsVersion, web.SpecFormatVersion)
		return exitOK
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "mm-ui: unexpected argument %q; every option is a switch\n", fs.Arg(0))
		return exitUsage
	}

	opts, err := options(bind, port, socket, allowRemote, configHome, hosts, dirs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mm-ui: %v\n", err)
		return exitFailed
	}

	server, err := web.New(opts)
	if err != nil {
		// A refused bind lands here, and its message names the address and says
		// what to do about it (spec-gui.md §9.6 rule 2).
		fmt.Fprintf(os.Stderr, "mm-ui: %v\n", err)
		return exitFailed
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "mm-ui %s listening on %s\n", web.Version, server.URL())
	if err := server.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "mm-ui: %v\n", err)
		return exitFailed
	}
	return exitOK
}

// options gathers the environment and the configuration into the values the
// service runs on. Command line beats config file, config file beats default.
func options(bind string, port int, socket string, allowRemote bool,
	configHome string, hosts, dirs []string,
) (web.Options, error) {
	home := mm.ConfigHome(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME"))
	if configHome != "" {
		abs, err := filepath.Abs(configHome)
		if err != nil {
			return web.Options{}, fmt.Errorf("--config-home %s: %w", configHome, err)
		}
		home = abs
	}

	var (
		cfg        = mm.DefaultConfig()
		warnings   []mm.ConfigWarning
		configFile *mm.ConfigFile
	)
	if home != "" {
		f, err := mm.LoadConfigFile(mm.NewSystemPaths(home).Config, mm.ScopeSystem)
		if err != nil {
			return web.Options{}, err
		}
		configFile = f
		cfg, warnings = mm.MergeConfig(f, nil)
	}

	startDir, err := os.Getwd()
	if err != nil {
		return web.Options{}, fmt.Errorf("working directory: %w", err)
	}

	opts := web.Options{
		Bind:         cfg.Server.Bind,
		Port:         cfg.Server.Port,
		Socket:       cfg.Server.Socket,
		AllowRemote:  cfg.Server.AllowRemote,
		Hosts:        hosts,
		ConfigHome:   home,
		StartDir:     startDir,
		Config:       cfg,
		SystemConfig: configFile,
		Warnings:     warnings,
		TestMode:     os.Getenv("MM_UI_TEST") == "1",
	}
	if bind != "" {
		opts.Bind = bind
	}
	if port != 0 {
		opts.Port = port
	}
	if socket != "" {
		abs, err := filepath.Abs(socket)
		if err != nil {
			return web.Options{}, fmt.Errorf("--socket %s: %w", socket, err)
		}
		opts.Socket = abs
	}
	if allowRemote {
		// One direction only: the flag turns it on, and nothing turns it off,
		// because the config file saying true is also the user saying true.
		opts.AllowRemote = true
	}

	// Expand startup roots by the same front-end rule used for live config
	// reloads. The library receives absolute paths (spec-gui.md §9.5 rule 2).
	opts.Config.Scan.Roots = web.ExpandRoots(opts.Config.Scan.Roots, os.Getenv("HOME"))
	for _, d := range dirs {
		abs, err := filepath.Abs(expandRoot(d, os.Getenv("HOME")))
		if err != nil {
			return web.Options{}, fmt.Errorf("--dir %s: %w", d, err)
		}
		opts.Dirs = append(opts.Dirs, abs)
	}
	return opts, nil
}

// expandRoot handles ~ and $VAR. os.ExpandEnv leaves an unset variable as an
// empty string, which would turn "$WORK/code" into "/code" - a root that exists
// on some machines. An unset variable therefore yields no root at all.
func expandRoot(path, home string) string {
	if path == "~" || (len(path) > 1 && path[:2] == "~/") {
		if home != "" {
			path = filepath.Join(home, path[1:])
		}
	}
	expanded := os.ExpandEnv(path)
	if expanded == "" {
		return path
	}
	return expanded
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return fmt.Sprint(*l) }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}
