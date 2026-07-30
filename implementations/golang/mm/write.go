package mm

import "strings"

// Line-level editing, and the round-trip property everything else depends on.
//
// spec-tools.md §7 forbids rewriting files a change did not touch, reordering
// ## Ready as a side effect, and reformatting untouched lines. These files are
// hand-edited: a tool that reformats on every write makes its own diffs
// unreadable and destroys the property that makes the format worth using.
//
// So writes are SPLICES over the original lines, never a re-render from the
// model. Reconstructing a file from parsed values would silently drop the things
// the parser deliberately ignores - prose, blank lines, HTML comments, YAML
// comments, lines with no colon - and normalise spacing nobody asked it to
// touch. Retaining the lines and editing in place is what makes "parse, write
// back unchanged, get identical bytes" true by construction rather than by
// careful bookkeeping.

// fileEdit is a line-oriented editor over one parsed file.
//
// Line numbers held elsewhere - frontmatter keys, item sources, section
// headings - are registered with track() and adjusted whenever a splice moves
// them, so a caller can hold onto an *Item across an edit without its
// Source.Line going stale.
type fileEdit struct {
	name  string
	lines []string
	fm    *Frontmatter
	refs  []*int // 1-based line numbers that move when lines are spliced

	// orig is the file as read. Dirty() compares against it rather than
	// tracking whether an operation ran, because operations that cancel out -
	// a --move that removes a line and re-inserts it where it already was -
	// must still count as no change. §7 requires a no-op to write nothing.
	orig string
}

func newFileEdit(name string, lines []string, fm *Frontmatter) *fileEdit {
	// Frontmatter line numbers are NOT registered here: shift() updates fm.End
	// and the fm.lines index directly, because Go map entries are not
	// addressable. Registering them as refs as well would move them twice.
	return &fileEdit{name: name, lines: lines, fm: fm, orig: strings.Join(lines, "\n")}
}

// track registers a line number that must move with future splices.
func (e *fileEdit) track(p *int) { e.refs = append(e.refs, p) }

// Dirty reports whether the file's CONTENT changed. A no-op operation must leave
// the file untouched - no rewrite, no mtime bump, no diff under version control -
// and that includes an operation whose individual splices cancel out.
func (e *fileEdit) Dirty() bool { return string(e.Bytes()) != e.orig }

// Bytes renders the file. When nothing changed this is the original input, byte
// for byte, including the trailing newline.
func (e *fileEdit) Bytes() []byte { return []byte(strings.Join(e.lines, "\n")) }

// shift moves every tracked line number at or after `from` by delta, and does
// the same for the frontmatter key index.
func (e *fileEdit) shift(from, delta int) {
	for _, p := range e.refs {
		if *p >= from {
			*p += delta
		}
	}
	if e.fm != nil {
		for k, ln := range e.fm.lines {
			if ln >= from {
				e.fm.lines[k] = ln + delta
			}
		}
		if e.fm.End >= from {
			e.fm.End += delta
		}
	}
}

// ReplaceLine rewrites one 1-based line. A rewrite to the identical text is a
// no-op and does not mark the file dirty.
func (e *fileEdit) ReplaceLine(n int, text string) {
	if n < 1 || n > len(e.lines) {
		return
	}
	if e.lines[n-1] == text {
		return
	}
	e.lines[n-1] = text
}

// InsertLine inserts text so that it becomes line n (1-based).
func (e *fileEdit) InsertLine(n int, text string) {
	if n < 1 {
		n = 1
	}
	if n > len(e.lines)+1 {
		n = len(e.lines) + 1
	}
	e.lines = append(e.lines, "")
	copy(e.lines[n:], e.lines[n-1:])
	e.lines[n-1] = text
	e.shift(n, 1)
}

// DeleteLine removes one 1-based line.
func (e *fileEdit) DeleteLine(n int) {
	if n < 1 || n > len(e.lines) {
		return
	}
	e.lines = append(e.lines[:n-1], e.lines[n:]...)
	e.shift(n+1, -1)
}

