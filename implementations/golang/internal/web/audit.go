package web

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/Hopasaurus/micro-manager/mm"
)

// The audit log view (spec-gui.md §5.11, T-0253): a read-only rendering of
// audit.md (format spec §5.7). Reads only — every mutation happens through
// the operations that already exist; enabling or disabling the log itself
// is the settings toggle (settings.go), not this page.

// auditData is the audit view model.
type auditData struct {
	Available bool // version 2 only (§5.1.8)
	Enabled   bool
	Entries   []auditEntryData // newest first
}

type auditEntryData struct {
	N         int
	Timestamp string
	ID        string
	Title     string // the item's current title, "" if it no longer exists (e.g. removed)
	Field     string
	Value     string
}

// auditLog serves /p/:projectId/audit.
func (s *Server) auditLog(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Audit Log", store)
	v.App.Nav = "audit"

	data, err := s.buildAudit(store)
	if err != nil {
		return err
	}
	v.Data = data

	return s.render(c, http.StatusOK, "audit", "audit", v)
}

// buildAudit reads the directory's audit.md, newest entry first (the order
// a reader wants: what just happened, not the file's own append order).
func (s *Server) buildAudit(store *mm.Store) (auditData, error) {
	dir, err := store.Directory()
	if err != nil {
		return auditData{}, err
	}
	data := auditData{Available: dir.Version == 2, Enabled: dir.StageCfg.AuditEnabled}
	if !data.Available {
		return data, nil
	}

	entries, err := mm.ReadAuditLog(dir.Path)
	if err != nil {
		return auditData{}, err
	}

	// Looked up once per distinct ID, not once per entry - the same item
	// commonly appears across several consecutive lines. A UI-only lookup:
	// audit.md itself never carries a title (spec-file-format.md §5.7 keeps
	// it to id/field/value), so this is purely a display convenience, not a
	// field the log format has any opinion about.
	titles := map[string]string{}
	titleFor := func(id string) string {
		if t, ok := titles[id]; ok {
			return t
		}
		t := ""
		if it, err := store.Get(mm.ID(id)); err == nil {
			t = it.Title
		}
		titles[id] = t
		return t
	}

	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		data.Entries = append(data.Entries, auditEntryData{
			N: len(data.Entries), Timestamp: e.Timestamp, ID: string(e.ID),
			Title: titleFor(string(e.ID)), Field: e.Field, Value: e.Value,
		})
	}
	return data, nil
}
