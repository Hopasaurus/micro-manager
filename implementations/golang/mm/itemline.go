package mm

import (
	"strings"
)

// Item line parsing, per spec-file-format.md §4.2.
//
//   - [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
//   - [BOX] [ID] TITLE | key:value | key:value | ...
//
// The prefix is fixed width and pure ASCII by construction, so the byte offsets
// below are safe even though titles are UTF-8:
//
//	index: 0123456789...
//	       - [x] [T-0042] title...
//	         ^     ^^^^^^  ^
//	         3     7..12   15
const (
	boxOffset   = 3  // the box character
	idOffset    = 7  // first byte of the ID
	idLen       = 6  // "T-0042"
	titleOffset = 15 // first byte of the title
	minLineLen  = titleOffset + 1
)

// fieldSep is the three-character sequence that separates an item line's title
// from its fields, and its fields from each other. The spaces are significant:
// a bare '|' inside a value does not split.
const fieldSep = " | "

// isItemLine reports whether the line is a candidate item line: matching
// `^- \[[ x]\] \[T-[0-9]{4}\] .` including the trailing character, which
// requires a non-empty title.
func isItemLine(line string) bool {
	if len(line) < minLineLen {
		return false
	}
	if line[0] != '-' || line[1] != ' ' || line[2] != '[' {
		return false
	}
	if line[boxOffset] != ' ' && line[boxOffset] != 'x' {
		return false
	}
	if line[4] != ']' || line[5] != ' ' || line[6] != '[' {
		return false
	}
	if line[13] != ']' || line[14] != ' ' {
		return false
	}
	return ID(line[idOffset : idOffset+idLen]).Valid()
}

// looksLikeItemLine reports whether a line was probably meant to be an item
// line. Used to distinguish "malformed item" from "ordinary prose", so a typo
// is reported instead of silently ignored.
func looksLikeItemLine(line string) bool {
	return strings.HasPrefix(line, "- [")
}

// parseItemLine parses one line into an Item. file and lineNo locate errors.
//
// Callers must only pass lines from backlog.md and done.md. A "- [ ]" line in a
// working file is a SUBTASK, which has no ID by design (spec-file-format.md
// §5.2.2); parsing one as an item invents a phantom.
func parseItemLine(file string, lineNo int, line string) (*Item, error) {
	if !isItemLine(line) {
		return nil, parseErrf(file, lineNo, "malformed item line: %s", line)
	}

	it := &Item{
		ID:     ID(line[idOffset : idOffset+idLen]),
		Source: Location{File: file, Line: lineNo},
		rawBox: line[boxOffset],
	}

	parts := strings.Split(line[titleOffset:], fieldSep)
	it.Title = strings.TrimSpace(parts[0])
	if it.Title == "" {
		return nil, parseErrf(file, lineNo, "%s has an empty title", it.ID)
	}

	seen := make(map[string]bool, len(parts))
	for _, part := range parts[1:] {
		i := strings.Index(part, ":")
		if i < 0 {
			return nil, parseErrf(file, lineNo, "%s field is not key:value: %s", it.ID, part)
		}
		key := strings.TrimSpace(part[:i])
		val := strings.TrimSpace(part[i+1:])

		if strings.Contains(val, "|") {
			return nil, parseErrf(file, lineNo, "%s field %s contains a pipe", it.ID, key)
		}
		if seen[key] {
			return nil, parseErrf(file, lineNo, "%s repeats field %s", it.ID, key)
		}
		seen[key] = true

		if err := it.setField(file, lineNo, key, val); err != nil {
			return nil, err
		}
	}
	return it, nil
}

// setField applies one key:value pair, validating the keys we know and
// preserving the ones we do not.
func (it *Item) setField(file string, lineNo int, key, val string) error {
	fail := func(err error) error {
		return parseErrf(file, lineNo, "%s has %s", it.ID, strings.TrimPrefix(
			strings.TrimPrefix(err.Error(), ErrInvalidArgument.Error()), ": "))
	}
	switch key {
	case "prio":
		p, err := ParsePrio(val)
		if err != nil {
			return fail(err)
		}
		it.Prio = p
	case "tags":
		tags, err := ParseTags(val)
		if err != nil {
			return fail(err)
		}
		it.Tags = tags
	case "detail":
		it.Detail = val
	case "created":
		d, err := ParseDate(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has created:%s (want YYYY-MM-DD)", it.ID, val)
		}
		it.Created = d
	case "started":
		d, err := ParseDate(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has started:%s (want YYYY-MM-DD)", it.ID, val)
		}
		it.Started = d
	case "done":
		d, err := ParseDate(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has done:%s (want YYYY-MM-DD)", it.ID, val)
		}
		it.Done = d
	case "outcome":
		o, err := ParseOutcome(val)
		if err != nil {
			return fail(err)
		}
		it.Outcome = o
	case "blocked":
		it.Blocked = val
	default:
		// Unregistered. Keep it exactly as written, in position. This is the
		// format's extension point; dropping it destroys data belonging to a
		// tool we have never heard of.
		it.Extra = append(it.Extra, Field{Key: key, Value: val})
	}
	return nil
}

// fieldOrder is the canonical order writers emit (spec-file-format.md §6.1).
// Readers must not depend on it; it exists so that lines a writer touches come
// out consistent.
var fieldOrder = []string{"prio", "tags", "detail", "created", "started",
	"blocked", "done", "outcome"}

// RenderItemLine serialises an item back to one line.
//
// Registered fields come first in canonical order, then unregistered ones in
// the order they were read. Empty values are omitted entirely - an absent prio
// must not come back as "prio:med", or the file changes on a no-op write.
func RenderItemLine(it *Item) string {
	box := " "
	if it.Closed() {
		box = "x"
	}
	var b strings.Builder
	b.WriteString("- [")
	b.WriteString(box)
	b.WriteString("] [")
	b.WriteString(string(it.ID))
	b.WriteString("] ")
	b.WriteString(it.Title)

	for _, key := range fieldOrder {
		if v := it.fieldValue(key); v != "" {
			b.WriteString(fieldSep)
			b.WriteString(key)
			b.WriteString(":")
			b.WriteString(v)
		}
	}
	for _, f := range it.Extra {
		b.WriteString(fieldSep)
		b.WriteString(f.Key)
		b.WriteString(":")
		b.WriteString(f.Value)
	}
	return b.String()
}

// fieldValue renders one registered field, or "" when it should be omitted.
func (it *Item) fieldValue(key string) string {
	switch key {
	case "prio":
		return string(it.Prio)
	case "tags":
		return FormatTags(it.Tags)
	case "detail":
		return it.Detail
	case "created":
		return it.Created.String()
	case "started":
		return it.Started.String()
	case "done":
		return it.Done.String()
	case "outcome":
		return string(it.Outcome)
	case "blocked":
		return it.Blocked
	}
	return ""
}
