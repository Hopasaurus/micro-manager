package cli

import (
	"fmt"
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
	out(env, "%s%s started in slot %d\n  %s\n",
		prefix(in), item.ID, item.Slot, mm.RenderItemLine(&item))
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
	show("created", item.Created.String())
	show("started", item.Started.String())
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

	if st.Next != nil {
		out(env, "next:   %s\n", shortItem(st.Next))
	}
	if st.OldestReady != nil {
		out(env, "oldest: %s\n", shortItem(st.OldestReady))
	}
}

// shortItem is the one-line form --status uses for its two featured items.
func shortItem(it *mm.Item) string {
	s := string(it.ID) + "  " + it.Title
	if it.Created.IsZero() {
		return s
	}
	return s + "  (created " + it.Created.String() + ")"
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
