package mm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// searchDir builds a directory with one item per state plus a detail file, so a
// query can be asked to reach each of them.
func searchDir(t *testing.T) string {
	t.Helper()
	dir := newDir(t, map[string]string{
		"backlog.md": "---\ndoc: backlog\nversion: 1\nproject: Sample One\nnext_id: T-0099\nupdated: 2026-07-30\n---\n\n" +
			"# Backlog\n\n## Ready\n\n" +
			"- [ ] [T-0020] Fix the deploy script | prio:high | tags:infra,ci | detail:details/T-0020.md | created:2026-07-01\n" +
			"- [ ] [T-0021] Rotate the leaked token | tags:security | created:2026-07-02\n\n" +
			"## Blocked\n\n## Someday\n\n" +
			"- [ ] [T-0022] Rewrite the DEPLOY documentation | created:2026-07-03\n",
		"working.02.md": busySlot,
	})
	writeString := func(name, body string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeString("details/T-0020.md", "---\ndoc: detail\nid: T-0020\ntitle: Fix the deploy script\nupdated: 2026-07-01\n---\n\n"+
		"# T-0020 — Fix the deploy script\n\n## Context\n\nThe cache key includes the build id.\nSomething about kubernetes.\n")
	return dir
}

func hitIDs(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, string(h.Item.ID)+":"+string(h.Field))
	}
	return out
}

func TestSearchTitlesTagsAndDetails(t *testing.T) {
	s := mustOpen(t, searchDir(t))

	cases := []struct {
		name string
		req  SearchRequest
		want []string
	}{
		{
			name: "a title substring is case-insensitive",
			req:  SearchRequest{Query: "deploy"},
			// T-0042 is the item busySlot holds, and it carries the same title.
			// Backlog first, then the working slots, then done.
			want: []string{"T-0020:title", "T-0020:detail", "T-0020:detail", "T-0022:title", "T-0042:title"},
		},
		{
			name: "a tag match reports the item once, not once per tag",
			req:  SearchRequest{Query: "infra"},
			want: []string{"T-0020:tags", "T-0042:tags"},
		},
		{
			name: "a detail body match carries its own location",
			req:  SearchRequest{Query: "kubernetes"},
			want: []string{"T-0020:detail"},
		},
		{
			name: "fields narrow the search",
			req:  SearchRequest{Query: "deploy", Fields: []SearchField{FieldTitle}},
			want: []string{"T-0020:title", "T-0022:title", "T-0042:title"},
		},
		{
			name: "nothing matches nothing",
			req:  SearchRequest{Query: "no such string anywhere"},
			want: []string{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hits, err := s.Search(c.req)
			if err != nil {
				t.Fatal(err)
			}
			got := hitIDs(hits)
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("hit %d = %s, want %s", i, got[i], c.want[i])
				}
			}
		})
	}
}

// spec-tools.md §5.2: reports state and location per hit.
func TestSearchHitsCarryStateAndLocation(t *testing.T) {
	s := mustOpen(t, searchDir(t))

	hits, err := s.Search(SearchRequest{Query: "kubernetes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("want 1 hit, got %d", len(hits))
	}
	h := hits[0]
	if h.Item.State != StateBacklog || h.Item.Section != SectionReady {
		t.Errorf("state = %s/%s", h.Item.State, h.Item.Section)
	}
	if h.At.File != "details/T-0020.md" {
		t.Errorf("file = %q, want the detail file the line is actually in", h.At.File)
	}
	if h.At.Line == 0 {
		t.Error("a detail hit needs a line number to be navigable")
	}
	if h.Text != "Something about kubernetes." {
		t.Errorf("Text = %q", h.Text)
	}

	titleHits, err := s.Search(SearchRequest{Query: "Rotate", Fields: []SearchField{FieldTitle}})
	if err != nil {
		t.Fatal(err)
	}
	if len(titleHits) != 1 || titleHits[0].At.File != "backlog.md" || titleHits[0].At.Line == 0 {
		t.Errorf("a title hit should point at its own line: %+v", titleHits)
	}
}

// An item in a slot is searchable, and its hit points at the working file.
func TestSearchReachesTheWorkingSlot(t *testing.T) {
	s := mustOpen(t, searchDir(t))
	hits, err := s.Search(SearchRequest{Query: "Fix the deploy script", State: StateWorking})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("the item in the slot was not found")
	}
	for _, h := range hits {
		if h.Item.State != StateWorking {
			t.Errorf("State filter leaked a %s item", h.Item.State)
		}
	}
	if hits[0].At.File != "working.02.md" {
		t.Errorf("a working item's hit points at %q", hits[0].At.File)
	}
}

func TestSearchRegex(t *testing.T) {
	s := mustOpen(t, searchDir(t))

	hits, err := s.Search(SearchRequest{Query: `^Rotate .* token$`, Regex: true, Fields: []SearchField{FieldTitle}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Item.ID != "T-0021" {
		t.Errorf("regex search returned %v", hitIDs(hits))
	}

	// A regex is matched as written: case sensitivity is the caller's to ask for.
	hits, err = s.Search(SearchRequest{Query: "DEPLOY", Regex: true, Fields: []SearchField{FieldTitle}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Item.ID != "T-0022" {
		t.Errorf("a case-sensitive regex returned %v", hitIDs(hits))
	}
	hits, err = s.Search(SearchRequest{Query: "(?i)DEPLOY", Regex: true, Fields: []SearchField{FieldTitle}})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 {
		t.Errorf("(?i) should have matched every spelling, got %v", hitIDs(hits))
	}
}

func TestSearchRejectsBadQueries(t *testing.T) {
	s := mustOpen(t, searchDir(t))

	for _, q := range []string{"", "   "} {
		if _, err := s.Search(SearchRequest{Query: q}); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("Search(%q) should refuse, got %v", q, err)
		}
	}
	if _, err := s.Search(SearchRequest{Query: "([unclosed", Regex: true}); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("a bad regex should be ErrInvalidArgument, got %v", err)
	}
}

func TestSearchLimit(t *testing.T) {
	s := mustOpen(t, searchDir(t))
	hits, err := s.Search(SearchRequest{Query: "e", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Errorf("Limit 2 returned %d hits", len(hits))
	}
}

// The board's ?q= filter renders cards, so it wants items, not lines.
func TestSearchItemsDeduplicates(t *testing.T) {
	s := mustOpen(t, searchDir(t))
	hits, err := s.Search(SearchRequest{Query: "deploy"})
	if err != nil {
		t.Fatal(err)
	}
	items := SearchItems(hits)
	// Five hits over three items: T-0020 matches its title and two detail lines.
	if len(items) != 3 {
		t.Fatalf("want 3 distinct items, got %d from %d hits", len(items), len(hits))
	}
	seen := map[ID]bool{}
	for _, it := range items {
		if seen[it.ID] {
			t.Errorf("%s appears twice", it.ID)
		}
		seen[it.ID] = true
	}
}

// A detail: pointing at a file that is not there is I8's business, not a reason
// for search to fail.
func TestSearchWithAMissingDetailFile(t *testing.T) {
	dir := searchDir(t)
	if err := os.Remove(filepath.Join(dir, "details", "T-0020.md")); err != nil {
		t.Fatal(err)
	}
	hits, err := mustOpen(t, dir).Search(SearchRequest{Query: "deploy"})
	if err != nil {
		t.Fatalf("search failed on a directory with a broken detail pointer: %v", err)
	}
	if len(hits) == 0 {
		t.Error("the title matches should still be there")
	}
}
