package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Operation dispatch.
//
// Each case does three things and no more: turn parsed strings into library
// types, call one library operation, and hand the result to the renderer. There
// is deliberately no logic here that a UI would also need — if something looks
// like a rule rather than a translation, it belongs in package mm.

// subjectIsID lists the operations whose subject IS an item id, for the JSON
// envelope's error id (§9.2: "where applicable"). --wip 100 and --search 42
// have subjects that happen to parse as ids without being one, and labeling a
// WIP-limit failure with "T-0100" would mislead a machine caller.
var subjectIsID = map[Op]bool{
	OpShow: true, OpEdit: true, OpRemove: true, OpMove: true,
	OpStart: true, OpPause: true, OpFinish: true,
	OpBlock: true, OpUnblock: true, OpNote: true,
}

func dispatch(env Env, in *Invocation) error {
	switch in.Op {
	case OpInit:
		return runInit(env, in)
	case OpFind:
		return runFind(env, in)
	case OpCheck:
		return runCheck(env, in)
	}

	// Everything else acts on exactly one directory.
	res, err := resolveDir(in.Dir, env.Dir, env.Cwd)
	if err != nil {
		return err
	}
	store, err := mm.Open(res.Path)
	if err != nil {
		return err
	}
	if in.Verbose {
		fmt.Fprintf(env.Stderr, "directory: %s (from %s)\n", res.Path, res.Source)
	}
	// The directory's declared ID grammar is resolved ONCE, before any ID
	// argument is interpreted (spec-file-format.md §3.3.2 rule 4). --add,
	// --show and friends all parse their subjects against this grammar, and the
	// library validates strictly on the way in.
	g := mm.DefaultIDGrammar()
	if d, err := store.Directory(); err == nil {
		env.json.directory = toJSONDirectory(d)
		g = mm.IDGrammar{Prefix: d.IDPrefix, Width: d.IDWidth}
	}
	// §9.2: an error carries the id "where applicable" — only when the
	// operation's subject IS an item id (subjectIsID above). Recorded once
	// here so that a failure anywhere below names the item the user asked
	// about, and never a number or a query that merely parses as one.
	if subjectIsID[in.Op] {
		if id, err := parseIDIn(in.Subject, g); err == nil {
			env.json.subject = string(id)
		}
	}

	switch in.Op {
	case OpAdd:
		return runAdd(env, in, store)
	case OpList:
		return runList(env, in, store)
	case OpShow:
		return runShow(env, in, store, g)
	case OpEdit:
		return runEdit(env, in, store, g)
	case OpRemove:
		return runRemove(env, in, store, g)
	case OpMove:
		return runMove(env, in, store, g)
	case OpStart:
		return runStart(env, in, store, g)
	case OpPause:
		return runPause(env, in, store, g)
	case OpFinish:
		return runFinish(env, in, store, g)
	case OpReport:
		return runReport(env, in, store)
	case OpWip:
		return runWip(env, in, store)
	case OpBlock:
		return runBlock(env, in, store, g)
	case OpUnblock:
		return runUnblock(env, in, store, g)
	case OpNote:
		return runNote(env, in, store, g)
	case OpStatus:
		return runStatus(env, in, store)
	case OpNext:
		return runNext(env, in, store)
	case OpSearch:
		return runSearch(env, in, store)
	case OpFix:
		return runFix(env, in, store)
	case OpArchive:
		return runArchive(env, in, store)
	case OpMigrate:
		return runMigrate(env, in, store)
	case OpStats:
		return runStats(env, in, store)
	case OpTick:
		return runTick(env, in, store)
	}
	return usagef("--%s is not implemented", in.Op)
}

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

