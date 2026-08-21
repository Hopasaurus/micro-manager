package mm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Configuration for the front ends (spec-gui.md §9).
//
// Nothing in this file is used by the format or by any invariant. A project
// config.json sits inside a micro-manager directory but is outside
// spec-file-format.md §1: format readers and the checker MUST ignore it
// (spec-gui.md §9.1), which is why the parsers in this package never look at it.
//
// The library owns loading, merging and writing because both front ends read
// the same files and must agree byte for byte. Deciding where $XDG_CONFIG_HOME
// points is NOT the library's job - it never reads the environment
// (spec-tools.md §2.2 rule 4) - so the path arrives as a parameter and
// ConfigHome exists to apply the rule to values a front end has already read.

// ConfigDirName is the directory both front ends keep system files in, under
// the XDG config home.
const ConfigDirName = "micro-manager"

// ConfigSchemaVersion is the schemaVersion this implementation writes.
const ConfigSchemaVersion = 1

// ConfigHome applies the rule of spec-gui.md §8.2: XDG_CONFIG_HOME, falling back
// to $HOME/.config when it is unset or not absolute.
//
// Both values are parameters. A front end reads the environment and passes what
// it found; the library only decides which of the two wins, so that the CLI, the
// UI service and the TUI cannot disagree about it.
func ConfigHome(xdgConfigHome, home string) string {
	if filepath.IsAbs(xdgConfigHome) {
		return filepath.Clean(xdgConfigHome)
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".config")
}

// SystemPaths are the files both front ends share, under one config home
// (spec-gui.md §8.2, §9.1, §10).
type SystemPaths struct {
	Dir       string // <configHome>/micro-manager
	Config    string // config.json
	Theme     string // theme.json
	Themes    string // themes/ - the theme library
	Recent    string // recent.json
	Favorites string // favorites.json
}

// NewSystemPaths derives the shared file locations from a config home.
func NewSystemPaths(configHome string) SystemPaths {
	dir := filepath.Join(configHome, ConfigDirName)
	return SystemPaths{
		Dir:       dir,
		Config:    filepath.Join(dir, "config.json"),
		Theme:     filepath.Join(dir, "theme.json"),
		Themes:    filepath.Join(dir, "themes"),
		Recent:    filepath.Join(dir, "recent.json"),
		Favorites: filepath.Join(dir, "favorites.json"),
	}
}

// ProjectConfigPath is <micro-manager dir>/config.json (spec-gui.md §9.1).
func ProjectConfigPath(dir string) string { return filepath.Join(dir, "config.json") }

// ProjectThemePath is <micro-manager dir>/theme.json (spec-gui.md §8.2).
func ProjectThemePath(dir string) string { return filepath.Join(dir, "theme.json") }

// ---------------------------------------------------------------------------
// Scope and warnings
// ---------------------------------------------------------------------------

// ConfigScope is which of the two files a setting lives in. Writing settings
// MUST target one scope explicitly (spec-gui.md §9.4).
type ConfigScope string

const (
	ScopeSystem  ConfigScope = "system"
	ScopeProject ConfigScope = "project"
)

// ConfigWarning is a setting that was read and not honoured.
//
// spec-gui.md §9.3 requires a project config that sets a system-scoped key to be
// reported by name: "present-but-ignored is not acceptable". These are warnings
// rather than errors because a config file the front end half-understands must
// still start the front end.
type ConfigWarning struct {
	File    string // the file the key was found in
	Key     string // dotted path, e.g. "ui.recentCount"
	Message string
}

func (w ConfigWarning) String() string {
	return fmt.Sprintf("%s: %s: %s", w.File, w.Key, w.Message)
}

// systemOnlyKeys may appear only in the system config (spec-gui.md §9.3, §9.5
// rule 1, §9.6 rule 4). A project cannot decide how many entries the UI shows,
// which projects exist, or what the service binds to.
var systemOnlyKeys = []string{
	"ui.recentCount",
	"ui.favoritesCount",
	"ui.recentMaxStored",
	"scan",
	"tickler",
	"server",
}

// ---------------------------------------------------------------------------
// The typed view
// ---------------------------------------------------------------------------

// Config is the merged, typed view of the configuration (spec-gui.md §9.2, §9.3).
//
// It is what a front end reads. Writing goes through ConfigFile, which keeps the
// raw document so that keys this implementation has never heard of survive.
type Config struct {
	Theme   ThemeSelection
	UI      UIConfig
	Report  ReportConfig
	Scan    ScanConfig
	Tickler TicklerConfig
	Server  ServerConfig
}

