package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Migrate brings a directory written by an earlier revision of the format up to
// the current one (spec-tools.md §5.3).
//
// The spec names exactly three legacy shapes and this repairs exactly those:
//
//   - working.md, the pre-slot name, renamed to working.NN.md
//     (spec-file-format.md §5.2.1 rule 4);
//   - backlog.md with no project, which I7 requires (§5.1);
//   - tags written as a YAML flow sequence, `[infra, ci]`, which a flat-map
//     parser cannot read as a list (§10.3) and which the format spells
//     `infra,ci` in every position (§5.2.2, §6).
//
// It does NOT invent migrations. A field reordered, a heading reworded, a
// missing `updated` — none of that is a format revision, and a repair that
// rewrites what it was not asked to rewrite is indistinguishable from
// corruption to the person reading the diff.
//
// **Why it tolerates unrelated violations, where Fix refuses.** Fix blocks on
// anything outside I1/I2, including I10 — so on a directory with a working.md
// it refuses. If Migrate blocked in the same way, a legacy directory that also
// has a duplicate ID could be repaired by neither tool. Migrate is the first
// thing you run on an old directory: it fixes what it understands, leaves
// everything else exactly as it found it, and hands you a directory --check can
// tell the truth about and --fix can then run on.
//
// Two things it refuses, both because writing would leave a directory the
// checker rejects:
//
//   - a working.md whose CONTENT is not a valid working file. Renaming it would
//     move a file the checker rejects to a name the checker reads. The
//     violations come back under the name the file would have had.
//   - a conversion that would REVEAL a duplicate ID: a line that could not be
//     parsed was never counted against I1, so making it readable can introduce
//     the finding. The transaction refuses and names the duplicate, which is
//     what a person needs in order to resolve it; --fix cannot, because a
//     duplicate it cannot see is one it cannot repair.
type MigrateRequest struct {
	// Project is the name to write when backlog.md has none. Empty means
	// derive one: the parent directory's name, since the directory's own name
	// is always one of the six recognized ones and carries no information.
	Project string

	DryRun bool
}

// MigrateKind names what a change repaired, so a caller can report or filter
// by it without matching on prose.
type MigrateKind string

const (
	MigrateRenamed MigrateKind = "renamed" // working.md -> working.NN.md
	MigrateProject MigrateKind = "project" // backlog.md gained a project name
	MigrateTags    MigrateKind = "tags"    // a flow sequence became a TAGLIST
)

// MigrateChange is one repair. Line is 0 when the change is to the file itself
// rather than to a line in it.
type MigrateChange struct {
	Kind   MigrateKind
	File   string
	Line   int
	Before string
	After  string
}

// MigrateResult reports every change, which §5.3 requires, and every legacy
// shape recognized but NOT converted.
type MigrateResult struct {
	Changes []MigrateChange

	// Warnings name values that look like the thing being migrated but do not
	// convert — `tags:[hello world]` has a space in a tag, and no rewriting of
	// it is this operation's to guess. Each stays exactly as written, and the
	// validator goes on reporting it.
	Warnings []string
}

// Migrate performs the migration. Running it twice is a no-op: every step is
// conditional on the legacy shape still being present.
func (s *Store) Migrate(req MigrateRequest, today Date) (MigrateResult, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero MigrateResult
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	var res MigrateResult

	if err := t.migrateWorkingMd(s, &res); err != nil {
		return zero, TxResult{}, err
	}
	// Tags BEFORE the project name, and the order is load-bearing: the tag
	// rewrite addresses lines by the number they had when the file was parsed,
	// and writing a missing project inserts a line into the frontmatter, which
	// moves every line below it. The other order silently rewrote the line
	// AFTER each item — turning the item into a duplicate of its neighbour.
	t.migrateTags(&res)
	t.migrateProject(s, req.Project, &res)

	// Staged once per file, at the end: two steps can touch backlog.md — a
	// missing project and a flow sequence on an item line — and staging it
	// twice would queue the same bytes twice and report the path twice.
	names := []string{"backlog.md", "done.md"}
	for _, w := range t.model.working {
		names = append(names, w.Name)
	}
	t.stage(names...)

	txr, err := t.commit(req.DryRun)
	if err != nil {
		return zero, txr, err
	}
	return res, txr, nil
}