func runInit(env Env, in *Invocation) error {
	project := in.Value("project")
	if project == "" {
		return usagef("--init needs --project NAME")
	}
	// --dir names the directory to create. Without it, the conventional name in
	// the working directory, which is what a person means by "set this project
	// up here".
	target := in.Dir
	if target == "" {
		if env.Cwd == "" {
			return usagef("--init needs --dir when there is no working directory")
		}
		target = filepath.Join(env.Cwd, "micro-manager")
	} else {
		abs, err := absolute(target, env.Cwd)
		if err != nil {
			return err
		}
		target = abs
	}

	req := mm.InitRequest{Project: project, DryRun: in.DryRun}
	// --slots, not --wip: --wip N is an operation, and §3.2 gives operations and
	// modifiers one namespace so that no switch changes meaning depending on
	// what preceded it. See the correction note at spec-tools.md §5.1.1.
	if v := in.Value("slots"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--slots takes a number, got %q", v)
		}
		// Zero is the library's "not given" value, so it must never reach Init
		// as an explicit request: --slots 0 would otherwise silently become the
		// default of one slot.
		if n < 1 {
			return usagef("--slots must be at least 1, got %d", n)
		}
		req.Wip = n
	}
	if v := in.Value("slot-width"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--slot-width takes a number, got %q", v)
		}
		// Same zero-means-unset trap as --slots: --slot-width 0 would silently
		// become the default width of 2.
		if n < 1 {
			return usagef("--slot-width must be at least 1, got %d", n)
		}
		req.SlotWidth = n
	}
	// The ID grammar is declared once, at init, and read back by every other
	// operation (spec-file-format.md §3.3.2). An invalid prefix or width is a
	// usage error from the library, exactly like the other bad values.
	if v := in.Value("prefix"); v != "" {
		req.IDPrefix = v
	}
	if v := in.Value("id-width"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--id-width takes a number, got %q", v)
		}
		// Zero is the library's "not given" value, so it must never reach Init
		// as an explicit request: --id-width 0 would otherwise be silently
		// ignored.
		if n < 1 {
			return usagef("--id-width must be at least 1, got %d", n)
		}
		req.IDWidth = n
	}

	// The width range is a recommendation, not a rule (§3.3.2 rule 3): warn
	// when creating a directory that declares an unusual width, never fail.
	g := mm.DefaultIDGrammar()
	if req.IDPrefix != "" {
		g.Prefix = req.IDPrefix
	}
	if req.IDWidth != 0 {
		g.Width = req.IDWidth
	}
	if msg := g.WidthWarning(); msg != "" {
		env.json.warn(msg)
		if !in.Quiet && !in.JSON && !in.Porcelain {
			fmt.Fprintf(env.Stderr, "mm: warning: %s\n", msg)
		}
	}

	_, res, err := mm.Init(target, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(map[string]any{"path": target, "project": project})
	env.porcelain.row(target, project)
	renderInit(env, in, target, project, res)
	return nil
}

// subjectValue resolves an operation's subject from its own value, or from
// the positionals after "--" when the value is absent — the documented form
// mm --add -- "Title with spaces". A subject AND surplus positionals is a
// typo — an unquoted multi-word query, say — and is refused, never silently
// dropped (code-review-007 F6).
func subjectValue(in *Invocation, op, what string) (string, error) {
	if in.Subject != "" {
		if len(in.Rest) > 0 {
			return "", usagef("--%s takes one %s; %q was not expected", op, what,
				strings.Join(in.Rest, " "))
		}
		return in.Subject, nil
	}
	if len(in.Rest) > 0 {
		return strings.Join(in.Rest, " "), nil
	}
	return "", usagef("--%s needs a %s", op, what)
}

