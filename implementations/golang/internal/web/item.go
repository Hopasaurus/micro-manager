package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The item panel and the mutating operations (spec-gui.md §5.6, §6.1).
//
// The panel renders OVER the board and the board stays in the DOM (§5.6), which
// is why every response here is a fragment the client swaps rather than a page
// that replaces what was there.

// itemPageData is what /p/:id/item/:itemId and /p/:id/new render: the board,
// with the panel over it. §5.6 requires the board to remain in the DOM, so a
// direct load of a panel URI has to produce both.
type itemPageData struct {
	Board boardData
	Panel panelData
}

// panelData is the item panel view model.
type panelData struct {
	Item    itemData
	Detail  string
	Notes   []noteLine
	Plan    []subtask
	Created string
	Started string
	Done    string

	// New marks the add-item panel, which is the same position and the same
	// form with nothing filled in (§4.1, /p/:id/new).
	New     bool
	Section string

	// Tickler pre-fills the Wake-up group (§5.6): the controls parsed back out
	// of the item's schedule, or kind never with everything empty for an
	// unscheduled someday item and for the new-item form.
	Tickler ticklerGroupData
}

type noteLine struct {
	Date string
	Text string
}

type subtask struct {
	N    int
	Done bool
	Text string
}

// itemID parses the :itemId route parameter against the directory's declared
// ID grammar (spec-file-format.md §3.3.2). The default grammar would reject
// X-003 in a directory declaring id_prefix: X / id_width: 3; an ID is
// interpreted in the grammar of the directory it names, never a fixed shape.
// The ID is case-sensitive and verbatim (§3.2).
func (s *Server) itemID(c *echo.Context, store *mm.Store) (mm.ID, error) {
	return parseID(c.Param("itemId"), store)
}

// parseID parses s as an ID in the directory's declared grammar. Every ID
// argument a front end accepts comes through here — the route parameter and
// the dialog's ?item= — so a directory declaring id_prefix: X / id_width: 3
// is addressed by X-003 everywhere, and by nothing else.
func parseID(s string, store *mm.Store) (mm.ID, error) {
	g, err := store.Grammar()
	if err != nil {
		return "", err
	}
	return g.ParseID(s)
}

// itemPanel serves /p/:projectId/item/:itemId.
func (s *Server) itemPanel(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}
	it, err := store.Get(id)
	if err != nil {
		return err
	}

	v, err := s.panelView(c, store, it)
	if err != nil {
		return err
	}
	if wantsFragment(c.Request()) {
		// The card click swaps the panel into #item-panel-root: render the
		// panel alone, rebound to .Data.Panel exactly as content does.
		return s.renderFragmentAlways(c, http.StatusOK, "item", "item-panel-fragment", v)
	}
	return s.render(c, http.StatusOK, "item", "item-panel", v)
}

// withBoard puts the board behind a panel, for a direct load of a panel URI.
func (s *Server) withBoard(c *echo.Context, store *mm.Store, v view, panel panelData) (view, error) {
	board, err := s.buildBoard(c, store)
	if err != nil {
		return v, err
	}
	v.Data = itemPageData{Board: board, Panel: panel}
	return v, nil
}

// newItemPanel serves /p/:projectId/new: the same panel in the same position,
// with an empty form.
func (s *Server) newItemPanel(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "New item", store)
	v.App.Nav = "board"
	section := c.Request().URL.Query().Get("section")
	if _, ok := parseSection(section); !ok {
		section = sectionKey(mm.SectionReady)
	}
	v, err = s.withBoard(c, store, v, panelData{
		New: true, Section: section,
		Item:    itemData{Prio: "med"},
		Tickler: ticklerGroupData{Kind: "never"},
	})
	if err != nil {
		return err
	}
	if wantsFragment(c.Request()) {
		return s.renderFragmentAlways(c, http.StatusOK, "item", "item-panel-fragment", v)
	}
	return s.render(c, http.StatusOK, "item", "item-panel", v)
}

