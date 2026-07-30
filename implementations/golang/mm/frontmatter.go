package mm

import "strings"

// Frontmatter is the header block at the top of every micro-manager file.
//
// It is a FLAT MAP OF STRING KEYS TO STRING VALUES, not general YAML
// (spec-file-format.md §4.1). Nested structures, multi-line values and anchors
// are not supported and never will be: the format is deliberately parseable by
// a shell script, and every consumer must agree byte for byte on what a value
// is. A value that looks like a YAML collection is an opaque string here.
//
// Key order is preserved so that a file can be written back unchanged, and
// unknown keys are retained for the same reason (spec-file-format.md §9).
type Frontmatter struct {
	keys   []string // in file order, deduplicated
	values map[string]string
	lines  map[string]int // 1-based line of each key, for diagnostics
	// End is the 1-based line number of the closing "---".
	End int
}

// NewFrontmatter returns an empty block ready to be written to.
func NewFrontmatter() *Frontmatter {
	return &Frontmatter{values: map[string]string{}, lines: map[string]int{}}
}

// parseFrontmatter reads the block from lines, which must be the whole file
// split on "\n". file is used only for diagnostics.
//
// Line 1 must be exactly "---"; the block ends at the next line that is exactly
// "---". A file without a terminated block is an error rather than a file with
// no frontmatter, because the alternative silently treats the entire document
// as a header.
func parseFrontmatter(file string, lines []string) (*Frontmatter, error) {
	fm := NewFrontmatter()
	if len(lines) == 0 || lines[0] != "---" {
		return nil, parseErrf(file, 1, "file must open with YAML frontmatter (---)")
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			fm.End = i + 1
			return fm, nil
		}
		k, v, ok := splitFrontmatterLine(lines[i])
		if !ok {
			continue // rule 3: a line with no ":" is ignored
		}
		fm.Set(k, v)
		fm.lines[k] = i + 1
	}
	return nil, parseErrf(file, len(lines), "frontmatter is not terminated by ---")
}

// splitFrontmatterLine applies rules 1, 2, 4 and 5 of §4.1 to one line.
func splitFrontmatterLine(line string) (key, value string, ok bool) {
	i := strings.Index(line, ":")
	if i < 0 {
		return "", "", false
	}
	key = strings.Trim(line[:i], " \t")
	value = strings.Trim(line[i+1:], " \t")

	// Rule 5: strip a trailing comment, except on title.
	//
	// title is exempt because titles legitimately contain '#' - "Fix issue #42"
	// is an ordinary item, and stripping from the '#' would silently truncate
	// it. This asymmetry is in the spec, not an accident here.
	if key != "title" {
		value = stripTrailingComment(value)
	}

	// Rule 4: strip one layer of surrounding double quotes. No other unescaping.
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return key, value, true
}

// stripTrailingComment removes whitespace followed by '#' and the rest of the
// line. A '#' that is not preceded by whitespace is part of the value.
func stripTrailingComment(v string) string {
	for i := 0; i < len(v); i++ {
		if v[i] != '#' {
			continue
		}
		if i > 0 && (v[i-1] == ' ' || v[i-1] == '\t') {
			return strings.TrimRight(v[:i], " \t")
		}
	}
	return v
}

// Get returns the value for key, or "" when absent.
func (f *Frontmatter) Get(key string) string { return f.values[key] }

// Has reports whether the key is present, distinguishing an absent key from one
// explicitly set to the empty string.
func (f *Frontmatter) Has(key string) bool {
	_, ok := f.values[key]
	return ok
}

// IsNull reports whether the value is absent, empty, or the literal "null".
// The format uses "null" in working file frontmatter to mean exactly what an
// absent field means on an item line (spec-file-format.md §5.2.2).
func (f *Frontmatter) IsNull(key string) bool {
	v, ok := f.values[key]
	return !ok || v == "" || v == "null"
}

// Line returns the 1-based line a key was read from, or 1 when unknown, so a
// diagnostic always has somewhere to point.
func (f *Frontmatter) Line(key string) int {
	if n, ok := f.lines[key]; ok {
		return n
	}
	return 1
}

// Set inserts or replaces a key, preserving first-seen order. Rule 6: a
// duplicate key means last one wins, and does not move in the ordering.
func (f *Frontmatter) Set(key, value string) {
	if _, seen := f.values[key]; !seen {
		f.keys = append(f.keys, key)
	}
	f.values[key] = value
}

// Delete removes a key. Absent keys are ignored.
func (f *Frontmatter) Delete(key string) {
	if _, ok := f.values[key]; !ok {
		return
	}
	delete(f.values, key)
	delete(f.lines, key)
	for i, k := range f.keys {
		if k == key {
			f.keys = append(f.keys[:i], f.keys[i+1:]...)
			break
		}
	}
}

// Keys returns the keys in file order.
func (f *Frontmatter) Keys() []string {
	out := make([]string, len(f.keys))
	copy(out, f.keys)
	return out
}

// Render writes the block back out, including the delimiters, with a trailing
// newline. Key order is the order they were read or first set, which is what
// makes an unmodified file round-trip byte for byte.
func (f *Frontmatter) Render() string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range f.keys {
		b.WriteString(k)
		b.WriteString(": ")
		b.WriteString(f.values[k])
		b.WriteString("\n")
	}
	b.WriteString("---\n")
	return b.String()
}
