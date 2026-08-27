package mm

import (
	"strings"
)

// Item line parsing, per spec-file-format.md §4.2.
//
//   - [ ] [T-0042] Fix the deploy script | prio:high | tags:infra,ci | created:2026-07-29
//   - [BOX] [ID] TITLE | key:value | key:value | ...
//
// The ID is located BY TOKEN against the directory's declared grammar, never
// by fixed byte offsets: the prefix is one to four letters and the width is
// variable (§3.3.2), so an offset that is safe under the default grammar is
// wrong the moment a directory declares its own (rule 4 of §3.1, §4.2 rule 2).

// fieldSep is the three-character sequence that separates an item line's title
// from its fields, and its fields from each other. The spaces are significant:
// a bare '|' inside a value does not split.
const fieldSep = " | "

// itemLineOffsets locates the box, ID and title of a candidate item line by
// token, returning false when the line does not match
// `^- \[[ x]\] [<grammar>] .` with a non-empty title. The ID is matched
// against the declared grammar; everything between the ID and the first
// " | " is the title (spec-file-format.md §4.2 rules 1-3).
func itemLineOffsets(line string, g IDGrammar) (box, idStart, idLen, titleStart int, ok bool) {
	i := 0
	// "- ["
	if len(line) < i+3 || line[i] != '-' || line[i+1] != ' ' || line[i+2] != '[' {
		return 0, 0, 0, 0, false
	}
	i += 3
	box = i
	// the box character
	if len(line) <= i || (line[i] != ' ' && line[i] != 'x') {
		return 0, 0, 0, 0, false
	}
	i++
	// "] ["
	if len(line) < i+3 || line[i] != ']' || line[i+1] != ' ' || line[i+2] != '[' {
		return 0, 0, 0, 0, false
	}
	i += 3
	idStart = i
	// the declared prefix, matched exactly
	if len(line) < i+len(g.Prefix) || line[i:i+len(g.Prefix)] != g.Prefix {
		return 0, 0, 0, 0, false
	}
	i += len(g.Prefix)
	// the hyphen
	if len(line) <= i || line[i] != '-' {
		return 0, 0, 0, 0, false
	}
	i++
	// exactly g.Width digits
	if len(line) < i+g.Width {
		return 0, 0, 0, 0, false
	}
	for j := 0; j < g.Width; j++ {
		if line[i+j] < '0' || line[i+j] > '9' {
			return 0, 0, 0, 0, false
		}
	}
	i += g.Width
	idLen = i - idStart
	// "] " and a non-empty title
	if len(line) < i+2 || line[i] != ']' || line[i+1] != ' ' {
		return 0, 0, 0, 0, false
	}
	i += 2
	if len(line) <= i {
		return 0, 0, 0, 0, false
	}
	return box, idStart, idLen, i, true
}

// isItemLine reports whether the line is a candidate item line under the
// default grammar: matching `^- \[[ x]\] \[T-[0-9]{4}\] .` including the
// trailing character, which requires a non-empty title.
func isItemLine(line string) bool {
	return isItemLineG(line, DefaultIDGrammar())
}

// isItemLineG reports whether the line is a candidate item line under the
// declared grammar.
func isItemLineG(line string, g IDGrammar) bool {
	_, _, _, _, ok := itemLineOffsets(line, g)
	return ok
}

// looksLikeItemLine reports whether a line was probably meant to be an item
// line. Used to distinguish "malformed item" from "ordinary prose", so a typo
// is reported instead of silently ignored.
func looksLikeItemLine(line string) bool {
	return strings.HasPrefix(line, "- [")
}

// isConflictMarker reports whether line is a git conflict marker: a line whose
// first non-blank characters are exactly <<<<<<<, =======, or >>>>>>> — the
// three shapes git writes into a file whose merge conflicted (spec-file-format.md
// §5.1, the one reserved exception to the prose allowance). A half-resolved
// merge must never be indistinguishable from valid prose.
func isConflictMarker(line string) bool {
	line = strings.TrimLeft(line, " \t")
	return strings.HasPrefix(line, "<<<<<<<") ||
		strings.HasPrefix(line, "=======") ||
		strings.HasPrefix(line, ">>>>>>>")
}

// parseItemLine parses one line into an Item under the default grammar. file
// and lineNo locate errors.
func parseItemLine(file string, lineNo int, line string) (*Item, error) {
	return parseItemLineG(file, lineNo, line, DefaultIDGrammar())
}

// parseItemLineG parses one line into an Item under the declared grammar. file
// and lineNo locate errors.
//
// Callers must only pass lines from backlog.md and done.md. A "- [ ]" line in a
// working file is a SUBTASK, which has no ID by design (spec-file-format.md
// §5.2.2); parsing one as an item invents a phantom.
func parseItemLineG(file string, lineNo int, line string, g IDGrammar) (*Item, error) {
	box, idStart, idLen, titleStart, ok := itemLineOffsets(line, g)
	if !ok {
		return nil, parseErrf(file, lineNo, "malformed item line: %s", line)
	}

	it := &Item{
		ID:     ID(line[idStart : idStart+idLen]),
		Source: Location{File: file, Line: lineNo},
		rawBox: line[box],
	}

	parts := strings.Split(line[titleStart:], fieldSep)
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
	case "stage":
		// Membership in the directory's declared stages is validated by the
		// caller (parseBoard), which has the StageConfig this function does
		// not; here the value is only carried through as a well-formed SLUG.
		it.Stage = Stage(val)
	case "reason":
		it.Reason = val
	case "tickler_dest":
		it.TicklerDest = Stage(val)
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
	case "refs":
		refs, err := ParseRefs(val)
		if err != nil {
			return fail(err)
		}
		it.Refs = refs
	case "detail":
		it.Detail = val
	case "created":
		d, suffix, err := ParseDateOrStamp(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has created:%s (want YYYY-MM-DD, optionally with a time)", it.ID, val)
		}
		it.Created = d
		it.CreatedTime = suffix
	case "started":
		d, suffix, err := ParseDateOrStamp(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has started:%s (want YYYY-MM-DD, optionally with a time)", it.ID, val)
		}
		it.Started = d
		it.StartedTime = suffix
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
	case "tickler":
		if _, err := ParseSchedule(val); err != nil {
			return parseErrf(file, lineNo, "%s has tickler:%s (%v)", it.ID, val, err)
		}
		it.Tickler = val
	case "tickled":
		d, err := ParseDate(val)
		if err != nil {
			return parseErrf(file, lineNo, "%s has tickled:%s (want YYYY-MM-DD)", it.ID, val)
		}
		it.Tickled = d
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
var fieldOrder = []string{"stage", "prio", "tags", "refs", "detail", "created", "started",
	"blocked", "reason", "tickler", "tickler_dest", "tickled", "done", "outcome"}

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
	case "stage":
		return string(it.Stage)
	case "reason":
		return it.Reason
	case "tickler_dest":
		return string(it.TicklerDest)
	case "prio":
		return string(it.Prio)
	case "tags":
		return FormatTags(it.Tags)
	case "refs":
		return FormatRefs(it.Refs)
	case "detail":
		return it.Detail
	case "created":
		return FormatDateOrStamp(it.Created, it.CreatedTime)
	case "started":
		return FormatDateOrStamp(it.Started, it.StartedTime)
	case "done":
		return it.Done.String()
	case "outcome":
		return string(it.Outcome)
	case "blocked":
		return it.Blocked
	case "tickler":
		return it.Tickler
	case "tickled":
		return it.Tickled.String()
	}
	return ""
}
