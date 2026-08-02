package cli

import (
	"encoding/json"
	"errors"

	"micromanager/mm"
)

// The --json envelope (spec-tools.md §9.2).
//
// ONE JSON OBJECT ON STDOUT AND NOTHING ELSE. No progress text, no warnings
// mixed in — those go to stderr, where a caller piping stdout into a parser
// never sees them.
//
// THE ENVELOPE IS PRESENT ON FAILURE TOO. That is the requirement worth stating
// twice: a caller should never have to distinguish "JSON error object" from
// "crash text". Any failure that reaches the output layer becomes ok:false with
// a populated errors array, and the process still exits with the code from §10.

type envelope struct {
	OK        bool           `json:"ok"`
	Operation string         `json:"operation"`
	Directory *jsonDirectory `json:"directory,omitempty"`
	Result    any            `json:"result"`
	Changes   []jsonChange   `json:"changes"`
	Warnings  []string       `json:"warnings"`
	Errors    []jsonError    `json:"errors"`
}

type jsonDirectory struct {
	Path string `json:"path"`
	// ProjectID is how spec-gui.md §4 addresses this directory in a URI. It is
	// here so that `mm --find --json` hands a script the ids the UI service
	// serves, without the script having to reimplement §3.1.
	ProjectID string `json:"projectId,omitempty"`
	Project   string `json:"project"`
	NextID    string `json:"nextId,omitempty"`
	WipLimit  int    `json:"wipLimit"`
	WipUsed   int    `json:"wipUsed"`
}

type jsonChange struct {
	Kind   string `json:"kind"`
	ID     string `json:"id,omitempty"`
	File   string `json:"file"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// jsonError carries the §6.3 error NAME rather than the exit code, so a machine
// caller reads the same vocabulary the specification uses.
type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
	File    string `json:"file,omitempty"`
}

// jsonItem is an item as the envelope reports it.
//
// Every field of the domain type appears, unregistered ones included: dropping
// them here would make the JSON a lossy view of a format whose whole extension
// point is fields this implementation does not know.
type jsonItem struct {
	ID       string            `json:"id"`
	Title    string            `json:"title"`
	State    string            `json:"state"`
	Section  string            `json:"section,omitempty"`
	Slot     int               `json:"slot,omitempty"`
	Position int               `json:"position,omitempty"`
	Prio     string            `json:"prio,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Detail   string            `json:"detail,omitempty"`
	Created  string            `json:"created,omitempty"`
	Started  string            `json:"started,omitempty"`
	Done     string            `json:"done,omitempty"`
	Outcome  string            `json:"outcome,omitempty"`
	Blocked  string            `json:"blocked,omitempty"`
	Extra    map[string]string `json:"extra,omitempty"`
	File     string            `json:"file,omitempty"`
	Line     int               `json:"line,omitempty"`
}

func toJSONItem(it mm.Item) jsonItem {
	out := jsonItem{
		ID:       string(it.ID),
		Title:    it.Title,
		State:    string(it.State),
		Section:  string(it.Section),
		Slot:     it.Slot,
		Position: it.Pos,
		Prio:     string(it.Prio),
		Tags:     it.Tags,
		Detail:   it.Detail,
		Created:  it.Created.String(),
		Started:  it.Started.String(),
		Done:     it.Done.String(),
		Outcome:  string(it.Outcome),
		Blocked:  it.Blocked,
		File:     it.Source.File,
		Line:     it.Source.Line,
	}
	if len(it.Extra) > 0 {
		out.Extra = make(map[string]string, len(it.Extra))
		for _, f := range it.Extra {
			out.Extra[f.Key] = f.Value
		}
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
	return out
}

// jsonFix is the --fix result: every renumbering plus the next_id the repair
// wrote (or would write).
type jsonFix struct {
	Changes    []jsonFixChange `json:"changes"`
	NextID     string          `json:"nextId"`
	WasNext    string          `json:"wasNext"`
	NextBumped bool            `json:"nextBumped"`
}

type jsonFixChange struct {
	OldID  string `json:"oldId"`
	NewID  string `json:"newId"`
	File   string `json:"file"`
	Detail string `json:"detail,omitempty"`
}

func toJSONFix(r mm.FixResult) jsonFix {
	out := jsonFix{
		NextID:     string(r.NextID),
		WasNext:    string(r.WasNext),
		NextBumped: r.NextBumped,
	}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, jsonFixChange{
			OldID:  string(c.OldID),
			NewID:  string(c.NewID),
			File:   c.File,
			Detail: c.Detail,
		})
	}
	return out
}