// ThemeSelection names a theme in the library (spec-gui.md §8.7 steps 2 and 4).
type ThemeSelection struct {
	ID string
}

// UIConfig is the ui object of spec-gui.md §9.2 and §9.3.
type UIConfig struct {
	// RecentCount and FavoritesCount are how many entries are DISPLAYED (0-50).
	// RecentMaxStored is how many are retained on disk and is independent:
	// shrinking the display count must not discard stored history.
	RecentCount     int
	FavoritesCount  int
	RecentMaxStored int

	Density        string // compact | normal | comfortable
	DefaultView    string // board | report | check | settings
	PollIntervalMs int
	ConfirmRemove  bool

	Board BoardConfig
}

// BoardConfig is ui.board, project-scoped in practice (spec-gui.md §9.3).
type BoardConfig struct {
	// CollapsedStages lists which columns render collapsed by default
	// (§5.5, §9.3) - a server-side default only, consulted when a client
	// has no stored preference of its own yet (T-0150's flicker fix keeps
	// the live, per-request truth entirely client-side). Renamed and
	// generalized from a single "someday" boolean (T-0240): any declared
	// stage, or none, or several, can be named here.
	CollapsedStages []string
	DoneLimit       int
}

// ReportConfig is the report object. Period occupies the same precedence slot as
// MM_REPORT_PERIOD (spec-tools.md §5.1.11 step 4); a UI service MUST NOT read
// that variable from its own environment (spec-gui.md §9.2).
type ReportConfig struct {
	Period     string
	GroupBy    GroupBy
	IncludeWip bool
}

// ScanConfig is the scan object (spec-gui.md §9.2, §9.5). System-scoped.
type ScanConfig struct {
	Roots          []string
	MaxDepth       int
	FollowSymlinks bool
	IncludeHidden  bool
	Excludes       []string
	MaxResults     int
	TimeoutMs      int
	RescanOnFocus  bool
}

// TicklerConfig is the tickler object (spec-gui.md §9.2, §2.4). System-scoped:
// the tickler service is a process concern, not a board's, and a project config
// MUST NOT set it.
//
// Interval is the raw duration string ("1m", "30s"). The empty string means
// off, the default; applyConfig validates the value parses as a duration and
// reports a warning otherwise, rather than letting a typo silently turn the
// service on or off. Parsing is the front end's: the library is told the
// interval as a string and never runs a clock itself.
type TicklerConfig struct {
	Interval string
}

// ServerConfig is the server object (spec-gui.md §9.2, §9.6). System-scoped, and
// AllowRemote is never settable from the web UI - a setting that removes a
// protection must not be reachable from the surface that protection defends.
type ServerConfig struct {
	Bind        string
	Port        int
	AllowRemote bool
	Socket      string
}

// DefaultConfig is the built-in layer, below both files (spec-gui.md §9.2, §9.4).
func DefaultConfig() Config {
	return Config{
		UI: UIConfig{
			RecentCount:     10,
			FavoritesCount:  10,
			RecentMaxStored: 100,
			Density:         "normal",
			DefaultView:     "board",
			PollIntervalMs:  5000,
			ConfirmRemove:   true,
			Board:           BoardConfig{CollapsedStages: []string{"someday"}, DoneLimit: 20},
		},
		Report: ReportConfig{
			Period:     "last-week",
			GroupBy:    GroupByNone,
			IncludeWip: false,
		},
		Scan: ScanConfig{
			// Empty by default. A front end with no roots falls back to the
			// directory it was started in and prompts - it MUST NOT default to
			// scanning $HOME or / (spec-gui.md §9.5).
			Roots:          nil,
			MaxDepth:       6,
			FollowSymlinks: false,
			// True on purpose: .micro-manager and .µmanager are conventional
			// names, and skipping dotted directories by default is
			// non-conforming (spec-gui.md §9.5 rule 6).
			IncludeHidden: true,
			Excludes:      []string{"node_modules", ".git", "target", "vendor", ".venv", "dist"},
			MaxResults:    500,
			TimeoutMs:     5000,
			RescanOnFocus: true,
		},
		Server: ServerConfig{
			Bind:        "127.0.0.1",
			Port:        7717,
			AllowRemote: false,
			Socket:      "",
		},
	}
}

