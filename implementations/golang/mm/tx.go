package mm

import (
	"fmt"
	"path/filepath"
	"strings"
)

// The transaction envelope, from spec-tools.md §7.
//
// Every mutation is a transaction over one directory, and must be
// all-or-nothing: --start touches a working file and backlog.md, --edit --title
// touches an item's file and a detail file, and a partial application of either
// leaves the directory invalid.
//
// The sequence is fixed:
//
//  1. read the whole directory into a model, recording each file's stamp
//  2. modify the model in memory - never a file incrementally
//  3. VALIDATE the result
//  4. write, atomically, in an order chosen so a crash is recoverable
//
// Step 3 is what makes it impossible for the tool to produce a directory its own
// checker rejects. It uses the same validator as --check; a second copy of the
// rules would drift, and the drift would surface as a file the tool wrote and
// then refused to read.

// TxResult reports what a transaction did, or would have done.
//
// Exported because it is part of the API: spec-tools.md §6.2 requires every
// mutating operation to return the change set it produced, so that a front end
// re-renders from the return value instead of re-reading the directory. An
// unexported result type would be returned but unnameable, which means a caller
// could not write a function that takes one.
type TxResult struct {
	Changes []Change
	Files   []string // paths that would be written or removed
	DryRun  bool
}

// tx is a transaction in progress.
type tx struct {
	store   *Store
	model   *dirModel
	ws      writeSet
	changes []Change
	dirty   map[string]*fileEdit // relative name -> editor, for files being changed

	// baseline is what was already wrong when the transaction opened. It MUST be
	// captured here rather than at commit time: by then the model has been
	// modified in memory, so "before" and "after" would be the same set and no
	// violation would ever look introduced.
	baseline map[string]struct{}
}

// begin loads the directory and prepares a transaction.
//
// Pre-existing violations do NOT block a transaction. spec-tools.md §8: a
// mutation on a directory that is already broken must fix or preserve the
// problem, and must not be blocked by a violation unrelated to the item being
// touched. Refusing would leave the user unable to use the tool to repair the
// very thing the tool is complaining about.
//
// A git conflict marker is the one exception, and the format spec makes it one
// (§5.1: "the one reserved exception"): a half-merged file is not a directory
// with a problem in it, it is two directories that have not been reconciled,
// and writing on top of one silently discards a side of the merge. Reads stay
// open — --list, --show and --check are exactly what you want at that moment —
// but nothing writes until the merge is resolved. --fix refuses on the same
// grounds, one layer up (op_fix.go).
func (s *Store) begin() (*tx, error) {
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	vs := m.validate()
	if markers := conflictMarkerFindings(vs); len(markers) > 0 {
		return nil, &InvariantError{Violations: markers}
	}
	return &tx{
		store:    s,
		model:    m,
		dirty:    map[string]*fileEdit{},
		baseline: violationKeys(vs),
	}, nil
}

// conflictMarkerFindings picks the marker lines out of a validation result.
//
// Matching on the message is not lovely, but the alternative — a second
// invariant code for something the format spec calls a parse error — would put
// a rule in the checker's vocabulary that the spec does not have.
func conflictMarkerFindings(vs []Violation) []Violation {
	var out []Violation
	for _, v := range vs {
		if strings.HasPrefix(v.Message, markerMessagePrefix) {
			out = append(out, v)
		}
	}
	return out
}

// backlog returns the backlog editor, creating it on first use.
func (t *tx) backlog() (*backlogFile, *fileEdit, error) {
	if t.model.backlog == nil {
		return nil, nil, fmt.Errorf("%w: backlog.md is missing", ErrNotFound)
	}
	e, ok := t.dirty["backlog.md"]
	if !ok {
		e = t.model.backlog.Edit()
		t.dirty["backlog.md"] = e
	}
	return t.model.backlog, e, nil
}

// done returns the done.md editor, creating it on first use.
func (t *tx) done() (*doneFile, *fileEdit, error) {
	if t.model.done == nil {
		return nil, nil, fmt.Errorf("%w: done.md is missing", ErrNotFound)
	}
	e, ok := t.dirty["done.md"]
	if !ok {
		e = t.model.done.Edit()
		t.dirty["done.md"] = e
	}
	return t.model.done, e, nil
}

// working returns a slot editor, creating it on first use.
func (t *tx) working(w *workingFile) *fileEdit {
	e, ok := t.dirty[w.Name]
	if !ok {
		e = w.Edit()
		t.dirty[w.Name] = e
	}
	return e
}

// record adds a Change to the transaction's report.
func (t *tx) record(c Change) { t.changes = append(t.changes, c) }

// stage queues the files a transaction touched, in the order given.
//
// ORDER IS THE DURABILITY STRATEGY (§7 rule 4). A multi-file update cannot be
// made atomic on a POSIX filesystem, so the order is chosen so that an
// interruption fails validation loudly rather than losing an item:
//
//	--start   working file first, then backlog.md
//	--pause   backlog.md first, then the working file
//	--finish  done.md first, then the working file
//
// The rule in every case: write the copy before removing the original. A crash
// between the two leaves a duplicate ID, which I1 reports; the other order
// leaves nothing at all.
func (t *tx) stage(names ...string) {
	for _, name := range names {
		e, ok := t.dirty[name]
		if !ok || !e.Dirty() {
			continue // untouched, or touched and unchanged: write nothing
		}
		t.ws.Add(filepath.Join(t.store.path, name), e.Bytes(), t.stampFor(name))
	}
}

