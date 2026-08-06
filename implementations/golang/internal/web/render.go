package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"text/template/parse"

	"github.com/labstack/echo/v5"
)

// Rendering (project/architecture.md §4.3, spec-gui.md §4.1 and §5).
//
// Two rules drive every decision in this file.
//
// First, EVERY view state a user can reach must be addressable, and loading a
// URI directly must produce the same state as navigating to it (§4.1 rules 1 and
// 2). So every route that htmx swaps a fragment from must ALSO serve a whole
// page when asked without the HX-Request header. One handler, two renderings,
// chosen here rather than in each handler.
//
// Second, a partial rendered on its own must be byte-identical to the same
// partial rendered inside its page. htmx swaps fragments in, and a fragment that
// differs from its first-render form breaks the external conformance suite in a
// way that only shows up after an interaction. That is guaranteed by
// construction here: the fragment is the SAME template text in both cases, so a
// page render and a fragment render cannot drift.

// pageSuffix is the extension every template file carries.
const pageSuffix = ".html"

// layoutTemplate is the app shell, defined once and shared by every page.
const layoutTemplate = "layout"

// renderer is a template cache: one template set per page, each set carrying the
// layout, that page, and every shared partial.
//
// A set per page rather than one big set is what lets two pages define the same
// block name - "content" - without colliding, which is the whole reason a Go
// template layout works at all.
type renderer struct {
	mu    sync.RWMutex
	fsys  fs.FS
	funcs template.FuncMap
	sets  map[string]*template.Template

	// dev re-reads and re-parses on every render, so a template edit shows up
	// without rebuilding. It is never on in a released binary.
	dev bool
}

// newRenderer parses every page in fsys.
//
// Parsing eagerly is deliberate: a broken template is a startup failure, not a
// 500 the first time somebody opens the report view.
func newRenderer(fsys fs.FS, funcs template.FuncMap, dev bool) (*renderer, error) {
	r := &renderer{fsys: fsys, funcs: funcs, dev: dev, sets: map[string]*template.Template{}}
	if err := r.parse(); err != nil {
		return nil, err
	}
	return r, nil
}

// pages lists the page templates - the files at the root of the tree. Anything
// under partials/ is shared and belongs to every page.
func (r *renderer) pages() ([]string, error) {
	entries, err := fs.ReadDir(r.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, pageSuffix) {
			continue
		}
		if name == layoutTemplate+pageSuffix {
			continue
		}
		out = append(out, strings.TrimSuffix(name, pageSuffix))
	}
	sort.Strings(out)
	return out, nil
}

func (r *renderer) parse() error {
	pages, err := r.pages()
	if err != nil {
		return err
	}
	partials, err := fs.Glob(r.fsys, "partials/*"+pageSuffix)
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}

	sets := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		files := append([]string{layoutTemplate + pageSuffix, page + pageSuffix}, partials...)
		t, err := template.New(layoutTemplate+pageSuffix).Funcs(r.funcs).ParseFS(r.fsys, files...)
		if err != nil {
			return fmt.Errorf("template %s: %w", page, err)
		}
		if err := verifyReferences(t); err != nil {
			return fmt.Errorf("template %s: %w", page, err)
		}
		sets[page] = t
	}

	r.mu.Lock()
	r.sets = sets
	r.mu.Unlock()
	return nil
}

// verifyReferences reports a {{template "x"}} naming something the set does not
// define.
//
// Go resolves those at EXECUTION, not at parse. A page whose fragment reaches
// for a definition living in a DIFFERENT page's file set therefore parses
// perfectly and fails only when a user reaches the one route that renders it —
// board.html's panel-replace-oob referenced item-panel while item-panel was
// defined inside item.html, so every page loaded fine and "Save and add
// another" answered `no such template "item-panel"` (T-0103).
//
// Sets are built per page (layout + that page + every partial), which is what
// makes the mistake possible at all: a definition is shared only if it lives in
// partials/. Checking here keeps the promise this file already makes — a broken
// template is a startup failure, not a 500 somebody finds later.
func verifyReferences(t *template.Template) error {
	defined := map[string]bool{}
	for _, tpl := range t.Templates() {
		defined[tpl.Name()] = true
	}

	for _, tpl := range t.Templates() {
		if tpl.Tree == nil {
			continue
		}
		refs := map[string]bool{}
		collectTemplateRefs(tpl.Tree.Root, refs)
		for _, name := range sortedKeys(refs) {
			if !defined[name] {
				return fmt.Errorf("%q references undefined template %q "+
					"(move the definition into partials/ to share it across pages)",
					tpl.Name(), name)
			}
		}
	}
	return nil
}

// collectTemplateRefs walks a parse tree for {{template}} and {{block}} nodes.
// The name is always a literal, so this sees every reference the set can make.
func collectTemplateRefs(n parse.Node, out map[string]bool) {
	switch v := n.(type) {
	case nil:
		return
	case *parse.ListNode:
		if v == nil {
			return
		}
		for _, child := range v.Nodes {
			collectTemplateRefs(child, out)
		}
	case *parse.TemplateNode:
		out[v.Name] = true
	case *parse.IfNode:
		collectBranch(v.BranchNode, out)
	case *parse.RangeNode:
		collectBranch(v.BranchNode, out)
	case *parse.WithNode:
		collectBranch(v.BranchNode, out)
	}
}

