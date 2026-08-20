package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// Store is an open micro-manager directory.
//
// It is safe for concurrent use: spec-tools.md §2.3 requires the library to
// satisfy the stricter of its front ends, and the UI service handles requests on
// many goroutines. Synchronisation is internal rather than the caller's problem
// because operations are short and file-bound.
//
// Nothing here reads the environment, the working directory, or argv. The path
// arrives as a parameter, already expanded by whoever owns the process.
type Store struct {
	mu   sync.Mutex
	path string
}

// Open prepares a Store for a directory. It does not read the files: every
// operation re-reads, because the CLI, the UI service and a text editor may all
// be writing and a cached model would go stale between calls.
func Open(path string) (*Store, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("%w: not a directory: %s", ErrNotFound, path)
	}
	return &Store{path: abs}, nil
}

// Path returns the absolute directory path.
func (s *Store) Path() string { return s.path }

// detailFile is a parsed details/T-NNNN.md.
type detailFile struct {
	Name  string // "details/T-0042.md"
	FM    *Frontmatter
	Lines []string
	stamp stamp
}

// dirModel is one consistent read of a whole directory.
//
// parseVs holds everything the parsers reported. Validation adds the cross-file
// invariants on top; the two together are what --check prints.
type dirModel struct {
	path string

	// version 1: backlog.md + working.NN.md. Populated when board.md is
	// absent (store.go's load()); nil for a version-2 directory.
	backlog *backlogFile
	working []*workingFile

	// version 2: board.md. Populated when board.md is present; nil for a
	// version-1 directory. Never both at once (Appendix B: a directory
	// found by both is a collision, reported, not silently merged).
	board *boardFile

	done    *doneFile
	details map[string]*detailFile // keyed by "details/T-0042.md"
	entries []string               // the directory listing
	stamps  map[string]stamp       // path -> as read
	parseVs []Violation
	warnVs  []Violation // non-fatal findings, e.g. id_width outside 3-6
}

// isV2 reports whether this model was loaded as a version-2 (board.md)
// directory.
func (m *dirModel) isV2() bool { return m.board != nil }

// load reads every file of the directory into one model.
//
// It never fails on malformed content - only on I/O. A directory that already
// violates its invariants must still open, list and report (spec-tools.md §8).
func (s *Store) load() (*dirModel, error) {
	entries, err := readDirNames(s.path)
	if err != nil {
		return nil, err
	}
	m := &dirModel{
		path:    s.path,
		details: map[string]*detailFile{},
		entries: entries,
		stamps:  map[string]stamp{},
	}

	read := func(name string) ([]byte, bool) {
		p := filepath.Join(s.path, name)
		m.stamps[name] = stampOf(p)
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, false
		}
		return data, true
	}

	// The ID grammar is declared in board.md's (or backlog.md's) frontmatter
	// (§3.3.2), and must be read BEFORE any other ID is interpreted (rule 4).
	// board.md is checked first: its presence is what makes a directory
	// version 2 (spec-file-format.md Appendix B); a directory carrying both
	// is the sibling-collision case, not a version to guess between, and is
	// left to discovery/--check to report rather than resolved silently here.
	var g IDGrammar
	switch {
	case slices.Contains(entries, "board.md"):
		data, ok := read("board.md")
		if !ok {
			m.parseVs = append(m.parseVs, Violation{
				Invariant: invFormat, At: Location{File: "board.md"}, Message: "missing",
			})
			g = DefaultIDGrammar()
			break
		}
		var vs []Violation
		m.board, vs = parseBoard("board.md", data)
		m.parseVs = append(m.parseVs, vs...)
		m.warnVs = append(m.warnVs, m.board.warnings...)
		g = m.board.grammar

	case slices.Contains(entries, "backlog.md"):
		data, ok := read("backlog.md")
		if !ok {
			m.parseVs = append(m.parseVs, Violation{
				Invariant: invFormat, At: Location{File: "backlog.md"}, Message: "missing",
			})
			g = DefaultIDGrammar()
			break
		}
		var vs []Violation
		m.backlog, vs = parseBacklog("backlog.md", data)
		m.parseVs = append(m.parseVs, vs...)
		m.warnVs = append(m.warnVs, m.backlog.warnings...)
		g = m.backlog.grammar

	default:
		m.parseVs = append(m.parseVs, Violation{
			Invariant: invFormat, At: Location{File: "board.md"}, Message: "missing",
		})
		g = DefaultIDGrammar()
	}

	if data, ok := read("done.md"); ok {
		var vs []Violation
		m.done, vs = parseDoneG("done.md", data, g)
		m.parseVs = append(m.parseVs, vs...)
	} else {
		m.parseVs = append(m.parseVs, Violation{
			Invariant: invFormat, At: Location{File: "done.md"}, Message: "missing",
		})
	}

	// working.NN.md is version 1 only (§5.2, retired at version 2); a
	// version-2 directory has none, and a bare directory listing must not be
	// scolded for lacking them.
	if m.board == nil {
		names, _, _, wvs := discoverWorkingFiles(entries, ".")
		m.parseVs = append(m.parseVs, wvs...)
		for _, name := range names {
			data, ok := read(name)
			if !ok {
				continue
			}
			w, vs := parseWorkingG(name, data, g)
			m.working = append(m.working, w)
			m.parseVs = append(m.parseVs, vs...)
		}
	}

	for _, name := range detailNames(s.path) {
		rel := "details/" + name
		data, ok := read(rel)
		if !ok {
			continue
		}
		lines := splitLines(data)
		fm, _, vs := readHeader(rel, lines)
		m.parseVs = append(m.parseVs, vs...)
		m.details[rel] = &detailFile{Name: rel, FM: fm, Lines: lines, stamp: m.stamps[rel]}
	}
	return m, nil
}

