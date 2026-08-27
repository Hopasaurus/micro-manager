package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Human output (spec-tools.md §9.1).
//
// Two rules shape all of it:
//
//   - EVERY MUTATION REPORTS WHAT CHANGED, including the assigned ID for --add.
//     Silence on success is wrong here: the ID is the handle for every command
//     that follows, and a caller who just created an item has no other way to
//     learn it.
//   - The report is pasted elsewhere, so it is plain markdown. No box drawing,
//     no colour — nothing essential may be carried by colour alone, and the
//     simplest way to honour that is not to use any.
//
// --dry-run prints exactly what a real run would, prefixed, so that reading the
// output is how you check a command before running it for real.

func out(env Env, format string, args ...any) {
	fmt.Fprintf(env.Stdout, format, args...)
}

// prefix marks dry-run output. It is a prefix rather than a trailing note so
// that it is visible on every line even when the output scrolls.
func prefix(in *Invocation) string {
	if in.DryRun {
		return "would: "
	}
	return ""
}

func renderInit(env Env, in *Invocation, path, project string, res mm.TxResult) {
	if in.Quiet {
		return
	}
	out(env, "%screated %s for %s\n", prefix(in), path, project)
	for _, f := range res.Files {
		out(env, "  %s\n", f)
	}
}

// renderArchive reports what left done.md, and always states the cutoff — it
// can come from a default or from --age, so an operation whose boundary is
// invisible is one the caller cannot check.
//
// The warnings go to stderr and are NOT suppressed by --quiet. §5.3 makes the
// ID-pool warning mandatory: this is the one operation that takes data out of
// the validated set, and quiet asks for less commentary, not for less of the
// one thing the specification insists is said out loud.
func renderArchive(env Env, in *Invocation, res mm.ArchiveResult) {
	if !in.Quiet {
		if res.Items == 0 {
			out(env, "%snothing to archive: no month group before %s\n",
				prefix(in), res.Cutoff)
		} else {
			out(env, "%sarchived %d item(s) from %d month(s) before %s\n",
				prefix(in), res.Items, len(res.Months), res.Cutoff)
			for _, m := range res.Months {
				out(env, "  %s\n", m)
			}
			for _, f := range res.Files {
				out(env, "  wrote %s\n", f)
			}
			if n := len(res.DetailsMoved); n > 0 {
				// Not listed one by one: on a real board this is fifty files,
				// and the JSON and porcelain streams carry every path for
				// anything that needs them.
				out(env, "  moved %d detail file(s) into details-YYYY/\n", n)
			}
		}
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(env.Stderr, "mm: warning: %s\n", w)
	}
}

// renderStats prints the four measures §5.3 names, each labelled with what it
// actually measures.
//
// The labels are not decoration. "in flight" rather than "WIP" because the
// number counts items between started: and done:, not slots occupied — a board
// that starts and finishes ten things in a day reads as ten in flight that day
// and never had more than one slot busy. Saying "WIP 10" would be a lie the
// format cannot even check.
func renderStats(env Env, in *Invocation, res mm.StatsResult) {
	if !in.Quiet {
		out(env, "%s — %s, by %s\n", res.Project, res.Period.String(), res.Bucket)

		out(env, "\nclosed %d", res.Closed)
		if len(res.ByOutcome) > 0 {
			out(env, " (%s)", joinCounts(res.ByOutcome))
		}
		out(env, "\n")

		if c := res.Cycle; c.N > 0 {
			out(env, "cycle time, started to done: mean %.1fd, median %.1fd, p90 %.1fd, "+
				"range %d-%dd, over %d item(s)\n",
				c.Mean, c.Median, c.P90, c.Min, c.Max, c.N)
		} else {
			out(env, "cycle time: nothing measurable\n")
		}
		if res.Cycle.Unknown > 0 {
			out(env, "  %d closed item(s) had no usable started date and are excluded\n",
				res.Cycle.Unknown)
		}
		out(env, "in flight: peak %d", res.WipPeak)
		if !res.WipPeakOn.IsZero() {
			out(env, " on %s", res.WipPeakOn)
		}
		out(env, ", mean %.1f per day\n", res.WipMean)

		if len(res.Buckets) > 0 {
			out(env, "\n%-12s %7s %10s %10s\n", "period", "closed", "flight max", "flight avg")
			for _, b := range res.Buckets {
				out(env, "%-12s %7d %10d %10.1f\n", b.Label, b.Closed, b.WipPeak, b.WipMean)
			}
		}
		if len(res.Tags) > 0 || res.Untagged > 0 {
			// The top few only: a mature board has dozens of tags, and forty of
			// them on one wrapped line is not a distribution anyone reads. The
			// whole list is in --json and --porcelain, which is where something
			// that wants all of it should be looking.
			const shown = 10
			tags := res.Tags
			rest := 0
			if len(tags) > shown {
				rest = len(tags) - shown
				tags = tags[:shown]
			}
			out(env, "\ntags: %s", joinCounts(tags))
			if rest > 0 {
				out(env, ", +%d more", rest)
			}
			if res.Untagged > 0 {
				out(env, ", untagged %d", res.Untagged)
			}
			out(env, "\n")
		}
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(env.Stderr, "mm: warning: %s\n", w)
	}
}

