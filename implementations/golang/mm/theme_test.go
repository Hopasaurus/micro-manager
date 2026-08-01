package mm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const specGUI = "../../../project/spec-gui.md"

// Appendix B of spec-gui.md is a literal index of custom property names. Reading
// it from the spec rather than from a copy is the point: a copied list drifts,
// and the drift is invisible until an external suite fails.
func TestCSSPropertyNamesMatchTheSpecIndex(t *testing.T) {
	want := appendixBProperties(t)
	if len(want) < 40 {
		t.Fatalf("only %d names parsed out of Appendix B; the parser is broken", len(want))
	}

	emitted := map[string]bool{}
	for _, token := range ColorTokens() {
		emitted[CSSPropertyName("color."+token)] = true
	}
	for _, token := range GUITokens() {
		emitted[CSSPropertyName("gui."+token)] = true
	}

	for _, name := range want {
		if !emitted[name] {
			t.Errorf("Appendix B requires %s and nothing emits it", name)
		}
	}
}

// appendixBProperties parses the fenced block under "Appendix B", expanding the
// brace shorthand the spec writes ranges with.
func appendixBProperties(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(specGUI)
	if err != nil {
		t.Skipf("the spec is not readable from here: %v", err)
	}
	text := string(data)
	i := strings.Index(text, "## Appendix B")
	if i < 0 {
		t.Fatal("Appendix B not found in the spec")
	}
	block := text[i:]
	start := strings.Index(block, "```")
	end := strings.Index(block[start+3:], "```")
	if start < 0 || end < 0 {
		t.Fatal("Appendix B has no fenced block")
	}
	body := block[start+3 : start+3+end]

	brace := regexp.MustCompile(`^([^{]+)\{([^}]*)\}$`)
	var out []string
	for _, field := range strings.Fields(body) {
		if !strings.HasPrefix(field, "--mm-") {
			continue
		}
		if m := brace.FindStringSubmatch(field); m != nil {
			for _, suffix := range strings.Split(m[2], ",") {
				out = append(out, m[1]+strings.TrimSpace(suffix))
			}
			continue
		}
		out = append(out, field)
	}
	return out
}

// The §8.4 examples, spelled out. lineHeight lowercases; gui loses its group.
func TestCSSPropertyName(t *testing.T) {
	cases := map[string]string{
		"color.bg.base":             "--mm-color-bg-base",
		"color.prio.high":           "--mm-color-prio-high",
		"gui.space.3":               "--mm-space-3",
		"gui.font.size.md":          "--mm-font-size-md",
		"gui.radius.lg":             "--mm-radius-lg",
		"gui.motion.duration.fast":  "--mm-motion-duration-fast",
		"gui.font.lineHeight.tight": "--mm-font-lineheight-tight",
	}
	for token, want := range cases {
		if got := CSSPropertyName(token); got != want {
			t.Errorf("CSSPropertyName(%q) = %q, want %q", token, got, want)
		}
	}
}

// Every token is REQUIRED, and the built-in is what every other layer falls back
// to per token - so a gap here is a gap everywhere (§8.3, §8.7).
func TestBuiltinThemeIsComplete(t *testing.T) {
	b := BuiltinTheme()
	for _, token := range ColorTokens() {
		if v := b.Color[token]; v == "" {
			t.Errorf("built-in light theme has no color.%s", token)
		} else if err := ValidateColor(v); err != nil {
			t.Errorf("color.%s: %v", token, err)
		}
		if v := b.ColorDark[token]; v == "" {
			t.Errorf("built-in dark theme has no colorDark.%s", token)
		} else if err := ValidateColor(v); err != nil {
			t.Errorf("colorDark.%s: %v", token, err)
		}
	}
	for _, token := range GUITokens() {
		if b.GUI[token] == "" {
			t.Errorf("built-in theme has no gui.%s", token)
		}
	}
	if len(b.Validate()) != 0 {
		t.Errorf("the built-in theme does not validate: %v", b.Validate())
	}
}

