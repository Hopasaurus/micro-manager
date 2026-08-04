package web

// T-0127 — the GUI renders refs as clickable cross-board links, resolved by
// board slug (plan-board-links.md decisions 4 and 7).
//
// Resolution is a lookup among the boards THIS service knows, never a path
// reconstructed from a slug. Three renderings, fixed by decision 4:
//
//	resolved  -> a clickable link addressed by the target board's projectId
//	             (routes keep projectId; the visible text is the slug:ID
//	             element). The testid names the slug and the target item,
//	             with the x- prefix the closed testid list of §5.1 demands
//	             for an implementation addition.
//	missing   -> no known board declares the slug, or the item is not in the
//	             resolved board: text with data-missing="true", never a dead
//	             link (§10 rule 5; a renumber by mm fix is decision 6's owned
//	             cost).
//	ambiguous -> several known boards declare the slug: text naming all of
//	             them, no guess.
//
// These tests drive custom boards written by writeRefsBoard — the shared
// corpus has no pair of boards whose slugs and items line up, and the
// resolution cases are the point of the fixture.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hopasaurus/micro-manager/mm"
)

// writeRefsBoard writes a minimal valid board at root/<sub>/micro-manager:
// a backlog declaring project and board slug, one Ready item per line given,
// an empty done file and two idle slots. It returns the board's path.
func writeRefsBoard(t *testing.T, root, sub, project, board string, items ...string) string {
	t.Helper()
	dir := filepath.Join(root, sub, "micro-manager")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("backlog.md", fmt.Sprintf(
		"---\ndoc: backlog\nversion: 1\nproject: %s\nboard: %s\nnext_id: T-0099\nupdated: 2026-08-02\n---\n\n# Backlog\n\n## Ready\n\n%s\n\n## Blocked\n\n## Someday\n",
		project, board, strings.Join(items, "\n")))
	write("done.md", "---\ndoc: done\nversion: 1\nupdated: 2026-08-02\n---\n\n# Done\n")
	slots := "---\ndoc: working\nversion: 1\nstatus: idle\nid: null\ntitle: null\nprio: null\ntags: null\ndetail: null\ncreated: null\nstarted: null\n---\n\n# Working\n\n## Task\n\n## Plan\n\n## Notes\n\n## Blockers\n"
	for i := 1; i <= 2; i++ {
		write(fmt.Sprintf("working.%02d.md", i), slots)
	}
	return dir
}

// refsServer serves the boards written under root.
func refsServer(t *testing.T, root string) *testServer {
	t.Helper()
	return newTestServerWith(t, func(o *Options) { o.StartDir = root })
}

// TestRefsResolveToLink: an item whose refs name a known board renders a
// clickable link to that board's projectId route, on the card and in the
// panel. The visible text stays the slug:ID element (decision 7).
func TestRefsResolveToLink(t *testing.T) {
	root := t.TempDir()
	alpha := writeRefsBoard(t, root, "alpha", "Alpha", "py",
		"- [ ] [T-0001] Target | created:2026-08-02")
	beta := writeRefsBoard(t, root, "beta", "Beta", "go",
		"- [ ] [T-0001] Source | refs:py:T-0001 | created:2026-08-02")
	ts := refsServer(t, root)

	alphaID, err := mm.ProjectID(alpha)
	if err != nil {
		t.Fatal(err)
	}
	betaID, err := mm.ProjectID(beta)
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + betaID + "/board").expectStatus(200).Body
	if !strings.Contains(body, `data-testid="x-item-T-0001-ref-py-T-0001"`) {
		t.Error("card ref element is missing its data-testid (slug + target item)")
	}
	if !strings.Contains(body, `href="/p/`+alphaID+`/item/T-0001"`) {
		t.Errorf("card ref is not a link to the target board's route /p/%s/item/T-0001", alphaID)
	}
	if !strings.Contains(body, ">py:T-0001</a>") {
		t.Error("card ref link does not show the slug:ID element as its text")
	}
	if strings.Contains(body, `data-testid="x-item-T-0001-ref-py-T-0001"`) &&
		(strings.Contains(body, `data-missing="true"`) || strings.Contains(body, `data-ambiguous="true"`)) {
		t.Error("a resolvable ref must not render as missing or ambiguous")
	}

	// The panel (item page) renders the same element with the same href.
	panel := ts.get("/p/" + betaID + "/item/T-0001").expectStatus(200).Body
	if !strings.Contains(panel, `data-testid="x-item-ref-py-T-0001"`) {
		t.Error("panel ref element is missing its data-testid")
	}
	if !strings.Contains(panel, `href="/p/`+alphaID+`/item/T-0001"`) {
		t.Errorf("panel ref is not a link to /p/%s/item/T-0001", alphaID)
	}
}

