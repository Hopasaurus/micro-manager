package mm

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The working file set: working.NN.md, one file per WIP slot.
//
// THE COUNT OF THESE FILES IS THE WIP LIMIT. There is no wip_limit setting and
// nothing enforces a limit at write time, because each file holds at most one
// item and an item cannot be started without a free file to put it in. The
// constraint is the shape of the directory rather than a number a tool has to
// check (spec-file-format.md §5.2.1).
//
// Which means the rules below are not bookkeeping - they are what makes the
// limit meaningful. Two files claiming slot 1 (working.1.md and working.01.md)
// would silently be one slot, and a gap in the numbering makes "the count is the
// limit" false.

var workingFileRE = regexp.MustCompile(`^working\.(\d+)\.md$`)

// workingItemKeys are the item's own fields in a slot's frontmatter
// (spec-file-format.md §5.2.2). All seven must be null when the slot is idle.
var workingItemKeys = []string{"id", "title", "prio", "tags", "detail", "created", "started"}

// workingFileKeys belong to the file rather than to the item it holds.
var workingFileKeys = []string{"doc", "version", "status"}

// isWorkingItemField reports whether a frontmatter key carries item data.
//
// Everything that is not a file key is: the format puts the item IN the
// frontmatter, so a key that is not doc/version/status arrived with an item and
// leaves with it. That is what lets --start carry an unregistered field through a
// slot and --pause put it back on the line intact (spec-tools.md §5.1.8), and
// why idling a slot clears those keys rather than leaving one item's data
// attached to the next item to occupy the file.
func isWorkingItemField(key string) bool {
	for _, k := range workingFileKeys {
		if k == key {
			return false
		}
	}
	return true
}

// reservedKey reports whether a key is reserved by spec-file-format.md §9 and so
// cannot be used as an unregistered field.
func reservedKey(key string) bool {
	switch key {
	case "id", "status", "next_id", "doc", "version":
		return true
	}
	return false
}

// workingFile is one parsed slot file.
type workingFile struct {
	Name   string // "working.01.md"
	Number int
	Width  int // digits in the filename
	FM     *Frontmatter
	Lines  []string
	Item   *Item // nil when idle

	edit *fileEdit
}

// Idle reports whether the slot is free to start an item into.
func (w *workingFile) Idle() bool { return w.Item == nil }

// isWorkingFileName reports whether a filename is a working file, and returns
// its slot number and digit width.
func isWorkingFileName(name string) (num, width int, ok bool) {
	m := workingFileRE.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, 0, false
	}
	return n, len(m[1]), true
}

// workingFileName renders a slot filename at a given digit width.
func workingFileName(num, width int) string {
	return fmt.Sprintf("working.%0*d.md", width, num)
}

// discoverWorkingFiles lists the working files in a directory, sorted by slot
// number, and reports problems with the set as a whole (I10).
//
// entries is the directory listing; readFile fetches a file's contents. Passing
// them in keeps this testable without a filesystem.
func discoverWorkingFiles(entries []string, dirLabel string) (names []string, nums, widths []int, vs []Violation) {
	type cand struct {
		name  string
		num   int
		width int
	}
	var cands []cand
	for _, e := range entries {
		if e == "working.md" {
			vs = append(vs, Violation{
				Invariant: "I10",
				At:        Location{File: "working.md"},
				Message:   "legacy name — rename it to working.01.md",
			})
			continue
		}
		if !strings.HasPrefix(e, "working.") || !strings.HasSuffix(e, ".md") {
			continue
		}
		num, width, ok := isWorkingFileName(e)
		if !ok {
			vs = append(vs, Violation{
				Invariant: "I10",
				At:        Location{File: e},
				Message:   "not a working file (want working.NN.md)",
			})
			continue
		}
		cands = append(cands, cand{e, num, width})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].num < cands[j].num })

	for _, c := range cands {
		names = append(names, c.name)
		nums = append(nums, c.num)
		widths = append(widths, c.width)
	}

	if len(names) == 0 {
		vs = append(vs, Violation{
			Invariant: "I10",
			At:        Location{File: dirLabel},
			Message:   "no working.NN.md file — at least one is required",
		})
		return
	}

	// Uniform digit width. Mixed widths would let working.1.md and
	// working.01.md both claim slot 1, so the file count would no longer be
	// the number of distinct slots.
	for _, w := range widths[1:] {
		if w != widths[0] {
			vs = append(vs, Violation{
				Invariant: "I10",
				At:        Location{File: dirLabel},
				Message:   "working files mix digit widths — all must use the same number of digits",
			})
			break
		}
	}

	// Contiguous from 1. A gap makes "the count is the limit" false.
	var missing []string
	for want := 1; want <= len(names); want++ {
		found := false
		for _, n := range nums {
			if n == want {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, strconv.Itoa(want))
		}
	}
	if len(missing) > 0 {
		vs = append(vs, Violation{
			Invariant: "I10",
			At:        Location{File: dirLabel},
			Message: fmt.Sprintf("working files are not numbered 1..%d (missing: %s)",
				len(names), strings.Join(missing, " ")),
		})
	}
	return
}

// parseWorking reads one working file under the default grammar.
func parseWorking(name string, data []byte) (*workingFile, []Violation) {
	return parseWorkingG(name, data, DefaultIDGrammar())
}

