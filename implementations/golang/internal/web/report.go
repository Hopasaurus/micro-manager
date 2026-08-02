package web

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"micromanager/mm"
)

// The report view (spec-gui.md §5.7).
//
// §4.1 gives it the same query vocabulary and the same precedence as
// spec-tools.md §5.1.11, so the CLI and the GUI resolve a period identically -
// and data-period-source makes the resolved answer visible, because a report
// whose period is invisible is a report you cannot check.

// reportData is the report view model.
type reportData struct {
	Period       string
	PeriodLabel  string
	PeriodSource string
	From         string
	To           string
	Count        int

	Groups   []reportGroupData
	Wip      []itemData
	Next     []itemData
	Warnings []string

	// Markdown is what report-copy puts on the clipboard: the library's
	// rendering, identical to the CLI's (§5.7).
	Markdown string

	Controls reportControls
}

type reportGroupData struct {
	Key   string
	Count int
	Items []reportItemData
}

type reportItemData struct {
	ID      string
	Title   string
	Outcome string
	Done    string
	Detail  string
	Tags    []string
}

// reportControls is the form state, so a reload of the URI reproduces it.
type reportControls struct {
	Period     string
	Since      string
	Until      string
	GroupBy    string
	IncludeWip bool
	Periods    []string
	GroupBys   []string
}

// report serves /p/:projectId/report.
func (s *Server) report(c *echo.Context) error {
	store, err := s.project(c)
	if store == nil {
		return err
	}

	v := s.newView(c, "Report", store)
	v.App.Nav = "report"

	data, err := s.buildReport(c, store)
	if err != nil {
		return err
	}
	v.Data = data

	return s.render(c, http.StatusOK, "report", "report", v)
}

// buildReport resolves the period and asks the library for the items.
func (s *Server) buildReport(c *echo.Context, store *mm.Store) (reportData, error) {
	q := c.Request().URL.Query()
	controls := reportControls{
		Period:     q.Get("period"),
		Since:      q.Get("since"),
		Until:      q.Get("until"),
		GroupBy:    q.Get("group-by"),
		IncludeWip: q.Get("include-wip") == "true" || q.Get("include-wip") == "on",
		Periods: []string{
			"last-week", "this-week", "last-7-days", "last-30-days",
			"this-month", "last-month", "all",
		},
		GroupBys: []string{"none", "outcome", "tag", "day"},
	}

	period, err := s.resolvePeriod(store, controls)
	if err != nil {
		return reportData{}, err
	}

	groupBy, err := mm.ParseGroupBy(controls.GroupBy)
	if err != nil {
		return reportData{}, err
	}
	if controls.GroupBy == "" {
		groupBy = s.opts.Config.Report.GroupBy
	}

	includeWip := controls.IncludeWip || (q.Get("include-wip") == "" && s.opts.Config.Report.IncludeWip)

	rep, err := store.Report(period, mm.ReportOptions{
		GroupBy:        groupBy,
		IncludeWip:     includeWip,
		IncludeBacklog: q.Get("include-backlog") == "true",
	})
	if err != nil {
		return reportData{}, err
	}

	data := reportData{
		Period:       rep.Period.String(),
		PeriodLabel:  rep.Period.Label,
		PeriodSource: string(rep.Period.Source),
		From:         rep.Period.Since.String(),
		To:           rep.Period.Until.String(),
		Count:        len(rep.Done),
		Warnings:     rep.Warnings,
		Markdown:     rep.Markdown(),
		Controls:     controls,
	}

	// Ungrouped, the whole list is one group so the DOM shape does not depend on
	// a setting: a suite asserting report-group-* finds one either way.
	if len(rep.Groups) > 0 {
		for _, g := range rep.Groups {
			data.Groups = append(data.Groups, reportGroup(g.Key, g.Items))
		}
	} else if len(rep.Done) > 0 {
		data.Groups = append(data.Groups, reportGroup("done", rep.Done))
	}

	dir, err := store.Directory()
	if err != nil {
		return reportData{}, err
	}
	resolver := s.registry.newRefResolver()
	for _, it := range rep.Wip {
		data.Wip = append(data.Wip, s.itemView(it, dir, len(data.Wip)+1, resolver))
	}
	for _, it := range rep.Next {
		data.Next = append(data.Next, s.itemView(it, dir, len(data.Next)+1, resolver))
	}
	return data, nil
}

