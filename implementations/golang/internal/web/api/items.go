package api

import (
	"errors"
	"fmt"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// Items: the §5.1 operations of spec-tools.md, over HTTP (spec-gui.md §4.2).
//
// Every mutating endpoint accepts dryRun and returns the change set without
// writing (spec-tools.md §3.4). The request bodies mirror the library request
// structs field for field, so nothing here is a rule — a translation only.

// addItemRequest is the body of POST /items (--add).
type addItemRequest struct {
	Title      string     `json:"title"`
	Top        bool       `json:"top"`
	Section    string     `json:"section"` // version 1
	Stage      string     `json:"stage"`   // version 2
	Reason     string     `json:"reason"`  // version 2
	Prio       string     `json:"prio"`
	Tags       []string   `json:"tags"`
	Blocked    string     `json:"blocked"`
	Created    string     `json:"created"`
	DetailBody string     `json:"detailBody"`
	Extra      []mm.Field `json:"extra"`
	DryRun     bool       `json:"dryRun"`
}

// addItem serves POST /api/v1/projects/:projectId/items (--add).
func (s *Server) addItem(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	var req addItemRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	ar := mm.AddRequest{
		Title:      req.Title,
		Top:        req.Top,
		Prio:       mm.Prio(req.Prio),
		Tags:       req.Tags,
		Blocked:    req.Blocked,
		Stage:      mm.Stage(req.Stage),
		Reason:     req.Reason,
		DetailBody: req.DetailBody,
		Extra:      req.Extra,
		DryRun:     req.DryRun || dryRun(c),
	}
	if req.Section != "" {
		section, ok := parseSection(req.Section)
		if !ok {
			return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, req.Section)
		}
		ar.Section = section
	}
	if req.Created != "" {
		d, err := mm.ParseDate(req.Created)
		if err != nil {
			return err
		}
		ar.Created = d
	}

	it, res, err := store.Add(ar, s.today())
	if err != nil {
		return err
	}
	return created(c, mutationResult(&it, res))
}

// editItemRequest is the body of PATCH /items/:itemId (--edit).
//
// Pointer fields: null means "leave alone", a value means "set". That is the
// same contract as mm.UpdateRequest, which is why the decode lands directly in
// it.
type editItemRequest struct {
	ExpectedRevision string     `json:"expectedRevision"`
	Detail           *string    `json:"detail"`
	Title            *string    `json:"title"`
	Prio             *string    `json:"prio"`
	Blocked          *string    `json:"blocked"`
	Created          *string    `json:"created"`
	Started          *string    `json:"started"`
	Tags             []string   `json:"tags"`
	SetTags          bool       `json:"setTags"`
	AddTags          []string   `json:"addTags"`
	RemoveTags       []string   `json:"removeTags"`
	Set              []mm.Field `json:"set"`
	Unset            []string   `json:"unset"`
	DryRun           bool       `json:"dryRun"`
}

// editItem serves PATCH /api/v1/projects/:projectId/items/:itemId (--edit).
func (s *Server) editItem(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}
	var req editItemRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	ur := mm.UpdateRequest{
		Tags:       req.Tags,
		SetTags:    req.SetTags,
		AddTags:    req.AddTags,
		RemoveTags: req.RemoveTags,
		Set:        req.Set,
		Unset:      req.Unset,
		DryRun:     req.DryRun || dryRun(c),
	}
	if req.Title != nil {
		ur.Title = req.Title
	}
	if req.Prio != nil {
		p, err := mm.ParsePrio(*req.Prio)
		if err != nil {
			return err
		}
		ur.Prio = &p
	}
	if req.Blocked != nil {
		ur.Blocked = req.Blocked
	}
	if req.Created != nil {
		d, err := mm.ParseDate(*req.Created)
		if err != nil {
			return err
		}
		ur.Created = &d
	}
	if req.Started != nil {
		d, err := mm.ParseDate(*req.Started)
		if err != nil {
			return err
		}
		ur.Started = &d
	}

	expected := mm.ItemRevision(req.ExpectedRevision)
	if expected == "" {
		expected, err = store.ItemRevision(id)
		if err != nil {
			return err
		}
	}
	it, res, err := store.Edit(id, mm.EditRequest{
		Update: ur, DetailBody: req.Detail,
		ExpectedRevision: expected,
	}, s.today())
	if err != nil {
		return err
	}
	return ok(c, mutationResult(&it, res))
}