func joinCounts(counts []mm.TagCount) string {
	parts := make([]string, 0, len(counts))
	for _, c := range counts {
		parts = append(parts, fmt.Sprintf("%s %d", c.Tag, c.Count))
	}
	return strings.Join(parts, ", ")
}

// renderMigrate reports every repair the legacy phase made and every change
// the versioned chain made, which §5.3/§5.3.4 require in as many words, and
// says so plainly when there was nothing to do at all: a directory that is
// already current is the answer to "is this old?", not a silence.
//
// blocked carries the version chain's own reason for not proceeding, when it
// has real content to report but could not (an unrelated, pre-existing
// problem the legacy repair does not understand) - reported, not swallowed,
// but not a command failure either: see runMigrate.
//
// The warnings — a tags value it recognized and could not convert, an
// uncommitted git tree — go to stderr and survive --quiet, for the same
// reason --archive's do: they name something still wrong or still worth
// knowing that the run did not itself fix.
func renderMigrate(env Env, in *Invocation, res mm.MigrateResult, steps []mm.MigrationResult, blocked string) {
	if !in.Quiet {
		did := false
		if len(res.Changes) > 0 {
			did = true
			out(env, "%smigrated %d thing(s)\n", prefix(in), len(res.Changes))
			for _, c := range res.Changes {
				switch c.Kind {
				case mm.MigrateRenamed:
					out(env, "  renamed %s -> %s\n", c.File, c.After)
				case mm.MigrateProject:
					out(env, "  %s: project: %s\n", c.File, c.After)
				default:
					out(env, "  %s:%d: %s\n", c.File, c.Line, c.After)
				}
			}
		}
		for _, st := range steps {
			did = true
			out(env, "%smigrated version %d -> %d (%d thing(s))\n",
				prefix(in), st.From, st.To, len(st.Changes))
			for _, c := range st.Changes {
				out(env, "  %s\n", stepChangeLine(c))
			}
		}
		if !did && blocked == "" {
			out(env, "%snothing to migrate: the directory is already current\n", prefix(in))
		}
	}
	for _, w := range res.Warnings {
		fmt.Fprintf(env.Stderr, "mm: warning: %s\n", w)
	}
	for _, st := range steps {
		for _, w := range st.Warnings {
			fmt.Fprintf(env.Stderr, "mm: warning: %s\n", w)
		}
	}
	if blocked != "" {
		fmt.Fprintf(env.Stderr, "mm: warning: version not migrated: %s\n", blocked)
	}
}

// stepChangeLine renders one version-chain Change. Unlike an ordinary
// operation's change set, several of these carry no item id at all — the
// board.md rename, done.md's version bump, a working file going away — so
// each Kind gets its own wording rather than one generic "id: after" line.
func stepChangeLine(c mm.Change) string {
	switch {
	case c.Kind == mm.ChangeMoved && c.ID == "" && c.Before != "" && c.After != "":
		return fmt.Sprintf("renamed %s -> %s", c.Before, c.After)
	case c.Kind == mm.ChangeMoved && c.ID != "":
		return fmt.Sprintf("%s -> %s", c.ID, c.After)
	case c.Kind == mm.ChangeCreated && c.ID != "":
		return fmt.Sprintf("%s: created %s", c.ID, c.File)
	case c.Kind == mm.ChangeUpdated && c.ID != "":
		return fmt.Sprintf("%s: %s", c.ID, c.File)
	case c.Kind == mm.ChangeUpdated:
		return fmt.Sprintf("updated %s", c.File)
	case c.Kind == mm.ChangeDeleted:
		return fmt.Sprintf("deleted %s", c.File)
	}
	return fmt.Sprintf("%s %s", c.Kind, c.File)
}

