package mm

import (
	"errors"
	"math"
	"testing"
)

// The color-wheel palette generator (T-0137).

// HexToHSL/HSLToHex must invert each other, and the round trip must survive
// the hex ↔ HSL precision loss: a derived palette is written back to hex and
// must not drift visibly.
func TestHexHSLRoundTrip(t *testing.T) {
	inputs := []string{"#1257c9", "#ff0000", "#00ff00", "#0000ff", "#ffffff", "#000000", "#8a6d1f", "#aabbcc", "#4493f8"}
	for _, hex := range inputs {
		h, s, l, err := HexToHSL(hex)
		if err != nil {
			t.Fatalf("HexToHSL(%s): %v", hex, err)
		}
		back, err := HSLToHex(h, s, l)
		if err != nil {
			t.Fatalf("HSLToHex: %v", err)
		}
		if !closeHex(back, hex, 1) {
			t.Errorf("round trip %s -> %s (h=%.1f s=%.3f l=%.3f)", hex, back, h, s, l)
		}
	}
}

// closeHex compares two hex colours channel by channel with a tolerance.
func closeHex(a, b string, tol int) bool {
	for i := 1; i < 7; i += 2 {
		na, _ := parseHexUint(a[i : i+2])
		nb, _ := parseHexUint(b[i : i+2])
		d := na - nb
		if d < 0 {
			d = -d
		}
		if int(d) > tol {
			return false
		}
	}
	return true
}

func parseHexUint(s string) (uint64, error) {
	var out uint64
	for _, r := range s {
		out *= 16
		switch {
		case r >= '0' && r <= '9':
			out += uint64(r - '0')
		case r >= 'a' && r <= 'f':
			out += uint64(r-'a') + 10
		case r >= 'A' && r <= 'F':
			out += uint64(r-'A') + 10
		default:
			return 0, errors.New("bad hex")
		}
	}
	return out, nil
}

// The anchors the derivation leans on: grey has no hue, and the primary
// colours sit where the wheel says.
func TestHexToHSLKnownValues(t *testing.T) {
	h, s, l, err := HexToHSL("#ff0000")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(h-0) > 0.001 || math.Abs(s-1) > 0.001 || math.Abs(l-0.5) > 0.001 {
		t.Errorf("red = h%.1f s%.3f l%.3f, want 0/1/0.5", h, s, l)
	}
	h, s, l, _ = HexToHSL("#00ff00")
	if math.Abs(h-120) > 0.001 {
		t.Errorf("green hue = %.1f, want 120", h)
	}
	h, s, l, _ = HexToHSL("#0000ff")
	if math.Abs(h-240) > 0.001 {
		t.Errorf("blue hue = %.1f, want 240", h)
	}
	h, s, l, _ = HexToHSL("#808080")
	if s != 0 {
		t.Errorf("grey saturation = %.3f, want 0", s)
	}
	if _, _, _, err := HexToHSL("blue"); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("a named colour should be rejected, got %v", err)
	}
}

// The generator must hand back a COMPLETE palette of VALID colours — the
// theme editor drops whatever it returns straight into the color block, and
// a missing or malformed token would silently fall back to the built-in.
func TestHarmonizedPaletteIsCompleteAndValid(t *testing.T) {
	for _, dark := range []bool{false, true} {
		p, err := HarmonizedPalette("#1257c9", dark)
		if err != nil {
			t.Fatal(err)
		}
		if len(p) != len(ColorTokens()) {
			t.Errorf("dark=%v: %d tokens, want %d", dark, len(p), len(ColorTokens()))
		}
		for _, token := range ColorTokens() {
			v := p[token]
			if v == "" {
				t.Errorf("dark=%v: no %s in the derived palette", dark, token)
				continue
			}
			if err := ValidateColor(v); err != nil {
				t.Errorf("dark=%v: %s = %q: %v", dark, token, v, err)
			}
		}
	}
}

// The base colour is the accent: the whole point of the feature is that the
// colour the user picks is the colour the theme leads with.
func TestHarmonizedPaletteUsesTheBaseAsAccent(t *testing.T) {
	p, err := HarmonizedPalette("#1257c9", false)
	if err != nil {
		t.Fatal(err)
	}
	if p["accent.base"] != "#1257c9" {
		t.Errorf("accent.base = %q, want the base", p["accent.base"])
	}
}

// The generator is deterministic: the same base, the same palette, every time
// — and the two appearances are not the same palette.
func TestHarmonizedPaletteIsDeterministicAndDistinct(t *testing.T) {
	a, _ := HarmonizedPalette("#1257c9", false)
	b, _ := HarmonizedPalette("#1257c9", false)
	for _, token := range ColorTokens() {
		if a[token] != b[token] {
			t.Errorf("%s differs between two runs: %q vs %q", token, a[token], b[token])
		}
	}
	dark, _ := HarmonizedPalette("#1257c9", true)
	if a["bg.base"] == dark["bg.base"] {
		t.Error("light and dark palettes have the same bg.base")
	}
}

// §11 rule 7: the two contrast pairs the editor warns about must pass AA. The
// generator picks accent.fg by luminance precisely so the accent pair clears.
func TestHarmonizedPaletteMeetsContrastAA(t *testing.T) {
	bases := []string{"#1257c9", "#ff0000", "#00aa00", "#ffdd00", "#ffffff", "#000000", "#8a6d1f"}
	for _, base := range bases {
		for _, dark := range []bool{false, true} {
			p, err := HarmonizedPalette(base, dark)
			if err != nil {
				t.Fatal(err)
			}
			// Rebuild the pair list the way CheckContrast would, because the
			// generator does not produce a ResolvedTheme.
			pairs := []struct{ fg, bg string }{
				{"fg.default", "bg.base"},
				{"accent.fg", "accent.base"},
			}
			for _, pair := range pairs {
				ratio, err := ContrastRatio(p[pair.fg], p[pair.bg])
				if err != nil {
					t.Errorf("base %s dark=%v: %v", base, dark, err)
					continue
				}
				if ratio < ContrastAA {
					t.Errorf("base %s dark=%v: %s on %s is %.2f:1, below AA", base, dark, pair.fg, pair.bg, ratio)
				}
			}
		}
	}
}

// A grey base must yield a monochrome palette: saturation travels from the
// base into every state, so a user who wants no colour gets no colour.
func TestHarmonizedPaletteGreyBaseIsMonochrome(t *testing.T) {
	p, err := HarmonizedPalette("#808080", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"state.ready", "state.blocked", "state.done", "prio.high"} {
		h, s, _, _ := HexToHSL(p[token])
		if s > 0.01 {
			t.Errorf("%s = %q has saturation %.2f, want grey", token, p[token], s)
		}
		_ = h
	}
}

func TestHarmonizedPaletteRejectsABadBase(t *testing.T) {
	if _, err := HarmonizedPalette("blue", false); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("a named colour should be rejected, got %v", err)
	}
}
