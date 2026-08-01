package mm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Theming (spec-gui.md §8).
//
// A theme lives with the project, so the look of the screen tells the user which
// project they are in before they read a word (§8.1). Resolution, the token
// taxonomy and the built-in default live here rather than in a front end because
// the GUI and the TUI must read the same files with the same precedence and
// agree on every value (§8.5, §13).
//
// A theme.json inside a micro-manager directory is outside spec-file-format.md
// §1: format readers and the checker MUST ignore it, and it never participates
// in I1-I10 (§8.2).

// ThemeSchemaVersion is the schema this implementation reads and writes.
// A file declaring anything else is rejected by name (§8.8 rule 1).
const ThemeSchemaVersion = 1

// ThemeSource says which layer of §8.7 supplied the theme.
type ThemeSource string

const (
	ThemeSourceProject ThemeSource = "project"
	ThemeSourceSystem  ThemeSource = "system"
	ThemeSourceBuiltin ThemeSource = "builtin"
)

// Appearance values (§8.2). Auto REQUIRES both colour sets and follows
// prefers-color-scheme.
const (
	AppearanceLight = "light"
	AppearanceDark  = "dark"
	AppearanceAuto  = "auto"
)

// ColorTokens is the shared contract with the TUI (§8.3). Every token is
// REQUIRED; a theme missing one inherits it from the built-in default rather
// than being rejected.
//
// The set is intentionally small and semantic: it is the largest palette a
// terminal can render faithfully. Nothing may be added here that a terminal
// cannot express - gradients, shadows and opacity effects belong in gui.
func ColorTokens() []string {
	return []string{
		"bg.base", "bg.raised", "bg.sunken", "bg.overlay",
		"fg.default", "fg.muted", "fg.subtle", "fg.inverted",
		"border.default", "border.strong", "border.focus",
		"accent.base", "accent.fg", "accent.muted",
		"state.ready", "state.blocked", "state.someday", "state.working", "state.done",
		"prio.high", "prio.med", "prio.low",
		"feedback.success", "feedback.warning", "feedback.danger", "feedback.info",
		"selection.bg", "selection.fg",
		"drag.valid", "drag.invalid",
	}
}

// GUITokens is the presentation half, ignored by the TUI (§8.3).
func GUITokens() []string {
	return []string{
		"font.family.ui", "font.family.mono",
		"font.size.xs", "font.size.sm", "font.size.md", "font.size.lg", "font.size.xl",
		"font.weight.normal", "font.weight.medium", "font.weight.bold",
		"font.lineHeight.tight", "font.lineHeight.normal", "font.lineHeight.loose",
		"space.0", "space.1", "space.2", "space.3", "space.4", "space.5", "space.6", "space.8",
		"radius.none", "radius.sm", "radius.md", "radius.lg", "radius.full",
		"border.width.thin", "border.width.thick",
		"shadow.none", "shadow.sm", "shadow.md", "shadow.lg",
		"motion.duration.fast", "motion.duration.normal", "motion.duration.slow",
		"motion.easing.standard", "motion.easing.enter", "motion.easing.exit",
		"density.compact", "density.normal", "density.comfortable",
	}
}

// CSSPropertyName maps a token path to its custom property (§8.4):
//
//	color.bg.base            -> --mm-color-bg-base
//	gui.space.3              -> --mm-space-3
//	gui.font.lineHeight.tight -> --mm-font-lineheight-tight
//
// color.* keeps its group in the name; each gui.<group>.* drops the gui.
// The whole name is lowercased, which is why lineHeight becomes lineheight.
func CSSPropertyName(tokenPath string) string {
	p := strings.TrimPrefix(tokenPath, "gui.")
	return "--mm-" + strings.ToLower(strings.ReplaceAll(p, ".", "-"))
}

// CSSProperty is one custom property ready to write onto the app root.
type CSSProperty struct {
	Name  string // --mm-color-bg-base
	Value string
}

// ---------------------------------------------------------------------------
// The file
// ---------------------------------------------------------------------------