// §8.8: an export is "one self-contained JSON file". Bytes marshalled the raw
// document alone while Save folded the parsed fields into it first, so a theme
// built in Go rather than read from a file rendered as "{}" — exporting the
// built-in downloaded an empty document under a .mm-theme.json filename.
func TestBuiltinThemeExportsItself(t *testing.T) {
	data, err := BuiltinTheme().Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}

	// It must survive the round trip it exists for: export, re-import, same theme.
	back, err := ParseTheme("exported.json", data)
	if err != nil {
		t.Fatalf("the exported built-in does not parse: %v\n%s", err, data)
	}
	if back.ID != BuiltinTheme().ID {
		t.Errorf("exported id = %q, want %q", back.ID, BuiltinTheme().ID)
	}
	if back.Name != BuiltinTheme().Name {
		t.Errorf("exported name = %q, want %q", back.Name, BuiltinTheme().Name)
	}
	if back.Appearance != BuiltinTheme().Appearance {
		t.Errorf("exported appearance = %q, want %q", back.Appearance, BuiltinTheme().Appearance)
	}
	for _, token := range ColorTokens() {
		if back.Color[token] == "" {
			t.Errorf("exported theme lost color.%s", token)
		}
		if back.ColorDark[token] == "" {
			t.Errorf("exported theme lost colorDark.%s", token)
		}
	}
	if len(back.Validate()) != 0 {
		t.Errorf("the exported built-in does not validate: %v", back.Validate())
	}
}

// spec-gui.md §11 rule 7: fg.default/bg.base and accent.fg/accent.base MUST meet
// WCAG AA. The editor only warns, so the built-in has to be right by itself.
func TestBuiltinThemeMeetsContrastAA(t *testing.T) {
	r := builtinResolved()
	for _, dark := range []bool{false, true} {
		for _, pair := range r.CheckContrast(dark) {
			if !pair.Passes {
				t.Errorf("dark=%v: %s on %s is %.2f:1, below AA %.1f:1",
					dark, pair.Foreground, pair.Background, pair.Ratio, ContrastAA)
			}
		}
	}
}

func TestContrastRatio(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"#ffffff", "#000000", 21},
		{"#ffffff", "#ffffff", 1},
		{"#000000", "#ffffff", 21}, // order does not matter
	}
	for _, c := range cases {
		got, err := ContrastRatio(c.a, c.b)
		if err != nil {
			t.Fatal(err)
		}
		if got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("ContrastRatio(%s, %s) = %.3f, want %.1f", c.a, c.b, got, c.want)
		}
	}
	if _, err := ContrastRatio("blue", "#ffffff"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("a named colour should be rejected, got %v", err)
	}
}

func TestValidateColor(t *testing.T) {
	ok := []string{"#000000", "#ffffff", "#AABBCC", "#12345678"}
	bad := []string{"", "#fff", "fff000", "#gggggg", "#1234567", "rgb(0,0,0)"}
	for _, v := range ok {
		if err := ValidateColor(v); err != nil {
			t.Errorf("ValidateColor(%q) = %v, want nil", v, err)
		}
	}
	for _, v := range bad {
		if err := ValidateColor(v); err == nil {
			t.Errorf("ValidateColor(%q) = nil, want an error", v)
		}
	}
}