// parseWorkingG reads one working file. The ID grammar comes from the
// directory's backlog.md (§3.3.2 rule 1); the working frontmatter carries no
// declaration of its own.
//
// The item is carried in the FRONTMATTER, not as an item line. Every "- [" line
// in the body is a subtask - unstructured by design, with no ID - and must never
// be parsed as an item (spec-file-format.md §5.2.2).
func parseWorkingG(name string, data []byte, g IDGrammar) (*workingFile, []Violation) {
	num, width, _ := isWorkingFileName(name)
	lines := splitLines(data)
	fm, _, vs := readHeader(name, lines)
	w := &workingFile{Name: name, Number: num, Width: width, FM: fm, Lines: lines}

	at := func(key string) Location {
		return Location{File: name, Line: fm.Line(key)}
	}
	bad := func(key, msg string) {
		vs = append(vs, Violation{Invariant: "I4", At: at(key), Message: msg})
	}

	status := fm.Get("status")
	switch status {
	case "idle":
		// Every item field must be null. A leftover value here is a half-
		// finished transition, which is exactly what this check is for.
		for _, key := range workingItemKeys {
			if !fm.IsNull(key) {
				bad(key, fmt.Sprintf("status is idle but %s is %s", key, fm.Get(key)))
			}
		}
		return w, vs

	case "working":
		// fall through

	default:
		bad("status", "status must be working or idle, got: "+status)
		return w, vs
	}

	it := &Item{State: StateWorking, Slot: num, Source: Location{File: name, Line: fm.Line("id")}}

	if fm.IsNull("id") {
		bad("id", "status is working but id is null")
	} else if id := ID(fm.Get("id")); !g.ValidID(string(id)) {
		bad("id", "id is not a "+g.String()+" id: "+fm.Get("id"))
	} else {
		it.ID = id
	}

	// Required while working: a nameless item cannot be returned to the backlog
	// intact, and started is defined by the act of entering this file.
	if fm.IsNull("title") {
		bad("title", "status is working but title is null")
	} else {
		it.Title = fm.Get("title")
	}

	if d, err := ParseDate(fm.Get("started")); err != nil {
		bad("started", fmt.Sprintf("started:%s (want YYYY-MM-DD; set when the item starts)",
			fm.Get("started")))
	} else {
		it.Started = d
	}

	// Optional, but validated with the same rules as on an item line - the
	// format gives both places the same lexical form precisely so one
	// implementation covers both.
	if !fm.IsNull("prio") {
		if p, err := ParsePrio(fm.Get("prio")); err != nil {
			bad("prio", "prio:"+fm.Get("prio")+" (want high, med or low)")
		} else {
			it.Prio = p
		}
	}
	if !fm.IsNull("tags") {
		if tags, err := ParseTags(fm.Get("tags")); err != nil {
			bad("tags", "malformed tags: "+fm.Get("tags")+" (want name,name or null)")
		} else {
			it.Tags = tags
		}
	}
	if !fm.IsNull("created") {
		if d, err := ParseDate(fm.Get("created")); err != nil {
			bad("created", "created:"+fm.Get("created")+" (want YYYY-MM-DD)")
		} else {
			it.Created = d
		}
	}
	if !fm.IsNull("detail") {
		it.Detail = fm.Get("detail")
	}

	// Unregistered fields. They arrived with the item and must leave with it, or
	// --start followed by --pause quietly destroys data written by another tool
	// (spec-file-format.md §9).
	for _, key := range fm.Keys() {
		if !isWorkingItemField(key) || fm.IsNull(key) {
			continue
		}
		if isRegisteredWorkingField(key) {
			continue
		}
		it.Extra = append(it.Extra, Field{Key: key, Value: fm.Get(key)})
	}

	w.Item = it
	return w, vs
}

// isRegisteredWorkingField reports whether a key is one this implementation
// parses into a named Item field rather than into Extra.
func isRegisteredWorkingField(key string) bool {
	for _, k := range workingItemKeys {
		if k == key {
			return true
		}
	}
	return false
}

// renderWorkingIdle produces the frontmatter of an idle slot: every item field
// null, which is the same thing an absent field means on an item line.
func renderWorkingIdle(fm *Frontmatter) {
	fm.Set("status", "idle")
	for _, key := range workingItemKeys {
		fm.Set(key, "null")
	}
}

// workingFields returns the frontmatter an occupied slot should carry, in order.
//
// Values use the SAME lexical form as an item line - tags stays a
// comma-separated TAGLIST, not a YAML list - so moving an item between files is
// a copy and never a conversion. An absent value becomes "null", which is what
// an absent field means on an item line.
//
// This returns the values rather than writing them so that a caller editing a
// file can feed them to fileEdit.SetFM, which rewrites only the lines that
// actually differ. Mutating the parsed frontmatter first would defeat that
// comparison: SetFM would find every value already equal to itself and rewrite
// nothing.
func workingFields(it *Item) []Field {
	nz := func(v string) string {
		if v == "" {
			return "null"
		}
		return v
	}
	out := []Field{
		{"status", "working"},
		{"id", string(it.ID)},
		{"title", it.Title},
		{"prio", nz(string(it.Prio))},
		{"tags", nz(FormatTags(it.Tags))},
		{"detail", nz(it.Detail)},
		{"created", nz(it.Created.String())},
		{"started", nz(it.Started.String())},
	}
	// Unregistered fields ride along as additional keys. §9 makes unknown
	// frontmatter keys valid and requires readers to ignore them, so this is the
	// one place they can live without changing what any other tool sees.
	return append(out, it.Extra...)
}

// renderWorkingItem writes an item into a detached frontmatter block.
func renderWorkingItem(fm *Frontmatter, it *Item) {
	for _, f := range workingFields(it) {
		fm.Set(f.Key, f.Value)
	}
}

// readDirNames lists the entries of a directory by name.
func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrIO, dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}
