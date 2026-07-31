package web

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRenderer builds a renderer over a small template tree written for the
// test, so the parity rules are asserted on something whose expected output is
// obvious rather than on whatever the app shell happens to look like.
func testRenderer(t *testing.T, files map[string]string) *renderer {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := newRenderer(os.DirFS(dir), templateFuncs(), false)
	if err != nil {
		t.Fatalf("new renderer: %v", err)
	}
	return r
}

// project/architecture.md §4.3: a partial rendered on its own MUST be
// byte-identical to the same partial rendered inside a full page. htmx swaps
// fragments in, and a fragment that differs from its first-render form breaks
// the external suite in a way that only shows up after an interaction.
func TestFragmentParity(t *testing.T) {
	r := testRenderer(t, map[string]string{
		"layout.html": "{{define \"layout\"}}<!DOCTYPE html>\n<html><body>\n{{template \"content\" .}}\n</body></html>\n{{end}}",
		"board.html": `{{define "content"}}{{template "board" .}}{{end}}
{{define "board"}}<section data-testid="board" data-count="{{.Count}}">
{{range .Items}}{{template "item" .}}
{{end}}</section>{{end}}`,
		"partials/item.html": `{{define "item"}}<article data-testid="item-{{.ID}}" data-prio="{{.Prio}}">{{.Title}}</article>{{end}}`,
	})

	data := struct {
		Count int
		Items []struct{ ID, Prio, Title string }
	}{
		Count: 2,
		Items: []struct{ ID, Prio, Title string }{
			{"T-0001", "high", "First"},
			{"T-0002", "low", "Second & <escaped>"},
		},
	}

	var page, fragment bytes.Buffer
	if err := r.renderPage(&page, "board", data); err != nil {
		t.Fatal(err)
	}
	if err := r.renderFragment(&fragment, "board", "board", data); err != nil {
		t.Fatal(err)
	}

	if fragment.Len() == 0 {
		t.Fatal("the fragment rendered empty")
	}
	if !strings.Contains(page.String(), fragment.String()) {
		t.Errorf("the fragment is not byte-identical inside the page\n--- fragment ---\n%s\n--- page ---\n%s",
			fragment.String(), page.String())
	}

	// The same holds one level down: a partial inside the fragment.
	var item bytes.Buffer
	if err := r.renderFragment(&item, "board", "item", data.Items[0]); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fragment.String(), item.String()) {
		t.Errorf("a nested partial is not byte-identical:\n%s\nnot in\n%s", item.String(), fragment.String())
	}
}

// Two pages may define the same block name. That is what a layout needs, and it
// only works because each page gets its own template set.
func TestPagesDoNotCollide(t *testing.T) {
	r := testRenderer(t, map[string]string{
		"layout.html": `{{define "layout"}}<html>{{template "content" .}}</html>{{end}}`,
		"one.html":    `{{define "content"}}ONE{{end}}`,
		"two.html":    `{{define "content"}}TWO{{end}}`,
	})

	var one, two bytes.Buffer
	if err := r.renderPage(&one, "one", nil); err != nil {
		t.Fatal(err)
	}
	if err := r.renderPage(&two, "two", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(one.String(), "ONE") || strings.Contains(one.String(), "TWO") {
		t.Errorf("page one rendered %q", one.String())
	}
	if !strings.Contains(two.String(), "TWO") || strings.Contains(two.String(), "ONE") {
		t.Errorf("page two rendered %q", two.String())
	}
}

// A broken template is a startup failure, not a 500 the first time somebody
// opens the view it belongs to.
func TestBrokenTemplateFailsAtParse(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("layout.html", `{{define "layout"}}<html>{{template "content" .}}</html>{{end}}`)
	write("broken.html", `{{define "content"}}{{.Unclosed`)

	if _, err := newRenderer(os.DirFS(dir), templateFuncs(), false); err == nil {
		t.Fatal("a malformed template was accepted")
	} else if !strings.Contains(err.Error(), "broken") {
		t.Errorf("the error must name the template, got %q", err)
	}
}

func TestUnknownPageAndFragment(t *testing.T) {
	r := testRenderer(t, map[string]string{
		"layout.html": `{{define "layout"}}{{template "content" .}}{{end}}`,
		"one.html":    `{{define "content"}}ONE{{end}}`,
	})

	var buf bytes.Buffer
	if err := r.renderPage(&buf, "nope", nil); err == nil {
		t.Error("an unknown page rendered without error")
	}
	if err := r.renderFragment(&buf, "one", "nope", nil); err == nil {
		t.Error("an unknown fragment rendered without error")
	}
}

// spec-gui.md §4.1 rules 1 and 2: every reachable state is addressable, and
// loading a URI directly produces the same state as navigating to it. So a route
// serves a fragment to htmx and a whole page to a direct load.
func TestFragmentSelection(t *testing.T) {
	cases := []struct {
		name     string
		target   string
		headers  map[string]string
		fragment bool
	}{
		{name: "a direct load gets the page", target: "/p/x/board"},
		{name: "htmx gets a fragment", target: "/p/x/board",
			headers: map[string]string{"HX-Request": "true"}, fragment: true},
		{name: "an explicit ?fragment=1 gets a fragment", target: "/p/x/board?fragment=1", fragment: true},
		{name: "a history restore gets the whole page", target: "/p/x/board",
			headers: map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.target, nil)
			for k, v := range c.headers {
				req.Header.Set(k, v)
			}
			if got := wantsFragment(req); got != c.fragment {
				t.Errorf("wantsFragment = %v, want %v", got, c.fragment)
			}
		})
	}
}

// The template helpers exist so the DOM contract is written once rather than
// retyped in every template that renders a card.
func TestTemplateFuncs(t *testing.T) {
	r := testRenderer(t, map[string]string{
		"layout.html": `{{define "layout"}}{{template "content" .}}{{end}}`,
		// attr is used INSIDE a tag, which is the only context html/template
		// trusts an HTMLAttr in - and the only place a real template uses one.
		"f.html": `{{define "content"}}<i {{attr "data-x" .X}}>[{{yesno .B}}][{{pad .N 2}}][{{join .L ","}}]</i>{{end}}`,
	})

	var buf bytes.Buffer
	data := struct {
		X string
		B bool
		N int
		L []string
	}{X: "", B: true, N: 3, L: []string{"a", "b"}}
	if err := r.renderPage(&buf, "f", data); err != nil {
		t.Fatal(err)
	}
	if got, want := buf.String(), `<i >[true][03][a,b]</i>`; got != want {
		t.Errorf("rendered %q, want %q", got, want)
	}

	// A value present renders the whole attribute; empty renders nothing at all,
	// so an absent value never becomes data-x="" and changes what a test sees.
	data.X = "hello"
	buf.Reset()
	if err := r.renderPage(&buf, "f", data); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `<i data-x="hello">`) {
		t.Errorf("rendered %q", buf.String())
	}
}

// The layout the service ships with must parse. Every page added later inherits
// it, so a mistake here breaks every view at once.
func TestShippedTemplatesParse(t *testing.T) {
	if _, err := newRenderer(templatesFS(), templateFuncs(), false); err != nil {
		t.Fatalf("the embedded templates do not parse: %v", err)
	}
}