// DiscoveryOptions renders the scan configuration as the library's walker input.
//
// Roots must already be absolute: expanding ~ and environment variables is the
// front end's job (spec-gui.md §9.5 rule 2, spec-tools.md §2.2 rule 4).
func (c ScanConfig) DiscoveryOptions() DiscoveryOptions {
	return DiscoveryOptions{
		Roots:          append([]string(nil), c.Roots...),
		MaxDepth:       c.MaxDepth,
		FollowSymlinks: c.FollowSymlinks,
		IncludeHidden:  c.IncludeHidden,
		Excludes:       append([]string(nil), c.Excludes...),
		MaxResults:     c.MaxResults,
		TimeoutMS:      c.TimeoutMs,
	}
}

// ---------------------------------------------------------------------------
// The file
// ---------------------------------------------------------------------------

// ConfigFile is one config.json, held as the raw document it was read from.
//
// The raw form is the point. spec-gui.md §9.4 requires every front end to
// preserve keys it does not understand when rewriting any shared file: two front
// ends over one set of files would otherwise strip each other's settings on
// alternate runs, each "correctly" writing back only what it knows. Decoding
// into a struct and re-encoding is exactly the mistake that causes it.
type ConfigFile struct {
	Path   string
	Scope  ConfigScope
	Exists bool

	raw map[string]any
}

// LoadConfigFile reads a config file. A missing file is not an error: it is
// equivalent to an empty object (spec-gui.md §9.1).
func LoadConfigFile(path string, scope ConfigScope) (*ConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ConfigFile{Path: path, Scope: scope, raw: map[string]any{}}, nil
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}
	f, err := ParseConfigFile(path, scope, data)
	if err != nil {
		return nil, err
	}
	f.Exists = true
	return f, nil
}

// ParseConfigFile builds a ConfigFile from content rather than from disk, which
// is what a JSON API needs when a client PUTs a whole file back (spec-gui.md
// §4.2, GET·PUT /config). The parse is the same one LoadConfigFile runs, so a
// file this accepts is a file that loads.
func ParseConfigFile(path string, scope ConfigScope, data []byte) (*ConfigFile, error) {
	f := &ConfigFile{Path: path, Scope: scope, raw: map[string]any{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return f, nil
	}
	if err := json.Unmarshal(data, &f.raw); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidArgument, path, err)
	}
	if f.raw == nil {
		f.raw = map[string]any{}
	}
	return f, nil
}

// Get returns a value by dotted key path, or nil when absent.
func (f *ConfigFile) Get(key string) any { return getPath(f.raw, key) }

// Set writes a value at a dotted key path, creating intermediate objects.
//
// It refuses a system-scoped key in a project file rather than writing something
// the merge will then ignore.
func (f *ConfigFile) Set(key string, value any) error {
	if f.Scope == ScopeProject && isSystemOnlyKey(key) {
		return fmt.Errorf("%w: %s is system-scoped and cannot be set in a project config",
			ErrInvalidArgument, key)
	}
	if key == "" {
		return fmt.Errorf("%w: empty key", ErrInvalidArgument)
	}
	setPath(f.raw, key, value)
	return nil
}

// Unset removes a dotted key path. Absent keys are not an error.
func (f *ConfigFile) Unset(key string) { unsetPath(f.raw, key) }

// SettableFromUI reports whether a settings screen may write this key.
//
// Everything under server is false, in either scope. spec-gui.md §9.6 rule 4:
// allowRemote is changed only by editing the config file or by a command line
// flag, because a setting that removes a protection must not be reachable from
// the surface that protection defends. The rule lives here rather than in a
// front end so that the GUI and the TUI cannot disagree about it - and so that
// neither one has to remember.
func SettableFromUI(key string) bool {
	return key != "server" && !strings.HasPrefix(key, "server.")
}

// Save writes the file atomically, creating its directory if needed.
//
// Unknown keys survive because they were never dropped: the document written is
// the document read, with the caller's changes applied to it.
func (f *ConfigFile) Save(dryRun bool) error {
	if _, ok := f.raw["schemaVersion"]; !ok {
		f.raw["schemaVersion"] = ConfigSchemaVersion
	}
	data, err := marshalJSONFile(f.raw)
	if err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrIO, filepath.Dir(f.Path), err)
	}
	if err := writeFileAtomic(f.Path, data); err != nil {
		return err
	}
	f.Exists = true
	return nil
}

// Bytes renders the file as it would be written. Used by a dry run and by tests.
func (f *ConfigFile) Bytes() ([]byte, error) { return marshalJSONFile(f.raw) }

// marshalJSONFile renders a document with stable key order and a trailing
// newline, so a config file diffs cleanly under version control.
func marshalJSONFile(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrIO, err)
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Merge
// ---------------------------------------------------------------------------