// renderFix reports each renumbering and the next_id the repair wrote. A
// directory with nothing to repair says so: an empty fix is a success worth
// stating, because a scripted merge flow gates on it.
func renderFix(env Env, in *Invocation, res mm.FixResult, tx mm.TxResult) {
	if in.Quiet {
		return
	}
	if len(res.Changes) == 0 {
		if !res.NextBumped {
			out(env, "%snothing to fix: no duplicate IDs\n", prefix(in))
			return
		}
		out(env, "%snext_id %s -> %s\n", prefix(in), res.WasNext, res.NextID)
		return
	}
	out(env, "%sfixed %d duplicate ID(s)\n", prefix(in), len(res.Changes))
	for _, c := range res.Changes {
		out(env, "%s  %s -> %s in %s", prefix(in), c.OldID, c.NewID, c.File)
		if c.Detail != "" {
			out(env, " (detail %s moved)", c.Detail)
		}
		out(env, "\n")
	}
	if res.NextBumped {
		out(env, "%snext_id %s -> %s\n", prefix(in), res.WasNext, res.NextID)
	}
	for _, f := range tx.Files {
		if f != "backlog.md" {
			out(env, "  %s\n", f)
		}
	}
}

func renderAdd(env Env, in *Invocation, item mm.Item, res mm.TxResult) {
	// The ID must be reported in every output mode (§5.1.2). Even --quiet keeps
	// it: quiet suppresses commentary, not the one value the caller needs.
	if in.Quiet {
		out(env, "%s\n", item.ID)
		return
	}
	out(env, "%s%s added to %s", prefix(in), item.ID, item.Section)
	if item.Pos > 0 {
		out(env, " (position %d)", item.Pos)
	}
	out(env, "\n  %s\n", mm.RenderItemLine(&item))
	for _, c := range res.Changes {
		if c.Kind == mm.ChangeCreated && c.File != "backlog.md" {
			out(env, "  created %s\n", c.File)
		}
	}
}

// renderAddMany reports a batch (§5.2.1).
//
// Every assigned ID is printed, in every output mode — the same rule --add
// follows for one item, and twelve items mean twelve handles the caller needs.
// Under --quiet that is all that is printed: quiet suppresses commentary, not
// the values the caller came for.
func renderAddMany(env Env, in *Invocation, items []mm.Item) {
	if in.Quiet {
		for _, it := range items {
			out(env, "%s\n", it.ID)
		}
		return
	}
	// The count leads, because the one thing a bulk add has to answer is "how
	// many did that add?" — and the sections, because a batch can land in more
	// than one when a line carried its own blocked: reason.
	out(env, "%s%d %s added to %s\n", prefix(in), len(items),
		plural(len(items), "item", "items"), sectionsOf(items))
	for _, it := range items {
		out(env, "  %s\n", mm.RenderItemLine(&it))
	}
}

// sectionsOf names the sections a batch landed in, in the order they were first
// written to.
func sectionsOf(items []mm.Item) string {
	var names []string
	seen := map[mm.Section]bool{}
	for _, it := range items {
		if !seen[it.Section] {
			seen[it.Section] = true
			names = append(names, string(it.Section))
		}
	}
	return strings.Join(names, " and ")
}

func renderChange(env Env, in *Invocation, verb string, item mm.Item, res mm.TxResult) {
	if in.Quiet {
		return
	}
	if len(res.Files) == 0 {
		// A no-op writes nothing, and saying so is more useful than a success
		// message that implies something happened.
		out(env, "%s is already %s; nothing changed\n", item.ID, verb)
		return
	}
	out(env, "%s%s %s\n  %s\n", prefix(in), item.ID, verb, mm.RenderItemLine(&item))
}

