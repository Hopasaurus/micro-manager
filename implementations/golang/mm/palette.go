package mm

import (
	"fmt"
	"math"
	"strconv"
)

// Palette generation from a base colour (T-0137).
//
// The theme editor offers "pick a colour, get a palette": a user supplies one
// hex value and the tool fills in the whole color.* token set in a way that
// belongs together. The math lives here rather than in the front end because
// it is deterministic and shared — a future TUI theme editor must derive the
// same tokens from the same base, and the derivation must be testable.
//
// The scheme is a color-wheel one, with one deliberate compromise:
//
//   - The semantic hues are FIXED at their meaning (blocked is red-ish, done is
//     green-ish, working is yellow-ish, ready is blue-ish). The wheel position
//     of a meaning must not move with the user's choice, or a "done" state
//     stops meaning "done" the moment the brand colour changes.
//   - What the base colour supplies is the CHROMA: every token takes the
//     base's saturation, so a saturated blue base yields a saturated palette
//     and a grey base yields a deliberately monochrome one. Lightness is set
//     by role (backgrounds light, text dark, states mid).
//   - The base colour itself is accent.base. Its COMPLEMENT (the opposite hue,
//     180° away on the wheel) tints the two places a focus or a selection
//     must stand out against an accent-coloured field: border.focus and
//     selection.bg.
//
// accent.fg is chosen by luminance so the accent pair always clears WCAG AA
// (the same pair §11 rule 7 warns about): white text on a dark accent, dark
// text on a light one.

// HexToHSL converts a #rrggbb colour to hue/saturation/lightness.
//
// h is in [0, 360), s and l in [0, 1]. The conversion is the standard RGB
// model one (not the artist's wheel); hue angles below are consistent with it.
func HexToHSL(hex string) (h, s, l float64, err error) {
	if err := ValidateColor(hex); err != nil {
		return 0, 0, 0, err
	}
	component := func(i int) float64 {
		n, _ := strconv.ParseUint(hex[1+i:3+i], 16, 8)
		return float64(n) / 255
	}
	r, g, b := component(0), component(2), component(4)
	max, min := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))

	l = (max + min) / 2
	if max == min {
		return 0, 0, l, nil // grey: hue is meaningless, saturation is zero
	}

	d := max - min
	if l > 0.5 {
		s = d / (2 - max - min)
	} else {
		s = d / (max + min)
	}

	switch max {
	case r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case g:
		h = (b-r)/d + 2
	case b:
		h = (r-g)/d + 4
	}
	h *= 60
	return h, s, l, nil
}

// HSLToHex converts hue/saturation/lightness back to #rrggbb.
//
// Out-of-range values are clamped rather than rejected: a derived palette
// rounds through HSL and a rounding error of one ulp must not fail.
func HSLToHex(h, s, l float64) (string, error) {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	s = clamp01(s)
	l = clamp01(l)

	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	byte := func(v float64) uint8 { return uint8(math.Round((v + m) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x", byte(r), byte(g), byte(b)), nil
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}

// hue is a semantic hue anchor on the same wheel HexToHSL uses. The anchors
// are the classic terminal traffic colours, so a generated palette reads the
// way the built-in does: blocked red, working yellow, done green, ready blue.
const (
	hueBlocked = 0    // red
	hueWorking = 55   // yellow
	hueDone    = 120  // green
	hueReady   = 240  // blue
)

// HarmonizedPalette derives the complete color.* token set from a base
// accent colour, for one appearance. Every token of ColorTokens() is present,
// so the result can be dropped straight into a theme's color (light) or
// colorDark (dark) block.
func HarmonizedPalette(base string, dark bool) (map[string]string, error) {
	if err := ValidateColor(base); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	bh, bs, _, err := HexToHSL(base)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}

	// The accent: the base itself in light mode; the same hue raised for dark
	// backgrounds (a saturated mid-blue reads as near-black on black).
	accent := base
	if dark {
		accent, _ = HSLToHex(bh, bs, 0.62)
	}
	accentLum, err := relativeLuminance(accent)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	// The AA crossover for white text is L ≈ 0.183; a brighter accent gets
	// dark text so accent.fg/accent.base always clears 4.5:1 (§11 rule 7).
	accentFG := "#ffffff"
	if accentLum > (1/ContrastAA - 0.05) {
		accentFG = "#111111"
	}

	complement := func(h float64) float64 { return math.Mod(h+180, 360) }

	state := func(h float64, lightL, darkL float64) string {
		if dark {
			return mustHSL(h, bs, darkL)
		}
		return mustHSL(h, bs, lightL)
	}
	grey := func(lightL, darkL float64) string {
		if dark {
			return mustHSL(0, 0, darkL)
		}
		return mustHSL(0, 0, lightL)
	}

	palette := map[string]string{
		"bg.base":    grey(0.98, 0.07),
		"bg.raised":  grey(0.96, 0.11),
		"bg.sunken":  grey(0.92, 0.04),
		"bg.overlay": grey(1.0, 0.11),

		"fg.default":  grey(0.13, 0.92),
		"fg.muted":    grey(0.42, 0.62),
		"fg.subtle":   grey(0.53, 0.52),
		"fg.inverted": grey(0.98, 0.08),

		"border.default": grey(0.88, 0.22),
		"border.strong":  grey(0.68, 0.35),
		"border.focus":   state(complement(bh), 0.45, 0.58),

		"accent.base":  accent,
		"accent.fg":    accentFG,
		"accent.muted": state(bh, 0.93, 0.20),

		"state.ready":   state(hueReady, 0.45, 0.65),
		"state.blocked": state(hueBlocked, 0.45, 0.62),
		"state.someday": grey(0.50, 0.55),
		"state.working": state(hueWorking, 0.47, 0.62),
		"state.done":    state(hueDone, 0.42, 0.62),

		"prio.high": state(hueBlocked, 0.45, 0.62),
		"prio.med":  state(hueWorking, 0.47, 0.62),
		"prio.low":  grey(0.50, 0.55),

		"feedback.success": state(hueDone, 0.42, 0.62),
		"feedback.warning": state(hueWorking, 0.47, 0.62),
		"feedback.danger":  state(hueBlocked, 0.45, 0.62),
		"feedback.info":    state(hueReady, 0.45, 0.65),

		"selection.bg": state(complement(bh), 0.93, 0.20),
		"selection.fg": grey(0.13, 0.92),

		"drag.valid":   state(hueDone, 0.42, 0.62),
		"drag.invalid": state(hueBlocked, 0.45, 0.62),
	}
	return palette, nil
}

// mustHSL is the derived-palette wrapper for HSLToHex: the inputs here are
// constants of this file, and HSLToHex clamps rather than rejecting, so the
// error is unreachable for them.
func mustHSL(h, s, l float64) string {
	out, _ := HSLToHex(h, s, l)
	return out
}
