package mm

import (
	"fmt"
	"sort"
)

// The invariants I1-I10 of spec-file-format.md §7, in one place.
//
// One implementation, used by --check AND by every mutation before it commits
// (spec-tools.md §8). That is what makes it impossible for the tool to write a
// directory its own checker would reject. A second copy of these rules would
// drift, and the drift would only show up as a file the tool wrote and then
// refused to read.

// validate returns every finding for a loaded directory, parse problems
// included, sorted by file then numeric line.
func (m *dirModel) validate() []Violation {
	vs := append([]Violation{}, m.parseVs...)
	vs = append(vs, m.checkIDs()...) // I1, I2
	if m.isV2() {
		vs = append(vs, m.checkBoard()...)     // I3, I5, I7: stage/reason
		vs = append(vs, m.checkTicklerV2()...) // I7: tickler placement, v2
	} else {
		vs = append(vs, m.checkBacklog()...) // I3, I5, backlog structure
		vs = append(vs, m.checkTickler()...) // I7: tickler placement, v1
	}
	vs = append(vs, m.checkDone()...)    // I3, I6
	vs = append(vs, m.checkProject()...) // I7
	vs = append(vs, m.checkDetails()...) // I8, I9
	sortViolations(vs)
	return vs
}

// checkIDs covers I1 (one home per ID) and I2 (every ID below next_id), plus
// the §3.3.2 rule 1 requirement that every ID in the directory be in its
// declared grammar - an ID from another grammar would be sharing a counter it
// does not belong to.
func (m *dirModel) checkIDs() []Violation {
	var vs []Violation
	seen := map[ID]Location{}
	g := m.grammar()

	for _, it := range m.items() {
		if it.ID == "" {
			continue // already reported by the parser
		}
		if !g.ValidID(string(it.ID)) {
			// The parsers reject these as malformed lines; this is the same
			// rule stated as an invariant, so a hand-built model cannot dodge it.
			vs = append(vs, Violation{
				Invariant: invFormat, At: it.Source,
				Message: fmt.Sprintf("%s is not a %s id in the declared grammar", it.ID, g),
			})
			continue
		}
		if at, dup := seen[it.ID]; dup {
			vs = append(vs, Violation{
				Invariant: "I1", At: it.Source,
				Message: fmt.Sprintf("%s is already defined at %s", it.ID, at),
			})
			continue
		}
		seen[it.ID] = it.Source
	}

	rootFile, rootFM := "backlog.md", (*Frontmatter)(nil)
	switch {
	case m.board != nil:
		rootFile, rootFM = "board.md", m.board.FM
	case m.backlog != nil:
		rootFile, rootFM = "backlog.md", m.backlog.FM
	default:
		return vs
	}
	next := ID(rootFM.Get("next_id"))
	switch {
	case next == "":
		// No line: the key is ABSENT, so there is no line to point at. Line 1
		// is the "---" delimiter, and naming it sends the reader to a line that
		// has nothing to do with the problem.
		vs = append(vs, Violation{
			Invariant: "I2", At: Location{File: rootFile},
			Message: "frontmatter has no next_id",
		})
	case !g.ValidID(string(next)):
		vs = append(vs, Violation{
			Invariant: "I2", At: Location{File: rootFile, Line: rootFM.Line("next_id")},
			Message: "next_id is not a " + g.String() + " id: " + string(next),
		})
	default:
		for id, at := range seen {
			if g.Num(string(id)) >= g.Num(string(next)) {
				vs = append(vs, Violation{
					Invariant: "I2", At: at,
					Message: fmt.Sprintf("%s is at or above next_id (%s)", id, next),
				})
			}
		}
	}
	return vs
}

// checkBacklog covers I3 (open boxes only), I5 (blocked field placement), and
// the requirement that all three sections exist.
func (m *dirModel) checkBacklog() []Violation {
	if m.backlog == nil {
		return nil
	}
	var vs []Violation

	for _, want := range Sections() {
		if m.backlog.Section(want) == nil {
			vs = append(vs, Violation{
				Invariant: invFormat, At: Location{File: "backlog.md"},
				Message: fmt.Sprintf("no ## %s heading", want),
			})
		}
	}

	for _, it := range m.backlog.Items {
		if it.rawBox == 'x' {
			vs = append(vs, Violation{
				Invariant: "I3", At: it.Source,
				Message: fmt.Sprintf("%s is closed but sits in backlog.md", it.ID),
			})
		}
		switch it.Section {
		case SectionNone:
			vs = append(vs, Violation{
				Invariant: "I5", At: it.Source,
				Message: fmt.Sprintf("%s is not under Ready, Blocked or Someday", it.ID),
			})
		case SectionBlocked:
			if it.Blocked == "" {
				vs = append(vs, Violation{
					Invariant: "I5", At: it.Source,
					Message: fmt.Sprintf("%s is under Blocked with no blocked: field", it.ID),
				})
			}
		default:
			if it.Blocked != "" {
				vs = append(vs, Violation{
					Invariant: "I5", At: it.Source,
					Message: fmt.Sprintf("%s has a blocked: field but is under %s", it.ID, it.Section),
				})
			}
		}
	}
	return vs
}