// spec-gui.md §8.7: resolution order, first hit wins.
func TestThemeResolutionOrder(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	sp := NewSystemPaths(home)

	writeJSON(t, ThemeLibraryPath(sp, "sys-lib"), `{"schemaVersion":1,"id":"sys-lib","name":"System Library","color":{"accent":{"base":"#040404"}}}`)
	req := ThemeRequest{ProjectDir: project, ConfigHome: home, SystemThemeID: "sys-lib"}

	// 4. the theme named by system config theme.id
	got, _, err := ResolveTheme(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "System Library" || got.Source != ThemeSourceSystem {
		t.Errorf("step 4: got %q from %q", got.Name, got.Source)
	}

	// 3. the system theme.json outranks it
	writeJSON(t, sp.Theme, `{"schemaVersion":1,"id":"sys","name":"System","color":{"accent":{"base":"#030303"}}}`)
	got, _, _ = ResolveTheme(req)
	if got.Name != "System" || got.Source != ThemeSourceSystem {
		t.Errorf("step 3: got %q from %q", got.Name, got.Source)
	}

	// 2. a theme named by the project config outranks both, and counts as the
	//    project's even though the file lives in the system library.
	writeJSON(t, ThemeLibraryPath(sp, "proj-lib"), `{"schemaVersion":1,"id":"proj-lib","name":"Project Library","color":{"accent":{"base":"#020202"}}}`)
	req.ProjectThemeID = "proj-lib"
	got, _, _ = ResolveTheme(req)
	if got.Name != "Project Library" || got.Source != ThemeSourceProject {
		t.Errorf("step 2: got %q from %q", got.Name, got.Source)
	}

	// 1. the project's own theme.json wins outright
	writeJSON(t, ProjectThemePath(project), `{"schemaVersion":1,"id":"proj","name":"Project","color":{"accent":{"base":"#010101"}}}`)
	got, _, _ = ResolveTheme(req)
	if got.Name != "Project" || got.Source != ThemeSourceProject {
		t.Errorf("step 1: got %q from %q", got.Name, got.Source)
	}
	if got.Color["accent.base"] != "#010101" {
		t.Errorf("accent.base = %q", got.Color["accent.base"])
	}
}

// 5. and with nothing anywhere, the built-in.
func TestThemeResolutionFallsBackToBuiltin(t *testing.T) {
	got, warnings, err := ResolveTheme(ThemeRequest{ProjectDir: t.TempDir(), ConfigHome: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if got.Source != ThemeSourceBuiltin {
		t.Errorf("source = %q, want builtin", got.Source)
	}
	if len(got.Properties(false)) != len(ColorTokens())+len(GUITokens()) {
		t.Errorf("the built-in resolution is not complete: %d properties", len(got.Properties(false)))
	}
}

// spec-gui.md §8.7: missing tokens fall through PER TOKEN, not per file. "A
// project theme that sets only accent.base and brand is valid and common."
func TestThemeFallbackIsPerToken(t *testing.T) {
	project := t.TempDir()
	writeJSON(t, ProjectThemePath(project), `{
	  "schemaVersion": 1,
	  "id": "sparse",
	  "name": "Sparse",
	  "brand": { "short": "SP" },
	  "color": { "accent": { "base": "#ff0000" } }
	}`)

	got, _, err := ResolveTheme(ThemeRequest{ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	builtin := BuiltinTheme()
	if got.Color["accent.base"] != "#ff0000" {
		t.Errorf("the theme's own token did not win: %q", got.Color["accent.base"])
	}
	for _, token := range ColorTokens() {
		if token == "accent.base" {
			continue
		}
		if got.Color[token] != builtin.Color[token] {
			t.Errorf("color.%s = %q, want the built-in %q", token, got.Color[token], builtin.Color[token])
		}
	}
	for _, token := range GUITokens() {
		if got.GUI[token] != builtin.GUI[token] {
			t.Errorf("gui.%s did not fall back", token)
		}
	}
	if got.Brand["short"] != "SP" || got.Brand["name"] == "" {
		t.Errorf("brand = %v, want the override plus the built-in's other keys", got.Brand)
	}
}

// A bad token is reported and ignored; the rest of the theme still applies.
func TestThemeWithABadTokenStillResolves(t *testing.T) {
	project := t.TempDir()
	writeJSON(t, ProjectThemePath(project), `{
	  "schemaVersion": 1,
	  "id": "broken",
	  "color": { "accent": { "base": "not-a-colour" }, "bg": { "base": "#101010" } }
	}`)

	got, warnings, err := ResolveTheme(ThemeRequest{ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarningFor(warnings, "color.accent.base") {
		t.Errorf("no warning names the bad token: %v", warnings)
	}
	if got.Color["accent.base"] != BuiltinTheme().Color["accent.base"] {
		t.Errorf("the bad value reached the DOM: %q", got.Color["accent.base"])
	}
	if got.Color["bg.base"] != "#101010" {
		t.Error("a sibling token was discarded along with the bad one")
	}
}

// spec-gui.md §8.8 rule 1: reject a schemaVersion this implementation does not
// implement, NAMING THE VERSION.
func TestThemeSchemaVersionIsChecked(t *testing.T) {
	_, err := ParseTheme("theme.json", []byte(`{"schemaVersion": 2, "id": "future"}`))
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("want ErrInvalidArgument, got %v", err)
	}
	if !strings.Contains(err.Error(), "2") {
		t.Errorf("the error must name the version, got %q", err)
	}
}

// The same preservation rule as the config file, for the same reason (§8.2).
func TestThemeUnknownKeysSurviveAWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "theme.json")
	writeJSON(t, path, `{
	  "schemaVersion": 1,
	  "id": "keep",
	  "name": "Keep",
	  "color": { "accent": { "base": "#010101" } },
	  "tui": { "ansi256": { "accent.base": 39 } },
	  "extra": { "somethingNew": true },
	  "unknownTopLevel": [1, 2, 3]
	}`)

	theme, err := LoadTheme(path)
	if err != nil {
		t.Fatal(err)
	}
	theme.Color["accent.base"] = "#020202"
	if err := theme.Save(path, false); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if v := getPath(got, "color.accent.base"); v != "#020202" {
		t.Errorf("the edit did not land: %v", v)
	}
	// The tui group's own keys contain dots ("accent.base" is one key, not two),
	// which is exactly why the GUI preserves this group verbatim rather than
	// reinterpreting it.
	ansi, ok := getPath(got, "tui.ansi256").(map[string]any)
	if !ok || ansi["accent.base"] == nil {
		t.Error("the tui group was dropped — it is the TUI's and the GUI must preserve it")
	}
	if getPath(got, "extra.somethingNew") == nil {
		t.Error("extra was dropped")
	}
	if getPath(got, "unknownTopLevel") == nil {
		t.Error("an unknown top-level key was dropped")
	}
}

// §8.2: auto REQUIRES both colour sets.
func TestAppearanceAutoNeedsBothColourSets(t *testing.T) {
	half, err := ParseTheme("theme.json", []byte(`{"schemaVersion":1,"appearance":"auto","color":{"bg":{"base":"#ffffff"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarningFor(half.Validate(), "appearance") {
		t.Error("auto with no colorDark should warn")
	}

	bad, err := ParseTheme("theme.json", []byte(`{"schemaVersion":1,"appearance":"sepia"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarningFor(bad.Validate(), "appearance") {
		t.Error("an unknown appearance should warn")
	}
}

// A dark-only theme has one colour set and it is the dark one; without this the
// dark rendering silently uses the built-in light palette.
func TestDarkOnlyThemeSuppliesBothRenderings(t *testing.T) {
	project := t.TempDir()
	writeJSON(t, ProjectThemePath(project), `{
	  "schemaVersion": 1, "appearance": "dark",
	  "color": { "bg": { "base": "#000000" }, "fg": { "default": "#ffffff" } }
	}`)
	got, _, err := ResolveTheme(ThemeRequest{ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	if got.ColorDark["bg.base"] != "#000000" {
		t.Errorf("colorDark.bg.base = %q, want the theme's only palette", got.ColorDark["bg.base"])
	}
}

func TestPropertiesAreOrderedAndComplete(t *testing.T) {
	r := builtinResolved()
	light := r.Properties(false)
	dark := r.Properties(true)
	if len(light) != len(dark) {
		t.Errorf("light has %d properties, dark %d", len(light), len(dark))
	}
	if light[0].Name != "--mm-color-bg-base" {
		t.Errorf("first property is %s, want --mm-color-bg-base", light[0].Name)
	}
	if light[0].Value == dark[0].Value {
		t.Error("the light and dark palettes have the same bg.base")
	}
	seen := map[string]bool{}
	for _, p := range light {
		if seen[p.Name] {
			t.Errorf("%s emitted twice", p.Name)
		}
		seen[p.Name] = true
		if !strings.HasPrefix(p.Name, "--mm-") || p.Value == "" {
			t.Errorf("bad property %+v", p)
		}
	}
}

// spec-gui.md §3.3: [a-z0-9][a-z0-9-]{0,63}
func TestValidThemeID(t *testing.T) {
	ok := []string{"a", "nord-dark", "sample-one-dark", "x9", strings.Repeat("a", 64)}
	bad := []string{"", "-leading", "Upper", "with_underscore", "with space", strings.Repeat("a", 65)}
	for _, id := range ok {
		if !ValidThemeID(id) {
			t.Errorf("ValidThemeID(%q) = false, want true", id)
		}
	}
	for _, id := range bad {
		if ValidThemeID(id) {
			t.Errorf("ValidThemeID(%q) = true, want false", id)
		}
	}
}

// A theme file inside a micro-manager directory is outside the format spec and
// MUST be ignored by format readers and the checker (§8.2). It never
// participates in I1-I10.
func TestThemeAndConfigAreInvisibleToTheValidator(t *testing.T) {
	dir := copyFixture(t, "clean-full")
	writeJSON(t, ProjectThemePath(dir), `{"schemaVersion":1,"id":"x","color":{"accent":{"base":"#010101"}}}`)
	writeJSON(t, ProjectConfigPath(dir), `{"schemaVersion":1,"ui":{"density":"compact"}}`)

	vs, err := mustOpen(t, dir).Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("a theme or config file produced violations: %v", vs)
	}
}