// migrateWorkingMd renames the pre-slot working.md to the lowest free slot.
//
// The file is read from disk rather than from the model, because the model
// never took it: discoverWorkingFiles reports it as a legacy name and skips it,
// which is exactly why it needs migrating.
func (t *tx) migrateWorkingMd(s *Store, res *MigrateResult) error {
	const legacy = "working.md"
	if !contains(t.model.entries, legacy) {
		return nil
	}
	path := filepath.Join(s.path, legacy)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("%w: reading %s: %v", ErrIO, legacy, err)
	}

	// Tags inside it are migrated here, on the bytes, so the file lands at its
	// new name already converted rather than needing a second pass over a name
	// the model has never heard of.
	lines := splitLines(data)
	name := freeSlotName(t.model)
	for i, line := range lines {
		out, changed, warn := migrateTagsLine(line)
		if warn != "" {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s:%d: %s", name, i+1, warn))
		}
		if changed {
			res.Changes = append(res.Changes, MigrateChange{
				Kind: MigrateTags, File: name, Line: i + 1, Before: line, After: out,
			})
			lines[i] = out
		}
	}
	body := []byte(joinLines(lines))

	// The content has to be a working file under its new name, or the rename
	// moves a file the checker rejects into a name the checker reads.
	if _, vs := parseWorkingG(name, body, t.model.grammar()); len(vs) > 0 {
		return &InvariantError{Violations: vs}
	}

	// Write the new name before removing the old one (§7 rule 4); the write set
	// applies every deletion after every write, so the order is structural.
	t.ws.Add(filepath.Join(s.path, name), body, stampOf(filepath.Join(s.path, name)))
	t.ws.Delete(path)
	res.Changes = append(res.Changes, MigrateChange{
		Kind: MigrateRenamed, File: legacy, After: name,
	})
	t.record(Change{Kind: ChangeMoved, File: name, Before: legacy, After: name})
	return nil
}

// freeSlotName is the working file name the legacy file takes: the lowest slot
// number not already in use, at the width the directory already uses.
//
// A directory whose only working file is working.md has no width to inherit, so
// it gets the recommended two digits (§5.2.1 rule 2).
func freeSlotName(m *dirModel) string {
	width := 2
	used := map[int]bool{}
	for _, w := range m.working {
		used[w.Number] = true
		width = w.Width // uniform by I10; the last is as good as the first
	}
	n := 1
	for used[n] {
		n++
	}
	return fmt.Sprintf("working.%0*d.md", width, n)
}

// migrateProject writes a project name into backlog.md when it has none (I7).
func (t *tx) migrateProject(s *Store, want string, res *MigrateResult) {
	if t.model.backlog == nil {
		return
	}
	if v := t.model.backlog.FM.Get("project"); v != "" && v != "null" {
		return
	}
	if want == "" {
		want = derivedProjectName(s.path)
	}
	_, e, err := t.backlog()
	if err != nil {
		return
	}
	e.SetFM("project", want)
	res.Changes = append(res.Changes, MigrateChange{
		Kind: MigrateProject, File: "backlog.md", After: want,
	})
	t.record(Change{Kind: ChangeUpdated, File: "backlog.md", After: "project: " + want})
}

// derivedProjectName guesses a name from the path.
//
// The directory's own name is one of the six recognized ones (§Appendix B) and
// says nothing about the project, so the name comes from its parent — the
// repository or working directory the board belongs to. It is free text (§10.8)
// and the operation reports what it chose, so a guess here is a starting point
// rather than a decision.
func derivedProjectName(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	switch parent {
	case "", ".", string(filepath.Separator):
	default:
		return parent
	}
	if base := filepath.Base(path); base != "" && base != "." {
		return base
	}
	return "micro-manager"
}

// migrateTags rewrites flow-sequence tags in every file the model holds.
func (t *tx) migrateTags(res *MigrateResult) {
	type target struct {
		name  string
		lines []string
	}
	var targets []target
	if t.model.backlog != nil {
		targets = append(targets, target{"backlog.md", t.model.backlog.Lines})
	}
	if t.model.done != nil {
		targets = append(targets, target{"done.md", t.model.done.Lines})
	}
	for _, w := range t.model.working {
		targets = append(targets, target{w.Name, w.Lines})
	}

	for _, tgt := range targets {
		var e *fileEdit
		for i, line := range tgt.lines {
			out, changed, warn := migrateTagsLine(line)
			if warn != "" {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("%s:%d: %s", tgt.name, i+1, warn))
			}
			if !changed {
				continue
			}
			if e == nil {
				e = t.editFor(tgt.name)
				if e == nil {
					break
				}
			}
			e.ReplaceLine(i+1, out)
			res.Changes = append(res.Changes, MigrateChange{
				Kind: MigrateTags, File: tgt.name, Line: i + 1, Before: line, After: out,
			})
			t.record(Change{Kind: ChangeUpdated, File: tgt.name, Before: line, After: out})
		}
	}
}