// checkDone covers I3 (closed boxes only) and I6 (dated, filed under the right
// month).
func (m *dirModel) checkDone() []Violation {
	if m.done == nil {
		return nil
	}
	var vs []Violation
	for _, it := range m.done.Items {
		if it.rawBox != 'x' {
			vs = append(vs, Violation{
				Invariant: "I3", At: it.Source,
				Message: fmt.Sprintf("%s is open but sits in done.md", it.ID),
			})
		}
		if it.Done.IsZero() {
			vs = append(vs, Violation{
				Invariant: "I6", At: it.Source,
				Message: fmt.Sprintf("%s has no done: field", it.ID),
			})
		}
		if it.Outcome == OutcomeNone {
			vs = append(vs, Violation{
				Invariant: "I6", At: it.Source,
				Message: fmt.Sprintf("%s has no outcome: field", it.ID),
			})
		}
		group := m.done.monthOf(it)
		switch {
		case group == nil || group.Month == "":
			vs = append(vs, Violation{
				Invariant: "I6", At: it.Source,
				Message: fmt.Sprintf("%s is not under a YYYY-MM heading", it.ID),
			})
		case !it.Done.IsZero() && it.Done.Month7() != group.Month:
			vs = append(vs, Violation{
				Invariant: "I6", At: it.Source,
				Message: fmt.Sprintf("%s has done:%s under heading %s", it.ID, it.Done, group.Month),
			})
		}
	}
	return vs
}

// checkProject covers the part of I7 that is not already enforced at parse time:
// a non-empty project name in backlog.md.
//
// The value forms - dates, prio, tags, no pipes - are checked by the parsers,
// which is why a malformed one arrives here as a parse violation rather than
// being re-derived.
func (m *dirModel) checkProject() []Violation {
	if m.board != nil {
		if v := m.board.FM.Get("project"); v == "" || v == "null" {
			return []Violation{{
				Invariant: "I7", At: Location{File: "board.md"},
				Message: "frontmatter has no project name",
			}}
		}
		return nil
	}
	if m.backlog == nil {
		return nil
	}
	if v := m.backlog.FM.Get("project"); v == "" || v == "null" {
		return []Violation{{
			Invariant: "I7", At: Location{File: "backlog.md"},
			Message: "frontmatter has no project name",
		}}
	}
	return nil
}

// checkBoard covers I3 (open boxes only), I5 (reason required where
// needs_reason lists the item's stage), and I7's stage-related rules: every
// item's stage: is a declared member of stages:, and started: is required
// whenever stage:working (folded in from version 1's I4, spec-file-format.md
// §7).
func (m *dirModel) checkBoard() []Violation {
	var vs []Violation
	cfg := m.board.stageCfg

	for _, it := range m.board.Items {
		if it.rawBox == 'x' {
			vs = append(vs, Violation{
				Invariant: "I3", At: it.Source,
				Message: fmt.Sprintf("%s is closed but sits in board.md", it.ID),
			})
		}
		if it.Stage != "" && !cfg.IsStage(it.Stage) {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s has stage:%s, which is not declared in stages:", it.ID, it.Stage),
			})
		}
		if cfg.StageNeedsReason(it.Stage) && it.Reason == "" {
			vs = append(vs, Violation{
				Invariant: "I5", At: it.Source,
				Message: fmt.Sprintf("%s is on stage %q, which needs_reason lists, but has no reason: field",
					it.ID, it.Stage),
			})
		}
		if it.Stage == "working" && it.Started.IsZero() {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s is on stage working but has no started: field", it.ID),
			})
		}
	}
	return vs
}

// checkTicklerV2 is checkTickler's version-2 form: tickler: is valid only on
// a stage named as a SOURCE in tickler_stages (§5.1.4), tickler_dest must
// itself be a declared stage, and — as in version 1 — a tickler item must
// also carry created:.
func (m *dirModel) checkTicklerV2() []Violation {
	var vs []Violation
	cfg := m.board.stageCfg
	for _, it := range m.items() {
		if it.Tickler == "" {
			continue
		}
		if _, ok := cfg.TicklerDestOf(it.Stage); !ok {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s carries tickler: but stage %q is not a tickler_stages source",
					it.ID, it.Stage),
			})
		}
		if it.TicklerDest != "" && !cfg.IsStage(it.TicklerDest) {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s has tickler_dest:%s, which is not declared in stages:",
					it.ID, it.TicklerDest),
			})
		}
		if it.Created.IsZero() {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s carries tickler: but has no created: (the anchor a never-fired schedule needs)",
					it.ID),
			})
		}
	}
	return vs
}