// MergeConfig layers built-in defaults, then system, then project, per LEAF key
// (spec-gui.md §9.4): a project overriding ui.density must not discard the
// system's ui.defaultView.
//
// Either file may be nil, meaning absent. Keys a project file may not set are
// reported and ignored; malformed values are reported and fall back to the layer
// below rather than failing the load, because a front end that will not start
// because of one bad value is worse than one that starts and says so.
func MergeConfig(system, project *ConfigFile) (Config, []ConfigWarning) {
	cfg := DefaultConfig()
	var warnings []ConfigWarning

	for _, f := range []*ConfigFile{system, project} {
		if f == nil {
			continue
		}
		if f.Scope == ScopeProject {
			warnings = append(warnings, systemScopedKeyWarnings(f)...)
		}
		warnings = append(warnings, applyConfig(&cfg, f)...)
	}

	warnings = append(warnings, clampConfig(&cfg)...)
	return cfg, warnings
}

// systemScopedKeyWarnings names every system-scoped key present in a project
// file (spec-gui.md §9.3).
func systemScopedKeyWarnings(f *ConfigFile) []ConfigWarning {
	var out []ConfigWarning
	for _, key := range systemOnlyKeys {
		v := getPath(f.raw, key)
		if v == nil {
			continue
		}
		if obj, ok := v.(map[string]any); ok {
			// scan and server: name each leaf, not just the object.
			for _, leaf := range sortedKeys(obj) {
				out = append(out, ConfigWarning{
					File: f.Path, Key: key + "." + leaf,
					Message: "system-scoped; ignored in a project config",
				})
			}
			continue
		}
		out = append(out, ConfigWarning{
			File: f.Path, Key: key,
			Message: "system-scoped; ignored in a project config",
		})
	}
	return out
}

// applyConfig folds one file over the typed view.
func applyConfig(cfg *Config, f *ConfigFile) []ConfigWarning {
	var w []ConfigWarning
	project := f.Scope == ScopeProject
	get := func(key string) any {
		if project && isSystemOnlyKey(key) {
			return nil // already reported by systemScopedKeyWarnings
		}
		return getPath(f.raw, key)
	}

	str := func(key string, dst *string, allowed ...string) {
		v := get(key)
		if v == nil {
			return
		}
		s, ok := v.(string)
		if !ok {
			w = append(w, ConfigWarning{f.Path, key, "want a string"})
			return
		}
		if len(allowed) > 0 && !containsString(allowed, s) {
			w = append(w, ConfigWarning{f.Path, key,
				fmt.Sprintf("%q is not one of %s", s, strings.Join(allowed, ", "))})
			return
		}
		*dst = s
	}
	num := func(key string, dst *int) {
		v := get(key)
		if v == nil {
			return
		}
		n, ok := jsonInt(v)
		if !ok {
			w = append(w, ConfigWarning{f.Path, key, "want a whole number"})
			return
		}
		*dst = n
	}
	boolean := func(key string, dst *bool) {
		v := get(key)
		if v == nil {
			return
		}
		b, ok := v.(bool)
		if !ok {
			w = append(w, ConfigWarning{f.Path, key, "want true or false"})
			return
		}
		*dst = b
	}
	strs := func(key string, dst *[]string) {
		v := get(key)
		if v == nil {
			return
		}
		arr, ok := v.([]any)
		if !ok {
			w = append(w, ConfigWarning{f.Path, key, "want a list of strings"})
			return
		}
		out := make([]string, 0, len(arr))
		for i, e := range arr {
			s, ok := e.(string)
			if !ok {
				w = append(w, ConfigWarning{f.Path, fmt.Sprintf("%s[%d]", key, i), "want a string"})
				continue
			}
			out = append(out, s)
		}
		*dst = out
	}

	str("theme.id", &cfg.Theme.ID)

	num("ui.recentCount", &cfg.UI.RecentCount)
	num("ui.favoritesCount", &cfg.UI.FavoritesCount)
	num("ui.recentMaxStored", &cfg.UI.RecentMaxStored)
	str("ui.density", &cfg.UI.Density, "compact", "normal", "comfortable")
	str("ui.defaultView", &cfg.UI.DefaultView, "board", "report", "check", "settings")
	num("ui.pollIntervalMs", &cfg.UI.PollIntervalMs)
	boolean("ui.confirmRemove", &cfg.UI.ConfirmRemove)
	strs("ui.board.collapsedStages", &cfg.UI.Board.CollapsedStages)
	num("ui.board.doneLimit", &cfg.UI.Board.DoneLimit)

	str("report.period", &cfg.Report.Period)
	if v := get("report.groupBy"); v != nil {
		if s, ok := v.(string); !ok {
			w = append(w, ConfigWarning{f.Path, "report.groupBy", "want a string"})
		} else if g, err := ParseGroupBy(s); err != nil {
			w = append(w, ConfigWarning{f.Path, "report.groupBy", err.Error()})
		} else {
			cfg.Report.GroupBy = g
		}
	}
	boolean("report.includeWip", &cfg.Report.IncludeWip)

	strs("scan.roots", &cfg.Scan.Roots)
	num("scan.maxDepth", &cfg.Scan.MaxDepth)
	boolean("scan.followSymlinks", &cfg.Scan.FollowSymlinks)
	boolean("scan.includeHidden", &cfg.Scan.IncludeHidden)
	strs("scan.excludes", &cfg.Scan.Excludes)
	num("scan.maxResults", &cfg.Scan.MaxResults)
	num("scan.timeoutMs", &cfg.Scan.TimeoutMs)
	boolean("scan.rescanOnFocus", &cfg.Scan.RescanOnFocus)

	// tickler.interval is a duration string, and a value that does not parse is
	// a warning rather than a refusal: the service still starts, with the
	// tickler off, and says why (§2.4's opt-in must not silently become a
	// different opt-in).
	if v := get("tickler.interval"); v != nil {
		if s, ok := v.(string); ok {
			if _, err := time.ParseDuration(s); err != nil {
				w = append(w, ConfigWarning{f.Path, "tickler.interval",
					fmt.Sprintf("%q is not a duration: %v", s, err)})
			} else {
				cfg.Tickler.Interval = s
			}
		} else if v != nil {
			// null is the documented "off" value and is not a mistake.
			if _, isNull := v.(nilValue); !isNull {
				w = append(w, ConfigWarning{f.Path, "tickler.interval", "want a duration like \"1m\" or null"})
			}
		}
	}

	str("server.bind", &cfg.Server.Bind)
	num("server.port", &cfg.Server.Port)
	boolean("server.allowRemote", &cfg.Server.AllowRemote)
	if v := get("server.socket"); v != nil {
		if s, ok := v.(string); ok {
			cfg.Server.Socket = s
		} else if v != nil {
			// null is the documented "no socket" value and is not a mistake.
			if _, isNull := v.(nilValue); !isNull {
				w = append(w, ConfigWarning{f.Path, "server.socket", "want a path or null"})
			}
		}
	}

	return w
}

