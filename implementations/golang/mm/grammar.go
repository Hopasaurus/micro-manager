package mm

import (
	"fmt"
	"strconv"
	"strings"
)

// IDGrammar is a directory's declared ID shape: a prefix of one to four
// uppercase ASCII letters, a hyphen, and exactly Width zero-padded digits
// (spec-file-format.md §3.3.2).
type IDGrammar struct {
	Prefix string
	Width  int
}

// DefaultIDGrammar is the grammar every directory uses unless backlog.md
// declares id_prefix/id_width: the prefix "T" and width 4.
func DefaultIDGrammar() IDGrammar { return IDGrammar{Prefix: "T", Width: 4} }

// ValidIDPrefix reports whether p is one to four uppercase ASCII letters,
// which is what §3.3.2 requires of id_prefix. Lowercase and mixed case are
// invalid: matching against a declared prefix is exact.
func ValidIDPrefix(p string) bool {
	if len(p) < 1 || len(p) > 4 {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 'A' || p[i] > 'Z' {
			return false
		}
	}
	return true
}

// ParseIDGrammar reads the id_prefix and id_width keys from a backlog
// frontmatter (spec-file-format.md §5.1). Both are optional and default
// independently - "T" and 4 - so a directory that declares nothing comes out
// as DefaultIDGrammar and behaves byte-identically to spec version 1 (rule 6).
//
// A present-but-invalid key is a format violation, and the default stands in
// for it so the rest of the file still parses: a directory that is already
// wrong must still open, list and report (spec-tools.md §8). A width outside
// the RECOMMENDED 3-6 range is a warning, never a violation - the grammar is
// honorably expressed, just outside the sweet spot (rule 3). A width above
// 15 is the shared cap, and a violation everywhere: at 16 digits the
// double-based readers the format must serve (check.sh's mawk, JavaScript
// `number`) silently round, so every reader refuses uniformly rather than
// disagreeing about which directories are valid (rule 3, T-0120).
func ParseIDGrammar(fm *Frontmatter) (g IDGrammar, vs []Violation, warns []Violation) {
	g = DefaultIDGrammar()
	if fm == nil {
		return g, nil, nil
	}
	if fm.Has("id_prefix") {
		p := fm.Get("id_prefix")
		if ValidIDPrefix(p) {
			g.Prefix = p
		} else {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: "backlog.md", Line: fm.Line("id_prefix")},
				Message: "id_prefix must be one to four uppercase letters (A-Z): " + p,
			})
		}
	}
	if fm.Has("id_width") {
		w, err := strconv.Atoi(fm.Get("id_width"))
		switch {
		case err != nil || w < 1 || w > 15:
			// "0" is an ASCII digit but cannot express a single ID: the counter
			// space is 10^W - 1 (rule 5), which at W=0 is zero items. A width
			// above 15 is the cap of rule 3: at 16 digits the narrowest readers
			// silently round, so the declaration is invalid rather than merely
			// unrecommended. Both cases keep the default width standing in.
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: "backlog.md", Line: fm.Line("id_width")},
				Message: "id_width must be one to fifteen digits: " + fm.Get("id_width"),
			})
		default:
			g.Width = w
			if w < 3 || w > 6 {
				warns = append(warns, Violation{
					Invariant: invFormat, At: Location{File: "backlog.md", Line: fm.Line("id_width")},
					Message: fmt.Sprintf("id_width %d is outside the RECOMMENDED 3-6 range (spec-file-format.md §3.3.2 rule 3)",
						w),
				})
			}
		}
	}
	return g, vs, warns
}

// ValidID reports whether s is an ID in the declared grammar: the prefix, a
// hyphen, and exactly g.Width digits.
func (g IDGrammar) ValidID(s string) bool {
	if len(s) != len(g.Prefix)+1+g.Width {
		return false
	}
	if !strings.HasPrefix(s, g.Prefix) || s[len(g.Prefix)] != '-' {
		return false
	}
	for i := len(g.Prefix) + 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Num returns the numeric part of an ID valid in the declared grammar, or -1
// when s is not one.
func (g IDGrammar) Num(s string) int {
	if !g.ValidID(s) {
		return -1
	}
	n, err := strconv.Atoi(s[len(g.Prefix)+1:])
	if err != nil {
		return -1
	}
	return n
}

// NewID formats n as an ID in the declared grammar, zero-padded to g.Width. n
// is not range checked here; the caller allocating from next_id is responsible
// for staying below Cap().
func (g IDGrammar) NewID(n int) ID {
	return ID(fmt.Sprintf("%s-%0*d", g.Prefix, g.Width, n))
}

// Cap is the largest usable counter value: 10^Width - 1 items (§3.3.2 rule 5),
// generalizing the default grammar's 9999-item cap.
func (g IDGrammar) Cap() int {
	cap := 1
	for i := 0; i < g.Width; i++ {
		cap *= 10
	}
	return cap - 1
}

// String renders the grammar as an example ID shape, for diagnostics: "T-####"
// or "MM-###". The hash marks stand for the digits.
func (g IDGrammar) String() string {
	return g.Prefix + "-" + strings.Repeat("#", g.Width)
}

// WidthWarning returns the non-fatal warning for a width outside the
// RECOMMENDED 3-6 range, or "" (§3.3.2 rule 3). A checker warns and never
// fails on such a width; --check reports it through ValidateWithWarnings, and
// --init uses this so a directory being created is warned about at the same
// moment.
func (g IDGrammar) WidthWarning() string {
	if g.Width >= 3 && g.Width <= 6 {
		return ""
	}
	return fmt.Sprintf("id_width %d is outside the RECOMMENDED 3-6 range (spec-file-format.md §3.3.2 rule 3)",
		g.Width)
}

// isDigits reports whether s is one or more ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
