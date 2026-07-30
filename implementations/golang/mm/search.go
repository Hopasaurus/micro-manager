package mm

import (
	"fmt"
	"regexp"
	"strings"
)

// Search (spec-tools.md §5.2): substring or regex match over titles, tags and
// detail bodies, reporting state and location per hit.
//
// It is here rather than in a front end because the CLI's --search, the GUI's
// search-input (spec-gui.md §6.1) and the board's ?q= filter must agree about
// what matches. A search that finds different things in two windows is worse
// than no search.

// SearchField names where a hit was found.
type SearchField string

const (
	FieldTitle  SearchField = "title"
	FieldTags   SearchField = "tags"
	FieldDetail SearchField = "detail"
)

// SearchRequest is one query.
type SearchRequest struct {
	Query string

	// Regex treats Query as a regular expression. Without it the query is a
	// case-insensitive substring, which is what a person typing into a box
	// expects; a regex is matched exactly as written, so case sensitivity is the
	// caller's to ask for with (?i).
	Regex bool

	// Fields narrows where to look. Empty means all three.
	Fields []SearchField

	// State narrows to backlog, working or done. Empty means everywhere, which
	// is the useful default: the reason to search is usually that you cannot
	// remember where something is.
	State State

	// Limit caps the number of hits. Zero means no cap.
	Limit int
}

// SearchHit is one match.
type SearchHit struct {
	Item  Item
	Field SearchField

	// At is the file and line the match was found on. For a title or tag match
	// that is the item's own line; for a detail match it is the line inside
	// details/T-NNNN.md, which is what makes a hit navigable.
	At Location

	// Text is the matching line, trimmed. It is context for a result list, not
	// a parseable field.
	Text string
}

// Search runs a query over the directory.
//
// Order is item order - backlog by section and position, then working slots,
// then done newest first - and within an item, title then tags then detail. A
// hit list that reordered itself between queries would be unusable as a way to
// find one thing twice.
func (s *Store) Search(req SearchRequest) ([]SearchHit, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("%w: an empty query matches everything; say what to look for",
			ErrInvalidArgument)
	}

	match, err := matcherFor(req)
	if err != nil {
		return nil, err
	}
	want := fieldSet(req.Fields)

	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.load()
	if err != nil {
		return nil, err
	}

	var out []SearchHit
	full := func() bool { return req.Limit > 0 && len(out) >= req.Limit }

	for _, it := range m.items() {
		if full() {
			break
		}
		if req.State != "" && req.State != StateAll && it.State != req.State {
			continue
		}

		if want[FieldTitle] && match(it.Title) {
			out = append(out, SearchHit{Item: *it, Field: FieldTitle, At: it.Source, Text: it.Title})
			if full() {
				break
			}
		}
		if want[FieldTags] {
			for _, tag := range it.Tags {
				if match(tag) {
					out = append(out, SearchHit{Item: *it, Field: FieldTags, At: it.Source,
						Text: FormatTags(it.Tags)})
					break // one hit per item: the tag list is one field
				}
			}
			if full() {
				break
			}
		}
		if want[FieldDetail] && it.Detail != "" {
			df := m.details[it.Detail]
			if df == nil {
				continue // I8 already reports a detail: pointing at nothing
			}
			for i, line := range df.Lines {
				if !match(line) {
					continue
				}
				out = append(out, SearchHit{
					Item: *it, Field: FieldDetail,
					At:   Location{File: df.Name, Line: i + 1},
					Text: strings.TrimSpace(line),
				})
				if full() {
					break
				}
			}
		}
	}
	return out, nil
}

// matcherFor builds the match function, compiling the pattern once rather than
// per item.
func matcherFor(req SearchRequest) (func(string) bool, error) {
	if req.Regex {
		re, err := regexp.Compile(req.Query)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not a valid regular expression: %v",
				ErrInvalidArgument, req.Query, err)
		}
		return re.MatchString, nil
	}
	needle := strings.ToLower(req.Query)
	return func(s string) bool {
		return strings.Contains(strings.ToLower(s), needle)
	}, nil
}

func fieldSet(fields []SearchField) map[SearchField]bool {
	if len(fields) == 0 {
		return map[SearchField]bool{FieldTitle: true, FieldTags: true, FieldDetail: true}
	}
	out := map[SearchField]bool{}
	for _, f := range fields {
		out[f] = true
	}
	return out
}

// SearchItems reduces hits to the distinct items they belong to, in hit order.
//
// The board's ?q= filter wants items, not hits: it renders cards, and one card
// per matching line would be a different view of the same project.
func SearchItems(hits []SearchHit) []Item {
	seen := map[ID]bool{}
	var out []Item
	for _, h := range hits {
		if seen[h.Item.ID] {
			continue
		}
		seen[h.Item.ID] = true
		out = append(out, h.Item)
	}
	return out
}
