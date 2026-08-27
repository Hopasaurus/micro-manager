package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Audit log (audit.md), opt-in via board.md's `audit: true` (T-0253,
// spec-file-format.md §5.1.8). One line per changed field, in the same
// key:value shape the rest of this format already uses, timestamped to the
// second in UTC. Append-only: an existing line is never rewritten, and the
// file is never parsed back into a model or validated - it is a log, not
// board state, the same relationship an archive has to the files it came
// from (§5.6). Version 2 only: the enabling key lives in board.md.

// auditHeader is written once, the first time a directory's audit.md is
// created. Never rewritten after that - only new lines are appended.
const auditHeader = "---\ndoc: audit\nversion: 2\n---\n\n# Audit\n\n"

// auditField is one changed field: the audit log's unit of record.
type auditField struct {
	Field string
	Value string
}

// lineField is one key:value pair read off a rendered item line, in the
// order it appeared.
type lineField struct{ Key, Value string }

// splitLineFields extracts an item line's key:value fields, in order, after
// the title. The line is one this package just rendered (a Change's
// Before/After), so it is already well-formed and needs no validation here
// — unlike parseItemLineG, which validates a line read from disk.
func splitLineFields(line string) []lineField {
	if line == "" {
		return nil
	}
	parts := strings.Split(line, fieldSep)
	out := make([]lineField, 0, len(parts)-1)
	for _, p := range parts[1:] {
		i := strings.Index(p, ":")
		if i < 0 {
			continue
		}
		out = append(out, lineField{Key: strings.TrimSpace(p[:i]), Value: strings.TrimSpace(p[i+1:])})
	}
	return out
}

func lookupField(fields []lineField, key string) (string, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f.Value, true
		}
	}
	return "", false
}

// auditFieldChanges turns one Change into the field-level entries audit.md
// records. Rules, in order:
//
//   - A detail-file touch (File is neither board.md nor done.md) logs one
//     "detail" entry with a blank value: the body is prose, not a field to
//     diff, per the request this shipped from.
//   - ChangeDeleted logs one "removed" entry with a blank value, rather than
//     diffing every field down to blank — noise, not signal.
//   - Otherwise Before and After are both full rendered item lines (Before
//     is empty for a creation): one entry per field whose value differs,
//     including a field present in Before but absent from After (logged
//     with a blank value — that field was cleared).
//
// A pure reorder (no field actually changed) produces no entries, correctly:
// nothing about the item changed, only its position, and position is not a
// field this format's item line carries.
func auditFieldChanges(c Change) []auditField {
	if c.File != "board.md" && c.File != "done.md" {
		return []auditField{{Field: "detail"}}
	}
	if c.Kind == ChangeDeleted {
		return []auditField{{Field: "removed"}}
	}
	after := splitLineFields(c.After)
	before := splitLineFields(c.Before)
	var out []auditField
	for _, f := range after {
		if bv, ok := lookupField(before, f.Key); ok && bv == f.Value {
			continue
		}
		out = append(out, auditField{Field: f.Key, Value: f.Value})
	}
	for _, f := range before {
		if _, ok := lookupField(after, f.Key); !ok {
			out = append(out, auditField{Field: f.Key})
		}
	}
	return out
}

// AuditEntry is one parsed audit.md line, for a reader that only wants to
// show the log — never to enforce anything about it (§5.7: audit.md is
// never validated, and ReadAuditLog does not try to be a second parser for
// the rest of the format; it understands nothing but its own four fields).
type AuditEntry struct {
	Timestamp string
	ID        ID
	Field     string
	Value     string
}

// ReadAuditLog reads a directory's audit.md, oldest entry first (the file's
// own append order). Returns a nil slice, not an error, when the file does
// not exist — an absent log is not a failure; §5.7 never requires one.
func ReadAuditLog(dir string) ([]AuditEntry, error) {
	data, err := os.ReadFile(filepath.Join(dir, "audit.md"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("%w: %v", ErrIO, err)
	}
	var out []AuditEntry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " | id:") {
			continue // frontmatter, the heading, a blank line
		}
		e := AuditEntry{}
		parts := strings.Split(line, " | ")
		e.Timestamp = parts[0]
		for _, p := range parts[1:] {
			key, val, ok := strings.Cut(p, ":")
			if !ok {
				continue
			}
			switch key {
			case "id":
				e.ID = ID(val)
			case "field":
				e.Field = val
			case "value":
				e.Value = val
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// renderAuditLine formats one entry.
func renderAuditLine(now time.Time, id ID, f auditField) string {
	return fmt.Sprintf("%s | id:%s | field:%s | value:%s",
		now.UTC().Format("2006-01-02T15:04:05Z"), id, f.Field, f.Value)
}

// appendAuditEntries builds audit.md's new content: existing bytes verbatim
// (header included, never rewritten) plus one new line per field change,
// across every Change that carries an ID. A Change with no ID is a
// board-level configuration edit (a WIP limit, the audit toggle itself),
// not an item action, and is not logged. Returns nil when there is nothing
// to append, so the caller writes nothing and audit.md's mtime does not
// move on a transaction that touched no item.
func appendAuditEntries(existing []byte, now time.Time, changes []Change) []byte {
	var lines []string
	for _, c := range changes {
		if c.ID == "" {
			continue
		}
		for _, f := range auditFieldChanges(c) {
			lines = append(lines, renderAuditLine(now, c.ID, f))
		}
	}
	if len(lines) == 0 {
		return nil
	}
	var b strings.Builder
	if len(existing) == 0 {
		b.WriteString(auditHeader)
	} else {
		b.Write(existing)
		if existing[len(existing)-1] != '\n' {
			b.WriteByte('\n')
		}
	}
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}
