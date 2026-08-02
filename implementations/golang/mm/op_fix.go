package mm

import (
	"fmt"
	"sort"
	"strings"
)

// Fix repairs the two findings a git merge manufactures — I1 (an ID in more
// than one home) and I2 (an ID at or above next_id) — and nothing else
// (plan-git-support.md decision 4).
//
// Two branches that both run --add against the same committed next_id both
// allocate the same ID; git auto-merges with no conflict markers and the
// directory ends up with two items sharing one ID (reproduced in T-0101).
// The collision cannot be prevented without a lock, which the format's
// no-server promise forbids, so the repair is the answer — and it is also the
// repair for a hand-edited directory that broke I1/I2.
//
// The repair is deterministic, idempotent, and NOT git-aware: it reads the
// directory, not the repository. Running it twice is a no-op.
type FixRequest struct {
	DryRun bool
}

// FixChange is one renumbering the repair performed.
type FixChange struct {
	OldID  ID
	NewID  ID
	File   string // where the renumbered item lives
	Detail string // the item's OLD detail path, or "" when it had none
}

// FixResult reports the repair. Changes lists every renumbering; NextID is
// the next_id the repair wrote (or would write), WasNext what it found.
type FixResult struct {
	Changes    []FixChange
	NextID     ID
	WasNext    ID
	NextBumped bool
}

// homeRank orders the three homes by how far the item has advanced: done.md
// beats a working slot, which beats backlog.md. When an ID lives in two
// places, the copy in the more advanced home is the one that is true.
func homeRank(it *Item) int {
	switch it.State {
	case StateDone:
		return 3
	case StateWorking:
		return 2
	default:
		return 1
	}
}

// fixPlanItem is one renumbering, planned before anything is touched so a tie
// or a refusal can abort with nothing written and a dry run reports exactly
// what a real run would do.
type fixPlanItem struct {
	loser  *Item
	slot   *workingFile // non-nil when the loser lives in a slot
	oldID  ID
	newID  ID
	detail string // the loser's new detail value; "" drops it
	rename string // old detail path renamed to the new ID, "" when none
	adopt  string // old detail path released to the winner, "" when none
}