// clampConfig holds the ranges the settings view exposes (spec-gui.md §5.9).
func clampConfig(cfg *Config) []ConfigWarning {
	var w []ConfigWarning
	clamp := func(key string, v *int, lo, hi int) {
		if *v < lo {
			w = append(w, ConfigWarning{"", key, fmt.Sprintf("%d is below the minimum %d", *v, lo)})
			*v = lo
		} else if hi > 0 && *v > hi {
			w = append(w, ConfigWarning{"", key, fmt.Sprintf("%d is above the maximum %d", *v, hi)})
			*v = hi
		}
	}
	clamp("ui.recentCount", &cfg.UI.RecentCount, 0, 50)
	clamp("ui.favoritesCount", &cfg.UI.FavoritesCount, 0, 50)
	clamp("ui.recentMaxStored", &cfg.UI.RecentMaxStored, 0, 0)
	clamp("ui.pollIntervalMs", &cfg.UI.PollIntervalMs, 100, 0)
	clamp("server.port", &cfg.Server.Port, 1, 65535)
	return w
}

// ---------------------------------------------------------------------------
// Dotted-path helpers over a decoded JSON document
// ---------------------------------------------------------------------------

// nilValue distinguishes an explicit JSON null from an absent key, which matters
// for server.socket.
type nilValue struct{}

func getPath(doc map[string]any, key string) any {
	parts := strings.Split(key, ".")
	var cur any = doc
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		v, present := obj[p]
		if !present {
			return nil
		}
		if v == nil {
			return nilValue{}
		}
		cur = v
	}
	return cur
}

func setPath(doc map[string]any, key string, value any) {
	parts := strings.Split(key, ".")
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func unsetPath(doc map[string]any, key string) {
	parts := strings.Split(key, ".")
	cur := doc
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return
		}
		cur = next
	}
	delete(cur, parts[len(parts)-1])
}

func isSystemOnlyKey(key string) bool {
	for _, k := range systemOnlyKeys {
		if key == k || strings.HasPrefix(key, k+".") {
			return true
		}
	}
	return false
}

// jsonInt accepts the float64 encoding/json produces for a whole number, and
// rejects a fraction rather than truncating one.
func jsonInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int(n)) {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