func renderStart(env Env, in *Invocation, item mm.Item, res mm.TxResult) {
	if in.Quiet {
		return
	}
	if item.State == mm.StateBoard {
		// No slot: version 2 has no working.NN.md files at all, and the
		// rendered line already shows stage:working.
		out(env, "%s%s started\n  %s\n", prefix(in), item.ID, mm.RenderItemLine(&item))
	} else {
		out(env, "%s%s started in slot %d\n  %s\n",
			prefix(in), item.ID, item.Slot, mm.RenderItemLine(&item))
	}
	_ = res
}

func renderRemove(env Env, in *Invocation, out2 mm.Removal, res mm.TxResult) {
	if !in.Quiet {
		out(env, "%s%s removed\n  %s\n", prefix(in), out2.Item.ID,
			mm.RenderItemLine(&out2.Item))
		if out2.DetailDeleted != "" {
			out(env, "  deleted %s\n", out2.DetailDeleted)
		}
	}
	// The orphan is reported even under --quiet: the directory is now invalid
	// and the user has to know (§5.1.6).
	if out2.DetailOrphan != "" {
		fmt.Fprintf(env.Stderr,
			"mm: %s was left behind with no item; the directory now fails I9. "+
				"Delete it, or attach it to another item.\n", out2.DetailOrphan)
	}
	_ = res
}

func renderNote(env Env, in *Invocation, item mm.Item, res mm.TxResult) {
	if in.Quiet {
		return
	}
	where := "its detail file"
	if item.State == mm.StateWorking {
		where = fmt.Sprintf("slot %d", item.Slot)
	}
	out(env, "%snote added to %s for %s\n", prefix(in), where, item.ID)
	for _, f := range res.Files {
		out(env, "  %s\n", f)
	}
}

func renderWip(env Env, in *Invocation, dir mm.Directory, res mm.TxResult) {
	if in.Quiet {
		return
	}
	if len(res.Files) == 0 {
		out(env, "wip limit is already %d\n", dir.WipLimit)
		return
	}
	out(env, "%swip limit is now %d\n", prefix(in), dir.WipLimit)
	for _, c := range res.Changes {
		out(env, "  %s %s\n", c.Kind, c.File)
	}
}

// renderStageWip is renderWip's version-2 form: one stage's own cap
// (spec-file-format.md §5.1.3), not the directory's single working-file
// count.
func renderStageWip(env Env, in *Invocation, stage mm.Stage, dir mm.Directory, res mm.TxResult) {
	if in.Quiet {
		return
	}
	limit, capped := dir.StageCfg.WipLimits[stage]
	if len(res.Files) == 0 {
		if capped {
			out(env, "wip limit for %s is already %d\n", stage, limit)
		} else {
			out(env, "%s is already uncapped\n", stage)
		}
		return
	}
	if capped {
		out(env, "%swip limit for %s is now %d\n", prefix(in), stage, limit)
	} else {
		out(env, "%s%s is now uncapped\n", prefix(in), stage)
	}
}

func renderList(env Env, in *Invocation, dir mm.Directory, items []mm.Item) {
	if len(items) == 0 {
		if !in.Quiet {
			out(env, "no items\n")
		}
		return
	}
	if !in.Quiet {
		out(env, "# %s\n\n", directoryName(dir))
	}
	// On-disk order, never re-sorted: ## Ready order is the user's own
	// prioritisation and a listing that reorders it hides the one thing the
	// section is for.
	section := mm.Section("")
	for _, item := range items {
		if !in.Quiet && item.State == mm.StateBacklog && item.Section != section {
			if section != "" {
				out(env, "\n")
			}
			section = item.Section
			out(env, "## %s\n\n", section)
		}
		out(env, "%s\n", listLine(item, in.Verbose))
	}
}

// listLine renders one item. The item line itself is the verbose form, because
// it is exactly what is in the file and is what a person would paste.
func listLine(item mm.Item, verbose bool) string {
	if verbose {
		return mm.RenderItemLine(&item)
	}
	var b strings.Builder
	box := " "
	if item.Closed() {
		box = "x"
	}
	fmt.Fprintf(&b, "- [%s] %s %s", box, item.ID, item.Title)
	if item.State == mm.StateWorking {
		fmt.Fprintf(&b, "  (slot %d)", item.Slot)
	}
	if item.Prio == mm.PrioHigh {
		b.WriteString("  !")
	}
	if item.Blocked != "" {
		fmt.Fprintf(&b, "  [blocked: %s]", item.Blocked)
	}
	return b.String()
}