// stampFor returns the stamp taken when a file was read.
//
// A file the load never saw is MISSING, not a zero-length file that exists: the
// zero stamp would compare unequal to the real absent file and every creation
// would abort as a phantom conflict.
func (t *tx) stampFor(name string) stamp {
	if st, ok := t.model.stamps[name]; ok {
		return st
	}
	return stamp{missing: true}
}

// stageRaw queues a file whose content is produced outside the line editors, such
// as a detail file being created.
func (t *tx) stageRaw(name string, data []byte) {
	t.ws.Add(filepath.Join(t.store.path, name), data, t.stampFor(name))
}

// commit validates the resulting model and, if it is sound, writes.
//
// dryRun performs every step except the write, so the report a caller gets is
// the report of what would really happen - including a validation failure. An
// operation that cannot be dry-run does not exist (spec-tools.md §3.4).
func (t *tx) commit(dryRun bool) (TxResult, error) {
	res := TxResult{Changes: t.changes, Files: t.ws.Paths(), DryRun: dryRun}

	// Step 3. Compare against what was already wrong at begin(): a pre-existing
	// violation must not be blamed on this change, and must not block it either.
	after := t.reparse()
	var introduced []Violation
	for _, v := range after {
		if _, existed := t.baseline[violationKey(v)]; !existed {
			introduced = append(introduced, v)
		}
	}
	if len(introduced) > 0 {
		return res, &InvariantError{Violations: introduced}
	}

	if dryRun || t.ws.Empty() {
		return res, nil
	}
	if err := t.ws.Commit(); err != nil {
		return res, err
	}
	return res, nil
}

// reparse validates the transaction's pending bytes by parsing them back.
//
// Re-parsing rather than validating the in-memory model is deliberate: it proves
// the bytes about to be written can be read, which catches a splice that
// produced something the parser cannot see - an item line inserted outside any
// section, a heading damaged by an off-by-one. Validating the model alone would
// trust the very code under test.
func (t *tx) reparse() []Violation {
	m := &dirModel{
		path:    t.model.path,
		details: t.model.details,
		entries: t.model.entries,
		stamps:  t.model.stamps,
	}
	bytesFor := func(name string, fallback []string) []byte {
		if e, ok := t.dirty[name]; ok {
			return e.Bytes()
		}
		return []byte(joinLines(fallback))
	}

	// The grammar comes from the re-parsed backlog, never from the in-memory
	// model: a transaction that edited the frontmatter must be validated under
	// the grammar it is about to write.
	g := DefaultIDGrammar()
	if t.model.backlog != nil {
		b, vs := parseBacklog("backlog.md", bytesFor("backlog.md", t.model.backlog.Lines))
		m.backlog = b
		m.parseVs = append(m.parseVs, vs...)
		g = b.grammar
	}
	if t.model.done != nil {
		d, vs := parseDoneG("done.md", bytesFor("done.md", t.model.done.Lines), g)
		m.done = d
		m.parseVs = append(m.parseVs, vs...)
	}
	for _, w := range t.model.working {
		nw, vs := parseWorkingG(w.Name, bytesFor(w.Name, w.Lines), g)
		m.working = append(m.working, nw)
		m.parseVs = append(m.parseVs, vs...)
	}
	return m.validate()
}

// violationKey identifies a finding well enough to tell a pre-existing problem
// from one this change introduced. Line numbers are excluded: a splice moves
// unrelated findings up or down without making them new.
//
// That applies to a line number INSIDE the message too — I1 says "T-0001 is
// already defined at backlog.md:11" — or the exclusion would only be half
// done. --migrate found this: inserting a `project:` line into the frontmatter
// shifts every line below it, so a pre-existing duplicate's message changed
// from :11 to :12 and the pre-commit check refused a migration for a violation
// it had not introduced and did not touch.
func violationKey(v Violation) string {
	return v.Invariant + "\x00" + v.At.File + "\x00" + maskLineRefs(v.Message)
}

// maskLineRefs replaces the line number in every "<file>.md:<n>" reference with
// a placeholder. Only that shape: a bare number in a message ("3 items") is
// content, not a location.
func maskLineRefs(msg string) string {
	const marker = ".md:"
	out := msg
	for i := 0; ; {
		j := strings.Index(out[i:], marker)
		if j < 0 {
			return out
		}
		start := i + j + len(marker)
		end := start
		for end < len(out) && out[end] >= '0' && out[end] <= '9' {
			end++
		}
		if end == start {
			i = start
			continue
		}
		out = out[:start] + "N" + out[end:]
		i = start + 1
	}
}

func violationKeys(vs []Violation) map[string]struct{} {
	out := make(map[string]struct{}, len(vs))
	for _, v := range vs {
		out[violationKey(v)] = struct{}{}
	}
	return out
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