// Theme is one theme file, held with the document it was read from.
//
// Unknown top-level keys and unknown keys inside extra MUST be preserved on
// read-modify-write (§8.2): a theme editor that silently drops what it does not
// understand cannot round-trip a theme from a newer implementation. Keeping the
// raw document is how that is guaranteed rather than remembered.
type Theme struct {
	Path string

	ID         string
	Name       string
	Author     string
	Appearance string

	Brand     map[string]string
	Color     map[string]string
	ColorDark map[string]string
	GUI       map[string]string

	raw map[string]any
}

// LoadTheme reads a theme file. A missing file returns nil with no error: a
// theme is optional at every level of §8.7.
func LoadTheme(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}
	return ParseTheme(path, data)
}

// ParseTheme decodes a theme document.
//
// A schemaVersion this implementation does not implement is rejected by name
// (§8.8 rule 1), and a rejected theme changes nothing anywhere: this returns an
// error rather than a half-applied value.
func ParseTheme(path string, data []byte) (*Theme, error) {
	t := &Theme{Path: path, raw: map[string]any{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return t, nil
	}
	if err := json.Unmarshal(data, &t.raw); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidArgument, path, err)
	}
	if v, ok := t.raw["schemaVersion"]; ok {
		n, ok := jsonInt(v)
		if !ok || n != ThemeSchemaVersion {
			return nil, fmt.Errorf("%w: %s: schemaVersion %v is not supported; this implementation reads version %d",
				ErrInvalidArgument, path, v, ThemeSchemaVersion)
		}
	}

	t.ID, _ = t.raw["id"].(string)
	t.Name, _ = t.raw["name"].(string)
	t.Author, _ = t.raw["author"].(string)
	t.Appearance, _ = t.raw["appearance"].(string)
	t.Brand = flatStrings(getPath(t.raw, "brand"), "")
	t.Color = flatStrings(getPath(t.raw, "color"), "")
	t.ColorDark = flatStrings(getPath(t.raw, "colorDark"), "")
	t.GUI = flatStrings(getPath(t.raw, "gui"), "")
	return t, nil
}

// document folds the parsed fields back into the raw document and returns it.
//
// raw IS the file: it holds every key, including the ones this implementation
// does not understand, which is how §8.2's round-trip rule is kept. This only
// overwrites the keys the struct owns, so unknown ones survive untouched.
//
// Save and Bytes BOTH go through here. They used to differ - Save folded the
// fields in, Bytes marshalled raw alone - so a theme built in Go rather than
// parsed from a file rendered as "{}". That is what exporting the built-in
// theme did: it served an empty document (§8.8 requires a self-contained one).
func (t *Theme) document() map[string]any {
	if t.raw == nil {
		t.raw = map[string]any{}
	}
	t.raw["schemaVersion"] = ThemeSchemaVersion
	setIfNotEmpty(t.raw, "id", t.ID)
	setIfNotEmpty(t.raw, "name", t.Name)
	setIfNotEmpty(t.raw, "author", t.Author)
	setIfNotEmpty(t.raw, "appearance", t.Appearance)
	writeNested(t.raw, "brand", t.Brand)
	writeNested(t.raw, "color", t.Color)
	writeNested(t.raw, "colorDark", t.ColorDark)
	writeNested(t.raw, "gui", t.GUI)
	return t.raw
}

// Save writes the theme atomically, preserving every key it did not understand.
func (t *Theme) Save(path string, dryRun bool) error {
	data, err := marshalJSONFile(t.document())
	if err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrIO, filepath.Dir(path), err)
	}
	if err := writeFileAtomic(path, data); err != nil {
		return err
	}
	t.Path = path
	return nil
}

// Bytes renders the theme as it would be written — the same document Save
// produces, which is what makes an export re-importable.
func (t *Theme) Bytes() ([]byte, error) { return marshalJSONFile(t.document()) }