// TestRefsToUnknownBoardRenderMissing: a slug no known board declares renders
// as text with data-missing, never as a link (§10 rule 5 — a board on an
// unmounted drive is not a deleted project).
func TestRefsToUnknownBoardRenderMissing(t *testing.T) {
	root := t.TempDir()
	beta := writeRefsBoard(t, root, "beta", "Beta", "go",
		"- [ ] [T-0001] Source | refs:py:T-0001 | created:2026-08-02")
	ts := refsServer(t, root)

	betaID, err := mm.ProjectID(beta)
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + betaID + "/board").expectStatus(200).Body
	if !strings.Contains(body, `data-testid="x-item-T-0001-ref-py-T-0001"`) {
		t.Error("card ref element is missing")
	}
	if !strings.Contains(body, `data-missing="true"`) {
		t.Error("a ref to an unknown board must render data-missing")
	}
	if strings.Contains(body, ">py:T-0001</a>") {
		t.Error("a missing ref must render as text, never as a dead link")
	}

	panel := ts.get("/p/" + betaID + "/item/T-0001").expectStatus(200).Body
	if !strings.Contains(panel, `data-testid="x-item-ref-py-T-0001"`) || !strings.Contains(panel, `data-missing="true"`) {
		t.Error("panel must render the unknown-board ref as missing")
	}
	if strings.Contains(panel, ">py:T-0001</a>") {
		t.Error("panel missing ref must not be a link")
	}
}

// TestRefsToRenumberedItemRenderMissing: the board resolves but the item is
// not in it — the mm fix renumber case (decision 6's owned cost). Same
// rendering as an unknown board: data-missing, text, no link.
func TestRefsToRenumberedItemRenderMissing(t *testing.T) {
	root := t.TempDir()
	writeRefsBoard(t, root, "alpha", "Alpha", "py",
		"- [ ] [T-0001] Target | created:2026-08-02")
	beta := writeRefsBoard(t, root, "beta", "Beta", "go",
		"- [ ] [T-0001] Source | refs:py:T-0099 | created:2026-08-02")
	ts := refsServer(t, root)

	betaID, err := mm.ProjectID(beta)
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + betaID + "/board").expectStatus(200).Body
	if !strings.Contains(body, `data-missing="true"`) {
		t.Error("a ref to a renumbered item must render data-missing")
	}
	if strings.Contains(body, ">py:T-0099</a>") {
		t.Error("a ref to a missing item must not be a link")
	}
}

// TestRefsToAmbiguousSlugNameBothBoards: two known boards declare the slug;
// the element renders as text naming BOTH boards, with no guess (the mm fix
// tie discipline).
func TestRefsToAmbiguousSlugNameBothBoards(t *testing.T) {
	root := t.TempDir()
	writeRefsBoard(t, root, "alpha1", "Alpha", "py",
		"- [ ] [T-0001] Target | created:2026-08-02")
	writeRefsBoard(t, root, "alpha2", "Alpha Twin", "py",
		"- [ ] [T-0001] Other target | created:2026-08-02")
	beta := writeRefsBoard(t, root, "beta", "Beta", "go",
		"- [ ] [T-0001] Source | refs:py:T-0001 | created:2026-08-02")
	ts := refsServer(t, root)

	betaID, err := mm.ProjectID(beta)
	if err != nil {
		t.Fatal(err)
	}

	body := ts.get("/p/" + betaID + "/board").expectStatus(200).Body
	if !strings.Contains(body, `data-testid="x-item-T-0001-ref-py-T-0001"`) {
		t.Error("card ref element is missing")
	}
	if !strings.Contains(body, `data-ambiguous="true"`) {
		t.Error("a slug declared by two boards must render data-ambiguous")
	}
	if strings.Contains(body, ">py:T-0001</a>") {
		t.Error("an ambiguous ref must not guess and must not be a link")
	}

	panel := ts.get("/p/" + betaID + "/item/T-0001").expectStatus(200).Body
	if !strings.Contains(panel, `data-ambiguous="true"`) {
		t.Error("panel must render the ambiguous ref as ambiguous")
	}
	// The panel says in words which boards declare the slug.
	if !strings.Contains(panel, "Alpha") || !strings.Contains(panel, "Alpha Twin") {
		t.Error("panel must name both boards that declare the slug")
	}
	if strings.Contains(panel, ">py:T-0001</a>") {
		t.Error("panel ambiguous ref must not be a link")
	}
}

// TestRefsApiCarriesBoardAndRefs: the API carries the DATA — board on the
// directory summary, refs on each item — leaving resolution to a client with
// the tree-wide view.
func TestRefsApiCarriesBoardAndRefs(t *testing.T) {
	root := t.TempDir()
	alpha := writeRefsBoard(t, root, "alpha", "Alpha", "py",
		"- [ ] [T-0001] Target | created:2026-08-02")
	beta := writeRefsBoard(t, root, "beta", "Beta", "go",
		"- [ ] [T-0001] Source | refs:py:T-0001 | created:2026-08-02")
	ts := refsServer(t, root)

	alphaID, err := mm.ProjectID(alpha)
	if err != nil {
		t.Fatal(err)
	}
	betaID, err := mm.ProjectID(beta)
	if err != nil {
		t.Fatal(err)
	}

	var summary struct {
		OK     bool `json:"ok"`
		Result struct {
			Board string `json:"board"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + alphaID).expectStatus(200).json(&summary)
	if summary.Result.Board != "py" {
		t.Errorf("directory summary board = %q, want py", summary.Result.Board)
	}

	var items struct {
		OK     bool `json:"ok"`
		Result struct {
			Items []struct {
				ID   string   `json:"id"`
				Refs []string `json:"refs"`
			} `json:"items"`
		} `json:"result"`
	}
	ts.get("/api/v1/projects/" + betaID + "/items").expectStatus(200).json(&items)
	if len(items.Result.Items) != 1 || len(items.Result.Items[0].Refs) != 1 ||
		items.Result.Items[0].Refs[0] != "py:T-0001" {
		t.Errorf("item refs = %+v, want [py:T-0001]", items.Result.Items[0].Refs)
	}
}