// editFor returns the editor for one of the model's files by name.
func (t *tx) editFor(name string) *fileEdit {
	switch name {
	case "backlog.md":
		_, e, err := t.backlog()
		if err != nil {
			return nil
		}
		return e
	case "done.md":
		_, e, err := t.done()
		if err != nil {
			return nil
		}
		return e
	}
	for _, w := range t.model.working {
		if w.Name == name {
			return t.working(w)
		}
	}
	return nil
}

// migrateTagsLine converts a flow-sequence tags value on one line, in either
// place the format puts one: a field on an item line, or a key in working-file
// frontmatter.
//
// It returns the rewritten line and whether anything changed, plus a warning
// for a value that IS a flow sequence but does not convert to a valid TAGLIST.
// A line it does not recognize comes back untouched: this migration reads two
// shapes and guesses at nothing.
func migrateTagsLine(line string) (out string, changed bool, warn string) {
	if looksLikeItemLine(line) {
		parts := strings.Split(line, fieldSep)
		for i := 1; i < len(parts); i++ {
			key, val, ok := strings.Cut(parts[i], ":")
			if !ok || strings.TrimSpace(key) != "tags" {
				continue
			}
			tags, ok := flowTags(val)
			if !ok {
				if isFlowSequence(val) {
					return line, false, "tags:" + strings.TrimSpace(val) +
						" is not a tag list this can convert; fix it by hand"
				}
				return line, false, ""
			}
			if tags == "" {
				// An empty list is no tags at all, and a tags field with no
				// value is not a TAGLIST. The field goes.
				parts = append(parts[:i], parts[i+1:]...)
			} else {
				parts[i] = "tags:" + tags
			}
			return strings.Join(parts, fieldSep), true, ""
		}
		return line, false, ""
	}

	// Working-file frontmatter: `tags: [infra, ci]`, at the start of the line.
	key, val, ok := strings.Cut(line, ":")
	if !ok || strings.TrimSpace(key) != "tags" || key != strings.TrimLeft(key, " \t") {
		return line, false, ""
	}
	tags, ok := flowTags(val)
	if !ok {
		if isFlowSequence(val) {
			return line, false, "tags: " + strings.TrimSpace(val) +
				" is not a tag list this can convert; fix it by hand"
		}
		return line, false, ""
	}
	if tags == "" {
		tags = "null" // no tags, in the frontmatter's spelling (§5.2.2)
	}
	return "tags: " + tags, true, ""
}

// isFlowSequence reports whether a value is written as a YAML flow sequence.
func isFlowSequence(val string) bool {
	v := strings.TrimSpace(val)
	return strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]")
}

// flowTags converts `[infra, ci]` to `infra,ci`.
//
// ok is false when the value is not a flow sequence at all, or when a member
// is not a valid TAG — a space inside one, say, which no rewriting fixes.
// An empty sequence converts to the empty string: no tags.
func flowTags(val string) (string, bool) {
	if !isFlowSequence(val) {
		return "", false
	}
	v := strings.TrimSpace(val)
	inner := strings.TrimSpace(v[1 : len(v)-1])
	if inner == "" {
		return "", true
	}
	var out []string
	for _, p := range strings.Split(inner, ",") {
		p = strings.TrimSpace(p)
		// Quoted scalars are still YAML, and the quotes are not part of the tag.
		if len(p) >= 2 && (p[0] == '"' && p[len(p)-1] == '"' || p[0] == '\'' && p[len(p)-1] == '\'') {
			p = strings.TrimSpace(p[1 : len(p)-1])
		}
		if !validTag(p) {
			return "", false
		}
		out = append(out, p)
	}
	return strings.Join(out, ","), true
}

func contains(list []string, want string) bool { return slices.Contains(list, want) }