func runAdd(env Env, in *Invocation, s *mm.Store) error {
	title, err := subjectValue(in, "add", "title")
	if err != nil {
		return err
	}

	req := mm.AddRequest{
		Title:   title,
		Top:     in.Bool("top"),
		Tags:    in.Tags,
		Blocked: in.Value("blocked"),
		DryRun:  in.DryRun,
	}
	if v := in.Value("section"); v != "" {
		sec, err := mm.ParseSection(v)
		if err != nil {
			return err
		}
		req.Section = sec
	}
	if v := in.Value("prio"); v != "" {
		p, err := mm.ParsePrio(v)
		if err != nil {
			return err
		}
		req.Prio = p
	}
	if v := in.Value("created"); v != "" {
		d, err := mm.ParseDate(v)
		if err != nil {
			return err
		}
		req.Created = d
	}
	if v := in.Value("tickler"); v != "" {
		// §5.1.2: a SCHEDULE expression (spec-file-format.md §3.3), validated
		// by the library like every value. The library also enforces I7 — the
		// field needs --section someday — and a violation of that is its error
		// to raise, not a second copy of the rule here.
		req.Tickler = v
	}
	body, err := detailBody(in)
	if err != nil {
		return err
	}
	req.DetailBody = body

	item, res, err := s.Add(req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderAdd(env, in, item, res)

	// §5.1.2: the CLI opens the new detail file, the library only creates it.
	// A failure to launch is reported and not returned: the item is already
	// written and validated, and losing that over a misconfigured $EDITOR would
	// be the tool destroying good work over a preference.
	if item.Detail != "" && wantsEditor(in, env, env.Interactive) {
		if err := openEditor(env, s.Path(), filepath.Join(s.Path(), item.Detail)); err != nil {
			fmt.Fprintf(env.Stderr, "mm: could not open an editor: %v\n", err)
		}
	}
	return nil
}

// detailBody resolves the three mutually exclusive detail switches.
func detailBody(in *Invocation) (string, error) {
	n := 0
	for _, set := range []bool{in.Bool("detail"), in.Has("detail-text"), in.Has("detail-file")} {
		if set {
			n++
		}
	}
	if n > 1 {
		return "", usagef("give at most one of --detail, --detail-text and --detail-file")
	}
	switch {
	case in.Has("detail-text"):
		return in.Value("detail-text"), nil
	case in.Has("detail-file"):
		// PATH is read, not linked or moved (§5.1.2).
		data, err := os.ReadFile(in.Value("detail-file"))
		if err != nil {
			return "", ioErrorf("reading %s: %v", in.Value("detail-file"), err)
		}
		return string(data), nil
	case in.Bool("detail"):
		return "\n", nil // empty body; the template supplies the shape
	}
	return "", nil
}

func runList(env Env, in *Invocation, s *mm.Store) error {
	f := mm.Filter{Blocked: in.Bool("blocked-only")}
	if v := in.Value("state"); v != "" {
		switch v {
		case "backlog", "working", "done":
			f.State = mm.State(v)
		case "all":
			f.State = mm.StateAll
		default:
			return usagef("--state takes backlog, working, done or all, got %q", v)
		}
	}
	if v := in.Value("section"); v != "" {
		sec, err := mm.ParseSection(v)
		if err != nil {
			return err
		}
		f.Section = sec
	}
	if v := in.Value("prio"); v != "" {
		p, err := mm.ParsePrio(v)
		if err != nil {
			return err
		}
		f.Prio = p
	}
	if len(in.Tags) > 1 {
		return usagef("--list takes one --tag")
	}
	if len(in.Tags) == 1 {
		f.Tag = in.Tags[0]
	}
	if v := in.Value("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return usagef("--limit takes a positive number, got %q", v)
		}
		f.Limit = n
	}

	items, err := s.List(f)
	if err != nil {
		return err
	}
	dir, err := s.Directory()
	if err != nil {
		return err
	}
	env.json.setResult(toJSONItems(items))
	env.porcelain.items(items)
	renderList(env, in, dir, items)
	return nil
}

func runShow(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--show", g)
	if err != nil {
		return err
	}
	item, err := s.Get(id)
	if err != nil {
		return err
	}
	var detail *mm.Detail
	if in.Bool("detail") && item.Detail != "" {
		d, err := s.Detail(id)
		if err != nil {
			return err
		}
		detail = &d
	}
	env.json.setResult(toJSONShow(item, detail))
	env.porcelain.item(item)
	renderShow(env, item, detail)
	return nil
}

func runEdit(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--edit", g)
	if err != nil {
		return err
	}
	req := mm.UpdateRequest{
		AddTags:    in.Tags,
		RemoveTags: in.Untags,
		Unset:      in.Unsets,
		DryRun:     in.DryRun,
	}
	if in.Has("title") {
		v := in.Value("title")
		req.Title = &v
	}
	if in.Has("prio") {
		p, err := mm.ParsePrio(in.Value("prio"))
		if err != nil {
			return err
		}
		req.Prio = &p
	}
	if in.Has("blocked") {
		v := in.Value("blocked")
		req.Blocked = &v
	}
	if in.Has("created") {
		d, err := mm.ParseDate(in.Value("created"))
		if err != nil {
			return err
		}
		req.Created = &d
	}
	if in.Has("started") {
		d, err := mm.ParseDate(in.Value("started"))
		if err != nil {
			return err
		}
		req.Started = &d
	}
	for _, pair := range in.Sets {
		key, value, found := strings.Cut(pair, "=")
		if !found {
			return usagef("--set takes KEY=VALUE, got %q", pair)
		}
		req.Set = append(req.Set, mm.Field{Key: key, Value: value})
	}

	item, res, err := s.Update(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "updated", item, res)
	return nil
}