// checkTickler covers I7's tickler rules (spec-file-format.md §5.1, §7).
// Shape — the SCHEDULE grammar — is enforced at parse time, which is why a
// malformed expression is reported on the line that carries it rather than
// here. What remains is placement and the created requirement, for items in
// every state:
//
//   - Someday is the only section a tickler item may sit in (else it would be
//     silently dropped when the item starts — the working slots do not carry
//     the field);
//   - a tickler item must also carry created, the anchor a never-fired
//     recurring schedule needs (a mon@08:00 written on a Tuesday must fire the
//     following Monday, not be judged overdue against the epoch).
//
// Like the rest of I7 this validates SHAPE, never meaning: whether a schedule
// is due is the caller's clock, which may not enter the format (§10.1).
func (m *dirModel) checkTickler() []Violation {
	var vs []Violation
	for _, it := range m.items() {
		if it.Tickler == "" {
			continue
		}
		if it.Section != SectionSomeday {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s carries tickler: but sits in %s; Someday is the only valid home",
					it.ID, itemWhere(it)),
			})
		}
		if it.Created.IsZero() {
			vs = append(vs, Violation{
				Invariant: "I7", At: it.Source,
				Message: fmt.Sprintf("%s carries tickler: but has no created: (the anchor a never-fired schedule needs)",
					it.ID),
			})
		}
	}
	return vs
}

// itemWhere names where an item sits, for a violation message.
func itemWhere(it *Item) string {
	switch it.State {
	case StateWorking:
		return "a working slot"
	case StateDone:
		return "done.md"
	case StateBacklog:
		if it.Section == "" {
			return "no section"
		}
		return "## " + string(it.Section)
	}
	return ""
}

// checkDetails covers I8 (every detail: path resolves and is named for its item)
// and I9 (every detail file is claimed by exactly one item, with matching id and
// title).
func (m *dirModel) checkDetails() []Violation {
	var vs []Violation
	claims := map[string][]*Item{}

	for _, it := range m.items() {
		if it.Detail == "" {
			continue
		}
		want := it.DetailPath()
		if it.Detail != want {
			vs = append(vs, Violation{
				Invariant: "I8", At: it.Source,
				Message: fmt.Sprintf("%s points at %s (expected %s)", it.ID, it.Detail, want),
			})
			continue
		}
		df, ok := m.details[it.Detail]
		if !ok {
			vs = append(vs, Violation{
				Invariant: "I8", At: it.Source,
				Message: fmt.Sprintf("detail file does not exist: %s", it.Detail),
			})
			continue
		}
		claims[it.Detail] = append(claims[it.Detail], it)

		// The id/title duplication exists solely so drift is detectable; these
		// two comparisons are the entire reason for it.
		if got := df.FM.Get("id"); got != string(it.ID) {
			vs = append(vs, Violation{
				Invariant: "I9", At: Location{File: df.Name, Line: df.FM.Line("id")},
				Message: fmt.Sprintf("frontmatter id is %q, expected %q", got, it.ID),
			})
		}
		if got := df.FM.Get("title"); got != it.Title {
			vs = append(vs, Violation{
				Invariant: "I9", At: Location{File: df.Name, Line: df.FM.Line("title")},
				Message: fmt.Sprintf("frontmatter title is %q, expected %q", got, it.Title),
			})
		}
	}

	for name, df := range m.details {
		vs = append(vs, checkUpdatedFM(name, df.FM)...)
		switch n := len(claims[name]); {
		case n == 0:
			vs = append(vs, Violation{
				Invariant: "I9", At: Location{File: name},
				Message: "orphan — no item references it",
			})
		case n > 1:
			vs = append(vs, Violation{
				Invariant: "I9", At: Location{File: name},
				Message: fmt.Sprintf("referenced by %d items (expected exactly 1)", n),
			})
		}
	}
	return vs
}

// sortViolations orders findings by file, then by NUMERIC line.
//
// Lexicographic ordering would put line 10 before line 5, which reads as though
// the tool cannot count.
func sortViolations(vs []Violation) {
	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].At.File != vs[j].At.File {
			return vs[i].At.File < vs[j].At.File
		}
		return vs[i].At.Line < vs[j].At.Line
	})
}
