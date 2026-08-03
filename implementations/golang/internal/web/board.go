package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// The board (spec-gui.md §5.5): the primary view and the drag-and-drop surface.
//
// Columns are in a fixed DOM order - ready, blocked, someday, one per working
// slot in slot order, then done - and every data-* attribute below is computed
// here rather than in the browser. State the client invents is state the server
// cannot be held to.

// boardData is the board view model.
type boardData struct {
	Columns  []columnData
	WipUsed  int
	WipLimit int
	Filters  filterData
	Empty    bool
}

// columnData is one column. Testid is the contract name; Key is what a filter
// or a drop target refers to.
type columnData struct {
	Testid    string
	Key       string
	Title     string
	Section   string // ready | blocked | someday, empty for working and done columns
	Slot      string // zero-padded slot number, empty for the others
	Occupied  bool
	IsSlot    bool
	IsWorking bool
	IsDone    bool
	// Collapsed marks the someday column as collapsed (§5.5). It is a CLIENT
	// preference - the server never persists it - so it is true only when the
	// request carries the state (T-0150).
	Collapsed bool
	Count     int
	Items     []itemData
}

// itemData is one card. Every field here corresponds to an attribute §5.5 fixes.
type itemData struct {
	ID        string
	Title     string
	State     string
	Section   string
	Prio      string
	Tags      []string
	TagList   string
	Blocked   bool
	Reason    string
	HasDetail bool
	Position  int
	Slot      string
	Outcome   string
	Refs      []refView
	Actions   []actionData
}

// actionData is one entry in an item's menu.
//
// §5.5: illegal actions MUST be present and disabled with data-reason naming the
// error code, rather than hidden - a test asserts WHY an action is unavailable,
// and a user gets told rather than left wondering where the button went.
type actionData struct {
	Op      string
	Label   string
	Enabled bool
	Reason  string
}

// filterData is the query-parameter state of §4.1, which the board is
// addressable by.
type filterData struct {
	Section string
	State   string
	Prio    string
	Tags    []string
	Query   string
	Active  bool
}

// board renders /p/:projectId/board.
func (s *Server) board(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Board", store)
	v.App.Nav = "board"

	// §10 rule 1: opening a project moves it to the front of recent. The board
	// is where "opening" happens - every other project route is reached from
	// here - and a full page load is the open, not an htmx fragment refresh or a
	// poll, which would rewrite the list constantly for a window left sitting.
	if !wantsFragment(c.Request()) {
		s.recordOpen(store, v.App.Theme.Name)
	}

	data, err := s.buildBoard(c, store)
	if err != nil {
		return err
	}
	v.Data = data

	return s.render(c, http.StatusOK, "board", "board", v)
}

// boardRedirect serves /p/:projectId, which §4.1 requires to redirect to the
// board with a 302.
func (s *Server) boardRedirect(c *echo.Context) error {
	id := c.Param("projectId")
	if _, err := s.registry.resolve(id); err != nil {
		return s.notFound(c, fmt.Sprintf("There is no project %s here.", id))
	}
	return c.Redirect(http.StatusFound, "/p/"+id+"/board")
}

// recordOpen adds the project to the recent list (§10 rule 1).
//
// A failure here is logged and swallowed: not being able to write a convenience
// list is not a reason to refuse to show somebody their board.
func (s *Server) recordOpen(store *mm.Store, themeName string) {
	d, err := store.Directory()
	if err != nil {
		return
	}
	if err := s.registry.touch(d, themeName, s.opts.Config.UI.RecentMaxStored); err != nil {
		s.log.Warn("recent", "project", d.ProjectID, "error", err)
	}
}

// project resolves the project a route names, or renders the not-found view.
//
// A NIL STORE means the response has already been written and the handler must
// return immediately; the error is whatever writing it produced, usually nil.
// Callers check the store, never the error: rendering the not-found view
// succeeds, so an err-only check reads "no error, carry on" and hands the
// handler a nil store.
//
// §4.1 rule 4: an unknown projectId is a 404 with the not-found view, never a
// redirect to /.
func (s *Server) project(c *echo.Context) (*mm.Store, error) {
	id := c.Param("projectId")
	store, err := s.registry.resolve(id)
	if err != nil {
		return nil, s.notFound(c, fmt.Sprintf("There is no project %s here.", id))
	}
	return store, nil
}