// panelView builds the panel for one item.
func (s *Server) panelView(c *echo.Context, store *mm.Store, it mm.Item) (view, error) {
	dir, err := store.Directory()
	if err != nil {
		return view{}, err
	}

	v := s.newView(c, it.Title, store)
	v.App.Nav = "board"

	data := panelData{
		Item:    s.itemView(it, dir, 1, s.registry.newRefResolver()),
		Created: it.Created.String(),
		Started: it.Started.String(),
		Done:    it.Done.String(),
	}
	// The Wake-up group's pre-fill: a someday item's schedule, when it has one
	// (§5.6). Everything else renders kind never with empty controls — an
	// unscheduled someday item can gain a tickler here, and no other state has
	// a schedule to edit.
	if it.State == mm.StateBacklog && it.Section == mm.SectionSomeday {
		data.Tickler = prefillTickler(it)
	}

	// The long-form description, when the item has one.
	if it.Detail != "" {
		if d, err := store.Detail(it.ID); err == nil {
			data.Detail = d.Body
		}
	}

	// For an item in a slot, item-plan renders subtasks as subtask-<n>
	// checkboxes and item-notes renders the dated log (§5.6).
	if it.State == mm.StateWorking {
		data.Plan, data.Notes = s.slotBody(store, it)
	}
	return s.withBoard(c, store, v, data)
}

// slotBody reads the working file's ## Plan and ## Notes sections.
func (s *Server) slotBody(store *mm.Store, it mm.Item) ([]subtask, []noteLine) {
	dir, err := store.Directory()
	if err != nil {
		return nil, nil
	}
	var file string
	for _, slot := range dir.Slots {
		if slot.Item != nil && slot.Item.ID == it.ID {
			file = slot.File
		}
	}
	if file == "" {
		return nil, nil
	}

	plan, notes := parseSlotSections(store, file)
	return plan, notes
}

// parseSlotSections reads ## Plan and ## Notes out of a working file.
//
// The library exposes an item's fields but not its slot body: the body is the
// human half of the file (spec-file-format.md §6) and no operation reads it.
// The panel does, so the parsing is here rather than pushed into the library
// for one caller.
func parseSlotSections(store *mm.Store, file string) ([]subtask, []noteLine) {
	data, err := os.ReadFile(filepath.Join(store.Path(), file))
	if err != nil {
		return nil, nil
	}

	var planLines, noteLines []string
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case "## Plan":
			section = "plan"
			continue
		case "## Notes":
			section = "notes"
			continue
		case "## Task", "## Blockers":
			section = ""
			continue
		}
		switch section {
		case "plan":
			planLines = append(planLines, line)
		case "notes":
			noteLines = append(noteLines, line)
		}
	}

	var plan []subtask
	for i, line := range planLines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- [") {
			continue
		}
		plan = append(plan, subtask{
			N:    i + 1,
			Done: strings.HasPrefix(trimmed, "- [x]"),
			Text: strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(trimmed, "- [x]"), "- [ ]")),
		})
	}

	var notes []noteLine
	for _, line := range noteLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		date, text := splitDatedNote(trimmed)
		notes = append(notes, noteLine{Date: date, Text: text})
	}
	return plan, notes
}

// splitDatedNote pulls the leading date off a note line. Notes are dated by
// convention rather than by grammar, so a line without one is not an error.
func splitDatedNote(line string) (date, text string) {
	trimmed := strings.TrimPrefix(line, "- ")
	if len(trimmed) >= 10 {
		if _, err := mm.ParseDate(trimmed[:10]); err == nil {
			return trimmed[:10], strings.TrimSpace(strings.TrimPrefix(trimmed[10:], ":"))
		}
	}
	return "", trimmed
}

// ---------------------------------------------------------------------------
// Mutations (§6.1)
// ---------------------------------------------------------------------------

// mutation is one operation's result, rendered as the swap the client needs.
type mutationResult struct {
	Item    *mm.Item
	Changes []mm.Change
	Message string

	// Severity overrides the toast's default "success". A mutation that
	// succeeded but left something the user has to know about - a removed item
	// whose detail file is now an orphan - is not a plain success.
	Severity string
}

