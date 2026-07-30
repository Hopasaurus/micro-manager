package mm

import (
	"fmt"
	"sort"
)

// Report building (spec-tools.md §5.1.11).
//
// The report is the artifact people paste elsewhere, so this produces the
// STRUCTURE and leaves rendering to the front end. What must not be left to the
// front end is the period and its source: every output mode has to state them,
// and a report that carried only its items could not.

// GroupBy selects how a report's completed items are grouped.
type GroupBy string

const (
	GroupByNone    GroupBy = "none"
	GroupByOutcome GroupBy = "outcome"
	GroupByTag     GroupBy = "tag"
	GroupByDay     GroupBy = "day"
)

func ParseGroupBy(s string) (GroupBy, error) {
	switch GroupBy(s) {
	case GroupByNone, GroupByOutcome, GroupByTag, GroupByDay:
		return GroupBy(s), nil
	case "":
		return GroupByNone, nil
	}
	return "", fmt.Errorf("%w: unknown grouping %q (want none, outcome, tag or day)",
		ErrInvalidArgument, s)
}

// ReportOptions controls what a report contains beyond the closed items.
type ReportOptions struct {
	GroupBy GroupBy

	// IncludeWip appends what is in the working slots, marked in progress.
	IncludeWip bool

	// IncludeBacklog appends the top of ## Ready as "next".
	IncludeBacklog bool

	// BacklogLimit caps that list. Zero means 5 - "the top few", not all of it.
	BacklogLimit int
}

// ReportGroup is one group of a grouped report.
type ReportGroup struct {
	Key   string
	Items []Item
}

// Report is what one directory did in a period.
type Report struct {
	Period  Period
	Project string
	Path    string

	// Done holds every closed item in the period, newest first. ALL OUTCOMES ARE
	// INCLUDED and each is labelled: a week's cancellations are part of the week.
	Done   []Item
	Groups []ReportGroup

	Wip  []Item
	Next []Item

	// Warnings are conditions that make the report incomplete but not wrong -
	// most importantly a period that predates what done.md still holds.
	Warnings []string
}

// Report gathers the items a directory closed in a period.
//
// The period arrives fully resolved. Precedence between switches, the
// environment and the default is the wrapper's job (spec-tools.md §5.1.11): the
// library must not read the environment, and a library that applied its own
// default could not be told apart from one whose caller had.
func (s *Store) Report(p Period, opts ReportOptions) (Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := ParseGroupBy(string(opts.GroupBy)); err != nil {
		return Report{}, err
	}
	m, err := s.load()
	if err != nil {
		return Report{}, err
	}

	rep := Report{Period: p, Path: s.path}
	if m.backlog != nil {
		rep.Project = m.backlog.FM.Get("project")
	}

	if m.done != nil {
		for _, it := range m.done.Items {
			if p.Unbounded() || p.Contains(it.Done) {
				rep.Done = append(rep.Done, *it)
			}
		}
	}
	// Newest first. done.md is already in that order, and a stable sort keeps
	// the file's ordering among items closed on the same day.
	sort.SliceStable(rep.Done, func(i, j int) bool {
		return rep.Done[j].Done.Before(rep.Done[i].Done)
	})

	if opts.GroupBy != "" && opts.GroupBy != GroupByNone {
		rep.Groups = groupItems(rep.Done, opts.GroupBy)
	}

	if opts.IncludeWip {
		for _, w := range m.working {
			if w.Item != nil {
				rep.Wip = append(rep.Wip, *w.Item)
			}
		}
	}
	if opts.IncludeBacklog {
		limit := opts.BacklogLimit
		if limit == 0 {
			limit = 5
		}
		if m.backlog != nil {
			if sec := m.backlog.Section(SectionReady); sec != nil {
				for i, it := range sec.Items {
					if i == limit {
						break
					}
					rep.Next = append(rep.Next, *it)
				}
			}
		}
	}

	rep.Warnings = append(rep.Warnings, archiveWarning(m, p)...)
	return rep, nil
}

// archiveWarning reports a period that reaches back past what done.md still
// holds.
//
// Without it a report over an archived period returns nothing and looks exactly
// like a period in which nothing was closed. Archives live in done-YYYY.md,
// which this does not read.
func archiveWarning(m *dirModel, p Period) []string {
	if p.Since.IsZero() || m.done == nil {
		return nil
	}
	oldest := ""
	for _, g := range m.done.Months {
		if g.Month == "" {
			continue
		}
		if oldest == "" || g.Month < oldest {
			oldest = g.Month
		}
	}
	if oldest == "" || p.Since.Month7() >= oldest {
		return nil
	}
	return []string{fmt.Sprintf(
		"the period starts %s but done.md goes back only to %s; "+
			"anything older has been archived and is not included",
		p.Since, oldest)}
}

// groupItems splits a report's items.
//
// Empty groups are dropped: a report that lists "cancelled: none" every week
// trains the reader to skip it.
func groupItems(items []Item, by GroupBy) []ReportGroup {
	switch by {
	case GroupByOutcome:
		// Fixed order rather than alphabetical, so the same shape comes out
		// every week and shipped work leads.
		order := []Outcome{OutcomeShipped, OutcomeCancelled, OutcomeObsolete}
		var out []ReportGroup
		seen := map[Outcome]bool{}
		for _, o := range order {
			g := ReportGroup{Key: string(o)}
			for _, it := range items {
				if it.Outcome == o {
					g.Items = append(g.Items, it)
					seen[o] = true
				}
			}
			if len(g.Items) > 0 {
				out = append(out, g)
			}
		}
		// An outcome this implementation does not know still has to appear, or
		// the grouped report would quietly hold fewer items than the flat one.
		var rest []Item
		for _, it := range items {
			switch it.Outcome {
			case OutcomeShipped, OutcomeCancelled, OutcomeObsolete:
			default:
				rest = append(rest, it)
			}
		}
		if len(rest) > 0 {
			out = append(out, ReportGroup{Key: "(no outcome)", Items: rest})
		}
		return out

	case GroupByTag:
		// An item REPEATS under each of its tags - the point is to see a tag's
		// week, not to partition the list.
		byTag := map[string][]Item{}
		var untagged []Item
		for _, it := range items {
			if len(it.Tags) == 0 {
				untagged = append(untagged, it)
				continue
			}
			for _, tag := range it.Tags {
				byTag[tag] = append(byTag[tag], it)
			}
		}
		keys := make([]string, 0, len(byTag))
		for k := range byTag {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]ReportGroup, 0, len(keys)+1)
		for _, k := range keys {
			out = append(out, ReportGroup{Key: k, Items: byTag[k]})
		}
		if len(untagged) > 0 {
			out = append(out, ReportGroup{Key: "(untagged)", Items: untagged})
		}
		return out

	case GroupByDay:
		var out []ReportGroup
		for _, it := range items { // already newest first
			key := it.Done.String()
			if n := len(out); n > 0 && out[n-1].Key == key {
				out[n-1].Items = append(out[n-1].Items, it)
				continue
			}
			out = append(out, ReportGroup{Key: key, Items: []Item{it}})
		}
		return out
	}
	return nil
}