// Fix repairs the directory. It refuses to run while anything other than an
// I1 duplicate or an I2 ceiling is wrong — conflict markers, a dangling
// detail reference, or a broken section must be resolved by hand first; the
// repair must never paper over a problem it does not understand.
func (s *Store) Fix(req FixRequest) (FixResult, TxResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var zero FixResult
	t, err := s.begin()
	if err != nil {
		return zero, TxResult{}, err
	}
	m := t.model
	g := m.grammar()

	// The counter: never lowered (I2). The repair only ever raises next_id to
	// clear the ceiling.
	next := ID(m.backlog.FM.Get("next_id"))
	wasNext := next
	nextNum := 0
	if g.ValidID(string(next)) {
		nextNum = g.Num(string(next))
	}
	maxID := 0
	for _, it := range m.items() {
		if g.ValidID(string(it.ID)) {
			if n := g.Num(string(it.ID)); n > maxID {
				maxID = n
			}
		}
	}
	if maxID+1 > nextNum {
		nextNum = maxID + 1
	}

	// Group items by ID: an ID in more than one place is the I1 duplicate the
	// repair exists for.
	groups := map[ID][]*Item{}
	var ids []ID
	for _, it := range m.items() {
		if _, seen := groups[it.ID]; !seen {
			ids = append(ids, it.ID)
		}
		groups[it.ID] = append(groups[it.ID], it)
	}
	sort.Slice(ids, func(i, j int) bool { return g.Num(string(ids[i])) < g.Num(string(ids[j])) })
	dupIDs := map[ID]bool{}
	for _, id := range ids {
		if len(groups[id]) > 1 {
			dupIDs[id] = true
		}
	}

	// Only I1 and I2 are repaired. Anything else wrong must be fixed by hand
	// before the repair may touch the directory — with one exception: the I9
	// double-claim and title-drift findings about a duplicated ID's OWN detail
	// file are manufactured by the same merge, and the repair resolves them
	// (the file follows the item whose title it carries). An orphan or an
	// id-mismatch on that file is a different problem and stays a blocker.
	vs := m.validate()
	var blockers []Violation
	for _, v := range vs {
		if v.Invariant == "I1" || v.Invariant == "I2" {
			continue
		}
		if v.Invariant == "I9" && detailOfDup(v.At.File, dupIDs, g) {
			if strings.Contains(v.Message, "referenced by") ||
				strings.Contains(v.Message, "frontmatter title is") {
				continue
			}
		}
		blockers = append(blockers, v)
	}
	if len(blockers) > 0 {
		return zero, TxResult{}, &InvariantError{Violations: blockers}
	}

	var plan []fixPlanItem
	winners := map[ID]*Item{}   // kept item per duplicate group
	renamed := map[ID]bool{}    // groups whose shared file is renamed away
	released := map[ID]string{} // groups whose shared file the winner takes over

	for _, id := range ids {
		group := groups[id]
		if len(group) < 2 {
			continue
		}
		winner := group[0]
		for _, it := range group[1:] {
			switch {
			case homeRank(it) > homeRank(winner):
				winner = it
			case homeRank(it) == homeRank(winner):
				// Same home: the earliest created wins. An absent created
				// counts as the oldest, being the least information.
				if it.Created.Before(winner.Created) {
					winner = it
				}
			}
		}
		// A tie — same home, same created — refuses with both items named:
		// the repair must never invent an answer a human should give.
		for _, it := range group {
			if it == winner || homeRank(it) != homeRank(winner) {
				continue
			}
			if !it.Created.Before(winner.Created) && !winner.Created.Before(it.Created) {
				return zero, TxResult{}, fmt.Errorf(
					"%w: duplicate %s: cannot choose between %s (%s) and %s (%s): "+
						"same home and same created; edit one item's created: or resolve the merge by hand",
					ErrConflict, id, winner.ID, winner.Title, it.ID, it.Title)
			}
		}
		winners[id] = winner

		// The group's shared detail file, if any member references it. Every
		// member's Detail is either empty or this path: any other value is an
		// I8 violation and the pre-check would have refused.
		filePath := "details/" + string(id) + ".md"
		df, fileExists := m.details[filePath]
		// The file follows the item whose title it carries. A title matching
		// nobody — or several losers at once — is a file that is not this
		// collision's file; refuse rather than guess.
		match := "" // "winner", or the title-matched loser's ID
		if fileExists {
			fileTitle := df.FM.Get("title")
			switch {
			case fileTitle == winner.Title:
				match = "winner"
			default:
				for _, it := range group {
					if it == winner || it.Title != fileTitle {
						continue
					}
					if match != "" {
						return zero, TxResult{}, fmt.Errorf(
							"%w: %s's title matches more than one duplicate of %s; "+
								"resolve the detail file by hand", ErrConflict, filePath, id)
					}
					match = string(it.ID)
				}
				if match == "" {
					return zero, TxResult{}, fmt.Errorf(
						"%w: %s carries a title matching no duplicate of %s; "+
							"resolve the detail file by hand", ErrConflict, filePath, id)
				}
			}
		}

		for _, loser := range group {
			if loser == winner {
				continue
			}
			pc := fixPlanItem{loser: loser, oldID: loser.ID, newID: g.NewID(nextNum)}
			nextNum++
			if loser.State == StateWorking {
				for _, w := range m.working {
					if w.Item == loser {
						pc.slot = w
						break
					}
				}
			}
			if old := loser.Detail; old != "" {
				if !fileExists || old != filePath {
					// I8 reported the dangling or mismatched reference, so the
					// pre-check would have refused. Defensive only.
					return zero, TxResult{}, fmt.Errorf(
						"%w: %s references %s, which cannot be repaired",
						ErrConflict, loser.ID, old)
				}
				switch {
				case match == "winner":
					pc.adopt = old // the winner owns it; this loser releases its claim
					released[id] = old
				case match == string(loser.ID):
					pc.detail = "details/" + string(pc.newID) + ".md"
					pc.rename = old
					renamed[id] = true
				}
			}
			plan = append(plan, pc)
		}
	}

	// Apply the renumberings. Every edit is a line splice in place — nothing
	// moves, so the transaction's baseline comparison sees only the change
	// this repair intends.
	out := FixResult{WasNext: wasNext}
	for _, pc := range plan {
		loser := pc.loser
		before := RenderItemLine(loser)
		oldID := pc.oldID
		loser.ID = pc.newID
		loser.Detail = pc.detail

		file := loser.Source.File
		switch loser.State {
		case StateBacklog:
			_, e, err := t.backlog()
			if err != nil {
				return zero, TxResult{}, err
			}
			e.ReplaceItem(loser)
			t.stage("backlog.md")
		case StateDone:
			_, e, err := t.done()
			if err != nil {
				return zero, TxResult{}, err
			}
			e.ReplaceItem(loser)
			t.stage("done.md")
		case StateWorking:
			e := t.working(pc.slot)
			e.SetFM("id", string(loser.ID))
			if loser.Detail != "" {
				e.SetFM("detail", loser.Detail)
			} else {
				e.SetFM("detail", "null")
			}
			t.stage(pc.slot.Name)
		}

		if pc.rename != "" {
			// Rename: write the new file first (id: line rewritten to match
			// its new owner), then delete the old — a crash between the two
			// leaves a duplicate rather than a hole (§7 rule 4).
			df := m.details[pc.rename]
			e := t.detailEdit(df)
			e.SetFM("id", string(pc.newID))
			newPath := "details/" + string(pc.newID) + ".md"
			t.stageRaw(newPath, e.Bytes())
			t.ws.Delete(s.path + "/" + pc.rename)
			m.details[newPath] = mustParseDetail(newPath, string(e.Bytes()))
			delete(m.details, pc.rename)
			t.record(Change{Kind: ChangeUpdated, ID: pc.newID, File: newPath})
		}

		out.Changes = append(out.Changes, FixChange{
			OldID:  oldID,
			NewID:  pc.newID,
			File:   file,
			Detail: pc.rename,
		})
		if pc.adopt != "" {
			out.Changes[len(out.Changes)-1].Detail = pc.adopt
		}
		t.record(Change{Kind: ChangeUpdated, ID: pc.newID, File: file,
			Before: before, After: RenderItemLine(loser)})
	}

	// Winner-side effects, after every renumbering so the winner's ID is still
	// the group's ID. Two cases:
	//
	//  - a loser released the shared file (its title matched the winner): a
	//    winner that had no detail field of its own adopts it — I9 holds
	//    because the file already carries the winner's title;
	//  - the file was renamed to a duplicate (its title matched that loser): a
	//    winner that referenced it loses the claim, or I8 would point the
	//    winner at a file that is no longer its own.
	for oldID, winner := range winners {
		oldPath := "details/" + string(oldID) + ".md"
		switch {
		case released[oldID] != "" && winner.Detail == "":
			winner.Detail = released[oldID]
		case renamed[oldID] && winner.Detail == oldPath:
			winner.Detail = ""
		default:
			continue
		}
		switch winner.State {
		case StateBacklog:
			_, e, err := t.backlog()
			if err != nil {
				return zero, TxResult{}, err
			}
			e.ReplaceItem(winner)
			t.stage("backlog.md")
		case StateDone:
			_, e, err := t.done()
			if err != nil {
				return zero, TxResult{}, err
			}
			e.ReplaceItem(winner)
			t.stage("done.md")
		case StateWorking:
			var w *workingFile
			for _, wf := range m.working {
				if wf.Item == winner {
					w = wf
					break
				}
			}
			e := t.working(w)
			e.SetFM("detail", winner.Detail)
			t.stage(w.Name)
		}
		t.record(Change{Kind: ChangeUpdated, ID: winner.ID, File: winner.Source.File,
			After: RenderItemLine(winner)})
	}

	// The counter, written once, after every renumbering is planned.
	target := g.NewID(nextNum)
	if string(target) != string(next) {
		_, e, err := t.backlog()
		if err != nil {
			return zero, TxResult{}, err
		}
		e.SetFM("next_id", string(target))
		t.stage("backlog.md")
		out.NextBumped = true
	}
	out.NextID = target

	res, err := t.commit(req.DryRun)
	if err != nil {
		return zero, res, err
	}
	return out, res, nil
}

// detailOfDup reports whether file is the detail file OF a duplicated ID —
// "details/T-0043.md" where T-0043 appears more than once. The repair owns
// the I9 findings about that file: it is the same merge that manufactured the
// duplicate. Any other detail file's problems stay blockers.
func detailOfDup(file string, dups map[ID]bool, g IDGrammar) bool {
	const prefix = "details/"
	if !strings.HasPrefix(file, prefix) || !strings.HasSuffix(file, ".md") {
		return false
	}
	id := ID(strings.TrimSuffix(strings.TrimPrefix(file, prefix), ".md"))
	return g.ValidID(string(id)) && dups[id]
}
