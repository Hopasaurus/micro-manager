package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"micromanager/mm"
)

// Operation dispatch.
//
// Each case does three things and no more: turn parsed strings into library
// types, call one library operation, and hand the result to the renderer. There
// is deliberately no logic here that a UI would also need — if something looks
// like a rule rather than a translation, it belongs in package mm.

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

	switch in.Op {
	case OpAdd:
		return runAdd(env, in, store)
	case OpList:
		return runList(env, in, store)
	case OpShow:
		return runShow(env, in, store)
	case OpEdit:
		return runEdit(env, in, store)
	case OpRemove:
		return runRemove(env, in, store)
	case OpMove:
		return runMove(env, in, store)
	case OpStart:
		return runStart(env, in, store)
	case OpPause:
		return runPause(env, in, store)
	case OpFinish:
		return runFinish(env, in, store)
	case OpReport:
		return runReport(env, in, store)
	case OpWip:
		return runWip(env, in, store)
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
		req.Wip = n
	}
	if v := in.Value("slot-width"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return usagef("--slot-width takes a number, got %q", v)
		}
		req.SlotWidth = n
	}

	_, res, err := mm.Init(target, req, env.Today)
	if err != nil {
		return err
	}
	renderInit(env, in, target, project, res)
	return nil
}

func runAdd(env Env, in *Invocation, s *mm.Store) error {
	title := in.Subject
	if title == "" && len(in.Rest) > 0 {
		title = strings.Join(in.Rest, " ")
	}
	if title == "" {
		return usagef("--add needs a title")
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
	body, err := detailBody(in)
	if err != nil {
		return err
	}
	req.DetailBody = body

	item, res, err := s.Add(req, env.Today)
	if err != nil {
		return err
	}
	renderAdd(env, in, item, res)
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
	renderList(env, in, dir, items)
	return nil
}

func runShow(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--show")
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
	renderShow(env, item, detail)
	return nil
}

func runEdit(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--edit")
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
	renderChange(env, in, "updated", item, res)
	return nil
}

func runRemove(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--remove")
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
	renderRemove(env, in, out, res)
	return nil
}

func runMove(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--move")
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
		bid, err := ParseID(v)
		if err != nil {
			return err
		}
		req.Before = bid
	}
	if v := in.Value("after"); v != "" {
		aid, err := ParseID(v)
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
	renderChange(env, in, "moved", item, res)
	return nil
}

func runStart(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--start")
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
	renderStart(env, in, item, res)
	return nil
}

func runPause(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--pause")
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
	renderChange(env, in, "paused", item, res)
	return nil
}

func runFinish(env Env, in *Invocation, s *mm.Store) error {
	id, err := subjectID(in, "--finish")
	if err != nil {
		return err
	}
	if in.Bool("keep-notes") && in.Bool("discard-notes") {
		return usagef("--keep-notes and --discard-notes contradict each other")
	}
	req := mm.FinishRequest{
		Note:         in.Value("note"),
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
	renderChange(env, in, "finished", item, res)
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
	renderWip(env, in, dir, res)
	return nil
}

func runReport(env Env, in *Invocation, s *mm.Store) error {
	period, err := resolvePeriod(env, in)
	if err != nil {
		return err
	}
	opts := mm.ReportOptions{
		IncludeWip:     in.Bool("include-wip"),
		IncludeBacklog: in.Bool("include-backlog"),
	}
	if v := in.Value("group-by"); v != "" {
		g, err := mm.ParseGroupBy(v)
		if err != nil {
			return err
		}
		opts.GroupBy = g
	}
	if in.Bool("include-archives") {
		return usagef("--include-archives is not implemented yet (T-0043)")
	}

	rep, err := s.Report(period, opts)
	if err != nil {
		return err
	}
	renderReport(env, rep)
	return nil
}

// resolvePeriod applies the §5.1.11 precedence. It lives here and not in the
// library because two of the five steps are the environment and a default, and
// the library is told the answer rather than how it was reached.
func resolvePeriod(env Env, in *Invocation) (mm.Period, error) {
	// 1. --since / --until, an explicit range.
	if in.Has("since") || in.Has("until") {
		var since, until mm.Date
		var err error
		if v := in.Value("since"); v != "" {
			if since, err = mm.ParseDate(v); err != nil {
				return mm.Period{}, err
			}
		}
		if v := in.Value("until"); v != "" {
			if until, err = mm.ParseDate(v); err != nil {
				return mm.Period{}, err
			}
		} else {
			until = env.Today // --until defaults to today
		}
		p, err := mm.PeriodBetween(since, until)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, nil
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
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
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
	p, err := mm.ParsePeriod("last-week", env.Today)
	if err != nil {
		return mm.Period{}, err
	}
	p.Source = mm.PeriodFromDefault
	return p, nil
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
	renderFind(env, mm.Discover(mm.DefaultDiscoveryOptions(roots...)))
	return nil
}

// subjectID reads the operation's own value as an item ID.
func subjectID(in *Invocation, op string) (mm.ID, error) {
	if in.Subject == "" {
		return "", usagef("%s needs an item id", op)
	}
	return ParseID(in.Subject)
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