// Validate reports every problem in a theme, by token path (§8.8 rule 2).
//
// It reports rather than fails: a theme with one bad colour still resolves,
// because every missing or invalid token falls back to the built-in default per
// token (§8.7). Import is the caller that turns these into a refusal.
func (t *Theme) Validate() []ConfigWarning {
	var out []ConfigWarning
	if t == nil {
		return nil
	}
	check := func(group string, set map[string]string) {
		for _, token := range sortedStringKeys(set) {
			path := group + "." + token
			if err := ValidateColor(set[token]); err != nil {
				out = append(out, ConfigWarning{t.Path, path, err.Error()})
			}
			if !containsString(ColorTokens(), token) {
				out = append(out, ConfigWarning{t.Path, path, "not a colour token this implementation knows"})
			}
		}
	}
	check("color", t.Color)
	check("colorDark", t.ColorDark)

	switch t.Appearance {
	case "", AppearanceLight, AppearanceDark:
	case AppearanceAuto:
		// §8.2: auto REQUIRES both sets, because it follows prefers-color-scheme
		// and half of it would be the other half's colours in the wrong mode.
		if len(t.Color) == 0 || len(t.ColorDark) == 0 {
			out = append(out, ConfigWarning{t.Path, "appearance",
				"auto requires both color and colorDark"})
		}
	default:
		out = append(out, ConfigWarning{t.Path, "appearance",
			fmt.Sprintf("%q is not light, dark or auto", t.Appearance)})
	}

	for _, token := range sortedStringKeys(t.GUI) {
		if !containsString(GUITokens(), token) {
			out = append(out, ConfigWarning{t.Path, "gui." + token,
				"not a gui token this implementation knows"})
		}
	}
	return out
}