// operate runs one mutating operation and re-renders the board from what the
// library returned.
//
// Every operation re-renders from the RETURN VALUE rather than re-reading:
// spec-tools.md §6.2 requires a mutating operation to return the resulting state
// precisely so a UI does not have to.
func (s *Server) operate(c *echo.Context, op string) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}

	today := mm.NewTimestamp(s.registry.now())
	date, err := mm.ParseDate(today.String()[:10])
	if err != nil {
		return err
	}

	dryRun := c.Request().URL.Query().Get("dryRun") == "true" ||
		strings.EqualFold(c.Request().FormValue("dryRun"), "true")

	var result mutationResult
	switch op {
	case "start":
		slot := 0
		if v := c.Request().FormValue("slot"); v != "" {
			fmt.Sscanf(v, "%d", &slot)
		}
		it, _, err := store.Start(id, mm.StartRequest{Slot: slot, DryRun: dryRun}, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " started"}

	case "pause":
		// A drag from a slot into a backlog column names which column it landed
		// in (§7.2); the item menu's Pause names none and takes the default.
		req := mm.PauseRequest{DryRun: dryRun}
		if v := c.Request().FormValue("section"); v != "" {
			section, ok := parseSection(v)
			if !ok {
				return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, v)
			}
			req.Section = section
			req.Blocked = c.Request().FormValue("reason")
		}
		if v := c.Request().FormValue("stage"); v != "" {
			// Version 2: unlike version 1's Blocked, pauseV2 does not accept
			// a NEW reason at pause time - it only checks whether the item
			// already carries one for a needs_reason destination.
			req.Stage = mm.Stage(v)
		}
		it, _, err := store.Pause(id, req, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " paused"}

	case "finish":
		outcome, err := mm.ParseOutcome(formOr(c, "outcome", "shipped"))
		if err != nil {
			return err
		}
		it, _, err := store.Finish(id, mm.FinishRequest{
			Outcome: outcome,
			Note:    c.Request().FormValue("note"),
			DryRun:  dryRun,
		}, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " finished"}

	case "block":
		// Sugar over --move --stage blocked (spec-gui.md §6.1). Both the
		// version-1 and version-2 fields are set unconditionally: Store.Move
		// dispatches on the directory's actual version and reads only the
		// pair that applies, so one call is correct for either.
		reason := c.Request().FormValue("reason")
		if strings.TrimSpace(reason) == "" {
			// §7.2: blocking MUST prompt for a reason, and cancelling aborts.
			// The library requires one too; refusing here names the field.
			return fmt.Errorf("%w: blocking %s needs a reason", mm.ErrInvalidArgument, id)
		}
		it, _, err := store.Move(id, mm.MoveRequest{
			Section: mm.SectionBlocked, Blocked: reason,
			Stage: "blocked", Reason: reason,
			DryRun: dryRun,
		}, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " blocked"}

	case "unblock":
		// Sugar over --move --stage ready. Unlike version 1's Blocked field,
		// version 2's reason: is not dropped by leaving a needs_reason stage
		// (research decision 18), so there is nothing to clear here.
		it, _, err := store.Move(id, mm.MoveRequest{
			Section: mm.SectionReady, Stage: "ready", Top: true,
			DryRun: dryRun,
		}, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " unblocked"}

	case "move":
		req := mm.MoveRequest{DryRun: dryRun}
		if v := c.Request().FormValue("section"); v != "" {
			section, ok := parseSection(v)
			if !ok {
				return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, v)
			}
			req.Section = section
			req.Blocked = c.Request().FormValue("reason")
		}
		if v := c.Request().FormValue("stage"); v != "" {
			req.Stage = mm.Stage(v)
			req.Reason = c.Request().FormValue("reason")
		}
		if v := c.Request().FormValue("position"); v != "" {
			var n int
			fmt.Sscanf(v, "%d", &n)
			req.Position = n
		}
		it, _, err := store.Move(id, req, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: string(id) + " moved"}

	case "move-top", "move-end":
		// T-0147: the menu's "Move to top"/"Move to bottom" are --move
		// --top/--end (spec-tools.md §5.1.7) without a section: reordering
		// never changes section, so the destination is the item's own.
		req := mm.MoveRequest{DryRun: dryRun}
		if op == "move-top" {
			req.Top = true
		} else {
			req.End = true
		}
		it, _, err := store.Move(id, req, date)
		if err != nil {
			return err
		}
		where := "top"
		if op == "move-end" {
			where = "bottom"
		}
		result = mutationResult{Item: &it, Message: string(id) + " moved to " + where}

	case "note":
		text := c.Request().FormValue("text")
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("%w: a note needs some text", mm.ErrInvalidArgument)
		}
		it, _, err := store.Note(id, mm.NoteRequest{Text: text, DryRun: dryRun}, date)
		if err != nil {
			return err
		}
		result = mutationResult{Item: &it, Message: "noted on " + string(id)}

	default:
		return fmt.Errorf("%w: unknown operation %q", mm.ErrInvalidArgument, op)
	}

	return s.afterMutation(c, store, result, dryRun)
}

