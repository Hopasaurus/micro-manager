package api

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Response conventions.
//
// The §4.3 ERROR envelope is fixed by the spec: { "ok": false, "errors": [ ... ] }.
// The success side mirrors it — one object, "ok" first — and carries whatever
// the endpoint computed. This file is the single place the conversions live, so
// an item looks the same in /items, /report and a mutation response.

// jsonItem is an item as the API reports it. Every field of the domain type
// appears, unregistered ones included: dropping them would make the JSON a
// lossy view of a format whose extension point is unknown fields
// (spec-file-format.md §9).
// jsonItem is the wire shape of one item (spec-gui.md §4.1). Refs ride as
// formatted "slug:ID" elements: the API carries the DATA, resolution stays
// with whatever client has the tree-wide view.
type jsonItem struct {
	ID          string            `json:"id"`
	Title       string            `json:"title"`
	State       string            `json:"state"`
	Section     string            `json:"section,omitempty"` // version 1
	Stage       string            `json:"stage,omitempty"`   // version 2
	Slot        int               `json:"slot,omitempty"`
	Position    int               `json:"position,omitempty"`
	Prio        string            `json:"prio,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Refs        []string          `json:"refs,omitempty"`
	Detail      string            `json:"detail,omitempty"`
	Created     string            `json:"created,omitempty"`
	Started     string            `json:"started,omitempty"`
	Done        string            `json:"done,omitempty"`
	Outcome     string            `json:"outcome,omitempty"`
	Blocked     string            `json:"blocked,omitempty"` // version 1
	Reason      string            `json:"reason,omitempty"`  // version 2
	TicklerDest string            `json:"ticklerDest,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	File        string            `json:"file,omitempty"`
	Line        int               `json:"line,omitempty"`
}

func toJSONItem(it mm.Item) jsonItem {
	out := jsonItem{
		ID:          string(it.ID),
		Title:       it.Title,
		State:       string(it.State),
		Section:     string(it.Section),
		Stage:       string(it.Stage),
		Slot:        it.Slot,
		Position:    it.Pos,
		Prio:        string(it.Prio),
		Tags:        it.Tags,
		Refs:        make([]string, 0, len(it.Refs)),
		Detail:      it.Detail,
		Created:     it.Created.String(),
		Started:     it.Started.String(),
		Done:        it.Done.String(),
		Outcome:     string(it.Outcome),
		Blocked:     it.Blocked,
		Reason:      it.Reason,
		TicklerDest: string(it.TicklerDest),
		File:        it.Source.File,
		Line:        it.Source.Line,
	}
	if len(it.Extra) > 0 {
		out.Extra = make(map[string]string, len(it.Extra))
		for _, f := range it.Extra {
			out.Extra[f.Key] = f.Value
		}
	}
	for _, ref := range it.Refs {
		out.Refs = append(out.Refs, ref.String())
	}
	return out
}

func toJSONItems(items []mm.Item) []jsonItem {
	out := make([]jsonItem, 0, len(items))
	for _, it := range items {
		out = append(out, toJSONItem(it))
	}
	return out
}

// jsonDirectory is the directory summary §4.2's GET /projects/:projectId
// returns. Slots are included because WIP is a property of slots, and a script
// that wants to know who occupies which slot should not have to re-implement
// the directory model.
type jsonDirectory struct {
	Path      string     `json:"path"`
	ProjectID string     `json:"projectId"`
	Project   string     `json:"project"`
	Board     string     `json:"board,omitempty"`
	NextID    string     `json:"nextId,omitempty"`
	WipLimit  int        `json:"wipLimit"`
	WipUsed   int        `json:"wipUsed"`
	Slots     []jsonSlot `json:"slots,omitempty"`
	Ready     int        `json:"ready,omitempty"`
	Blocked   int        `json:"blocked,omitempty"`
	Someday   int        `json:"someday,omitempty"`
	Done      int        `json:"done,omitempty"`
}

