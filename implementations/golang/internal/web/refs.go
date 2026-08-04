package web

import (
	"github.com/Hopasaurus/micro-manager/mm"
)

// Cross-board refs (plan-board-links.md, T-0127).
//
// A refs value is data on an item line (spec-file-format.md §6); the GUI is
// the only component with the tree-wide view that can resolve it. Decision 4
// of the plan fixes the three renderings:
//
//	resolved  -> exactly one known board declares the slug: a clickable link
//	             addressed by the target board's projectId and the item's ID.
//	             Routes keep projectId; the visible link is the slug:ID element
//	             (decision 7).
//	missing   -> no known board declares the slug, or the item is not in the
//	             resolved board (a renumber by mm fix in the target board is
//	             decision 6's owned cost): text, data-missing="true", never a
//	             dead link. The §10 rule 5 policy: a board on an unmounted
//	             drive is not a deleted project.
//	ambiguous -> several known boards declare the slug: text naming all of
//	             them, no guess (the mm fix tie discipline).
//
// Resolution is a lookup among KNOWN directories, exactly as projectId is
// resolved today (registry.resolve), never a path reconstructed from a slug.

// refMatch is one known directory that declares a board slug.
type refMatch struct {
	ProjectID string
	Project   string
	Path      string
}

// refView is one element of an item's refs value as rendered. Exactly one of
// Resolved, Missing, Ambiguous is true.
type refView struct {
	Element   string // the "slug:ID" as written on the item line
	Slug      string
	TargetID  string
	Resolved  bool
	ProjectID string // target board's route id when resolved
	Project   string // target board's display name when resolved
	Missing   bool
	Ambiguous bool
	Boards    []string // display names, ambiguous only
}

// refResolver answers the two lookups a refs element needs: which known
// boards declare this slug, and does the item exist there. Built once per
// render from the registry's known set, so a renumber or a removed board
// shows up on the next page render without a rescan.
type refResolver struct {
	reg    *registry
	bySlug map[string][]refMatch
	exist  map[string]bool // "projectId/itemId" -> checked
}

// newRefResolver snapshots the registry's known directories into a slug map.
// A directory that declares no board is not link-targetable and never
// appears.
func (r *registry) newRefResolver() *refResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rr := &refResolver{reg: r, bySlug: map[string][]refMatch{}, exist: map[string]bool{}}
	for _, d := range r.known {
		if d.Board == "" {
			continue
		}
		rr.bySlug[d.Board] = append(rr.bySlug[d.Board], refMatch{
			ProjectID: d.ProjectID,
			Project:   d.Project,
			Path:      d.Path,
		})
	}
	return rr
}

// itemExists reports whether the target board still holds the item. Memoised
// per render: several refs to the same board share one lookup, and a board
// whose store is not open yet pays the open once, not once per ref.
func (rr *refResolver) itemExists(projectID, id string) bool {
	key := projectID + "/" + id
	if v, ok := rr.exist[key]; ok {
		return v
	}
	store, err := rr.reg.resolve(projectID)
	v := err == nil
	if v {
		if _, err := store.Get(mm.ID(id)); err != nil {
			v = false
		}
	}
	rr.exist[key] = v
	return v
}

// refsView resolves one item's refs value into render views.
func (rr *refResolver) refsView(it mm.Item) []refView {
	if len(it.Refs) == 0 {
		return nil
	}
	out := make([]refView, 0, len(it.Refs))
	for _, ref := range it.Refs {
		v := refView{
			Element:  ref.String(),
			Slug:     ref.Slug,
			TargetID: string(ref.ID),
		}
		matches := rr.bySlug[ref.Slug]
		switch {
		case len(matches) == 0:
			v.Missing = true
		case len(matches) > 1:
			v.Ambiguous = true
			for _, m := range matches {
				v.Boards = append(v.Boards, m.Project)
			}
		default:
			m := matches[0]
			if !rr.itemExists(m.ProjectID, string(ref.ID)) {
				v.Missing = true
			} else {
				v.Resolved = true
				v.ProjectID = m.ProjectID
				v.Project = m.Project
			}
		}
		out = append(out, v)
	}
	return out
}