// ValidateColor accepts #rrggbb and #rrggbbaa (§8.3).
func ValidateColor(v string) error {
	if len(v) != 7 && len(v) != 9 {
		return fmt.Errorf("%w: %q is not #rrggbb or #rrggbbaa", ErrInvalidArgument, v)
	}
	if v[0] != '#' {
		return fmt.Errorf("%w: %q does not start with #", ErrInvalidArgument, v)
	}
	for _, r := range v[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return fmt.Errorf("%w: %q is not hexadecimal", ErrInvalidArgument, v)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// ThemeRequest is what §8.7 needs to resolve a theme.
//
// Both ids come from the merged configuration; both paths are already absolute,
// because expanding them is the front end's job.
type ThemeRequest struct {
	ProjectDir     string // the micro-manager directory; may be empty
	ConfigHome     string // as returned by ConfigHome; may be empty
	ProjectThemeID string // project config theme.id
	SystemThemeID  string // system config theme.id
}

// ResolvedTheme is a complete theme: every token present, nothing to fall back
// to at render time.
type ResolvedTheme struct {
	ID         string
	Name       string
	Source     ThemeSource
	Path       string // the file it came from; empty for the built-in
	Appearance string

	Brand     map[string]string
	Color     map[string]string
	ColorDark map[string]string
	GUI       map[string]string
}

// ResolveTheme applies the precedence of §8.7, first hit wins:
//
//  1. <micro-manager dir>/theme.json
//  2. the theme named by project config theme.id, from the theme library
//  3. $XDG_CONFIG_HOME/micro-manager/theme.json
//  4. the theme named by system config theme.id
//  5. the built-in default
//
// Missing tokens fall through to the built-in PER TOKEN, not per file: a project
// theme that sets only accent.base and brand is valid and common.
//
// Warnings name any theme file that could not be read or that carries values
// this implementation does not understand. A broken theme never prevents a
// resolution - the UI must still render.
func ResolveTheme(req ThemeRequest) (ResolvedTheme, []ConfigWarning, error) {
	var warnings []ConfigWarning

	type candidate struct {
		path   string
		source ThemeSource
	}
	var candidates []candidate

	if req.ProjectDir != "" {
		candidates = append(candidates, candidate{ProjectThemePath(req.ProjectDir), ThemeSourceProject})
	}
	if req.ConfigHome != "" {
		sp := NewSystemPaths(req.ConfigHome)
		if req.ProjectThemeID != "" {
			// Named by the PROJECT, so the source is the project even though the
			// file lives in the system library.
			candidates = append(candidates, candidate{ThemeLibraryPath(sp, req.ProjectThemeID), ThemeSourceProject})
		}
		candidates = append(candidates, candidate{sp.Theme, ThemeSourceSystem})
		if req.SystemThemeID != "" {
			candidates = append(candidates, candidate{ThemeLibraryPath(sp, req.SystemThemeID), ThemeSourceSystem})
		}
	}

	base := builtinResolved()
	for _, c := range candidates {
		t, err := LoadTheme(c.path)
		if err != nil {
			warnings = append(warnings, ConfigWarning{c.path, "", err.Error()})
			continue
		}
		if t == nil {
			continue
		}
		warnings = append(warnings, t.Validate()...)
		return overlay(base, t, c.source), warnings, nil
	}
	return base, warnings, nil
}

// ThemeLibraryPath is $XDG_CONFIG_HOME/micro-manager/themes/<themeId>.json.
func ThemeLibraryPath(sp SystemPaths, themeID string) string {
	return filepath.Join(sp.Themes, themeID+".json")
}

// ValidThemeID matches [a-z0-9][a-z0-9-]{0,63} (spec-gui.md §3.3).
func ValidThemeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// overlay lays a theme over the built-in default, token by token.
func overlay(base ResolvedTheme, t *Theme, source ThemeSource) ResolvedTheme {
	out := base
	out.Source = source
	out.Path = t.Path
	if t.ID != "" {
		out.ID = t.ID
	}
	if t.Name != "" {
		out.Name = t.Name
	}
	if t.Appearance != "" {
		out.Appearance = t.Appearance
	}
	out.Brand = mergeTokens(base.Brand, t.Brand)
	out.Color = mergeKnownTokens(base.Color, t.Color, ColorTokens())
	out.ColorDark = mergeKnownTokens(base.ColorDark, t.ColorDark, ColorTokens())
	out.GUI = mergeKnownTokens(base.GUI, t.GUI, GUITokens())

	// A theme that sets only `color` and declares itself dark has no colorDark
	// to fall back on but is not auto, so the one set it has is the one to use.
	if t.Appearance == AppearanceDark && len(t.Color) > 0 && len(t.ColorDark) == 0 {
		out.ColorDark = out.Color
	}
	return out
}

// mergeKnownTokens overlays only tokens this implementation knows. An unknown
// token was already reported by Validate; rendering it as a custom property
// would put an unnamed value on the app root that no stylesheet reads.
func mergeKnownTokens(base, over map[string]string, known []string) map[string]string {
	out := make(map[string]string, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if containsString(known, k) && ValidateColorOrValue(k, v) {
			out[k] = v
		}
	}
	return out
}

// ValidateColorOrValue accepts any non-empty gui value and only well-formed
// colours for colour tokens. It is the last gate before a value reaches the DOM.
func ValidateColorOrValue(token, value string) bool {
	if value == "" {
		return false
	}
	if containsString(ColorTokens(), token) {
		return ValidateColor(value) == nil
	}
	return true
}

func mergeTokens(base, over map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

// Properties renders the custom properties for the app root (§8.4).
//
// dark selects colorDark. Order is stable - colour tokens then gui tokens, each
// in the order of the taxonomy - so a rendered style block diffs cleanly and a
// test can assert on it.
func (r ResolvedTheme) Properties(dark bool) []CSSProperty {
	colors := r.Color
	if dark {
		colors = r.ColorDark
	}
	out := make([]CSSProperty, 0, len(ColorTokens())+len(GUITokens()))
	for _, token := range ColorTokens() {
		if v, ok := colors[token]; ok {
			out = append(out, CSSProperty{CSSPropertyName("color." + token), v})
		}
	}
	for _, token := range GUITokens() {
		if v, ok := r.GUI[token]; ok {
			out = append(out, CSSProperty{CSSPropertyName("gui." + token), v})
		}
	}
	return out
}

// ContrastPair is one pair §11 rule 7 requires to meet WCAG AA.
type ContrastPair struct {
	Foreground string // token path
	Background string
	Ratio      float64
	Passes     bool
}

// ContrastAA is the WCAG AA threshold for normal text.
const ContrastAA = 4.5

// CheckContrast measures the two pairs §11 rule 7 names.
//
// The theme editor MUST warn when a pair fails and MUST NOT block saving: a
// theme is the user's, and a tool that refuses to save one is a tool they will
// edit the file behind the back of.
func (r ResolvedTheme) CheckContrast(dark bool) []ContrastPair {
	colors := r.Color
	if dark {
		colors = r.ColorDark
	}
	pairs := []ContrastPair{
		{Foreground: "fg.default", Background: "bg.base"},
		{Foreground: "accent.fg", Background: "accent.base"},
	}
	for i := range pairs {
		ratio, err := ContrastRatio(colors[pairs[i].Foreground], colors[pairs[i].Background])
		if err != nil {
			continue
		}
		pairs[i].Ratio = ratio
		pairs[i].Passes = ratio >= ContrastAA
	}
	return pairs
}

// ContrastRatio is the WCAG 2.1 contrast ratio of two colours.
//
// Alpha is ignored: a ratio needs a composited colour, and what a translucent
// value composites against is a layout question the library cannot answer.
func ContrastRatio(a, b string) (float64, error) {
	la, err := relativeLuminance(a)
	if err != nil {
		return 0, err
	}
	lb, err := relativeLuminance(b)
	if err != nil {
		return 0, err
	}
	if lb > la {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), nil
}

func relativeLuminance(hex string) (float64, error) {
	if err := ValidateColor(hex); err != nil {
		return 0, err
	}
	component := func(s string) float64 {
		n, _ := strconv.ParseUint(s, 16, 8)
		c := float64(n) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r := component(hex[1:3])
	g := component(hex[3:5])
	b := component(hex[5:7])
	return 0.2126*r + 0.7152*g + 0.0722*b, nil
}

// ---------------------------------------------------------------------------
// The built-in default
// ---------------------------------------------------------------------------

// BuiltinTheme is the bottom of §8.7 and the per-token fallback for every layer
// above it. Every token in the taxonomy is present here, by definition: this is
// the file that makes "a theme setting only accent.base is valid" true.
func BuiltinTheme() *Theme {
	return &Theme{
		// The id every caller already advertises this theme under. It was
		// "mm-default", which nothing anywhere resolved by, so an exported
		// builtin carried an id no listing offered and no import could match.
		ID:         "micro-manager",
		Name:       "micro-manager",
		Appearance: AppearanceAuto,
		Brand: map[string]string{
			"name":   "micro-manager",
			"short":  "mm",
			"accent": "#1257c9",
		},
		Color:     builtinLight(),
		ColorDark: builtinDark(),
		GUI:       builtinGUI(),
		raw:       map[string]any{},
	}
}

func builtinResolved() ResolvedTheme {
	t := BuiltinTheme()
	return ResolvedTheme{
		ID:         t.ID,
		Name:       t.Name,
		Source:     ThemeSourceBuiltin,
		Appearance: t.Appearance,
		Brand:      t.Brand,
		Color:      t.Color,
		ColorDark:  t.ColorDark,
		GUI:        t.GUI,
	}
}

func builtinLight() map[string]string {
	return map[string]string{
		"bg.base":    "#ffffff",
		"bg.raised":  "#f6f8fa",
		"bg.sunken":  "#eaeef2",
		"bg.overlay": "#ffffff",

		"fg.default":  "#1f2328",
		"fg.muted":    "#59636e",
		"fg.subtle":   "#6e7781",
		"fg.inverted": "#ffffff",

		"border.default": "#d1d9e0",
		"border.strong":  "#8c959f",
		"border.focus":   "#1257c9",

		"accent.base":  "#1257c9",
		"accent.fg":    "#ffffff",
		"accent.muted": "#ddf4ff",

		"state.ready":   "#1257c9",
		"state.blocked": "#a40e26",
		"state.someday": "#6e7781",
		"state.working": "#9a6700",
		"state.done":    "#1a7f37",

		"prio.high": "#a40e26",
		"prio.med":  "#9a6700",
		"prio.low":  "#6e7781",

		"feedback.success": "#1a7f37",
		"feedback.warning": "#9a6700",
		"feedback.danger":  "#a40e26",
		"feedback.info":    "#1257c9",

		"selection.bg": "#ddf4ff",
		"selection.fg": "#1f2328",

		"drag.valid":   "#1a7f37",
		"drag.invalid": "#a40e26",
	}
}

func builtinDark() map[string]string {
	return map[string]string{
		"bg.base":    "#0d1117",
		"bg.raised":  "#151b23",
		"bg.sunken":  "#010409",
		"bg.overlay": "#151b23",

		"fg.default":  "#e6edf3",
		"fg.muted":    "#9198a1",
		"fg.subtle":   "#7d8590",
		"fg.inverted": "#0d1117",

		"border.default": "#3d444d",
		"border.strong":  "#656c76",
		"border.focus":   "#4493f8",

		"accent.base":  "#4493f8",
		"accent.fg":    "#0d1117",
		"accent.muted": "#121d2f",

		"state.ready":   "#4493f8",
		"state.blocked": "#ff8080",
		"state.someday": "#9198a1",
		"state.working": "#e3b341",
		"state.done":    "#3fb950",

		"prio.high": "#ff8080",
		"prio.med":  "#e3b341",
		"prio.low":  "#9198a1",

		"feedback.success": "#3fb950",
		"feedback.warning": "#e3b341",
		"feedback.danger":  "#ff8080",
		"feedback.info":    "#4493f8",

		"selection.bg": "#121d2f",
		"selection.fg": "#e6edf3",

		"drag.valid":   "#3fb950",
		"drag.invalid": "#ff8080",
	}
}

func builtinGUI() map[string]string {
	return map[string]string{
		"font.family.ui":   "system-ui, -apple-system, Segoe UI, Roboto, sans-serif",
		"font.family.mono": "ui-monospace, SFMono-Regular, Menlo, monospace",

		"font.size.xs": "0.75rem",
		"font.size.sm": "0.875rem",
		"font.size.md": "1rem",
		"font.size.lg": "1.25rem",
		"font.size.xl": "1.5rem",

		"font.weight.normal": "400",
		"font.weight.medium": "500",
		"font.weight.bold":   "700",

		"font.lineHeight.tight":  "1.25",
		"font.lineHeight.normal": "1.5",
		"font.lineHeight.loose":  "1.75",

		"space.0": "0",
		"space.1": "0.25rem",
		"space.2": "0.5rem",
		"space.3": "0.75rem",
		"space.4": "1rem",
		"space.5": "1.5rem",
		"space.6": "2rem",
		"space.8": "3rem",

		"radius.none": "0",
		"radius.sm":   "3px",
		"radius.md":   "6px",
		"radius.lg":   "12px",
		"radius.full": "9999px",

		"border.width.thin":  "1px",
		"border.width.thick": "2px",

		"shadow.none": "none",
		"shadow.sm":   "0 1px 2px rgba(0,0,0,0.12)",
		"shadow.md":   "0 3px 6px rgba(0,0,0,0.16)",
		"shadow.lg":   "0 10px 24px rgba(0,0,0,0.20)",

		"motion.duration.fast":   "80ms",
		"motion.duration.normal": "160ms",
		"motion.duration.slow":   "320ms",

		"motion.easing.standard": "cubic-bezier(0.2, 0, 0.38, 0.9)",
		"motion.easing.enter":    "cubic-bezier(0, 0, 0.38, 0.9)",
		"motion.easing.exit":     "cubic-bezier(0.2, 0, 1, 0.9)",

		"density.compact":     "0.75",
		"density.normal":      "1",
		"density.comfortable": "1.25",
	}
}

// ---------------------------------------------------------------------------
// Helpers over the raw document
// ---------------------------------------------------------------------------

// flatStrings flattens a nested JSON object into dotted token paths, keeping
// only string leaves. A theme's nesting is presentation; the token path is the
// identity, and flattening once here means nothing downstream walks the tree.
func flatStrings(v any, prefix string) map[string]string {
	out := map[string]string{}
	obj, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, val := range obj {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch t := val.(type) {
		case string:
			out[key] = t
		case float64:
			out[key] = strconv.FormatFloat(t, 'f', -1, 64)
		case map[string]any:
			for kk, vv := range flatStrings(t, key) {
				out[kk] = vv
			}
		}
	}
	return out
}

// writeNested puts dotted token paths back as nested objects, leaving any
// sibling key the theme carried in place.
func writeNested(doc map[string]any, group string, tokens map[string]string) {
	if len(tokens) == 0 {
		return
	}
	obj, _ := doc[group].(map[string]any)
	if obj == nil {
		obj = map[string]any{}
	}
	for _, token := range sortedStringKeys(tokens) {
		setPath(obj, token, tokens[token])
	}
	doc[group] = obj
}

func setIfNotEmpty(doc map[string]any, key, value string) {
	if value != "" {
		doc[key] = value
	}
}

func sortedStringKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