// detailNames lists the non-template files in details/.
//
// A leading underscore marks a template, which is exempt from I9 - it belongs to
// no item by design.
func detailNames(dir string) []string {
	entries, err := os.ReadDir(filepath.Join(dir, "details"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") || strings.HasPrefix(n, "_") {
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// grammar returns the directory's declared ID grammar (§3.3.2). The
// declaration lives in backlog.md frontmatter; without one the default
// applies. An invalid declaration has already been reported by ParseIDGrammar
// at parse time, so the default stands in here and the directory still
// parses.
func (m *dirModel) grammar() IDGrammar {
	switch {
	case m.board != nil:
		g, _, _ := ParseIDGrammar(m.board.FM)
		return g
	case m.backlog != nil:
		g, _, _ := ParseIDGrammar(m.backlog.FM)
		return g
	}
	return DefaultIDGrammar()
}

// items returns every item in the directory, in a stable order: board (or
// backlog by section and working slots by number) items first, then done
// newest first.
func (m *dirModel) items() []*Item {
	var out []*Item
	if m.board != nil {
		out = append(out, m.board.Items...)
	}
	if m.backlog != nil {
		out = append(out, m.backlog.Items...)
	}
	for _, w := range m.working {
		if w.Item != nil {
			out = append(out, w.Item)
		}
	}
	if m.done != nil {
		out = append(out, m.done.Items...)
	}
	return out
}

// find locates an item by ID anywhere in the directory.
func (m *dirModel) find(id ID) *Item {
	for _, it := range m.items() {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// slots builds the public slot view.
func (m *dirModel) slots() []Slot {
	out := make([]Slot, 0, len(m.working))
	for _, w := range m.working {
		out = append(out, Slot{Number: w.Number, Width: w.Width, File: w.Name, Item: w.Item})
	}
	return out
}

// ---------------------------------------------------------------------------
// Read operations
// ---------------------------------------------------------------------------

// Directory summarises the open directory.
func (s *Store) Directory() (Directory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return Directory{}, err
	}
	return m.directory(), nil
}

// Grammar returns the directory's declared ID grammar (spec-file-format.md
// §3.3.2). A directory that declares nothing gets the default grammar, exactly
// as if the keys were written.
//
// A front end parses ID arguments against this, never against a shape it
// assumes: the grammar is a property of the directory the ID names, and an ID
// that does not match it belongs to a different board.
func (s *Store) Grammar() (IDGrammar, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return IDGrammar{}, err
	}
	return m.grammar(), nil
}

func (m *dirModel) directory() Directory {
	d := Directory{Path: m.path, Slots: m.slots(), WipLimit: len(m.working)}
	// ProjectID fails only on an empty path, which a loaded model cannot have.
	// Path stays as opened rather than canonicalised: the id is derived from the
	// canonical form, but the path a caller reads back is the one it passed.
	d.ProjectID, _ = ProjectID(m.path)
	for _, sl := range d.Slots {
		if sl.Occupied() {
			d.WipUsed++
		}
	}
	switch {
	case m.board != nil:
		d.Version = 2
		d.Project = m.board.FM.Get("project")
		d.Board = m.board.FM.Get("board")
		d.NextID = ID(m.board.FM.Get("next_id"))
		g := m.grammar()
		d.IDPrefix = g.Prefix
		d.IDWidth = g.Width
		d.StageCfg = m.board.stageCfg
		d.StageUsed = map[Stage]int{}
		for stage := range d.StageCfg.WipLimits {
			d.StageUsed[stage] = len(m.board.StageItems(stage))
		}
	case m.backlog != nil:
		d.Version = 1
		d.Project = m.backlog.FM.Get("project")
		d.Board = m.backlog.FM.Get("board")
		d.NextID = ID(m.backlog.FM.Get("next_id"))
		g := m.grammar()
		d.IDPrefix = g.Prefix
		d.IDWidth = g.Width
	}
	return d
}

// Filter narrows a listing. A zero Filter matches the backlog, which is what
// `mm --list` shows by default.
type Filter struct {
	State   State // "" means backlog only; StateAll spans everything
	Section Section
	Prio    Prio
	Tag     string
	Blocked bool // only items carrying a blocked: field
	Limit   int
}

// StateAll asks List for every item regardless of where it lives.
const StateAll State = "all"

// List returns items matching a filter.
//
// Order is ON-DISK ORDER, never re-sorted. ## Ready order is the user's own
// prioritisation (spec-file-format.md §5.1); a listing that silently re-sorts it
// hides the one thing the section is for.
func (s *Store) List(f Filter) ([]Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return nil, err
	}

	want := f.State
	if want == "" {
		want = StateBacklog
	}
	var out []Item
	for _, it := range m.items() {
		if want != StateAll && it.State != want {
			continue
		}
		if f.Section != "" && it.Section != f.Section {
			continue
		}
		if f.Prio != "" && it.Prio.Effective() != f.Prio {
			continue
		}
		if f.Tag != "" && !hasTag(it.Tags, f.Tag) {
			continue
		}
		if f.Blocked && it.Blocked == "" {
			continue
		}
		out = append(out, *it)
		if f.Limit > 0 && len(out) == f.Limit {
			break
		}
	}
	return out, nil
}

// Get returns one item, wherever it lives.
func (s *Store) Get(id ID) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return Item{}, err
	}
	it := m.find(id)
	if it == nil {
		return Item{}, fmt.Errorf("%w: %s is not in %s", ErrNotFound, id, filepath.Base(s.path))
	}
	return *it, nil
}

// Validate runs every invariant and returns the findings, sorted by file then
// line. Violations are results, not errors: an error here would mean the
// directory could not be read at all.
func (s *Store) Validate() ([]Violation, error) {
	vs, _, err := s.ValidateWithWarnings()
	return vs, err
}

// ValidateWithWarnings runs every invariant and returns the findings plus the
// non-fatal warnings, both sorted by file then numeric line. A warning is a
// finding that does not make the directory invalid - an id_width outside the
// RECOMMENDED 3-6 range (spec-file-format.md §3.3.2 rule 3) - so it must never
// fail --check, and the two slices are kept apart to keep that distinction
// visible.
func (s *Store) ValidateWithWarnings() ([]Violation, []Violation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return nil, nil, err
	}
	vs := m.validate()
	ws := append([]Violation{}, m.warnVs...)
	sortViolations(vs)
	sortViolations(ws)
	return vs, ws, nil
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}