func runRemove(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--remove", g)
	if err != nil {
		return err
	}
	out, res, err := s.Remove(id, mm.RemoveRequest{
		Force:      in.Force,
		WithDetail: in.Bool("with-detail"),
		DryRun:     in.DryRun,
	}, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONRemoval(out))
	env.porcelain.item(out.Item)
	if out.DetailOrphan != "" {
		env.json.warn(out.DetailOrphan + " was left with no item; the directory now fails I9")
	}
	renderRemove(env, in, out, res)
	return nil
}

func runMove(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--move", g)
	if err != nil {
		return err
	}
	req := mm.MoveRequest{
		Top:     in.Bool("top"),
		End:     in.Bool("end"),
		Blocked: in.Value("blocked"),
		DryRun:  in.DryRun,
	}
	if v := in.Value("position"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--position takes a number, got %q", v)
		}
		req.Position = n
	}
	if v := in.Value("before"); v != "" {
		bid, err := parseIDIn(v, g)
		if err != nil {
			return err
		}
		req.Before = bid
	}
	if v := in.Value("after"); v != "" {
		aid, err := parseIDIn(v, g)
		if err != nil {
			return err
		}
		req.After = aid
	}
	if v := in.Value("section"); v != "" {
		sec, err := mm.ParseSection(v)
		if err != nil {
			return err
		}
		req.Section = sec
	}

	item, res, err := s.Move(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "moved", item, res)
	return nil
}

func runStart(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--start", g)
	if err != nil {
		return err
	}
	req := mm.StartRequest{DryRun: in.DryRun}
	if v := in.Value("slot"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--slot takes a number, got %q", v)
		}
		req.Slot = n
	}
	item, res, err := s.Start(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderStart(env, in, item, res)
	return nil
}

func runPause(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--pause", g)
	if err != nil {
		return err
	}
	if in.Bool("keep-notes") && in.Bool("discard-notes") {
		return usagef("--keep-notes and --discard-notes contradict each other")
	}
	req := mm.PauseRequest{
		End:          in.Bool("end"),
		Blocked:      in.Value("blocked"),
		DiscardNotes: in.Bool("discard-notes"),
		DryRun:       in.DryRun,
	}
	if v := in.Value("section"); v != "" {
		sec, err := mm.ParseSection(v)
		if err != nil {
			return err
		}
		req.Section = sec
	}
	item, res, err := s.Pause(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "paused", item, res)
	return nil
}

func runFinish(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--finish", g)
	if err != nil {
		return err
	}
	if in.Bool("keep-notes") && in.Bool("discard-notes") {
		return usagef("--keep-notes and --discard-notes contradict each other")
	}
	req := mm.FinishRequest{
		Note:         in.Value("closing-note"),
		DiscardNotes: in.Bool("discard-notes"),
		DryRun:       in.DryRun,
	}
	if v := in.Value("outcome"); v != "" {
		o, err := mm.ParseOutcome(v)
		if err != nil {
			return err
		}
		req.Outcome = o
	}
	if v := in.Value("done"); v != "" {
		d, err := mm.ParseDate(v)
		if err != nil {
			return err
		}
		req.Done = d
	}
	item, res, err := s.Finish(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "finished", item, res)
	return nil
}

// --block and --unblock are SUGAR OVER --move (spec-tools.md §5.2). They call
// the same operation with the section set, rather than being a second path that
// writes a blocked: field — two paths into I5 would eventually disagree.
func runBlock(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--block", g)
	if err != nil {
		return err
	}
	reason := in.Value("reason")
	if reason == "" && len(in.Rest) > 0 {
		reason = strings.Join(in.Rest, " ")
	}
	if reason == "" {
		return usagef("--block needs --reason TEXT; I5 requires every blocked item to say why")
	}
	item, res, err := s.Move(id, mm.MoveRequest{
		Section: mm.SectionBlocked,
		Blocked: reason,
		DryRun:  in.DryRun,
	}, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "blocked", item, res)
	return nil
}