func toJSONDirectory(d mm.Directory) *jsonDirectory {
	return &jsonDirectory{
		Path:      d.Path,
		ProjectID: d.ProjectID,
		Project:   d.Project,
		NextID:    string(d.NextID),
		WipLimit:  d.WipLimit,
		WipUsed:   d.WipUsed,
	}
}

// jsonOut collects an operation's output while it runs.
//
// It exists because §9.2 requires ONE object: an operation cannot print as it
// goes and then decide it failed. Everything is gathered and emitted once, at
// the end, by emit.
type jsonOut struct {
	enabled   bool
	operation string
	subject   string // the id the operation was asked about, if any
	directory *jsonDirectory
	result    any
	changes   []jsonChange
	warnings  []string
}

func (j *jsonOut) setResult(v any)          { j.result = v }
func (j *jsonOut) setChanges(r mm.TxResult) { j.changes = toJSONChanges(r) }
func (j *jsonOut) warn(s string)            { j.warnings = append(j.warnings, s) }

// emit writes the envelope. err is nil on success.
func (j *jsonOut) emit(env Env, err error) {
	e := envelope{
		OK:        err == nil,
		Operation: j.operation,
		Directory: j.directory,
		Result:    j.result,
		// Non-nil slices: a caller iterating changes should not have to
		// special-case null, and "no changes" is [] rather than absent.
		Changes:  j.changes,
		Warnings: j.warnings,
		Errors:   []jsonError{},
	}
	if e.Changes == nil {
		e.Changes = []jsonChange{}
	}
	if e.Warnings == nil {
		e.Warnings = []string{}
	}
	if err != nil {
		je := toJSONError(err)
		je.ID = j.subject
		e.Errors = append(e.Errors, je)
		e.Result = nil
	}

	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	// An encoding failure has nowhere useful to go: the envelope IS the output.
	// It is ignored rather than half-written over the top of a valid object.
	_ = enc.Encode(e)
}

// toJSONError renders one error, unwrapping the taxonomy's richer types so that
// a machine caller gets the same detail a person does.
func toJSONError(err error) jsonError {
	out := jsonError{Code: errorCode(err), Message: err.Error()}

	// An invariant failure names the first file it found a problem in, which is
	// the one a caller would act on.
	var inv *mm.InvariantError
	if errors.As(err, &inv) && len(inv.Violations) > 0 {
		out.File = inv.Violations[0].At.File
	}
	return out
}

// ---------------------------------------------------------------------------
// Per-operation result shapes
// ---------------------------------------------------------------------------

// toJSONShow is --show: the item, and its detail body when asked for.
func toJSONShow(item mm.Item, detail *mm.Detail) any {
	out := map[string]any{"item": toJSONItem(item)}
	if detail != nil {
		out["detail"] = map[string]any{
			"path":  detail.Path,
			"title": detail.Title,
			"body":  detail.Body,
		}
	}
	return out
}

// toJSONRemoval is --remove. detailOrphan is reported as a field and not only
// as a warning: a caller has to be able to act on the path, not parse prose.
func toJSONRemoval(r mm.Removal) any {
	return map[string]any{
		"item":          toJSONItem(r.Item),
		"detailDeleted": r.DetailDeleted,
		"detailOrphan":  r.DetailOrphan,
	}
}