func reportGroup(key string, items []mm.Item) reportGroupData {
	g := reportGroupData{Key: groupKey(key), Count: len(items)}
	for _, it := range items {
		g.Items = append(g.Items, reportItemData{
			ID:      string(it.ID),
			Title:   it.Title,
			Outcome: string(it.Outcome),
			Done:    it.Done.String(),
			Detail:  it.Detail,
			Tags:    it.Tags,
		})
	}
	return g
}

// groupKey makes a group heading safe to interpolate into a testid. A tag
// grouping's key is user data, and report-group-<key> is a locator.
func groupKey(key string) string {
	if key == "" {
		return "none"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// resolvePeriod applies the precedence of spec-tools.md §5.1.11, which
// spec-gui.md §4.1 binds this view to:
//
//	--since/--until  ->  the switch  ->  config  ->  default
//
// The environment step is deliberately skipped: a UI service MUST NOT read
// MM_REPORT_PERIOD from its own environment, because the service outlives any
// one user's shell (§9.2). Config takes that slot instead.
func (s *Server) resolvePeriod(store *mm.Store, c reportControls) (mm.Period, error) {
	today := s.today()

	// 1. since/until, an explicit range.
	if c.Since != "" || c.Until != "" {
		var since, until mm.Date
		var err error
		if c.Since != "" {
			if since, err = mm.ParseDate(c.Since); err != nil {
				return mm.Period{}, err
			}
		}
		if c.Until != "" {
			if until, err = mm.ParseDate(c.Until); err != nil {
				return mm.Period{}, err
			}
		} else {
			until = today // an open end means "up to now", as --until does
		}
		p, err := mm.PeriodBetween(since, until)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, nil
	}

	// 2. the period parameter, which is this view's equivalent of the switch.
	if c.Period != "" {
		p, err := mm.ParsePeriod(c.Period, today)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromSwitch
		return p, nil
	}

	// 3. config. This occupies the slot MM_REPORT_PERIOD holds for the CLI, and
	//    the environment step is SKIPPED entirely: a UI service must not read
	//    that variable from its own environment (§9.2).
	//
	//    What is asked for is the value a user WROTE, not the merged one: the
	//    merged report.period is "last-week" whether the user chose it or the
	//    built-in supplied it, and §5.7 needs those told apart.
	if configured := s.configuredPeriod(store); configured != "" {
		p, err := mm.ParsePeriod(configured, today)
		if err != nil {
			return mm.Period{}, err
		}
		p.Source = mm.PeriodFromConfig
		return p, nil
	}

	// 4. The default is last-week, not this-week: a report over a closed period
	//    is reproducible and one over an open period is not.
	p, err := mm.ParsePeriod("last-week", today)
	if err != nil {
		return mm.Period{}, err
	}
	p.Source = mm.PeriodFromDefault
	return p, nil
}

// configuredPeriod returns report.period as a user actually wrote it, project
// config first (§9.4 merges per leaf key, project last), then system config.
// Empty means nobody wrote one and the built-in default applies.
func (s *Server) configuredPeriod(store *mm.Store) string {
	if d, err := store.Directory(); err == nil {
		if f, err := mm.LoadConfigFile(mm.ProjectConfigPath(d.Path), mm.ScopeProject); err == nil {
			if v, ok := f.Get("report.period").(string); ok && v != "" {
				return v
			}
		}
	}
	if s.opts.SystemConfig != nil {
		if v, ok := s.opts.SystemConfig.Get("report.period").(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// today is the service's clock, injectable so a test can pin a period.
func (s *Server) today() mm.Date {
	ts := mm.NewTimestamp(s.registry.now())
	d, _ := mm.ParseDate(ts.String()[:10])
	return d
}