// buildBoard reads the directory and lays it out in column order.
func (s *Server) buildBoard(c *echo.Context, store *mm.Store) (boardData, error) {
	dir, err := store.Directory()
	if err != nil {
		return boardData{}, err
	}
	filters := readFilters(c)

	items, err := s.selectItems(store, filters)
	if err != nil {
		return boardData{}, err
	}

	data := boardData{
		WipUsed:  dir.WipUsed,
		WipLimit: dir.WipLimit,
		Filters:  filters,
	}
	// One refs resolver per render: resolution is a lookup among the boards
	// this service knows, and the memo it keeps for item existence is only
	// useful while the render lasts.
	resolver := s.registry.newRefResolver()

	// The backlog columns (someday, ready, blocked), then working, then done:
	// the DOM order of §5.5, which a test reads positionally.
	for _, section := range []mm.Section{mm.SectionSomeday, mm.SectionReady, mm.SectionBlocked} {
		// The library's Section values are capitalised because they name the
		// "## Ready" headings in the file. The DOM contract fixes them
		// LOWERCASE - data-section="ready", board-column-ready - so the two
		// spellings are converted here, at the one boundary between them.
		key := sectionKey(section)
		col := columnData{
			Testid:  "board-column-" + key,
			Key:     key,
			Title:   string(section),
			Section: key,
		}
		if key == "someday" {
			col.Collapsed = somedayCollapsed(c)
		}
		for _, it := range items {
			if it.State == mm.StateBacklog && it.Section == section {
				col.Items = append(col.Items, s.itemView(it, dir, len(col.Items)+1, resolver))
			}
		}
		col.Count = len(col.Items)
		data.Columns = append(data.Columns, col)
	}

	working := columnData{
		Testid:    "board-column-working",
		Key:       "working",
		Title:     "Working",
		IsWorking: true,
	}
	for _, slot := range dir.Slots {
		if slot.Item != nil {
			for _, it := range items {
				if it.ID == slot.Item.ID {
					working.Items = append(working.Items, s.itemView(it, dir, len(working.Items)+1, resolver))
				}
			}
		}
	}
	working.Count = len(working.Items)
	data.Columns = append(data.Columns, working)

	done := columnData{Testid: "board-column-done", Key: "done", Title: "Done", IsDone: true}
	limit := s.opts.Config.UI.Board.DoneLimit
	for _, it := range items {
		if it.State != mm.StateDone {
			continue
		}
		if limit > 0 && len(done.Items) >= limit {
			break
		}
		done.Items = append(done.Items, s.itemView(it, dir, len(done.Items)+1, resolver))
	}
	done.Count = len(done.Items)
	data.Columns = append(data.Columns, done)

	total := 0
	for _, col := range data.Columns {
		total += col.Count
	}
	data.Empty = total == 0
	return data, nil
}

// somedayCollapsed reports the someday column's collapse state (§5.5).
//
// The toggle is a client preference, kept in localStorage, so a render cannot
// know it from the directory. The client sends it on every htmx request;
// without it a board refresh would render the column expanded for a frame and
// morph it back a beat later (T-0150).
func somedayCollapsed(c *echo.Context) bool {
	return strings.EqualFold(c.Request().Header.Get("X-Someday-Collapsed"), "true")
}

// sectionKey is the lowercase form the DOM contract uses (§5.1). The library
// spells a section the way the file's heading does; the UI spells it the way a
// testid and a data-section value do.
func sectionKey(s mm.Section) string { return strings.ToLower(string(s)) }

// parseSection is the reverse, for a query parameter or a form value.
func parseSection(key string) (mm.Section, bool) {
	for _, s := range mm.Sections() {
		if sectionKey(s) == strings.ToLower(key) {
			return s, true
		}
	}
	return "", false
}

func slotWidth(s mm.Slot) int {
	if s.Width < 2 {
		return 2
	}
	return s.Width
}

// selectItems applies the query parameters of §4.1.
//
// A free-text query goes through the library's search so the board, the CLI's
// --search and the API all agree about what matches.
func (s *Server) selectItems(store *mm.Store, f filterData) ([]mm.Item, error) {
	if f.Query != "" {
		hits, err := store.Search(mm.SearchRequest{Query: f.Query})
		if err != nil {
			return nil, err
		}
		return filterItems(mm.SearchItems(hits), f), nil
	}

	items, err := store.List(mm.Filter{State: mm.StateAll})
	if err != nil {
		return nil, err
	}
	return filterItems(items, f), nil
}

// filterItems applies section, state, prio and tag.
//
// Tags accumulate as an AND: ?tag=infra&tag=ci means both, which is what a
// person narrowing a board expects from adding a second filter.
func filterItems(items []mm.Item, f filterData) []mm.Item {
	out := make([]mm.Item, 0, len(items))
	for _, it := range items {
		if f.Section != "" && sectionKey(it.Section) != f.Section {
			continue
		}
		if f.State != "" && f.State != "all" && string(it.State) != f.State {
			continue
		}
		if f.Prio != "" && string(it.Prio.Effective()) != f.Prio {
			continue
		}
		if !hasEveryTag(it.Tags, f.Tags) {
			continue
		}
		out = append(out, it)
	}
	return out
}