// editItem is PATCH-shaped: the item form's save (§5.6, --edit).
func (s *Server) editItem(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}

	req := mm.UpdateRequest{DryRun: c.Request().FormValue("dryRun") == "true"}
	// The pointers distinguish "absent" from "present and empty", which is what
	// lets the form clear a field rather than leave it alone.
	if v, ok := formValue(c, "title"); ok {
		req.Title = &v
	}
	if v, ok := formValue(c, "prio"); ok && v != "" {
		prio, err := mm.ParsePrio(v)
		if err != nil {
			return err
		}
		req.Prio = &prio
	}
	if v, ok := formValue(c, "tags"); ok {
		tags, err := mm.ParseTags(v)
		if err != nil {
			return err
		}
		req.Tags = tags
		req.SetTags = true
	}
	if v, ok := formValue(c, "blocked"); ok {
		req.Blocked = &v
	}

	// §4.2: the Wake-up group's controls compose the tickler: value; present
	// but empty (kind never) removes an existing one, absent leaves it alone —
	// a non-someday item's form has no group on it.
	schedule, present, err := composeTickler(c)
	if err != nil {
		return err
	}
	applyTicklerUpdate(&req, schedule, present)

	today, err := mm.ParseDate(mm.NewTimestamp(s.registry.now()).String()[:10])
	if err != nil {
		return err
	}
	it, _, err := store.Update(id, req, today)
	if err != nil {
		return err
	}

	// The detail body is a separate file and a separate write (§4.2).
	if body, ok := formValue(c, "detail"); ok && body != "" {
		if it.Detail == "" {
			if _, _, err := store.AttachDetail(it.ID, mm.AttachDetailRequest{Body: body}, today); err != nil {
				return err
			}
		} else if _, err := store.SetDetailBody(it.ID, body, false, today); err != nil {
			return err
		}
	}

	return s.afterMutation(c, store, mutationResult{Item: &it, Message: string(id) + " saved"}, req.DryRun)
}

// addItem is the add-item panel's save (§5.6, --add).
func (s *Server) addItem(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	title := strings.TrimSpace(c.Request().FormValue("title"))
	if title == "" {
		return fmt.Errorf("%w: an item needs a title", mm.ErrInvalidArgument)
	}

	req := mm.AddRequest{Title: title}
	if v := c.Request().FormValue("prio"); v != "" {
		prio, err := mm.ParsePrio(v)
		if err != nil {
			return err
		}
		req.Prio = prio
	}
	if v := c.Request().FormValue("tags"); v != "" {
		tags, err := mm.ParseTags(v)
		if err != nil {
			return err
		}
		req.Tags = tags
	}
	if v := c.Request().FormValue("section"); v != "" {
		section, ok := parseSection(v)
		if !ok {
			return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, v)
		}
		req.Section = section
	}
	req.Blocked = c.Request().FormValue("blocked")
	req.DetailBody = c.Request().FormValue("detail")
	req.DryRun = c.Request().FormValue("dryRun") == "true"

	// §4.2: the Wake-up group's controls compose the tickler: value server-side;
	// the library validates it (and I7's Someday-only placement) on write.
	schedule, present, err := composeTickler(c)
	if err != nil {
		return err
	}
	applyTicklerAdd(&req, schedule, present)

	today, err := mm.ParseDate(mm.NewTimestamp(s.registry.now()).String()[:10])
	if err != nil {
		return err
	}
	it, _, err := store.Add(req, today)
	if err != nil {
		return err
	}
	if c.Request().FormValue("addAnother") == "1" {
		return s.afterMutationAddAnother(c, store, it, req.Section, req.DryRun)
	}
	return s.afterMutation(c, store, mutationResult{Item: &it, Message: string(it.ID) + " added"}, req.DryRun)
}