// SetFM sets a frontmatter value, rewriting only that one line.
//
// Re-rendering the whole block would be simpler and wrong: it would drop lines
// with no colon, strip YAML comments the parser removed from values, and
// normalise spacing on keys nobody touched.
func (e *fileEdit) SetFM(key, value string) {
	if e.fm == nil {
		return
	}
	if e.fm.Has(key) && e.fm.Get(key) == value {
		return // no change; do not dirty the file
	}
	if e.fm.Has(key) {
		e.ReplaceLine(e.fm.Line(key), key+": "+value)
		e.fm.Set(key, value)
		return
	}
	// New key: insert immediately before the closing delimiter.
	at := e.fm.End
	if at < 2 {
		at = 2
	}
	e.InsertLine(at, key+": "+value)
	e.fm.Set(key, value)
	e.fm.lines[key] = at
}

// DeleteFM removes a frontmatter key and its line.
//
// Needed when a slot goes idle: the keys an item brought with it have to go with
// it, or the next item to occupy the file inherits the previous one's fields.
func (e *fileEdit) DeleteFM(key string) {
	if e.fm == nil || !e.fm.Has(key) {
		return
	}
	line := e.fm.Line(key)
	e.fm.Delete(key)
	e.DeleteLine(line)
}

// ---------------------------------------------------------------------------
// Editors over the parsed file types
// ---------------------------------------------------------------------------

// Edit returns a line editor over the backlog, with every item and section
// heading registered so their recorded positions stay accurate across splices.
func (b *backlogFile) Edit() *fileEdit {
	e := newFileEdit(b.Name, b.Lines, b.FM)
	for _, s := range b.Sections {
		e.track(&s.HeadLine)
	}
	for _, it := range b.Items {
		e.track(&it.Source.Line)
	}
	b.edit = e
	return e
}

// Edit returns a line editor over done.md.
func (d *doneFile) Edit() *fileEdit {
	e := newFileEdit(d.Name, d.Lines, d.FM)
	for _, m := range d.Months {
		e.track(&m.HeadLine)
	}
	for _, it := range d.Items {
		e.track(&it.Source.Line)
	}
	d.edit = e
	return e
}

// Edit returns a line editor over a working file.
func (w *workingFile) Edit() *fileEdit {
	e := newFileEdit(w.Name, w.Lines, w.FM)
	if w.Item != nil {
		e.track(&w.Item.Source.Line)
	}
	w.edit = e
	return e
}

// ---------------------------------------------------------------------------
// Section bodies
// ---------------------------------------------------------------------------

// sectionRange locates a "## heading" and returns its 1-based heading line and
// the 1-based line of whatever ends it - the next "## " heading, or one past the
// end of the file.
//
// The scan starts after the frontmatter so that a "## " inside the header block
// cannot be mistaken for a section.
func sectionRange(e *fileEdit, fm *Frontmatter, heading string) (head, end int, ok bool) {
	want := "## " + heading
	start := 0
	if fm != nil {
		start = fm.End
	}
	for i := start; i < len(e.lines); i++ {
		if strings.TrimSpace(e.lines[i]) == want {
			head = i + 1
			break
		}
	}
	if head == 0 {
		return 0, 0, false
	}
	end = len(e.lines) + 1
	for i := head; i < len(e.lines); i++ {
		if strings.HasPrefix(e.lines[i], "## ") {
			end = i + 1
			break
		}
	}
	return head, end, true
}

// sectionBody returns a section's content with surrounding blank lines removed.
func sectionBody(e *fileEdit, fm *Frontmatter, heading string) string {
	head, end, ok := sectionRange(e, fm, heading)
	if !ok {
		return ""
	}
	return strings.TrimSpace(strings.Join(e.lines[head:end-1], "\n"))
}

