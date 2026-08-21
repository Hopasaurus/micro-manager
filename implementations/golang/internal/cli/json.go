package cli

import (
	"encoding/json"
	"errors"

	"github.com/Hopasaurus/micro-manager/mm"
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
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	State    string   `json:"state"`
	Section  string   `json:"section,omitempty"` // version 1 only
	Stage    string   `json:"stage,omitempty"`   // version 2 only
	Slot     int      `json:"slot,omitempty"`    // version 1 only
	Position int      `json:"position,omitempty"`
	Prio     string   `json:"prio,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Created  string   `json:"created,omitempty"`
	Started  string   `json:"started,omitempty"`
	Tickler  string   `json:"tickler,omitempty"`
	// TicklerDest overrides the fire destination (§5.1.4); version 2 only.
	TicklerDest string            `json:"ticklerDest,omitempty"`
	Tickled     string            `json:"tickled,omitempty"`
	Done        string            `json:"done,omitempty"`
	Outcome     string            `json:"outcome,omitempty"`
	Blocked     string            `json:"blocked,omitempty"` // version 1 only
	Reason      string            `json:"reason,omitempty"`  // version 2 only, renamed from blocked
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
		Detail:      it.Detail,
		Created:     it.Created.String(),
		Started:     it.Started.String(),
		Tickler:     it.Tickler,
		TicklerDest: string(it.TicklerDest),
		Tickled:     it.Tickled.String(),
		Done:        it.Done.String(),
		Outcome:     string(it.Outcome),
		Blocked:     it.Blocked,
		Reason:      it.Reason,
		File:        it.Source.File,
		Line:        it.Source.Line,
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

// jsonCapabilities is the --help --json result (spec-tools.md §3.4.1): what
// this build accepts.
//
// Built from the PARSER's tables, never from the help prose. Most of the
// surface is optional (§5.2, §5.3), so a caller has to be able to ask what is
// here — and asking a document written for a human is how a re-indented line
// silently changes what a machine believes.
//
// Names carry no leading dashes: a caller compares strings rather than
// stripping punctuation off them.
type jsonCapabilities struct {
	Version    string   `json:"version"`
	FormatSpec string   `json:"formatSpec"`
	Operations []string `json:"operations"`
	Modifiers  []string `json:"modifiers"`
	// About and Usage are set only when an operation was named, so that
	// `mm --help --add-many --json` answers "what are its switches" as well as
	// "does this build have it".
	About string `json:"about,omitempty"`
	Usage string `json:"usage,omitempty"`
}

func toJSONCapabilities(op Op) jsonCapabilities {
	out := jsonCapabilities{
		Version:    Version,
		FormatSpec: FormatSpecVersion,
		Operations: operationNames(),
		Modifiers:  modifierNames(),
	}
	if op != OpNone {
		out.About = string(op)
		out.Usage = operationUsage[op]
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

// jsonArchive is the --archive result. The cutoff is included because it can
// come from a default or from --age; detailsMoved because those files are no
// longer where §5.4 says a detail file lives, and a caller restoring an item
// has to move each one back; detailOrphans because each one names a detail:
// that pointed at nothing.
type jsonArchive struct {
	Cutoff        string           `json:"cutoff"`
	Months        []string         `json:"months"`
	Items         int              `json:"items"`
	Files         []string         `json:"files"`
	DetailsMoved  []jsonDetailMove `json:"detailsMoved"`
	DetailOrphans []string         `json:"detailOrphans"`
}

type jsonDetailMove struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

func toJSONArchive(r mm.ArchiveResult) jsonArchive {
	out := jsonArchive{
		Cutoff:        r.Cutoff,
		Months:        r.Months,
		Items:         r.Items,
		Files:         r.Files,
		DetailsMoved:  []jsonDetailMove{},
		DetailOrphans: r.DetailOrphans,
	}
	for _, mv := range r.DetailsMoved {
		out.DetailsMoved = append(out.DetailsMoved, jsonDetailMove{
			ID: string(mv.ID), From: mv.From, To: mv.To,
		})
	}
	// §9.2: a list is always a list. An absent one would make a caller test for
	// null before iterating, on a field that means "none".
	if out.Months == nil {
		out.Months = []string{}
	}
	if out.Files == nil {
		out.Files = []string{}
	}
	if out.DetailOrphans == nil {
		out.DetailOrphans = []string{}
	}
	return out
}

// jsonMigrate is the --migrate result. Changes is the legacy repair's own
// shape (spec-tools.md §5.3, T-0044) — kept exactly as it was, since a caller
// already parses it — and Steps is the versioned chain's result (§5.3.4),
// empty when the directory was already at the latest version this build
// implements or the legacy repair was the whole story.
type jsonMigrate struct {
	Changes []jsonMigrateChange `json:"changes"`
	Steps   []jsonMigrationStep `json:"steps,omitempty"`
}

type jsonMigrateChange struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Line   int    `json:"line,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// jsonMigrationStep is one link of the chain --migrate walked.
type jsonMigrationStep struct {
	From     int          `json:"from"`
	To       int          `json:"to"`
	Changes  []jsonChange `json:"changes"`
	Warnings []string     `json:"warnings"`
}

func toJSONMigrate(r mm.MigrateResult, steps []mm.MigrationResult) jsonMigrate {
	out := jsonMigrate{Changes: []jsonMigrateChange{}, Steps: []jsonMigrationStep{}}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, jsonMigrateChange{
			Kind: string(c.Kind), File: c.File, Line: c.Line,
			Before: c.Before, After: c.After,
		})
	}
	for _, st := range steps {
		warnings := st.Warnings
		if warnings == nil {
			warnings = []string{}
		}
		out.Steps = append(out.Steps, jsonMigrationStep{
			From: st.From, To: st.To,
			Changes:  toJSONChanges(mm.TxResult{Changes: st.Changes}),
			Warnings: warnings,
		})
	}
	return out
}