// toJSONReport is --report. The period and its source are part of the result
// because §5.1.11 requires EVERY output mode to state them.
func toJSONReport(rep mm.Report) any {
	out := map[string]any{
		"project": rep.Project,
		"period": map[string]any{
			"label":  rep.Period.Label,
			"since":  rep.Period.Since.String(),
			"until":  rep.Period.Until.String(),
			"source": string(rep.Period.Source),
		},
		"done": toJSONItems(rep.Done),
	}
	if len(rep.Groups) > 0 {
		groups := make([]map[string]any, 0, len(rep.Groups))
		for _, g := range rep.Groups {
			groups = append(groups, map[string]any{
				"key":   g.Key,
				"items": toJSONItems(g.Items),
			})
		}
		out["groups"] = groups
	}
	if len(rep.Wip) > 0 {
		out["wip"] = toJSONItems(rep.Wip)
	}
	if len(rep.Next) > 0 {
		out["next"] = toJSONItems(rep.Next)
	}
	return out
}

// toJSONFind is --find. partial is on the object as well as in warnings, since
// a truncated scan is indistinguishable from a missing project without it.
func toJSONFind(res mm.DiscoveryResult) any {
	dirs := make([]*jsonDirectory, 0, len(res.Directories))
	for _, d := range res.Directories {
		dirs = append(dirs, toJSONDirectory(d))
	}
	return map[string]any{
		"directories": dirs,
		"partial":     res.Partial,
		"skipped":     res.Skipped,
	}
}

// toJSONStatus is --status: everything the one-screen summary carries, as
// data. Slots are listed with their item or empty, and next/oldestReady are
// null when ## Ready is empty.
func toJSONStatus(st mm.Status) any {
	slots := make([]map[string]any, 0, len(st.Directory.Slots))
	for _, slot := range st.Directory.Slots {
		s := map[string]any{"file": slot.File, "occupied": slot.Occupied()}
		if slot.Occupied() {
			s["item"] = toJSONItem(*slot.Item)
		}
		slots = append(slots, s)
	}
	out := map[string]any{
		"project": st.Directory.Project,
		"path":    st.Directory.Path,
		"wip": map[string]any{
			"used":  st.WipUsed(),
			"limit": st.WipLimit(),
		},
		"counts": map[string]any{
			"ready":   st.Ready,
			"blocked": st.Blocked,
			"someday": st.Someday,
			"done":    st.Done,
		},
		"slots": slots,
		"next":  nil,
	}
	if st.Next != nil {
		out["next"] = toJSONItem(*st.Next)
	}
	if st.OldestReady != nil {
		out["oldestReady"] = toJSONItem(*st.OldestReady)
	}
	return out
}

// toJSONSearch is --search: each hit is the item, the field it matched on, and
// the exact file:line — the navigable location.
func toJSONSearch(hits []mm.SearchHit) any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, map[string]any{
			"item":  toJSONItem(h.Item),
			"field": string(h.Field),
			"at": map[string]any{
				"file": h.At.File,
				"line": h.At.Line,
			},
			"text": h.Text,
		})
	}
	return out
}

// toJSONCheck is --check: the violations, as file/line/invariant/message, which
// is more than the human output can convey in one line.
func toJSONCheck(results []checkResult) any {
	out := make([]map[string]any, 0, len(results))
	for _, r := range results {
		violations := make([]map[string]any, 0, len(r.Violations))
		for _, v := range r.Violations {
			violations = append(violations, map[string]any{
				"invariant": v.Invariant,
				"file":      v.At.File,
				"line":      v.At.Line,
				"message":   v.Message,
			})
		}
		warnings := make([]map[string]any, 0, len(r.Warnings))
		for _, w := range r.Warnings {
			warnings = append(warnings, map[string]any{
				"file":    w.At.File,
				"line":    w.At.Line,
				"message": w.Message,
			})
		}
		out = append(out, map[string]any{
			"path":       r.Path,
			"project":    r.Project,
			"ok":         len(r.Violations) == 0,
			"violations": violations,
			"warnings":   warnings,
		})
	}
	return out
}