func hasEveryTag(have []string, want []string) bool {
	for _, w := range want {
		found := false
		for _, h := range have {
			if h == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func readFilters(c *echo.Context) filterData {
	q := c.Request().URL.Query()
	f := filterData{
		Section: q.Get("section"),
		State:   q.Get("state"),
		Prio:    q.Get("prio"),
		Tags:    q["tag"],
		Query:   q.Get("q"),
	}
	f.Active = f.Section != "" || f.State != "" || f.Prio != "" || len(f.Tags) > 0 || f.Query != ""
	return f
}

// itemView builds one card. resolver is the per-render refs resolver; it is
// never nil at the call sites in this package.
func (s *Server) itemView(it mm.Item, dir mm.Directory, position int, resolver *refResolver) itemData {
	d := itemData{
		ID:        string(it.ID),
		Title:     it.Title,
		State:     string(it.State),
		Section:   sectionKey(it.Section),
		Prio:      string(it.Prio.Effective()),
		Tags:      it.Tags,
		TagList:   mm.FormatTags(it.Tags),
		Blocked:   it.Blocked != "",
		Reason:    it.Blocked,
		HasDetail: it.Detail != "",
		Position:  position,
		Outcome:   string(it.Outcome),
		Refs:      resolver.refsView(it),
	}
	if it.Slot > 0 {
		d.Slot = fmt.Sprintf("%02d", it.Slot)
	}
	d.Actions = actionsFor(it, dir)
	return d
}

// actionsFor decides which operations are legal for an item (§5.5, §6.1).
//
// This is computed ONCE, on the server, and shared by the card menu and the item
// panel. Two copies of this rule would diverge, and the client is not trusted
// with it in any case: §2.1 lets the client replicate rules to disable controls
// early, but the server's answer is the authoritative one.
func actionsFor(it mm.Item, dir mm.Directory) []actionData {
	full := dir.WipUsed >= dir.WipLimit && dir.WipLimit > 0

	action := func(op, label string, enabled bool, reason string) actionData {
		return actionData{Op: op, Label: label, Enabled: enabled, Reason: reason}
	}

	switch it.State {
	case mm.StateBacklog:
		startReason := ""
		if full {
			startReason = codeWipLimitReached
		}
		return []actionData{
			action("start", "Start", !full, startReason),
			action("pause", "Pause", false, codePreconditionFailed),
			action("finish", "Finish", true, ""),
			action("block", "Block", it.Blocked == "", conflictUnless(it.Blocked == "")),
			action("unblock", "Unblock", it.Blocked != "", conflictUnless(it.Blocked != "")),
			action("move", "Move", true, ""),
			// T-0147: convenience accelerators for --move --top/--end
			// (spec-tools.md §5.1.7). Legal for every backlog item: the library
			// no-ops a same-position move rather than refusing it, exactly as
			// the CLI does.
			action("move-top", "Move to top", true, ""),
			action("move-end", "Move to bottom", true, ""),
			action("note", "Note", true, ""),
			action("edit", "Edit", true, ""),
			action("remove", "Remove", true, ""),
		}
	case mm.StateWorking:
		return []actionData{
			action("start", "Start", false, codeConflict),
			action("pause", "Pause", true, ""),
			action("finish", "Finish", true, ""),
			action("block", "Block", false, codeConflict),
			action("unblock", "Unblock", false, codeConflict),
			// §5.1.7 of the tools spec: move repositions a BACKLOG item.
			action("move", "Move", false, codeConflict),
			action("move-top", "Move to top", false, codeConflict),
			action("move-end", "Move to bottom", false, codeConflict),
			action("note", "Note", true, ""),
			action("edit", "Edit", true, ""),
			// The library refuses to remove an item that is in progress.
			action("remove", "Remove", false, codeConflict),
		}
	default: // done
		return []actionData{
			// §7.2: reopening is not a specified operation in v1.
			action("start", "Start", false, codeConflict),
			action("pause", "Pause", false, codeConflict),
			action("finish", "Finish", false, codeConflict),
			action("block", "Block", false, codeConflict),
			action("unblock", "Unblock", false, codeConflict),
			action("move", "Move", false, codeConflict),
			action("move-top", "Move to top", false, codeConflict),
			action("move-end", "Move to bottom", false, codeConflict),
			action("note", "Note", true, ""),
			action("edit", "Edit", true, ""),
			action("remove", "Remove", true, ""),
		}
	}
}

func conflictUnless(ok bool) string {
	if ok {
		return ""
	}
	return codeConflict
}