// jsonStats is the --stats result. The period and bucket are included because
// every number below depends on them, and a series with no stated resolution is
// one a caller can misplot.
type jsonStats struct {
	Period    string            `json:"period"`
	Since     string            `json:"since,omitempty"`
	Until     string            `json:"until,omitempty"`
	Bucket    string            `json:"bucket"`
	Closed    int               `json:"closed"`
	ByOutcome []jsonCount       `json:"byOutcome"`
	Buckets   []jsonStatsBucket `json:"buckets"`
	Cycle     jsonCycleTime     `json:"cycleTime"`
	Wip       jsonWipSummary    `json:"wip"`
	Tags      []jsonCount       `json:"tags"`
	Untagged  int               `json:"untagged"`
}

type jsonCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type jsonStatsBucket struct {
	Label   string  `json:"label"`
	Since   string  `json:"since"`
	Until   string  `json:"until"`
	Closed  int     `json:"closed"`
	WipPeak int     `json:"wipPeak"`
	WipMean float64 `json:"wipMean"`
}

type jsonCycleTime struct {
	N       int     `json:"n"`
	Unknown int     `json:"unknown"`
	Mean    float64 `json:"mean"`
	Median  float64 `json:"median"`
	P90     float64 `json:"p90"`
	Min     int     `json:"min"`
	Max     int     `json:"max"`
}

type jsonWipSummary struct {
	Peak   int     `json:"peak"`
	PeakOn string  `json:"peakOn,omitempty"`
	Mean   float64 `json:"mean"`
}

func toJSONStats(r mm.StatsResult) jsonStats {
	counts := func(in []mm.TagCount) []jsonCount {
		out := make([]jsonCount, 0, len(in))
		for _, c := range in {
			out = append(out, jsonCount{Name: c.Tag, Count: c.Count})
		}
		return out
	}
	out := jsonStats{
		Period:    r.Period.String(),
		Since:     r.Period.Since.String(),
		Until:     r.Period.Until.String(),
		Bucket:    string(r.Bucket),
		Closed:    r.Closed,
		ByOutcome: counts(r.ByOutcome),
		Buckets:   []jsonStatsBucket{},
		Cycle: jsonCycleTime{
			N: r.Cycle.N, Unknown: r.Cycle.Unknown, Mean: r.Cycle.Mean,
			Median: r.Cycle.Median, P90: r.Cycle.P90,
			Min: r.Cycle.Min, Max: r.Cycle.Max,
		},
		Wip:      jsonWipSummary{Peak: r.WipPeak, PeakOn: r.WipPeakOn.String(), Mean: r.WipMean},
		Tags:     counts(r.Tags),
		Untagged: r.Untagged,
	}
	for _, b := range r.Buckets {
		out.Buckets = append(out.Buckets, jsonStatsBucket{
			Label: b.Label, Since: b.Since.String(), Until: b.Until.String(),
			Closed: b.Closed, WipPeak: b.WipPeak, WipMean: b.WipMean,
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
	if len(rep.StageIncluded) > 0 {
		groups := make([]map[string]any, 0, len(rep.StageIncluded))
		for _, g := range rep.StageIncluded {
			groups = append(groups, map[string]any{
				"stage": string(g.Stage),
				"label": g.Label,
				"items": toJSONItems(g.Items),
			})
		}
		out["stageIncluded"] = groups
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
//
// counts/wip keep their version-1 shape and meaning exactly, for a caller
// already parsing them; they read zero for a version-2 directory, same as
// the human renderer's slot listing is simply empty for one, rather than
// being repurposed to mean something else. stages is version 2's own shape,
// present only there, in stages: order - a version-2-aware caller reads
// this instead, not counts reinterpreted.
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
		"version": st.Directory.Version,
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
	if st.Directory.Version == 2 {
		stages := make([]map[string]any, 0, len(st.Directory.StageCfg.Stages))
		for _, stage := range st.Directory.StageCfg.Stages {
			row := map[string]any{
				"slug":  string(stage),
				"label": st.Directory.StageCfg.Label(stage),
				"count": st.StageCounts[stage],
			}
			if limit, capped := st.Directory.StageCfg.WipLimits[stage]; capped {
				row["wipLimit"] = limit
			}
			stages = append(stages, row)
		}
		out["stages"] = stages
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

// toJSONTick is --tick: what fired and what errored, exactly the two halves
// spec-tools.md §5.3.3 requires a run to report. Each fired entry names its
// kind and the spawned ID, so a caller can drive a board off a cron without
// parsing prose; errors carry the per-item failure verbatim.
func toJSONTick(res mm.TickResult) any {
	fired := make([]map[string]any, 0, len(res.Fired))
	for _, f := range res.Fired {
		out := map[string]any{
			"id":      string(f.ID),
			"kind":    string(f.Kind),
			"tickled": f.Tickled.String(),
		}
		if f.Kind == mm.FireSpawn {
			out["spawned"] = string(f.Spawned)
		}
		fired = append(fired, out)
	}
	errors := make([]map[string]any, 0, len(res.Errors))
	for _, e := range res.Errors {
		errors = append(errors, map[string]any{
			"id":    string(e.ID),
			"error": e.Error.Error(),
		})
	}
	return map[string]any{
		"fired":  fired,
		"errors": errors,
	}
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
