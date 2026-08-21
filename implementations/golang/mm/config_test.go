package mm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeJSON(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadConfig(t *testing.T, path string, scope ConfigScope) *ConfigFile {
	t.Helper()
	f, err := LoadConfigFile(path, scope)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return f
}

// spec-gui.md §8.2: XDG_CONFIG_HOME, falling back to $HOME/.config when unset or
// not absolute.
func TestConfigHome(t *testing.T) {
	cases := []struct{ xdg, home, want string }{
		{"/xdg", "/home/u", "/xdg"},
		{"", "/home/u", "/home/u/.config"},
		{"relative/path", "/home/u", "/home/u/.config"},
		{"/xdg/", "/home/u", "/xdg"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := ConfigHome(c.xdg, c.home); got != c.want {
			t.Errorf("ConfigHome(%q, %q) = %q, want %q", c.xdg, c.home, got, c.want)
		}
	}
}

func TestSystemPaths(t *testing.T) {
	sp := NewSystemPaths("/home/u/.config")
	want := SystemPaths{
		Dir:       "/home/u/.config/micro-manager",
		Config:    "/home/u/.config/micro-manager/config.json",
		Theme:     "/home/u/.config/micro-manager/theme.json",
		Themes:    "/home/u/.config/micro-manager/themes",
		Recent:    "/home/u/.config/micro-manager/recent.json",
		Favorites: "/home/u/.config/micro-manager/favorites.json",
	}
	if sp != want {
		t.Errorf("SystemPaths =\n%+v\nwant\n%+v", sp, want)
	}
}

// A missing file is equivalent to an empty object (spec-gui.md §9.1).
func TestMissingConfigIsEmpty(t *testing.T) {
	f := loadConfig(t, filepath.Join(t.TempDir(), "config.json"), ScopeSystem)
	if f.Exists {
		t.Error("a missing file should not report Exists")
	}
	cfg, warnings := MergeConfig(f, nil)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("a missing file should leave the defaults untouched:\n%+v", cfg)
	}
}

func TestMalformedConfigIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{"ui": `)
	_, err := LoadConfigFile(path, ScopeSystem)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error must name the file, got %q", err)
	}
}

// spec-gui.md §9.4: last writer wins per LEAF key, not per object. A project
// overriding ui.density must not discard the system's ui.defaultView.
func TestMergeIsPerLeafKey(t *testing.T) {
	dir := t.TempDir()
	sys := filepath.Join(dir, "system.json")
	proj := filepath.Join(dir, "project.json")
	writeJSON(t, sys, `{
	  "schemaVersion": 1,
	  "ui": { "density": "normal", "defaultView": "report", "confirmRemove": false },
	  "report": { "period": "this-week", "groupBy": "outcome" }
	}`)
	writeJSON(t, proj, `{
	  "schemaVersion": 1,
	  "ui": { "density": "compact" },
	  "report": { "includeWip": true }
	}`)

	cfg, warnings := MergeConfig(
		loadConfig(t, sys, ScopeSystem),
		loadConfig(t, proj, ScopeProject),
	)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if cfg.UI.Density != "compact" {
		t.Errorf("ui.density = %q, want the project's compact", cfg.UI.Density)
	}
	if cfg.UI.DefaultView != "report" {
		t.Errorf("ui.defaultView = %q, want the system's report — the object was replaced instead of merged", cfg.UI.DefaultView)
	}
	if cfg.UI.ConfirmRemove {
		t.Error("ui.confirmRemove lost the system's false")
	}
	if cfg.Report.Period != "this-week" || cfg.Report.GroupBy != GroupByOutcome {
		t.Errorf("report = %+v, want the system's period and grouping kept", cfg.Report)
	}
	if !cfg.Report.IncludeWip {
		t.Error("report.includeWip lost the project's true")
	}
}

// spec-gui.md §9.3: a project config MUST NOT set these, and an implementation
// MUST report them as a validation warning NAMING THE KEY.
func TestProjectConfigCannotSetSystemScopedKeys(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "config.json")
	writeJSON(t, proj, `{
	  "ui": { "recentCount": 3, "favoritesCount": 4, "recentMaxStored": 5, "density": "compact" },
	  "scan": { "roots": ["/tmp"] },
	  "server": { "allowRemote": true, "port": 9999 }
	}`)

	cfg, warnings := MergeConfig(nil, loadConfig(t, proj, ScopeProject))

	def := DefaultConfig()
	if cfg.UI.RecentCount != def.UI.RecentCount ||
		cfg.UI.FavoritesCount != def.UI.FavoritesCount ||
		cfg.UI.RecentMaxStored != def.UI.RecentMaxStored {
		t.Errorf("a project config changed the list counts: %+v", cfg.UI)
	}
	if len(cfg.Scan.Roots) != 0 {
		t.Errorf("a project config set scan.roots: %v — a project cannot decide which projects exist", cfg.Scan.Roots)
	}
	if cfg.Server.AllowRemote || cfg.Server.Port != def.Server.Port {
		t.Errorf("a project config reached server.*: %+v", cfg.Server)
	}
	if cfg.UI.Density != "compact" {
		t.Error("the legal key in the same file was dropped along with the illegal ones")
	}

	for _, key := range []string{
		"ui.recentCount", "ui.favoritesCount", "ui.recentMaxStored",
		"scan.roots", "server.allowRemote", "server.port",
	} {
		if !hasWarningFor(warnings, key) {
			t.Errorf("no warning names %s; present-but-ignored is not acceptable", key)
		}
	}
}

func TestSetRefusesASystemScopedKeyInAProjectFile(t *testing.T) {
	f := loadConfig(t, filepath.Join(t.TempDir(), "config.json"), ScopeProject)
	if err := f.Set("server.allowRemote", true); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
	if err := f.Set("ui.density", "compact"); err != nil {
		t.Errorf("a project-scoped key must be settable: %v", err)
	}
}

// spec-gui.md §9.6 rule 4: no settings screen may reach server.*, in either
// scope. There is no UI control for binding, and there must not be one.
func TestServerKeysAreNotSettableFromTheUI(t *testing.T) {
	for _, key := range []string{"server", "server.bind", "server.port", "server.allowRemote", "server.socket"} {
		if SettableFromUI(key) {
			t.Errorf("SettableFromUI(%q) = true; binding is configured by file or command line only", key)
		}
	}
	for _, key := range []string{"ui.density", "scan.roots", "theme.id", "report.period"} {
		if !SettableFromUI(key) {
			t.Errorf("SettableFromUI(%q) = false; the settings view needs it", key)
		}
	}
}

// spec-gui.md §9.4: EVERY front end MUST preserve keys it does not understand
// when rewriting any shared file. Two front ends would otherwise strip each
// other's settings on alternate runs.
func TestUnknownKeysSurviveAWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := `{
  "schemaVersion": 1,
  "ui": { "density": "compact", "somethingFromTheFuture": 42 },
  "tui": { "mouse": true, "keymap": { "quit": "q" } },
  "somethingElseEntirely": { "nested": ["a", "b"] }
}`
	writeJSON(t, path, original)

	f := loadConfig(t, path, ScopeProject)
	if err := f.Set("ui.density", "comfortable"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(false); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if v := getPath(got, "ui.density"); v != "comfortable" {
		t.Errorf("the edit did not land: %v", v)
	}
	// The TUI's object is reserved and the GUI must preserve it (§9.3).
	if v := getPath(got, "tui.keymap.quit"); v != "q" {
		t.Error("the tui object was not preserved")
	}
	if v := getPath(got, "ui.somethingFromTheFuture"); v == nil {
		t.Error("an unknown key inside a known object was dropped")
	}
	if v := getPath(got, "somethingElseEntirely.nested"); v == nil {
		t.Error("an unknown top-level object was dropped")
	}
}

func TestSaveIsAtomicAndCreatesItsDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "deep", "nested", "config.json")
	f := loadConfig(t, path, ScopeSystem)
	if err := f.Set("ui.density", "compact"); err != nil {
		t.Fatal(err)
	}
	if err := f.Save(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote the file")
	}
	if err := f.Save(false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("the file should end with a newline")
	}
	if getPath(map[string]any{}, "x") != nil { // keep the helper honest
		t.Fatal("getPath on an empty document should be nil")
	}
}

func TestConfigValueErrorsAreWarningsNotFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSON(t, path, `{
	  "ui": { "density": "roomy", "pollIntervalMs": "soon", "recentCount": 500 },
	  "report": { "groupBy": "phase" },
	  "scan": { "roots": "/tmp" }
	}`)

	cfg, warnings := MergeConfig(loadConfig(t, path, ScopeSystem), nil)
	def := DefaultConfig()

	if cfg.UI.Density != def.UI.Density {
		t.Errorf("an invalid density was accepted: %q", cfg.UI.Density)
	}
	if cfg.UI.PollIntervalMs != def.UI.PollIntervalMs {
		t.Errorf("a string poll interval was accepted: %d", cfg.UI.PollIntervalMs)
	}
	if cfg.UI.RecentCount != 50 {
		t.Errorf("recentCount = %d, want it clamped to the documented maximum of 50", cfg.UI.RecentCount)
	}
	if cfg.Report.GroupBy != def.Report.GroupBy {
		t.Errorf("an unknown grouping was accepted: %q", cfg.Report.GroupBy)
	}
	for _, key := range []string{"ui.density", "ui.pollIntervalMs", "ui.recentCount", "report.groupBy", "scan.roots"} {
		if !hasWarningFor(warnings, key) {
			t.Errorf("no warning names %s", key)
		}
	}
}

// The scan configuration is the walker's input, and expanding roots is the front
// end's job - the library receives absolute paths (spec-gui.md §9.5 rule 2).
func TestScanConfigBecomesDiscoveryOptions(t *testing.T) {
	c := ScanConfig{
		Roots: []string{"/a", "/b"}, MaxDepth: 3, FollowSymlinks: true,
		IncludeHidden: true, Excludes: []string{"x"}, MaxResults: 7, TimeoutMs: 11,
	}
	opts := c.DiscoveryOptions()
	if len(opts.Roots) != 2 || opts.MaxDepth != 3 || !opts.FollowSymlinks ||
		!opts.IncludeHidden || opts.MaxResults != 7 || opts.TimeoutMS != 11 {
		t.Errorf("DiscoveryOptions = %+v", opts)
	}
	opts.Roots[0] = "/mutated"
	if c.Roots[0] != "/a" {
		t.Error("DiscoveryOptions shares its slice with the config")
	}
}

// spec-gui.md §9.5: includeHidden defaults to TRUE, because .micro-manager and
// .µmanager are conventional names. Skipping dotted directories is non-conforming.
func TestDefaultsMatchTheSpec(t *testing.T) {
	c := DefaultConfig()
	if !c.Scan.IncludeHidden {
		t.Error("scan.includeHidden must default to true")
	}
	if len(c.Scan.Roots) != 0 {
		t.Error("scan.roots must default to empty, never to $HOME or /")
	}
	if c.Server.Bind != "127.0.0.1" || c.Server.Port != 7717 || c.Server.AllowRemote {
		t.Errorf("server defaults are %+v, want loopback 7717 with allowRemote false", c.Server)
	}
	if c.UI.RecentCount != 10 || c.UI.FavoritesCount != 10 || c.UI.RecentMaxStored != 100 {
		t.Errorf("list defaults are %+v", c.UI)
	}
	if c.UI.PollIntervalMs != 5000 {
		t.Errorf("ui.pollIntervalMs = %d, want the documented 5000", c.UI.PollIntervalMs)
	}
	if got := c.UI.Board.CollapsedStages; len(got) != 1 || got[0] != "someday" {
		t.Errorf("ui.board.collapsedStages default = %v, want [\"someday\"] (§9.3)", got)
	}
}

// §9.3: ui.board.collapsedStages replaced version 1's single showSomeday
// boolean (T-0240) - a list of stage slugs, parsed the same way scan.roots
// and scan.excludes already are.
func TestBoardCollapsedStagesIsAStringList(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "project.json")
	writeJSON(t, proj, `{
	  "schemaVersion": 1,
	  "ui": { "board": { "collapsedStages": ["someday", "review"] } }
	}`)

	cfg, warnings := MergeConfig(nil, loadConfig(t, proj, ScopeProject))
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got := cfg.UI.Board.CollapsedStages; len(got) != 2 || got[0] != "someday" || got[1] != "review" {
		t.Errorf("ui.board.collapsedStages = %v, want [someday review]", got)
	}
}

func hasWarningFor(warnings []ConfigWarning, key string) bool {
	for _, w := range warnings {
		if w.Key == key {
			return true
		}
	}
	return false
}