func runUnblock(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--unblock", g)
	if err != nil {
		return err
	}
	// Back to Ready, at the top by default: something that has just become
	// possible is usually the next thing to pick up.
	req := mm.MoveRequest{Section: mm.SectionReady, Top: true, DryRun: in.DryRun}
	if in.Bool("end") {
		req.Top, req.End = false, true
	}
	item, res, err := s.Move(id, req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderChange(env, in, "unblocked", item, res)
	return nil
}

func runNote(env Env, in *Invocation, s *mm.Store, g mm.IDGrammar) error {
	id, err := subjectID(in, "--note", g)
	if err != nil {
		return err
	}
	// mm --note T-0042 "the text": the id is the operation's value and the text
	// is positional, because the operation takes two.
	text := strings.Join(in.Rest, " ")
	if text == "" {
		return usagef("--note needs some text: mm --note %s \"what happened\"", id)
	}
	item, res, err := s.Note(id, mm.NoteRequest{Text: text, DryRun: in.DryRun}, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderNote(env, in, item, res)
	return nil
}

func runWip(env Env, in *Invocation, s *mm.Store) error {
	v := in.Subject
	if v == "" {
		return usagef("--wip needs a number")
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return usagef("--wip takes a number, got %q", v)
	}
	dir, res, err := s.SetWipLimit(n, in.DryRun)
	if err != nil {
		return err
	}
	env.json.setChanges(res)
	env.json.setResult(toJSONDirectory(dir))
	env.porcelain.row(dir.Path, dir.Project,
		strconv.Itoa(dir.WipUsed), strconv.Itoa(dir.WipLimit))
	renderWip(env, in, dir, res)
	return nil
}

func runReport(env Env, in *Invocation, s *mm.Store) error {
	period, err := resolvePeriod(env, in)
	if err != nil {
		return err
	}
	opts := mm.ReportOptions{
		IncludeWip:      in.Bool("include-wip"),
		IncludeBacklog:  in.Bool("include-backlog"),
		IncludeArchives: in.Bool("include-archives"),
	}
	if v := in.Value("group-by"); v != "" {
		g, err := mm.ParseGroupBy(v)
		if err != nil {
			return err
		}
		opts.GroupBy = g
	}
	rep, err := s.Report(period, opts)
	if err != nil {
		return err
	}
	env.json.setResult(toJSONReport(rep))
	for _, it := range rep.Done {
		env.porcelain.row(string(it.ID), it.Done.String(), string(it.Outcome),
			mm.FormatTags(it.Tags), it.Title)
	}
	for _, w := range rep.Warnings {
		env.json.warn(w)
	}
	renderReport(env, rep)
	return nil
}

// resolvePeriod applies the §5.1.11 precedence. It lives here and not in the
// library because two of the five steps are the environment and a default, and
// the library is told the answer rather than how it was reached.
func resolvePeriod(env Env, in *Invocation) (mm.Period, error) {
	p, given, err := periodFromSwitches(env, in)
	if err != nil {
		return mm.Period{}, err
	}
	if given {
		return p, nil
	}

	// 4. MM_REPORT_PERIOD. An unparseable value is a usage error NAMING THE
	//    VARIABLE — never a silent fallback, because a typo in a shell profile
	//    would otherwise quietly change every report the user ever runs.
	if env.ReportPeriod != "" {
		p, err := mm.ParsePeriod(env.ReportPeriod, env.Today)
		if err != nil {
			return mm.Period{}, usagef(
				"MM_REPORT_PERIOD is set to %q, which is not a period: %v",
				env.ReportPeriod, err)
		}
		p.Source = mm.PeriodFromEnvironment
		return p, nil
	}

	// 5. The default is last-week, not this-week: a report over a closed period
	//    is reproducible and one over an open period is not.
	p, err = mm.ParsePeriod("last-week", env.Today)
	if err != nil {
		return mm.Period{}, err
	}
	p.Source = mm.PeriodFromDefault
	return p, nil
}

// resolveStatsPeriod is the same switches with a different default and no
// environment step.
//
// MM_REPORT_PERIOD is documented as the default --report period, and stats is
// not a report: letting it narrow a stats run would mean a variable set for one
// operation silently changing another. The default is ALL of history, because
// throughput and cycle time over one week are a sample, and the question
// --stats answers is usually about the trend.
func resolveStatsPeriod(env Env, in *Invocation) (mm.Period, error) {
	p, given, err := periodFromSwitches(env, in)
	if err != nil {
		return mm.Period{}, err
	}
	if given {
		return p, nil
	}
	p, err = mm.ParsePeriod("all", env.Today)
	if err != nil {
		return mm.Period{}, err
	}
	p.Source = mm.PeriodFromDefault
	return p, nil
}

// periodFromSwitches applies steps 1-3 of §5.1.11 — the ones that are a switch
// the user typed — and reports whether any of them fired.
func periodFromSwitches(env Env, in *Invocation) (mm.Period, bool, error) {
	// 1. --since / --until, an explicit range.
	if in.Has("since") || in.Has("until") {
		var since, until mm.Date
		var err error
		if v := in.Value("since"); v != "" {
			if since, err = mm.ParseDate(v); err != nil {
				return mm.Period{}, false, err
			}
		}
		if v := in.Value("until"); v != "" {
			if until, err = mm.ParseDate(v); err != nil {
				return mm.Period{}, false, err
			}
		} else {
			until = env.Today // --until defaults to today
		}
		p, err := mm.PeriodBetween(since, until)
		if err != nil {
			return mm.Period{}, false, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, true, nil
	}

	// 2. --week, 3. --period / --last-week / --this-week.
	token := ""
	switch {
	case in.Has("week"):
		token = in.Value("week")
	case in.Has("period"):
		token = in.Value("period")
	case in.Bool("last-week"):
		token = "last-week"
	case in.Bool("this-week"):
		token = "this-week"
	}
	if token != "" {
		p, err := mm.ParsePeriod(token, env.Today)
		if err != nil {
			return mm.Period{}, false, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, true, nil
	}
	return mm.Period{}, false, nil
}

func runFind(env Env, in *Invocation) error {
	roots := discoveryRoots(env.Cwd)
	if in.Dir != "" {
		abs, err := absolute(in.Dir, env.Cwd)
		if err != nil {
			return err
		}
		roots = []string{abs}
	}
	if len(roots) == 0 {
		return usagef("--find needs a directory to scan from")
	}
	found := mm.Discover(mm.DefaultDiscoveryOptions(roots...))
	env.json.setResult(toJSONFind(found))
	for _, d := range found.Directories {
		env.porcelain.row(d.Path, d.Project,
			strconv.Itoa(d.WipUsed), strconv.Itoa(d.WipLimit))
	}
	if found.Partial {
		env.json.warn("the scan hit a limit; results may be incomplete")
	}
	renderFind(env, found)
	return nil
}

// runStatus is --status (spec-tools.md §5.2): one screen over the whole
// directory — slots, WIP n/N, counts by section, next, and the oldest Ready
// item nobody has started. One call answers all of it, so the screen cannot
// show two moments in time.
func runStatus(env Env, in *Invocation, s *mm.Store) error {
	st, err := s.Status()
	if err != nil {
		return err
	}
	env.json.setResult(toJSONStatus(st))
	env.porcelain.status(st)
	renderStatus(env, in, st)
	return nil
}

// runNext is --next: the top of ## Ready, or a not-found exit so a script can
// stop rather than start something arbitrary.
func runNext(env Env, in *Invocation, s *mm.Store) error {
	item, err := s.Next()
	if err != nil {
		return err
	}
	env.json.setResult(toJSONItem(item))
	env.porcelain.item(item)
	renderNext(env, in, item)
	return nil
}

// runSearch is --search: the same match the GUI's search-input and the board's
// ?q= filter use, because the library owns the matcher and the front ends only
// pass a query (T-0042).
func runSearch(env Env, in *Invocation, s *mm.Store) error {
	query, err := subjectValue(in, "search", "query")
	if err != nil {
		return err
	}

	req := mm.SearchRequest{
		Query: query,
		Regex: in.Bool("regex"),
	}
	for _, f := range in.Fields {
		switch f {
		case "title", "tags", "detail":
			req.Fields = append(req.Fields, mm.SearchField(f))
		default:
			return usagef("--field takes title, tags or detail, got %q", f)
		}
	}
	if v := in.Value("state"); v != "" {
		switch v {
		case "backlog", "working", "done":
			req.State = mm.State(v)
		default:
			return usagef("--state takes backlog, working or done, got %q", v)
		}
	}
	if v := in.Value("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return usagef("--limit takes a positive number, got %q", v)
		}
		req.Limit = n
	}

	hits, err := s.Search(req)
	if err != nil {
		return err
	}
	env.json.setResult(toJSONSearch(hits))
	env.porcelain.search(hits)
	renderSearch(env, in, hits)
	return nil
}

// subjectID reads the operation's own value as an item ID, in the directory's
// declared grammar.
func subjectID(in *Invocation, op string, g mm.IDGrammar) (mm.ID, error) {
	if in.Subject == "" {
		return "", usagef("%s needs an item id", op)
	}
	return parseIDIn(in.Subject, g)
}

// --fix (plan-git-support.md decision 4).
//
// The deterministic repair for what a git merge manufactures: an I1 duplicate
// (two branches both allocated the same ID) and the I2 ceiling left behind. It
// refuses while anything else is wrong — markers, dangling details, a tie — and
// is safe to run twice: the second run is a no-op.
func runFix(env Env, in *Invocation, s *mm.Store) error {
	res, tx, err := s.Fix(mm.FixRequest{DryRun: in.DryRun})
	if err != nil {
		return err
	}
	env.json.setChanges(tx)
	env.json.setResult(toJSONFix(res))
	for _, c := range res.Changes {
		env.porcelain.row(string(c.OldID), string(c.NewID), c.File, c.Detail)
	}
	renderFix(env, in, res, tx)
	return nil
}

// runArchive wires spec-tools.md §5.3.1, the first of the optional operations
// to reach the CLI.
//
// The two spellings of the cutoff resolve to the one thing the library takes, a
// Before month: --age is turned into it by mm.ArchiveCutoff, which is in the
// library because a UI running this on a schedule must reach the same answer
// from the same policy.
func runArchive(env Env, in *Invocation, s *mm.Store) error {
	req := mm.ArchiveRequest{DryRun: in.DryRun}

	switch {
	case in.Has("before") && in.Has("age"):
		return usagef("--before and --age are two spellings of one cutoff; give one")

	case in.Has("before"):
		v := in.Value("before")
		text := v
		if len(text) == 7 {
			// A month is the switch's own grain (§5.3.1). A full date is
			// accepted and its day ignored, so --before 2026-07-23 keeps all of
			// July rather than being refused.
			text += "-01"
		}
		d, err := mm.ParseDate(text)
		if err != nil {
			return usagef("--before takes YYYY-MM, or a full date whose day is "+
				"ignored; got %q", v)
		}
		req.Before = d

	case in.Has("age"):
		v := in.Value("age")
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--age takes a number of days, got %q", v)
		}
		d, err := mm.ArchiveCutoff(env.Today, n)
		if err != nil {
			return err
		}
		req.Before = d
	}

	res, tx, err := s.Archive(req, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(tx)
	env.json.setResult(toJSONArchive(res))
	// One record per item moved, from the change set rather than from a name
	// this layer would have to construct: the library already knows which
	// archive each item landed in, and where each detail file went. A detail
	// move is a change too, so it is folded into its item's record rather than
	// emitted as a second row for the same id.
	detailFor := map[mm.ID]string{}
	movedInto := map[string]bool{}
	for _, mv := range res.DetailsMoved {
		detailFor[mv.ID] = mv.To
		movedInto[mv.To] = true
	}
	for _, c := range tx.Changes {
		if c.Kind == mm.ChangeMoved && !movedInto[c.File] {
			env.porcelain.row(string(c.ID), c.File, detailFor[c.ID])
		}
	}
	for _, w := range res.Warnings {
		env.json.warn(w)
	}
	renderArchive(env, in, res)
	return nil
}

// runMigrate wires spec-tools.md §5.3's --migrate, the second optional
// operation to reach the CLI.
//
// --project is reused from --init rather than given a name of its own: it means
// the same thing in both, the human name of the directory, and a second switch
// for one concept is a second thing to remember.
func runMigrate(env Env, in *Invocation, s *mm.Store) error {
	res, tx, err := s.Migrate(mm.MigrateRequest{
		Project: in.Value("project"),
		DryRun:  in.DryRun,
	}, env.Today)
	if err != nil {
		return err
	}
	env.json.setChanges(tx)
	env.json.setResult(toJSONMigrate(res))
	for _, c := range res.Changes {
		env.porcelain.row(string(c.Kind), c.File, strconv.Itoa(c.Line), c.After)
	}
	for _, w := range res.Warnings {
		env.json.warn(w)
	}
	renderMigrate(env, in, res)
	return nil
}

// runTick wires spec-tools.md §5.3.3's --tick, the fourth optional operation
// to reach the CLI, and the one a cron is expected to run on a schedule.
//
// The whole run reports what fired and what errored: the two kinds of fire are
// the output's first column, and per-item failures go to stderr exactly like
// --archive's warnings, because a run that mostly succeeded must still say the
// part that did not. The exit code stays 0 when an item errored: the run
// completed, the error is a result the caller acts on, not a crashed process
// (spec-tools.md §5.3.3: "a failing item never aborts the run").
func runTick(env Env, in *Invocation, s *mm.Store) error {
	res, err := s.Tick(env.Today, in.DryRun)
	if err != nil {
		return err
	}
	for _, e := range res.Errors {
		env.json.warn(string(e.ID) + ": " + e.Error.Error())
	}
	env.json.setResult(toJSONTick(res))
	env.porcelain.tick(res)
	// The change set is synthesised from the fires, exactly as the human
	// renderer reports them: each one-shot is a move within backlog.md, each
	// recurring fire updates its prototype and creates the spawned item.
	for _, f := range res.Fired {
		if f.Kind == mm.FireMove {
			env.json.changes = append(env.json.changes, jsonChange{
				Kind: "moved", ID: string(f.ID), File: "backlog.md",
				Before: f.Before, After: f.After,
			})
		} else {
			env.json.changes = append(env.json.changes,
				jsonChange{Kind: "updated", ID: string(f.ID), File: "backlog.md",
					Before: f.Before, After: f.After},
				jsonChange{Kind: "created", ID: string(f.Spawned), File: "backlog.md"},
			)
		}
	}
	renderTick(env, in, res)
	return nil
}

// runStats wires spec-tools.md §5.3's --stats, the third optional operation.
//
// The period defaults to all of history rather than to last week: throughput
// and cycle time over seven days are a sample, and the question this answers is
// usually about the trend.
func runStats(env Env, in *Invocation, s *mm.Store) error {
	period, err := resolveStatsPeriod(env, in)
	if err != nil {
		return err
	}
	bucket, err := mm.ParseBucket(in.Value("bucket"))
	if err != nil {
		return err
	}
	res, err := s.Stats(period, mm.StatsOptions{
		Bucket:          bucket,
		IncludeArchives: in.Bool("include-archives"),
	}, env.Today)
	if err != nil {
		return err
	}
	env.json.setResult(toJSONStats(res))
	// One record per bucket: the series is the part a pipeline plots, and the
	// totals are all derivable from it.
	for _, b := range res.Buckets {
		env.porcelain.row(b.Label, b.Since.String(), b.Until.String(),
			strconv.Itoa(b.Closed), strconv.Itoa(b.WipPeak),
			strconv.FormatFloat(b.WipMean, 'f', 2, 64))
	}
	for _, w := range res.Warnings {
		env.json.warn(w)
	}
	renderStats(env, in, res)
	return nil
}

// dirLabel shortens a path for display, relative to the working directory.
//
// cwd is a parameter rather than "." because filepath.Rel cannot relate a
// relative base to an absolute path: it returns an error, and the fallback
// printed the full absolute path for every directory under the one the user is
// standing in.
func dirLabel(cwd, path string) string {
	if cwd == "" {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	if rel == "." {
		return path
	}
	return rel
}