// clearSection empties a section, leaving the heading and one blank line.
//
// Used when a slot goes idle: the subtasks in ## Plan are scratch by design and
// the other sections belong to the item that just left.
func clearSection(e *fileEdit, fm *Frontmatter, heading string) {
	head, end, ok := sectionRange(e, fm, heading)
	if !ok {
		return
	}
	for n := end - 1; n > head; n-- {
		e.DeleteLine(n)
	}
	e.InsertLine(head+1, "")
}

// ---------------------------------------------------------------------------
// Item-level splices
// ---------------------------------------------------------------------------

// ReplaceItem rewrites an item's line in place. Field order is normalised to
// canonical order on this line only - never as a pass over the whole file.
func (e *fileEdit) ReplaceItem(it *Item) {
	e.ReplaceLine(it.Source.Line, RenderItemLine(it))
}

// RemoveItem deletes an item's line, and collapses a blank line left behind so
// that repeated removals do not accumulate whitespace.
func (e *fileEdit) RemoveItem(it *Item) {
	n := it.Source.Line
	e.DeleteLine(n)
	// If the removal left two consecutive blanks where there was one, drop one.
	if n >= 2 && n <= len(e.lines) &&
		strings.TrimSpace(e.lines[n-1]) == "" && strings.TrimSpace(e.lines[n-2]) == "" {
		e.DeleteLine(n)
	}
}

// insertPos computes the 1-based line at which to insert the index'th item of a
// run, given the heading line and the items already present.
//
// index is 0-based: 0 means "first under the heading", len(items) means "last".
// With no items yet, the item goes after the heading and any blank line that
// follows it, so the file keeps the shape a person would have typed.
func insertPos(e *fileEdit, headLine int, items []*Item, index int) int {
	if index < 0 {
		index = 0
	}
	if index > len(items) {
		index = len(items)
	}
	if len(items) == 0 {
		// Skip blank lines and HTML comments under the heading. The comment is
		// the placeholder a person leaves in an empty section - "the first one
		// goes below this line" - and inserting above it makes the file
		// contradict itself.
		//
		// The scan stops one short of the end. splitLines gives a file ending in
		// a newline a final empty element, and that element IS the trailing
		// newline: stepping past it appends after the end of the file and the
		// result has no final newline at all.
		at := headLine + 1
		for at < len(e.lines) {
			l := strings.TrimSpace(e.lines[at-1])
			if l == "" || isHTMLComment(l) {
				at++
				continue
			}
			break
		}
		// A list that starts immediately after prose or an HTML comment does not
		// render as a list, and a heading pressed against the item above reads as
		// a mistake. Both blanks are inserted here rather than by the caller,
		// which only ever writes one line.
		if at > 1 && strings.TrimSpace(e.lines[at-2]) != "" {
			e.InsertLine(at, "")
			at++
		}
		if at <= len(e.lines) && strings.HasPrefix(strings.TrimSpace(e.lines[at-1]), "## ") {
			e.InsertLine(at, "")
		}
		return at
	}
	if index == len(items) {
		return items[index-1].Source.Line + 1
	}
	return items[index].Source.Line
}

// RemoveItem deletes an item's line and drops it from the model.
//
// The line splice alone is not enough: b.Items and the section's list are what
// later index arithmetic reads, so leaving a removed item in them makes the next
// insert land in the wrong place.
func (b *backlogFile) RemoveItem(e *fileEdit, it *Item) {
	e.RemoveItem(it)
	for _, sec := range b.Sections {
		for i, x := range sec.Items {
			if x == it {
				sec.Items = append(sec.Items[:i], sec.Items[i+1:]...)
				renumber(sec.Items)
				break
			}
		}
	}
	for i, x := range b.Items {
		if x == it {
			b.Items = append(b.Items[:i], b.Items[i+1:]...)
			break
		}
	}
}

