package cli

import (
	"fmt"
	"strings"

	"micromanager/mm"
)

// --porcelain (spec-tools.md §9.3).
//
// Line-oriented, tab-separated, stable across versions, no header. Intended for
// shell pipelines, which means `cut -f2` has to keep meaning the same thing
// next year.
//
// THE FIELD LISTS BELOW ARE A PROMISE. §9.3 allows fields to be APPENDED and
// nothing else — never reordered, never removed, never repurposed. A test pins
// the exact order for that reason: the compiler cannot catch a swapped column,
// and a swapped column silently corrupts every pipeline downstream rather than
// failing.
//
// Nothing here is decorated: no prefixes, no counts, no "ok" line. A pipeline
// consumes records, and a record that is sometimes a summary is not a record.

// porcelainFields documents the columns of each operation, in order. It is
// exported through the help text and pinned by a test.
var porcelainFields = map[Op][]string{
	OpList:    {"id", "state", "section", "prio", "tags", "title"},
	OpShow:    {"id", "state", "section", "prio", "tags", "title"},
	OpAdd:     {"id", "state", "section", "prio", "tags", "title"},
	OpEdit:    {"id", "state", "section", "prio", "tags", "title"},
	OpMove:    {"id", "state", "section", "prio", "tags", "title"},
	OpStart:   {"id", "state", "section", "prio", "tags", "title"},
	OpPause:   {"id", "state", "section", "prio", "tags", "title"},
	OpFinish:  {"id", "state", "section", "prio", "tags", "title"},
	OpBlock:   {"id", "state", "section", "prio", "tags", "title"},
	OpUnblock: {"id", "state", "section", "prio", "tags", "title"},
	OpNote:    {"id", "state", "section", "prio", "tags", "title"},
	OpRemove:  {"id", "state", "section", "prio", "tags", "title"},
	OpCheck:   {"path", "file", "line", "invariant", "message"},
	OpFind:    {"path", "project", "wipUsed", "wipLimit"},
	OpInit:    {"path", "project"},
	OpWip:     {"path", "project", "wipUsed", "wipLimit"},
	OpReport:  {"id", "done", "outcome", "tags", "title"},
}

// porcelainOut accumulates records, for the same reason the JSON envelope does:
// an operation cannot emit as it goes and then discover it failed.
type porcelainOut struct {
	enabled bool
	op      Op
	rows    [][]string
}

func (p *porcelainOut) row(fields ...string) {
	p.rows = append(p.rows, fields)
}

func (p *porcelainOut) item(it mm.Item) {
	p.row(string(it.ID), string(it.State), string(it.Section),
		string(it.Prio.Effective()), mm.FormatTags(it.Tags), it.Title)
}

func (p *porcelainOut) items(items []mm.Item) {
	for _, it := range items {
		p.item(it)
	}
}

// emit writes the rows. On failure it writes nothing at all: a pipeline reading
// records must not be handed a partial set that looks complete, and the exit
// code is what says so.
func (p *porcelainOut) emit(w interface{ Write([]byte) (int, error) }, err error) {
	if err != nil {
		return
	}
	for _, row := range p.rows {
		clean := make([]string, len(row))
		for i, f := range row {
			clean[i] = escapePorcelain(f)
		}
		fmt.Fprintln(w, strings.Join(clean, "\t"))
	}
}

// escapePorcelain keeps one record on one line.
//
// A title cannot contain a pipe (the format forbids it) but nothing stops it
// containing a tab, and a tab inside a field would invent a column. Both tab
// and newline become a space: the value is for a pipeline to match on, and
// silently splitting a record is far worse than losing exact whitespace.
func escapePorcelain(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}