func renderShow(env Env, item mm.Item, detail *mm.Detail) {
	out(env, "%s  %s\n", item.ID, item.Title)
	out(env, "  state     %s", item.State)
	switch item.State {
	case mm.StateBacklog:
		out(env, " (%s, position %d)", item.Section, item.Pos)
	case mm.StateWorking:
		out(env, " (slot %d)", item.Slot)
	}
	out(env, "\n  file      %s\n", item.Source)

	show := func(label, value string) {
		if value != "" {
			out(env, "  %-9s %s\n", label, value)
		}
	}
	show("prio", string(item.Prio))
	show("tags", mm.FormatTags(item.Tags))
	show("detail", item.Detail)
	show("created", mm.FormatDateOrStamp(item.Created, item.CreatedTime))
	show("started", mm.FormatDateOrStamp(item.Started, item.StartedTime))
	show("tickler", item.Tickler)
	show("tickled", item.Tickled.String())
	show("done", item.Done.String())
	show("outcome", string(item.Outcome))
	show("blocked", item.Blocked)
	for _, f := range item.Extra {
		show(f.Key, f.Value)
	}

	if detail != nil {
		out(env, "\n%s\n", strings.TrimRight(detail.Body, "\n"))
	}
}

func renderReport(env Env, rep mm.Report) {
	// The markdown itself is the library's, because spec-gui.md §5.7 requires
	// the GUI to put THE SAME text on the clipboard. Warnings stay here: they go
	// to stderr, so they never contaminate what someone pastes.
	out(env, "%s", rep.Markdown())
	for _, w := range rep.Warnings {
		fmt.Fprintf(env.Stderr, "mm: warning: %s\n", w)
	}
}

func renderFind(env Env, res mm.DiscoveryResult) {
	if len(res.Directories) == 0 {
		out(env, "no micro-manager directories found\n")
		return
	}
	for _, d := range res.Directories {
		out(env, "%s  %s  (wip %d/%d)\n",
			dirLabel(env.Cwd, d.Path), directoryName(d), d.WipUsed, d.WipLimit)
	}
	if res.Partial {
		fmt.Fprintf(env.Stderr,
			"mm: warning: the scan hit a limit, so this list may be incomplete\n")
	}
	if res.Skipped > 0 {
		fmt.Fprintf(env.Stderr, "mm: %d unreadable director%s skipped\n",
			res.Skipped, plural(res.Skipped, "y", "ies"))
	}
	// Two boards in one parent (spec-file-format.md Appendix B). BOTH are
	// listed above — hiding one would pick a winner, which is the thing the
	// format never does with ambiguity — and the warning is what stops the
	// list reading as two unrelated projects that happen to be adjacent.
	for _, parent := range collidingParents(res.Directories) {
		fmt.Fprintf(env.Stderr,
			"mm: warning: %s holds two micro-manager directories; run --check there\n",
			dirLabel(env.Cwd, parent))
	}
}

// collidingParents returns the parents that hold more than one discovered
// board, in the order they first appear, so the warning is per situation rather
// than per directory.
func collidingParents(dirs []mm.Directory) []string {
	seen := map[string]int{}
	var order []string
	for _, d := range dirs {
		parent := filepath.Dir(d.Path)
		seen[parent]++
		if seen[parent] == 2 {
			order = append(order, parent)
		}
	}
	return order
}

