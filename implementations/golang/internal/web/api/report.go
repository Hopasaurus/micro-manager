package api

import (
	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// Reports, validation and WIP (spec-tools.md §5.1.11, §5.1.12, §5.2).

// jsonReport mirrors the CLI's --report JSON result: the period and its source
// are part of the result because §5.1.11 requires every output mode to state
// them.
type jsonReport struct {
	Project  string      `json:"project"`
	Period   jsonPeriod  `json:"period"`
	Done     []jsonItem  `json:"done"`
	Groups   []jsonGroup `json:"groups,omitempty"`
	Wip      []jsonItem  `json:"wip,omitempty"`
	Next     []jsonItem  `json:"next,omitempty"`
	Warnings []string    `json:"warnings,omitempty"`
	Markdown string      `json:"markdown,omitempty"`
}

type jsonPeriod struct {
	Label  string `json:"label"`
	Since  string `json:"since"`
	Until  string `json:"until"`
	Source string `json:"source"`
}

type jsonGroup struct {
	Key   string     `json:"key"`
	Items []jsonItem `json:"items"`
}

// report serves GET /api/v1/projects/:projectId/report (--report).
//
// The period vocabulary and precedence are those of spec-tools.md §5.1.11, the
// same ones the CLI and the report view use. The environment step is skipped —
// a UI service must not read MM_REPORT_PERIOD from its own environment (§9.2).
func (s *Server) report(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	q := c.Request().URL.Query()

	period, err := s.resolvePeriod(store, q.Get("period"), q.Get("since"), q.Get("until"))
	if err != nil {
		return err
	}

	groupBy := mm.GroupBy(q.Get("group-by"))
	if q.Get("group-by") == "" {
		groupBy = s.svc.Config().Report.GroupBy
	}
	if groupBy != "" {
		if _, err := mm.ParseGroupBy(string(groupBy)); err != nil {
			return err
		}
	}

	includeWip := q.Get("include-wip") == "true"
	if q.Get("include-wip") == "" {
		includeWip = s.svc.Config().Report.IncludeWip
	}

	rep, err := store.Report(period, mm.ReportOptions{
		GroupBy:        groupBy,
		IncludeWip:     includeWip,
		IncludeBacklog: q.Get("include-backlog") == "true",
	})
	if err != nil {
		return err
	}

	out := jsonReport{
		Project: rep.Project,
		Period: jsonPeriod{
			Label:  rep.Period.Label,
			Since:  rep.Period.Since.String(),
			Until:  rep.Period.Until.String(),
			Source: string(rep.Period.Source),
		},
		Done:     toJSONItems(rep.Done),
		Wip:      toJSONItems(rep.Wip),
		Next:     toJSONItems(rep.Next),
		Warnings: rep.Warnings,
	}
	for _, g := range rep.Groups {
		out.Groups = append(out.Groups, jsonGroup{Key: g.Key, Items: toJSONItems(g.Items)})
	}
	return ok(c, out)
}

// resolvePeriod applies the precedence of spec-tools.md §5.1.11: an explicit
// since/until range, then the period token, then config, then the default.
func (s *Server) resolvePeriod(store *mm.Store, period, since, until string) (mm.Period, error) {
	today := s.today()

	if since != "" || until != "" {
		var s, u mm.Date
		var err error
		if since != "" {
			if s, err = mm.ParseDate(since); err != nil {
				return mm.Period{}, err
			}
		}
		if until != "" {
			if u, err = mm.ParseDate(until); err != nil {
				return mm.Period{}, err
			}
		} else {
			u = today
		}
		p, err := mm.PeriodBetween(s, u)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, nil
	}

	if period != "" {
		p, err := mm.ParsePeriod(period, today)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, nil
	}

	if configured := s.configuredPeriod(store); configured != "" {
		p, err := mm.ParsePeriod(configured, today)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromConfig
		return p, nil
	}

	p, err := mm.ParsePeriod("last-week", today)
	if err != nil {
		return mm.Period{}, err
	}
	p.Source = mm.PeriodFromDefault
	return p, nil
}

// configuredPeriod returns report.period as a user actually wrote it: project
// config first, then system config. Empty means nobody wrote one.
func (s *Server) configuredPeriod(store *mm.Store) string {
	if d, err := store.Directory(); err == nil {
		if f, err := mm.LoadConfigFile(mm.ProjectConfigPath(d.Path), mm.ScopeProject); err == nil {
			if v, ok := f.Get("report.period").(string); ok && v != "" {
				return v
			}
		}
	}
	if sys := s.svc.SystemConfig(); sys != nil {
		if v, ok := sys.Get("report.period").(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// check serves GET /api/v1/projects/:projectId/check (--check).
//
// A directory with violations is still 200: the findings ARE the answer, not an
// error (spec-tools.md §5.1.12).
func (s *Server) check(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	violations, err := store.Validate()
	if err != nil {
		return err
	}
	vs := toJSONViolations(violations)
	return ok(c, map[string]any{
		"ok":         len(vs) == 0,
		"violations": vs,
	})
}

// getWip serves GET /api/v1/projects/:projectId/wip.
func (s *Server) getWip(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	st, err := store.Status()
	if err != nil {
		return err
	}
	return ok(c, map[string]any{
		"used":  st.WipUsed(),
		"limit": st.WipLimit(),
	})
}

// putWip serves PUT /api/v1/projects/:projectId/wip (--wip).
func (s *Server) putWip(c *echo.Context) error {
	store, err := s.projectFor(c)
	if err != nil {
		return err
	}
	var req struct {
		Limit  int  `json:"limit"`
		DryRun bool `json:"dryRun"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	d, res, err := store.SetWipLimit(req.Limit, req.DryRun || dryRun(c))
	if err != nil {
		return err
	}
	return ok(c, map[string]any{
		"directory": toJSONDirectory(d),
		"changes":   toJSONChanges(res),
		"dryRun":    res.DryRun,
	})
}