// removeItem is DELETE, behind the force guard (§5.6, §4.2).
//
// item-action-remove carries data-guarded="true" and opens dialog-confirm-remove;
// this route additionally REQUIRES force=true, so the guard is not something the
// client can forget its way past.
//
// The item's detail file is the other half of the operation. spec-tools.md
// §5.1.6: an implementation MUST either delete it in the same transaction or
// report the orphan it left behind — "silently leaving an invalid directory is
// not conforming". This route did neither: it discarded the Removal, so
// deleting an item with a detail file left the project failing I9 with nothing
// on screen to say so. withDetail chooses; both answers are now spoken aloud.
func (s *Server) removeItem(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}

	force := c.Request().URL.Query().Get("force") == "true" ||
		strings.EqualFold(c.Request().FormValue("force"), "true")
	if !force {
		return fmt.Errorf("%w: removing %s needs force=true", mm.ErrPreconditionFailed, id)
	}

	today, err := mm.ParseDate(mm.NewTimestamp(s.registry.now()).String()[:10])
	if err != nil {
		return err
	}
	dryRun := c.Request().FormValue("dryRun") == "true"
	withDetail := c.Request().URL.Query().Get("withDetail") == "true" ||
		strings.EqualFold(c.Request().FormValue("withDetail"), "true")

	removal, _, err := store.Remove(id, mm.RemoveRequest{
		Force:      true,
		WithDetail: withDetail,
		DryRun:     dryRun,
	}, today)
	if err != nil {
		return err
	}

	result := mutationResult{Message: string(id) + " removed"}
	switch {
	case removal.DetailDeleted != "":
		result.Message += " with " + removal.DetailDeleted
	case removal.DetailOrphan != "":
		// The write went through, so this is not an error - but the directory
		// now fails I9, and the only place the user can learn that is here.
		result.Message += "; " + removal.DetailOrphan + " is now an orphan"
		result.Severity = "warning"
	}
	return s.afterMutation(c, store, result, dryRun)
}

// afterMutation renders what the change invalidated.
//
// A mutation invalidates more than the fragment it returns: column counts, the
// WIP indicator, the status bar and the check count are all stale afterwards, and
// every card whose data-position moved has to be re-rendered. The whole board
// plus the status bar is the honest answer, and on a local tool over loopback it
// is also the cheap one.
func (s *Server) afterMutation(c *echo.Context, store *mm.Store, result mutationResult, dryRun bool) error {
	v := s.newView(c, "Board", store)
	v.App.Nav = "board"

	data, err := s.buildBoard(c, store)
	if err != nil {
		return err
	}
	v.Data = data
	severity := result.Severity
	if severity == "" {
		severity = "success"
	}
	v.App.Toast = &toastData{
		Message:  result.Message,
		Severity: severity,
		DryRun:   dryRun,
	}

	// The mutation dismissed whatever overlay opened it — the item panel, the
	// dialogs — and swapped in the board. The address bar must follow: after
	// saving an edit the URL is still /p/:id/item/:itemId, and loading that
	// URI again would re-open the panel over the new state (spec-gui.md §4.1
	// rule 1: the URI names the view). HX-Replace-Url rather than HX-Push-Url:
	// the panel URL is a transient editing state, not a destination, and a
	// mutation launched from the board itself must not stack a duplicate
	// history entry per action.
	if v.App.Project != nil {
		c.Response().Header().Set("HX-Replace-Url", "/p/"+v.App.Project.ID+"/board")
	}
	return s.render(c, http.StatusOK, "board", "board-swap", v)
}

// afterMutationAddAnother is the "save and add another" form of afterMutation
// (T-0080): the saved item lands on the board exactly as a plain save does,
// but the panel is NOT dismissed - a fresh empty form swaps into
// #item-panel-root so the next title can be typed immediately. The response is
// board-swap-again, whose panel OOB renders the same item-panel template the
// /new route serves, so the re-opened form cannot drift from a direct load.
func (s *Server) afterMutationAddAnother(c *echo.Context, store *mm.Store, it mm.Item, section mm.Section, dryRun bool) error {
	if section == "" {
		section = mm.SectionReady
	}
	v := s.newView(c, "New item", store)
	v.App.Nav = "board"
	v.App.Toast = &toastData{
		Message:  string(it.ID) + " added",
		Severity: "success",
		DryRun:   dryRun,
	}
	board, err := s.buildBoard(c, store)
	if err != nil {
		return err
	}
	v.Data = itemPageData{Board: board, Panel: panelData{
		New: true, Section: sectionKey(section),
		Item:    itemData{Prio: "med"},
		Tickler: ticklerGroupData{Kind: "never"},
	}}
	return s.render(c, http.StatusOK, "board", "board-swap-again", v)
}

// toastData is one toast (§5.10).
type toastData struct {
	Message  string
	Severity string
	Code     string
	DryRun   bool
}

func formOr(c *echo.Context, name, fallback string) string {
	if v := c.Request().FormValue(name); v != "" {
		return v
	}
	return fallback
}

// formValue distinguishes "absent" from "present and empty", which is what lets
// an edit clear a field rather than leave it alone.
func formValue(c *echo.Context, name string) (string, bool) {
	if err := c.Request().ParseForm(); err != nil {
		return "", false
	}
	values, ok := c.Request().PostForm[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}