func collectBranch(b parse.BranchNode, out map[string]bool) {
	collectTemplateRefs(b.List, out)
	collectTemplateRefs(b.ElseList, out)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lookup returns a page's template set, re-parsing first in dev mode.
func (r *renderer) lookup(page string) (*template.Template, error) {
	if r.dev {
		if err := r.parse(); err != nil {
			return nil, err
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.sets[page]
	if !ok {
		return nil, fmt.Errorf("template %q is not a page", page)
	}
	return t, nil
}

// renderPage writes the whole document: layout, with the page inside it.
func (r *renderer) renderPage(w io.Writer, page string, data any) error {
	t, err := r.lookup(page)
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(w, layoutTemplate, data)
}

// renderFragment writes one named template from a page's set.
//
// The name is a template defined by that page or by a shared partial -
// "content", "board", "item-panel". Because it is the same definition the page
// render reaches, the bytes cannot differ.
func (r *renderer) renderFragment(w io.Writer, page, fragment string, data any) error {
	t, err := r.lookup(page)
	if err != nil {
		return err
	}
	if t.Lookup(fragment) == nil {
		return fmt.Errorf("template %q defines no fragment %q", page, fragment)
	}
	return t.ExecuteTemplate(w, fragment, data)
}

// ---------------------------------------------------------------------------
// The handler side
// ---------------------------------------------------------------------------

// wantsFragment reports whether this request should get a fragment rather than a
// whole page.
//
// htmx sets HX-Request on every request it makes. HX-History-Restore-Request is
// the exception: htmx is rebuilding a page from history and needs the whole
// document back, so a fragment there would leave the user on a page with no
// shell. ?fragment=1 is the same ask spelled explicitly, which is what the
// polling and SSE-triggered refreshes of §2.3 use.
func wantsFragment(req *http.Request) bool {
	if req.Header.Get("HX-History-Restore-Request") == "true" {
		return false
	}
	if req.Header.Get("HX-Request") == "true" {
		return true
	}
	return req.URL.Query().Get("fragment") == "1"
}

// render writes a page or its fragment, chosen by the request.
//
// Rendering goes to a buffer first. A template that fails halfway would
// otherwise have already written a 200 and half a document, and the error
// handler could only append an error message to a broken page.
func (s *Server) render(c *echo.Context, status int, page, fragment string, data any) error {
	var buf bytes.Buffer

	var err error
	if fragment != "" && wantsFragment(c.Request()) {
		err = s.renderer.renderFragment(&buf, page, fragment, data)
	} else {
		err = s.renderer.renderPage(&buf, page, data)
	}
	if err != nil {
		return fmt.Errorf("render %s: %w", page, err)
	}
	return c.HTMLBlob(status, buf.Bytes())
}

// templateFuncs are the helpers every template may use.
//
// The repetitive attribute groups of §5 - an item card's data-* set, a column's
// - get their own functions as they arrive, so the DOM contract stays in one
// place instead of being retyped in every template that renders a card.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// attr renders a data-* attribute only when it has a value, so an empty
		// string never becomes data-thing="" and changes what a test sees.
		"attr": func(name, value string) template.HTMLAttr {
			if value == "" {
				return ""
			}
			return template.HTMLAttr(fmt.Sprintf(`%s="%s"`, name, template.HTMLEscapeString(value)))
		},
		// weekdays and ordinals are the option lists of the Wake-up group's
		// weekly controls (§5.6), in the one order the library's parser accepts.
		"weekdays": func() []string {
			return []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
		},
		"ordinals": func() []string {
			return []string{"first", "second", "third", "fourth", "last"}
		},
		// yesno renders the true/false strings the spec's data-* attributes use.
		// Go's default for a bool is the same text, but going through here makes
		// the intent explicit and survives a future change of spelling.
		"yesno": func(b bool) string {
			if b {
				return "true"
			}
			return "false"
		},
		// pad renders a slot number as the zero-padded form the DOM contract
		// uses: data-slot="01", dialog-wip-limit-slot-01 (§5.1).
		"pad": func(n, width int) string {
			if width < 2 {
				width = 2
			}
			return fmt.Sprintf("%0*d", width, n)
		},
		// dict builds a map so a template can pass more than one value to
		// another. Go templates take a single argument, and a card needs both
		// the item and the project it belongs to.
		"dict": func(pairs ...any) (map[string]any, error) {
			if len(pairs)%2 != 0 {
				return nil, fmt.Errorf("dict takes key/value pairs, got %d values", len(pairs))
			}
			out := make(map[string]any, len(pairs)/2)
			for i := 0; i < len(pairs); i += 2 {
				key, ok := pairs[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict key %v is not a string", pairs[i])
				}
				out[key] = pairs[i+1]
			}
			return out, nil
		},
		// withData re-points a view at different page data, so one page can
		// render two things that each expect to be ".Data" - the item panel
		// renders OVER the board, and §5.6 requires the board to stay in the DOM.
		"withData": func(v view, data any) view {
			v.Data = data
			return v
		},
		"join": strings.Join,
		"path": path.Join,
		// navItem builds one nav link's data. The href points at /projects when
		// no project is open, because §5.1 forbids dropping a required testid
		// conditionally: nav-board exists on every route whether or not there is
		// a board to go to.
		"navItem": func(v view, key, label string) navLink {
			href := "/projects"
			if v.App.Project != nil {
				href = fmt.Sprintf("/p/%s/%s", v.App.Project.ID, key)
			}
			// settings and about are SYSTEM routes, reachable without a project:
			// with one open they are project-scoped (/p/:id/settings) or global
			// (/about) respectively.
			if key == "settings" {
				href = "/settings"
				if v.App.Project != nil {
					href = fmt.Sprintf("/p/%s/settings", v.App.Project.ID)
				}
			}
			if key == "about" {
				href = "/about"
			}
			return navLink{Key: key, Label: label, Href: href, Current: v.App.Nav == key}
		},
	}
}