type jsonSlot struct {
	Number int    `json:"number"`
	File   string `json:"file"`
	ID     string `json:"id,omitempty"`
	Title  string `json:"title,omitempty"`
}

func toJSONDirectory(d mm.Directory) jsonDirectory {
	out := jsonDirectory{
		Path:      d.Path,
		ProjectID: d.ProjectID,
		Project:   d.Project,
		Board:     d.Board,
		NextID:    string(d.NextID),
		WipLimit:  d.WipLimit,
		WipUsed:   d.WipUsed,
	}
	for _, s := range d.Slots {
		js := jsonSlot{Number: s.Number, File: s.File}
		if s.Item != nil {
			js.ID = string(s.Item.ID)
			js.Title = s.Item.Title
		}
		out.Slots = append(out.Slots, js)
	}
	return out
}

// jsonChange is one effect of a transaction (mm.Change).
type jsonChange struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	File   string `json:"file"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

func toJSONChanges(res mm.TxResult) []jsonChange {
	out := make([]jsonChange, 0, len(res.Changes))
	for _, c := range res.Changes {
		out = append(out, jsonChange{
			Kind:   string(c.Kind),
			ID:     string(c.ID),
			File:   c.File,
			Before: c.Before,
			After:  c.After,
		})
	}
	if out == nil {
		out = []jsonChange{}
	}
	return out
}

// jsonViolation is one invariant finding, in the shape the check view renders
// (§5.8) and the error envelope carries (§4.3).
type jsonViolation struct {
	Invariant string `json:"invariant"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Message   string `json:"message"`
}

func toJSONViolations(vs []mm.Violation) []jsonViolation {
	out := make([]jsonViolation, 0, len(vs))
	for _, v := range vs {
		out = append(out, jsonViolation{
			Invariant: v.Invariant,
			File:      v.At.File,
			Line:      v.At.Line,
			Message:   v.Message,
		})
	}
	if out == nil {
		out = []jsonViolation{}
	}
	return out
}

// ---------------------------------------------------------------------------
// Sending
// ---------------------------------------------------------------------------

// ok writes a success envelope. Every endpoint goes through this so that "ok"
// is the first key of every success body, matching the error envelope's shape.
func ok(c *echo.Context, v any) error {
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "result": v})
}

// created writes a success envelope with 201, for POSTs that create something.
func created(c *echo.Context, v any) error {
	return c.JSON(http.StatusCreated, map[string]any{"ok": true, "result": v})
}

// mutation is the response shape every mutating operation returns: the item
// plus the change set, which is what a caller re-renders from
// (spec-tools.md §6.2). dryRun marks a change that was computed and not written.
type mutation struct {
	Item    jsonItem     `json:"item,omitempty"`
	Changes []jsonChange `json:"changes"`
	DryRun  bool         `json:"dryRun"`
}

func mutationResult(it *mm.Item, res mm.TxResult) mutation {
	m := mutation{Changes: toJSONChanges(res), DryRun: res.DryRun}
	if it != nil {
		m.Item = toJSONItem(*it)
	}
	return m
}

// projectFor resolves the :projectId parameter to an open store.
//
// The id is the derived identifier of §3.1; the registry owns the id-to-path
// mapping. A path in a URI position is never accepted anywhere.
func (s *Server) projectFor(c *echo.Context) (*mm.Store, error) {
	store, err := s.svc.ResolveProject(c.Param("projectId"))
	if err != nil {
		return nil, err
	}
	return store, nil
}

// itemID parses the :itemId parameter against the directory's declared
// grammar (§3.3.2). The ID is case-sensitive and verbatim (§3.2), and an ID
// that does not match the grammar the directory declares belongs to a
// different board.
func (s *Server) itemID(c *echo.Context, store *mm.Store) (mm.ID, error) {
	g, err := store.Grammar()
	if err != nil {
		return "", err
	}
	return g.ParseID(c.Param("itemId"))
}
