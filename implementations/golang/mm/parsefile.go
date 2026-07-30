package mm

import (
	"fmt"
	"strings"
)

// Structural parsing of backlog.md and done.md.
//
// These parsers are DELIBERATELY PERMISSIVE. spec-tools.md §8 requires a tool to
// open, list and report on a directory that already has violations - refusing to
// read a broken directory removes the tool exactly when it is most needed, and
// `mm --check` could not report a problem it cannot parse past.
//
// So nothing here returns an error. Every problem becomes a Violation collected
// alongside whatever was parsed, and validation (I1-I10) decides what it means.
// The reference checker behaves the same way: it reports every problem in a file
// rather than stopping at the first.

// invFormat labels a structural problem that is not one of I1-I10 - a file that
// does not open with frontmatter, a line that cannot be read as an item.
const invFormat = "format"

// sectionSpan is one "## Name" section of backlog.md.
type sectionSpan struct {
	Name     Section // SectionNone when the heading is not one of the three
	Raw      string  // heading text exactly as written
	HeadLine int     // 1-based line of the heading
	Items    []*Item
}

// backlogFile is a parsed backlog.md.
//
// Lines is retained verbatim so that an unmodified file can be written back byte
// for byte, and so that an edit rewrites only the lines it touches (T-0008).
type backlogFile struct {
	Name     string
	FM       *Frontmatter
	Lines    []string
	Sections []*sectionSpan // in file order
	Items    []*Item        // every item, in file order

	edit *fileEdit // set by Edit(); the live view of Lines
}

// Section returns the span for a named section, or nil when absent.
func (b *backlogFile) Section(name Section) *sectionSpan {
	for _, s := range b.Sections {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// monthSpan is one "## YYYY-MM" group of done.md.
type monthSpan struct {
	Month    string // "2026-07"; empty when the heading was not a valid month
	Raw      string
	HeadLine int
	Items    []*Item
}

// doneFile is a parsed done.md.
type doneFile struct {
	Name   string
	FM     *Frontmatter
	Lines  []string
	Months []*monthSpan // in file order, expected newest first
	Items  []*Item

	edit *fileEdit
}

// splitLines splits a file into lines such that strings.Join(lines, "\n")
// reproduces the input exactly, trailing newline included.
func splitLines(data []byte) []string {
	return strings.Split(string(data), "\n")
}

// readHeader parses the frontmatter and reports where the body begins.
//
// Mirrors the reference checker's permissiveness: a file with no opening "---"
// is reported and then treated as all body, so its items are still found.
func readHeader(name string, lines []string) (fm *Frontmatter, bodyStart int, vs []Violation) {
	fm, err := parseFrontmatter(name, lines)
	if err == nil {
		return fm, fm.End, nil
	}
	v := Violation{Invariant: invFormat, At: Location{File: name, Line: 1}}
	if len(lines) == 0 || lines[0] != "---" {
		v.Message = "file must open with YAML frontmatter (---)"
		return NewFrontmatter(), 0, []Violation{v}
	}
	// Opened but never closed: there is no body to walk.
	v.Message = "frontmatter is not terminated by ---"
	return NewFrontmatter(), len(lines), []Violation{v}
}

// headingName returns the text of a "## " heading, or "" if the line is not one.
func headingName(line string) (string, bool) {
	if !strings.HasPrefix(line, "## ") {
		return "", false
	}
	return strings.TrimSpace(line[3:]), true
}

// parseBacklog reads backlog.md.
func parseBacklog(name string, data []byte) (*backlogFile, []Violation) {
	lines := splitLines(data)
	fm, body, vs := readHeader(name, lines)
	b := &backlogFile{Name: name, FM: fm, Lines: lines}

	var cur *sectionSpan
	for i := body; i < len(lines); i++ {
		line := lines[i]
		lineNo := i + 1

		if raw, ok := headingName(line); ok {
			sec, err := ParseSection(raw)
			if err != nil {
				// Section vocabulary is closed in backlog.md.
				vs = append(vs, Violation{
					Invariant: invFormat,
					At:        Location{File: name, Line: lineNo},
					Message:   "unknown section heading: " + raw,
				})
				sec = SectionNone
			}
			cur = &sectionSpan{Name: sec, Raw: raw, HeadLine: lineNo}
			b.Sections = append(b.Sections, cur)
			continue
		}

		if !looksLikeItemLine(line) {
			continue
		}
		it, err := parseItemLine(name, lineNo, line)
		if err != nil {
			vs = append(vs, violationFrom(invFormat, name, lineNo, err))
			continue
		}
		it.State = StateBacklog
		if cur != nil {
			it.Section = cur.Name
			it.Pos = len(cur.Items) + 1
			cur.Items = append(cur.Items, it)
		}
		b.Items = append(b.Items, it)
	}
	return b, vs
}

// parseDone reads done.md.
func parseDone(name string, data []byte) (*doneFile, []Violation) {
	lines := splitLines(data)
	fm, body, vs := readHeader(name, lines)
	d := &doneFile{Name: name, FM: fm, Lines: lines}

	var cur *monthSpan
	for i := body; i < len(lines); i++ {
		line := lines[i]
		lineNo := i + 1

		if raw, ok := headingName(line); ok {
			cur = &monthSpan{Raw: raw, HeadLine: lineNo}
			if isMonth(raw) {
				cur.Month = raw
			} else {
				// Every "## " heading in done.md is a month group.
				vs = append(vs, Violation{
					Invariant: invFormat,
					At:        Location{File: name, Line: lineNo},
					Message:   "heading must be YYYY-MM, got: " + raw,
				})
			}
			d.Months = append(d.Months, cur)
			continue
		}

		if !looksLikeItemLine(line) {
			continue
		}
		it, err := parseItemLine(name, lineNo, line)
		if err != nil {
			vs = append(vs, violationFrom(invFormat, name, lineNo, err))
			continue
		}
		it.State = StateDone
		if cur != nil {
			it.Pos = len(cur.Items) + 1
			cur.Items = append(cur.Items, it)
		}
		d.Items = append(d.Items, it)
	}
	return d, vs
}

// Month returns the group for a month, or nil when absent.
func (d *doneFile) Month(month string) *monthSpan {
	for _, m := range d.Months {
		if m.Month == month {
			return m
		}
	}
	return nil
}

// monthOf returns the month group containing an item, or nil.
func (d *doneFile) monthOf(it *Item) *monthSpan {
	for _, m := range d.Months {
		for _, x := range m.Items {
			if x == it {
				return m
			}
		}
	}
	return nil
}

// isMonth reports whether s is exactly YYYY-MM.
func isMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	for i, r := range s {
		if i == 4 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// violationFrom converts a located parse error into a Violation, so a caller
// gets one uniform finding type regardless of where the problem came from.
func violationFrom(inv, file string, line int, err error) Violation {
	msg := err.Error()
	if pe, ok := err.(*ParseError); ok {
		msg = pe.Message
		return Violation{Invariant: inv, At: pe.At, Message: msg}
	}
	return Violation{
		Invariant: inv,
		At:        Location{File: file, Line: line},
		Message:   fmt.Sprint(msg),
	}
}