// renderStatus is the --status screen (spec-tools.md §5.2).
//
// One screen: the project name and WIP n/N, what is in each slot, counts by
// section, the top of ## Ready, and the oldest untouched item. All of it comes
// from ONE Status call, so the screen shows one moment in time.
func renderStatus(env Env, in *Invocation, st mm.Status) {
	if in.Quiet {
		return
	}
	dir := st.Directory

	if dir.Version == 2 {
		if wip := stageWipSummary(dir); wip != "" {
			out(env, "# %s  (wip %s)\n\n", directoryName(dir), wip)
		} else {
			out(env, "# %s\n\n", directoryName(dir))
		}
		// No slot listing: version 2 has no working.NN.md files at all, and
		// StageCounts already shows where every item sits.
		for _, stage := range dir.StageCfg.Stages {
			out(env, "%-10s %d\n", dir.StageCfg.Label(stage)+":", st.StageCounts[stage])
		}
		out(env, "%-10s %d\n\n", "done:", st.Done)
	} else {
		out(env, "# %s  (wip %d/%d)\n\n", directoryName(dir), st.WipUsed(), st.WipLimit())

		for _, slot := range dir.Slots {
			if slot.Occupied() {
				out(env, "%s: %s\n", slot.File, mm.RenderItemLine(slot.Item))
			} else {
				out(env, "%s: idle\n", slot.File)
			}
		}
		out(env, "\n")

		out(env, "%-10s %d\n", "ready:", st.Ready)
		out(env, "%-10s %d\n", "blocked:", st.Blocked)
		out(env, "%-10s %d\n", "someday:", st.Someday)
		out(env, "%-10s %d\n\n", "done:", st.Done)
	}

	if st.Next != nil {
		out(env, "next:   %s\n", shortItem(st.Next))
	}
	if st.OldestReady != nil {
		out(env, "oldest: %s\n", shortItem(st.OldestReady))
	}
}

// stageWipSummary lists every WIP-capped stage as "slug U/L", in stages:
// order, or "" when nothing is capped. Version 2 has no single directory-wide
// limit to headline the way version 1's slot count is; several stages may
// each carry their own.
func stageWipSummary(dir mm.Directory) string {
	var parts []string
	for _, stage := range dir.StageCfg.Stages {
		limit, capped := dir.StageCfg.WipLimits[stage]
		if !capped {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %d/%d", stage, dir.StageUsed[stage], limit))
	}
	return strings.Join(parts, ", ")
}

// shortItem is the one-line form --status uses for its two featured items.
func shortItem(it *mm.Item) string {
	s := string(it.ID) + "  " + it.Title
	if it.Created.IsZero() {
		return s
	}
	return s + "  (created " + mm.FormatDateOrStamp(it.Created, it.CreatedTime) + ")"
}

// renderNext prints the top of ## Ready exactly as --list would.
func renderNext(env Env, in *Invocation, item mm.Item) {
	if in.Quiet {
		return
	}
	out(env, "%s\n", listLine(item, in.Verbose))
}

// renderSearch lists hits, one per line: state, item, field, and the exact
// file and line the match is on — a detail hit points INTO the detail file,
// which is what makes it navigable.
func renderSearch(env Env, in *Invocation, hits []mm.SearchHit) {
	if len(hits) == 0 {
		if !in.Quiet {
			out(env, "no matches\n")
		}
		return
	}
	for _, h := range hits {
		state := string(h.Item.State)
		if h.Item.State == mm.StateBacklog {
			state += "/" + string(h.Item.Section)
		}
		out(env, "%-12s %-7s %-7s %-24s %s\n",
			state, h.Item.ID, h.Field, h.At, h.Text)
	}
}

// renderTick reports what a tick run fired and what it could not fire
// (spec-tools.md §5.3.3). Every fire names its kind, because "fired" is two
// different mutations: a one-shot moves, a recurring schedule spawns, and
// which one happened is the first thing a cron wants to know. The dry-run
// prefix marks the whole report, so reading the output IS how you preview a
// run.
//
// A run with nothing due says so in as many words — a scheduled cron's silence
// would otherwise be indistinguishable from the process never running.
// Per-item errors go to stderr and survive --quiet, like --archive's warnings:
// they are the "what errored" half of the required report, not commentary.
func renderTick(env Env, in *Invocation, res mm.TickResult) {
	if len(res.Fired) == 0 {
		if !in.Quiet && len(res.Errors) == 0 {
			out(env, "%snothing due; no someday item has a schedule to fire\n", prefix(in))
		}
	} else {
		for _, f := range res.Fired {
			if f.Kind == mm.FireMove {
				out(env, "%s%s: moved to Ready (tickled %s)\n",
					prefix(in), f.ID, f.Tickled)
			} else {
				out(env, "%s%s: spawned %s into Ready (tickled %s)\n",
					prefix(in), f.ID, f.Spawned, f.Tickled)
			}
		}
	}
	for _, e := range res.Errors {
		fmt.Fprintf(env.Stderr, "mm: %s did not fire: %v\n", e.ID, e.Error)
	}
}
