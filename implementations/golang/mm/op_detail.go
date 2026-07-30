package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Detail files: creation, reading, writing, and detaching.
//
// A detail file holds the long-form description of exactly one item. Two
// properties make it worth having its own operations rather than being a blob
// the caller manages:
//
//   - It NEVER MOVES. The item line travels between backlog.md, a working slot
//     and done.md; details/<ID>.md stays at one path for the item's whole life,
//     which is why the long text survives every transition without being
//     re-pasted, and why done.md keeps the detail: field.
//   - Its frontmatter duplicates the item's id and title, and that duplication
//     is the ONLY mechanism by which drift is detectable (I9). Every path that
//     changes a title has to maintain it - see tx.syncDetailTitle.

// templateName is the conventional starting shape for a new detail file. Files
// beginning with "_" are templates: exempt from I9, never any item's detail.
const templateName = "details/_template.md"

// Detail is one item's long-form description.
type Detail struct {
	Path  string // "details/T-0042.md"
	ID    ID
	Title string
	Body  string // everything after the frontmatter
}

// Detail reads an item's detail file.
func (s *Store) Detail(id ID) (Detail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.load()
	if err != nil {
		return Detail{}, err
	}
	it := m.find(id)
	if it == nil {
		return Detail{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.Detail == "" {
		return Detail{}, fmt.Errorf("%w: %s has no detail file", ErrNotFound, id)
	}
	df, ok := m.details[it.Detail]
	if !ok {
		return Detail{}, fmt.Errorf("%w: %s references %s, which does not exist",
			ErrNotFound, id, it.Detail)
	}
	return Detail{
		Path:  df.Name,
		ID:    ID(df.FM.Get("id")),
		Title: df.FM.Get("title"),
		Body:  bodyOf(df.Lines, df.FM),
	}, nil
}

// AttachDetailRequest creates a detail file for an item that has none.
type AttachDetailRequest struct {
	// Body, when non-empty, becomes the file's body. When empty the template is
	// used, or a built-in shape if the directory has no template.
	Body string

	// FromTemplate forces the template even when Body is set, appending Body
	// after it. Mostly useful for --add --detail with a first note.
	FromTemplate bool

	DryRun bool
}

// AttachDetail creates details/<ID>.md and points the item at it.
//
// The path is not a parameter. I8 requires details/<that item's ID>.md, so
// offering a choice would only offer a way to be wrong.
func (s *Store) AttachDetail(id ID, req AttachDetailRequest, today Date) (Detail, txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero Detail
	t, err := s.begin()
	if err != nil {
		return zero, txResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return zero, txResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	path := it.DetailPath()
	if it.Detail != "" {
		return zero, txResult{}, fmt.Errorf("%w: %s already has a detail file at %s",
			ErrAlreadyExists, id, it.Detail)
	}
	if _, exists := t.model.details[path]; exists {
		// The file is there but nothing points at it - an I9 orphan. Adopting it
		// is the repair, and losing its contents would not be.
		return zero, txResult{}, fmt.Errorf(
			"%w: %s already exists as an orphan; adopt it with --set detail:%s",
			ErrConflict, path, path)
	}

	body := req.Body
	if body == "" || req.FromTemplate {
		tmpl := s.readTemplate()
		if req.Body != "" {
			body = tmpl + "\n" + strings.TrimRight(req.Body, "\n") + "\n"
		} else {
			body = tmpl
		}
	}
	content := renderDetailFile(it, body, today)

	it.Detail = path
	file, err := t.writeItemLine(it, today)
	if err != nil {
		return zero, txResult{}, err
	}
	t.record(Change{Kind: ChangeUpdated, ID: id, File: file})
	t.stageRaw(path, []byte(content))
	t.record(Change{Kind: ChangeCreated, ID: id, File: path})
	t.model.details[path] = mustParseDetail(path, content)

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return Detail{Path: path, ID: id, Title: it.Title, Body: body}, res, nil
}

// SetDetailBody replaces a detail file's body, leaving its frontmatter alone.
//
// The frontmatter is not the caller's to write: id and title are maintained by
// the operations that change them, and a caller that could set them freely could
// break I9 at will.
func (s *Store) SetDetailBody(id ID, body string, dryRun bool, today Date) (txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.begin()
	if err != nil {
		return txResult{}, err
	}
	df, _, err := t.detailFor(id)
	if err != nil {
		return txResult{}, err
	}
	e := t.detailEdit(df)
	replaceBody(e, df.FM, body)
	if df.FM.Has("updated") {
		e.SetFM("updated", today.String())
	}
	if e.Dirty() {
		t.stageRaw(df.Name, e.Bytes())
		t.record(Change{Kind: ChangeUpdated, ID: id, File: df.Name})
		t.model.details[df.Name] = mustParseDetail(df.Name, string(e.Bytes()))
	}
	return t.commit(dryRun)
}

// AppendDetailSection appends text under a heading, creating the heading if it
// is absent.
//
// This is how --pause and --finish preserve a working file's ## Notes: those
// notes exist nowhere else, and the moment an item leaves its slot is the last
// moment they exist at all.
func (s *Store) AppendDetailSection(id ID, heading, text string, dryRun bool, today Date) (txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.begin()
	if err != nil {
		return txResult{}, err
	}
	df, _, err := t.detailFor(id)
	if err != nil {
		return txResult{}, err
	}
	e := t.detailEdit(df)
	appendUnderHeading(e, df.FM, heading, text)
	if df.FM.Has("updated") {
		e.SetFM("updated", today.String())
	}
	if e.Dirty() {
		t.stageRaw(df.Name, e.Bytes())
		t.record(Change{Kind: ChangeUpdated, ID: id, File: df.Name})
		t.model.details[df.Name] = mustParseDetail(df.Name, string(e.Bytes()))
	}
	return t.commit(dryRun)
}

// DetachDetailRequest clears an item's detail: field.
//
// Exactly one of Delete or AllowOrphan MUST be set. Clearing the field on its own
// leaves a file nothing points at, which is an I9 violation, and the transaction
// envelope refuses to write a directory its own checker would reject. Making the
// caller choose is the only honest option: silently leaving an invalid directory
// is what spec-tools.md §5.1.6 forbids, and silently deleting a file full of the
// user's notes is worse.
type DetachDetailRequest struct {
	// Delete removes the file in the same transaction.
	Delete bool

	// AllowOrphan keeps the file and accepts the resulting I9 violation. The
	// repair afterwards is to attach it to another item or delete it by hand.
	AllowOrphan bool

	DryRun bool
}

// DetachDetail removes the link between an item and its detail file.
func (s *Store) DetachDetail(id ID, req DetachDetailRequest, today Date) (txResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, err := s.begin()
	if err != nil {
		return txResult{}, err
	}
	it := t.model.find(id)
	if it == nil {
		return txResult{}, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.Detail == "" {
		return txResult{}, fmt.Errorf("%w: %s has no detail file", ErrNotFound, id)
	}
	if req.Delete == req.AllowOrphan {
		return txResult{}, fmt.Errorf(
			"%w: detaching %s from %s leaves the file with no item; pass Delete to remove it, "+
				"or AllowOrphan to keep it and accept the I9 violation",
			ErrPreconditionFailed, id, it.Detail)
	}
	path := it.Detail

	it.Detail = ""
	if _, err := t.writeItemLine(it, today); err != nil {
		return txResult{}, err
	}

	if req.Delete {
		t.ws.Delete(filepath.Join(s.path, path))
		t.record(Change{Kind: ChangeDeleted, ID: id, File: path})
		// Validation must not see a file that is about to be removed, or it
		// would report the orphan this deletion exists to prevent.
		delete(t.model.details, path)
	} else {
		// AllowOrphan: the I9 violation is deliberate, so it is added to the
		// baseline rather than treated as introduced by this change.
		t.baseline[violationKey(Violation{
			Invariant: "I9", At: Location{File: path},
			Message: "orphan — no item references it",
		})] = struct{}{}
		t.record(Change{Kind: ChangeUpdated, ID: id, File: path})
	}
	return t.commit(req.DryRun)
}

// readTemplate returns the directory's detail template, or a built-in shape.
//
// The template is read from disk rather than kept in the binary so that a
// directory can carry its own conventions - which is the point of shipping a
// _template.md at all.
func (s *Store) readTemplate() string {
	data, err := os.ReadFile(filepath.Join(s.path, templateName))
	if err != nil {
		return builtinDetailBody
	}
	lines := splitLines(data)
	fm, err2 := parseFrontmatter(templateName, lines)
	if err2 != nil {
		// A template with no frontmatter is still usable as a body.
		return strings.TrimLeft(string(data), "\n")
	}
	body := bodyOf(lines, fm)
	// Drop the template's own H1: renderDetailFile writes one for the item.
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "# ") {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimLeft(strings.Join(out, "\n"), "\n")
}

const builtinDetailBody = `## Context

## Requirements

## Open questions

## References
`

// ---------------------------------------------------------------------------
// transaction helpers
// ---------------------------------------------------------------------------

// detailFor resolves an item's detail file within a transaction.
func (t *tx) detailFor(id ID) (*detailFile, *Item, error) {
	it := t.model.find(id)
	if it == nil {
		return nil, nil, fmt.Errorf("%w: %s is not in this directory", ErrNotFound, id)
	}
	if it.Detail == "" {
		return nil, nil, fmt.Errorf("%w: %s has no detail file", ErrNotFound, id)
	}
	df, ok := t.model.details[it.Detail]
	if !ok {
		return nil, nil, fmt.Errorf("%w: %s references %s, which does not exist",
			ErrNotFound, id, it.Detail)
	}
	return df, it, nil
}

// detailEdit returns the editor for a detail file, creating it on first use.
func (t *tx) detailEdit(df *detailFile) *fileEdit {
	if e, ok := t.dirty[df.Name]; ok {
		return e
	}
	e := newFileEdit(df.Name, df.Lines, df.FM)
	t.dirty[df.Name] = e
	return e
}

// writeItemLine rewrites an item wherever it lives and returns the file it wrote
// to. Shared by every operation that changes a field without moving the item.
func (t *tx) writeItemLine(it *Item, today Date) (string, error) {
	switch it.State {
	case StateBacklog:
		_, e, err := t.backlog()
		if err != nil {
			return "", err
		}
		e.ReplaceItem(it)
		touchUpdated(e, today)
		t.stage("backlog.md")
		return "backlog.md", nil

	case StateDone:
		_, e, err := t.done()
		if err != nil {
			return "", err
		}
		e.ReplaceItem(it)
		touchUpdated(e, today)
		t.stage("done.md")
		return "done.md", nil

	case StateWorking:
		w := t.slotOf(it.ID)
		if w == nil {
			return "", fmt.Errorf("%w: %s claims to be working but is in no slot",
				ErrConflict, it.ID)
		}
		e := t.working(w)
		// SetFM compares against what is in the file, so the new values must not
		// be written into the parsed frontmatter first.
		for _, f := range workingFields(it) {
			e.SetFM(f.Key, f.Value)
		}
		t.stage(w.Name)
		return w.Name, nil
	}
	return "", fmt.Errorf("%w: %s has no state", ErrConflict, it.ID)
}

// ---------------------------------------------------------------------------
// body manipulation
// ---------------------------------------------------------------------------

// bodyOf returns everything after the frontmatter, with the leading blank line
// removed so a body round-trips through Detail.Body unchanged.
func bodyOf(lines []string, fm *Frontmatter) string {
	if fm == nil || fm.End <= 0 || fm.End >= len(lines) {
		return ""
	}
	rest := lines[fm.End:]
	for len(rest) > 0 && strings.TrimSpace(rest[0]) == "" {
		rest = rest[1:]
	}
	return strings.Join(rest, "\n")
}

// replaceBody swaps everything after the frontmatter for new content.
func replaceBody(e *fileEdit, fm *Frontmatter, body string) {
	for len(e.lines) > fm.End {
		e.DeleteLine(len(e.lines))
	}
	e.InsertLine(len(e.lines)+1, "")
	for _, l := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		e.InsertLine(len(e.lines)+1, l)
	}
	e.InsertLine(len(e.lines)+1, "")
}

// appendUnderHeading appends text to the end of a "## heading" section, adding
// the heading at the end of the file when it is absent.
func appendUnderHeading(e *fileEdit, fm *Frontmatter, heading, text string) {
	want := "## " + heading
	at := -1
	for i := fm.End; i < len(e.lines); i++ {
		if strings.TrimSpace(e.lines[i]) == want {
			at = i + 1 // 1-based
			break
		}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")

	if at < 0 {
		// New section at the end of the file.
		if len(e.lines) > 0 && strings.TrimSpace(e.lines[len(e.lines)-1]) != "" {
			e.InsertLine(len(e.lines)+1, "")
		}
		e.InsertLine(len(e.lines)+1, want)
		e.InsertLine(len(e.lines)+1, "")
		for _, l := range lines {
			e.InsertLine(len(e.lines)+1, l)
		}
		e.InsertLine(len(e.lines)+1, "")
		return
	}

	// Find the end of the section: the next "## " heading, or end of file.
	end := len(e.lines)
	for i := at; i < len(e.lines); i++ {
		if strings.HasPrefix(e.lines[i], "## ") {
			end = i
			break
		}
	}
	// Back up over trailing blanks so the insert lands against the content.
	for end > at && strings.TrimSpace(e.lines[end-1]) == "" {
		end--
	}
	ins := end + 1
	e.InsertLine(ins, "")
	ins++
	for _, l := range lines {
		e.InsertLine(ins, l)
		ins++
	}
}