// RemoveItem deletes an item's line from done.md and drops it from the model.
//
// An emptied month group keeps its heading: the format requires every "## "
// heading here to be a month, not that a month hold anything.
func (d *doneFile) RemoveItem(e *fileEdit, it *Item) {
	e.RemoveItem(it)
	for _, m := range d.Months {
		for i, x := range m.Items {
			if x == it {
				m.Items = append(m.Items[:i], m.Items[i+1:]...)
				renumber(m.Items)
				break
			}
		}
	}
	for i, x := range d.Items {
		if x == it {
			d.Items = append(d.Items[:i], d.Items[i+1:]...)
			break
		}
	}
}

// InsertItem inserts an item into a backlog section at a 0-based index.
func (b *backlogFile) InsertItem(e *fileEdit, sec Section, index int, it *Item) {
	s := b.Section(sec)
	if s == nil {
		return
	}
	// State first: the box is a function of the file an item lives in, so the
	// line cannot be rendered until the item knows where it is going.
	it.State = StateBacklog
	it.Section = sec

	at := insertPos(e, s.HeadLine, s.Items, index)
	e.InsertLine(at, RenderItemLine(it))
	it.Source = Location{File: b.Name, Line: at}
	e.track(&it.Source.Line)

	if index > len(s.Items) {
		index = len(s.Items)
	}
	s.Items = append(s.Items, nil)
	copy(s.Items[index+1:], s.Items[index:])
	s.Items[index] = it
	b.Items = append(b.Items, it)
	renumber(s.Items)
}

// InsertItem inserts an item at the top of a month group in done.md, creating
// the group if absent. Newest first, within the file and within the group.
func (d *doneFile) InsertItem(e *fileEdit, month string, it *Item) {
	m := d.Month(month)
	if m == nil {
		m = d.newMonth(e, month)
	}
	// State first, or the line renders with an open box and I3 rejects the very
	// file this call is writing.
	it.State = StateDone

	at := insertPos(e, m.HeadLine, m.Items, 0)
	e.InsertLine(at, RenderItemLine(it))
	it.Source = Location{File: d.Name, Line: at}
	e.track(&it.Source.Line)

	m.Items = append([]*Item{it}, m.Items...)
	d.Items = append(d.Items, it)
	renumber(m.Items)
}

// newMonth creates a "## YYYY-MM" group in newest-first position.
func (d *doneFile) newMonth(e *fileEdit, month string) *monthSpan {
	at, index := d.monthInsertPos(e, month)
	e.InsertLine(at, "")
	e.InsertLine(at+1, "## "+month)

	m := &monthSpan{Month: month, Raw: month, HeadLine: at + 1}
	e.track(&m.HeadLine)
	d.Months = append(d.Months, nil)
	copy(d.Months[index+1:], d.Months[index:])
	d.Months[index] = m
	return m
}

// monthInsertPos finds where a new month heading belongs so that groups stay in
// descending order, and returns the line and the index within d.Months.
func (d *doneFile) monthInsertPos(e *fileEdit, month string) (line, index int) {
	for i, m := range d.Months {
		if m.Month != "" && m.Month < month {
			return m.HeadLine - 1, i
		}
	}
	if n := len(d.Months); n > 0 {
		last := d.Months[n-1]
		at := last.HeadLine
		if len(last.Items) > 0 {
			at = last.Items[len(last.Items)-1].Source.Line
		}
		return at + 1, n
	}
	// No groups yet: after the frontmatter and any leading prose.
	at := len(e.lines)
	for at > 0 && strings.TrimSpace(e.lines[at-1]) == "" {
		at--
	}
	return at + 1, 0
}

// isHTMLComment reports whether a line is a whole HTML comment, which is how a
// person marks an empty section: "the first one goes below this line".
func isHTMLComment(line string) bool {
	return strings.HasPrefix(line, "<!--") && strings.HasSuffix(line, "-->")
}

// renumber refreshes the 1-based Pos of every item in a run.
func renumber(items []*Item) {
	for i, it := range items {
		it.Pos = i + 1
	}
}