// removeItem serves DELETE /api/v1/projects/:projectId/items/:itemId
// (--remove). The force guard is mandatory: the format keeps cancelled work in
// done.md precisely so abandonment leaves a record (spec-tools.md §5.1.6).
func (s *Server) removeItem(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}
	var req struct {
		Force      bool `json:"force"`
		WithDetail bool `json:"withDetail"`
		DryRun     bool `json:"dryRun"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if c.QueryParam("force") == "true" {
		req.Force = true
	}

	removal, res, err := store.Remove(id, mm.RemoveRequest{
		Force:      req.Force,
		WithDetail: req.WithDetail,
		DryRun:     req.DryRun || dryRun(c),
	}, s.today())
	if err != nil {
		return err
	}

	return ok(c, map[string]any{
		"item":          toJSONItem(removal.Item),
		"detailDeleted": removal.DetailDeleted,
		"detailOrphan":  removal.DetailOrphan,
		"changes":       toJSONChanges(res),
		"dryRun":        res.DryRun,
	})
}

// showItem serves GET /api/v1/projects/:projectId/items/:itemId (--show). The
// detail body is included when the item has a detail file, so a caller does not
// need a second round trip to read it.
func (s *Server) showItem(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}
	snapshot, err := store.ItemSnapshot(id)
	if err != nil {
		return err
	}
	it := snapshot.Item
	out := map[string]any{"item": toJSONItem(it), "revision": snapshot.Revision.String()}
	if snapshot.Detail != nil {
		detail := *snapshot.Detail
		out["detail"] = map[string]any{
			"path":  detail.Path,
			"id":    string(detail.ID),
			"title": detail.Title,
			"body":  detail.Body,
		}
	}
	return ok(c, out)
}

// operation adapts one operation name to a handler, so the seven routes are
// seven registrations of one code path rather than seven near-copies.
func (s *Server) operation(op string) echo.HandlerFunc {
	return func(c *echo.Context) error { return s.operate(c, op) }
}

// operate runs one mutating operation.
func (s *Server) operate(c *echo.Context, op string) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	id, err := s.itemID(c, store)
	if err != nil {
		return err
	}
	today := s.today()

	// The request bodies are deliberately flat per operation: each carries the
	// fields that operation's library call takes, and nothing it does not.
	var body map[string]any
	if err := c.Bind(&body); err != nil {
		return err
	}
	str := func(key string) string {
		v, _ := body[key].(string)
		return v
	}
	boolv := func(key string) bool {
		v, _ := body[key].(bool)
		return v
	}
	dry := boolv("dryRun") || dryRun(c)

	var it mm.Item
	var res mm.TxResult
	switch op {
	case "start":
		it, res, err = store.Start(id, mm.StartRequest{
			Slot:   intOf(body["slot"]),
			DryRun: dry,
		}, today)

	case "pause":
		req := mm.PauseRequest{DryRun: dry}
		reason := str("reason")
		if sec := str("section"); sec != "" {
			section, ok := parseSection(sec)
			if !ok {
				return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, sec)
			}
			req.Section = section
			req.Blocked = reason
		}
		if stage := str("stage"); stage != "" {
			req.Stage = mm.Stage(stage)
			// Version 2: pauseV2 does not accept a NEW reason itself (unlike
			// version 1's Blocked, which travels with the move) - it only
			// checks whether the item already carries one for a
			// needs_reason destination (research decision 18). A caller
			// submitting both a stage and a reason in one call - mirroring
			// dialog-block's single-submission UX (internal/web/item.go's
			// own "pause" case) - needs the reason set first (T-0245).
			if reason != "" {
				if _, _, err := store.Update(id, mm.UpdateRequest{Blocked: &reason, DryRun: dry}, today); err != nil {
					return err
				}
			}
		}
		req.End = boolv("end")
		req.DiscardNotes = boolv("discardNotes")
		it, res, err = store.Pause(id, req, today)

	case "finish":
		outcome := str("outcome")
		if outcome == "" {
			outcome = "shipped"
		}
		o, parseErr := mm.ParseOutcome(outcome)
		if parseErr != nil {
			return parseErr
		}
		req := mm.FinishRequest{Outcome: o, Note: str("note"), DryRun: dry}
		if d := str("done"); d != "" {
			date, parseErr := mm.ParseDate(d)
			if parseErr != nil {
				return parseErr
			}
			req.Done = date
		}
		req.DiscardNotes = boolv("discardNotes")
		it, res, err = store.Finish(id, req, today)

	case "block":
		reason := str("reason")
		if strings.TrimSpace(reason) == "" {
			return fmt.Errorf("%w: blocking %s needs a reason", mm.ErrInvalidArgument, id)
		}
		// Both version-1 and version-2 fields are set unconditionally:
		// Store.Move dispatches on the directory's actual version and reads
		// only the pair that applies (matching the CLI's --block and the
		// GUI's block, T-0230/T-0236).
		it, res, err = store.Move(id, mm.MoveRequest{
			Section: mm.SectionBlocked, Blocked: reason,
			Stage: "blocked", Reason: reason,
			DryRun: dry,
		}, today)

	case "unblock":
		it, res, err = store.Move(id, mm.MoveRequest{
			Section: mm.SectionReady, Stage: "ready", Top: true, DryRun: dry,
		}, today)

	case "move":
		req := mm.MoveRequest{DryRun: dry}
		if sec := str("section"); sec != "" {
			section, ok := parseSection(sec)
			if !ok {
				return fmt.Errorf("%w: %q is not a section", mm.ErrInvalidArgument, sec)
			}
			req.Section = section
			req.Blocked = str("reason")
		}
		if stage := str("stage"); stage != "" {
			req.Stage = mm.Stage(stage)
			req.Reason = str("reason")
		}
		if n := intOf(body["position"]); n > 0 {
			req.Position = n
		}
		req.Top = boolv("top")
		req.End = boolv("end")
		if before := str("before"); before != "" {
			g, err := store.Grammar()
			if err != nil {
				return err
			}
			bid, parseErr := g.ParseID(before)
			if parseErr != nil {
				return parseErr
			}
			req.Before = bid
		}
		if after := str("after"); after != "" {
			g, err := store.Grammar()
			if err != nil {
				return err
			}
			aid, parseErr := g.ParseID(after)
			if parseErr != nil {
				return parseErr
			}
			req.After = aid
		}
		it, res, err = store.Move(id, req, today)

	case "note":
		it, res, err = store.Note(id, mm.NoteRequest{
			Text:    str("note"),
			Undated: boolv("undated"),
			DryRun:  dry,
		}, today)
	}
	if err != nil {
		return err
	}
	return ok(c, mutationResult(&it, res))
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func isNotFound(err error) bool {
	return errors.Is(err, mm.ErrNotFound)
}
