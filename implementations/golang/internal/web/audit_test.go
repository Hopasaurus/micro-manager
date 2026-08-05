package web

// T-0070 — the contract audit.
//
// spec-gui.md §12 says a conforming implementation renders every required
// data-testid, exposes every CSS custom property, and serves every route, and
// that the external suite locates elements EXCLUSIVELY by those names. That
// suite is not a Go test and is not here. This test is the closest thing this
// module has to it: it parses the spec's own appendices — never a copy, because
// a copied list drifts and the drift is invisible — and audits the shipped
// implementation against them.
//
// When the spec is edited, this test is what tells us the implementation
// drifted. It runs in the default `go test ./...` and is deliberately not
// tagged or skipped.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// specGUI is the normative source. The path is relative to the package
// directory, which is where go test runs.
const specGUI = "../../../../project/spec-gui.md"

func readSpec(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(specGUI)
	if err != nil {
		t.Fatalf("read %s: %v — the audit parses the spec's own appendices and needs the file", specGUI, err)
	}
	return string(b)
}

// fenced extracts the fenced block between two headings. to may be empty to
// mean "to the end of the file".
func fenced(spec, from, to string) string {
	i := strings.Index(spec, from)
	if i < 0 {
		return ""
	}
	rest := spec[i+len(from):]
	j := strings.Index(rest, "```")
	if j < 0 {
		return ""
	}
	rest = rest[j+3:]
	k := strings.Index(rest, "```")
	if k < 0 {
		return ""
	}
	block := rest[:k]
	if to != "" {
		if m := strings.Index(block, to); m >= 0 {
			block = block[:m]
		}
	}
	return block
}

// expandBraces expands the appendix shorthand --mm-x-{a,b,c} into its list,
// recursing for nested braces (none today, but the shape costs nothing).
func expandBraces(tok string) []string {
	i := strings.Index(tok, "{")
	if i < 0 {
		return []string{tok}
	}
	j := strings.Index(tok[i:], "}")
	if j < 0 {
		return []string{tok}
	}
	j += i
	var out []string
	for _, item := range strings.Split(tok[i+1:j], ",") {
		out = append(out, expandBraces(tok[:i]+strings.TrimSpace(item)+tok[j+1:])...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Appendix A: the testid index

// parseTestids reads Appendix A. Continuation tokens (-title after
// board-column-<key>-header) are joined to the token they continue;
// parenthetical notes are skipped. The result is the raw list, patterns
// (containing <placeholders>) and concrete ids alike — nothing is resolved
// here, so a placeholder rename fails this test too.
func parseTestids(spec string) []string {
	block := fenced(spec, "## Appendix A", "## Appendix B")
	var out []string
	prev := ""
	inParen := false
	for _, tok := range strings.Fields(block) {
		if strings.Contains(tok, "(") {
			inParen = true
		}
		if inParen {
			if strings.Contains(tok, ")") {
				inParen = false
			}
			continue // a parenthetical note, not an id
		}
		if strings.HasPrefix(tok, "-") {
			// Continuation: -title continues board-column-<key>-header.
			if prev == "" || strings.LastIndex(prev, "-") < 0 {
				continue
			}
			base := prev[:strings.LastIndex(prev, "-")]
			tok = base + tok
		}
		out = append(out, tok)
		prev = tok
	}
	return out
}

func isPattern(id string) bool { return strings.Contains(id, "<") }

// auditViews renders every view that exists in this build against the fixture
// and returns the concatenated markup, so one Contains per required id is the
// whole assertion. Fragments that only exist after an interaction — a toast, a
// WIP-limit dialog, an import collision — are rendered by performing the
// interaction, which is also a test that the interaction works.
func auditViews(t *testing.T, ts *testServer, id string) string {
	t.Helper()
	var b []string
	add := func(path string, status int) {
		b = append(b, ts.get(path).expectStatus(status).Body)
	}

	add("/", 200)
	add("/projects", 200)
	add("/p/"+id+"/board", 200)
	// period=all, not the default period: the audit's question is whether
	// report-item-<ID> CAN render, and the default is the last complete ISO
	// week — which stops containing the fixture's done dates (newest is
	// 2026-07-24) as soon as the calendar moves past them. Asking for the
	// default here made this a test that passed in July and failed in August
	// (T-0140). Which period the default resolves to is report_test.go's job.
	add("/p/"+id+"/report?period=all&include-backlog=1", 200)
	add("/p/"+id+"/check", 200)
	add("/p/"+id+"/settings", 200)
	add("/settings", 200)
	add("/settings/theme", 200)
	add("/settings/themes", 200)
	add("/settings/themes/micro-manager", 200)
	add("/about", 200)

	// Panels: a ready item, a working item (whose slot carries subtasks), a
	// blocked item, and the add panel.
	add("/p/"+id+"/item/T-0001", 200)
	add("/p/"+id+"/item/T-0003", 200)
	add("/p/"+id+"/item/T-0004", 200)
	add("/p/"+id+"/new", 200)

	// The dialogs that have their own route.
	for _, name := range []string{"confirm-remove", "block", "finish"} {
		add("/p/"+id+"/dialog/"+name+"?item=T-0001", 200)
	}

	// The WIP-limit dialog: starting while the single slot is full.
	blocked := ts.post("/p/"+id+"/items/T-0001/start", "{}",
		"HX-Request", "true").expectStatus(409).Body
	b = append(b, blocked)

	// A toast, from a mutation that succeeds.
	toast := ts.post("/p/"+id+"/items/T-0001/note", "text=audit",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").expectStatus(200).Body
	b = append(b, toast)

	// The import collision dialog: import a theme, then import it again.
	themeJSON := `{"id":"audit-import","name":"Audit Import","appearance":"light",` +
		`"color":{"fg.default":"#111111","bg.base":"#ffffff","accent.fg":"#ffffff","accent.base":"#1257c9"}}`
	multipart := "--auditboundary\r\n" +
		"Content-Disposition: form-data; name=\"theme_file\"; filename=\"audit.json\"\r\n" +
		"Content-Type: application/json\r\n\r\n" + themeJSON + "\r\n--auditboundary--\r\n"
	ts.post("/settings/themes/import", multipart,
		"Content-Type", "multipart/form-data; boundary=auditboundary").expectStatus(200)
	collision := ts.post("/settings/themes/import", multipart,
		"Content-Type", "multipart/form-data; boundary=auditboundary").expectStatus(409).Body
	b = append(b, collision)

	// check-violation-<n> only renders when a directory is broken, and the
	// clean fixture never is. The check view of a broken fixture joins the
	// battery for that one pattern.
	broken := newTestServer(t, "broken-i9-title-drift")
	bid := projectIDOf(t, broken, broken.Dirs[0])
	b = append(b, broken.get("/p/"+bid+"/check").expectStatus(200).Body)

	// board-column-done-show-all only renders when the done column is
	// truncated, and the clean fixture's three done items never are. A
	// done-heavy board joins the battery for that one element.
	heavy := bigDoneFixture(t, t.TempDir(), 12, 25)
	heavyServer := serverOver(t, heavy, nil)
	hid := projectIDOf(t, heavyServer, heavy)
	b = append(b, heavyServer.get("/p/"+hid+"/board").expectStatus(200).Body)

	return strings.Join(b, "\n")
}

// patternInstances maps a placeholder pattern to the concrete testids it must
// produce when rendered against the fixture. Every pattern in Appendix A is
// here; an unhandled pattern fails the audit rather than being ignored.
func patternInstances(projectID string) map[string][]string {
	ops := []string{"start", "pause", "finish", "block", "unblock", "move", "move-top", "move-end", "note", "edit", "remove"}
	keys := []string{"someday", "ready", "blocked", "working", "done"}
	column := func(suffix string, skipAdd bool) []string {
		var out []string
		for _, k := range keys {
			if skipAdd && k == "working" {
				continue
			}
			out = append(out, "board-column-"+k+suffix)
		}
		return out
	}
	item := func(suffix string) []string { return []string{"item-T-0001" + suffix} }
	itemOps := func() []string {
		var out []string
		for _, op := range ops {
			out = append(out, "item-T-0001-action-"+op)
		}
		return out
	}

	return map[string][]string{
		"toast-<n>":                     {"toast-1"},
		"dialog-<name>":                 {"dialog-confirm-remove", "dialog-block", "dialog-finish", "dialog-wip-limit", "dialog-import-theme"},
		"project-card-<projectId>":      {"project-card-" + projectID, "project-card-name", "project-card-path", "project-card-wip", "project-card-favorite-toggle"},
		"board-column-<key>-header":     column("-header", false),
		"board-column-<key>-title":      column("-title", false),
		"board-column-<key>-count":      column("-count", false),
		"board-column-<key>-add":        column("-add", true),
		"board-column-<key>-body":       column("-body", false),
		"item-<ID>":                     item(""),
		"item-<ID>-title":               item("-title"),
		"item-<ID>-id":                  item("-id"),
		"item-<ID>-prio":                item("-prio"),
		"item-<ID>-tags":                item("-tags"),
		"item-<ID>-tag-<tag>":           {"item-T-0001-tag-infra", "item-T-0001-tag-ci"},
		"item-<ID>-detail-indicator":    item("-detail-indicator"),
		"item-<ID>-menu":                item("-menu"),
		"item-<ID>-action-<operation>":  itemOps(),
		"item-field-<field>":            {"item-field-title", "item-field-prio", "item-field-tags", "item-field-blocked", "item-field-detail"},
		"item-action-<operation>":       {"item-action-start"},
		"settings-scan-root-<n>":        {"settings-scan-root-0"},
		"settings-scan-root-<n>-remove": {"settings-scan-root-0-remove"},
		"projects-root-<n>":             {"projects-root-0"},
	}
}

func TestAuditTestids(t *testing.T) {
	spec := readSpec(t)
	ids := parseTestids(spec)
	if len(ids) < 80 {
		t.Fatalf("Appendix A parsed to %d ids; the parser is broken", len(ids))
	}

	ts := newTestServer(t, "clean-full")
	// Pin the clock for the same reason reportServer does: the audit renders
	// every view in the build, several of which resolve dates relative to
	// today, and a test that drifts with the calendar is not a test.
	ts.registry.now = func() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }
	id := projectIDOf(t, ts, ts.Dirs[0])
	all := auditViews(t, ts, id)

	instances := patternInstances(id)

	// Dynamic patterns: the concrete form is decided by the fixture and the
	// calendar (report group keys, subtask numbering, violation numbering), so
	// the audit requires the FAMILY to render rather than a fixed instance.
	dynamic := map[string]string{
		"report-group-<key>":  "report-group-",
		"report-item-<ID>":    "report-item-",
		"subtask-<n>":         "subtask-",
		"check-violation-<n>": "check-violation-",
	}

	for _, tid := range ids {
		if !isPattern(tid) {
			if tid == "drop-placeholder" {
				continue // created by mm.js during a drag; checked below
			}
			if !strings.Contains(all, `data-testid="`+tid+`"`) {
				t.Errorf("Appendix A: missing data-testid %q in every rendered view", tid)
			}
			continue
		}
		if prefix, ok := dynamic[tid]; ok {
			if !strings.Contains(all, `data-testid="`+prefix) {
				t.Errorf("Appendix A: no %s<key> rendered anywhere", prefix)
			}
			continue
		}
		want, ok := instances[tid]
		if !ok {
			t.Errorf("Appendix A: pattern %q has no instance table entry — the audit must resolve every pattern", tid)
			continue
		}
		for _, concrete := range want {
			if !strings.Contains(all, `data-testid="`+concrete+`"`) {
				t.Errorf("Appendix A: pattern %q must render %q, which is missing from every view", tid, concrete)
			}
		}
	}

	// drop-placeholder exists only while a drag is in flight; it is created by
	// mm.js, never by a template. The contract is that the JS creates it under
	// that exact testid.
	js := ts.get("/static/mm.js").expectStatus(200).Body
	if !strings.Contains(js, "drop-placeholder") {
		t.Error("Appendix A: drop-placeholder is not created by mm.js")
	}
}

// ---------------------------------------------------------------------------
// Appendix C: the route index

func parseRoutes(spec string) []string {
	block := fenced(spec, "## Appendix C", "")
	// The braces span a newline with alignment padding between the items
	// ({move,start,pause,finish,\n                 block,...}), so whitespace
	// inside a brace pair is collapsed before tokenizing; otherwise the
	// continuation would split into a token that does not start with "/" and
	// vanish.
	brace := regexp.MustCompile(`\{[^{}]*\}`)
	block = brace.ReplaceAllStringFunc(block, func(s string) string {
		return regexp.MustCompile(`\s+`).ReplaceAllString(s, "")
	})
	var out []string
	for _, tok := range strings.Fields(block) {
		if !strings.HasPrefix(tok, "/") {
			continue // the "view" and "api" labels
		}
		out = append(out, expandBraces(tok)...)
	}
	return out
}

func TestAuditRoutes(t *testing.T) {
	spec := readSpec(t)
	appendix := parseRoutes(spec)
	if len(appendix) < 25 {
		t.Fatalf("Appendix C parsed to %d routes; the parser is broken", len(appendix))
	}

	ts := newTestServer(t, "clean-full")
	registered := map[string]bool{}
	for _, r := range ts.Server.echo.Router().Routes() {
		registered[r.Path] = true
	}

	var missing []string
	for _, path := range appendix {
		if !registered[path] {
			missing = append(missing, path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("Appendix C: %d routes are not registered: %s", len(missing), strings.Join(missing, ", "))
	}

	// The half that matters more (§4: "MUST NOT add a route that shadows one
	// below"). A registered route shadows an appendix route when it is at least
	// as general — a parameter where the spec names a fixed segment. Echo's
	// static-over-param priority makes most shadows harmless at runtime, but
	// they still mean the route table no longer says what the spec says.
	for _, r := range ts.Server.echo.Router().Routes() {
		if registered[r.Path] && containsPath(appendix, r.Path) {
			continue
		}
		for _, ap := range appendix {
			if shadows(r.Path, ap) {
				t.Errorf("registered route %s shadows appendix route %s", r.Path, ap)
			}
		}
	}
}

func containsPath(paths []string, p string) bool {
	for _, x := range paths {
		if x == p {
			return true
		}
	}
	return false
}

func isParam(seg string) bool { return strings.HasPrefix(seg, ":") || seg == "*" }

// shadows reports whether r matches every URL ap matches, with a wildcard where
// ap has a fixed segment — the shape of a route stealing another's traffic.
func shadows(r, ap string) bool {
	rs, as := strings.Split(r, "/"), strings.Split(ap, "/")
	if len(rs) != len(as) {
		return false
	}
	hasWildcard := false
	for i := range rs {
		if rs[i] == as[i] {
			continue
		}
		if isParam(rs[i]) && !isParam(as[i]) {
			hasWildcard = true
			continue
		}
		return false
	}
	return hasWildcard
}

// ---------------------------------------------------------------------------
// Appendix B: the custom property index

func parseProperties(spec string) []string {
	block := fenced(spec, "## Appendix B", "## Appendix C")
	var out []string
	for _, tok := range strings.Fields(block) {
		out = append(out, expandBraces(tok)...)
	}
	return out
}

func TestAuditCustomProperties(t *testing.T) {
	spec := readSpec(t)
	appendix := parseProperties(spec)
	if len(appendix) < 70 {
		t.Fatalf("Appendix B parsed to %d properties; the parser is broken", len(appendix))
	}

	ts := newTestServer(t, "clean-full")
	body := ts.get("/p/unknown/board").expectStatus(404).Body
	style := regexp.MustCompile(`<style>(.*?)</style>`).FindStringSubmatch(body)
	if style == nil {
		t.Fatal("no style block on the app root")
	}

	// Every name the appendix lists is defined in the style block.
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(--mm-[a-z0-9-]+):`).FindAllStringSubmatch(style[1], -1) {
		defined[m[1]] = true
	}
	for _, name := range appendix {
		if !defined[name] {
			t.Errorf("Appendix B: %s is not defined in the app-root style block", name)
		}
	}
	// And the reverse: the implementation must not invent properties the spec
	// does not name (spec-gui.md §12 rule 4: styled exclusively through them).
	for name := range defined {
		if !containsPath(appendix, name) {
			t.Errorf("Appendix B: %s is emitted but not in the index", name)
		}
	}

	// §12 rule 4, the second half: mm.css contains no literal colour. The
	// theme is the only place a colour may come from.
	css := ts.get("/static/mm.css").expectStatus(200).Body
	if m := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|rgb\(|hsl\(`).FindString(css); m != "" {
		t.Errorf("mm.css contains a literal colour (%s); all colours must resolve through theme tokens", m)
	}
}

// ---------------------------------------------------------------------------
// Fragment parity (architecture.md §4.3): a partial rendered on its own MUST
// be byte-identical to the same partial rendered inside a full page.

func TestAuditFragmentParity(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	// Warm up: a full page load records the project in recent, and the shell
	// fragment carries the recent list. Fetch the fragment first and the page
	// second would compare two different lists.
	ts.get("/p/" + id + "/board").expectStatus(200)

	page := ts.get("/p/" + id + "/board").expectStatus(200).Body
	shell := ts.get("/p/"+id+"/shell", "HX-Request", "true").expectStatus(200).Body
	status := ts.get("/p/"+id+"/status", "HX-Request", "true").expectStatus(200).Body
	boardFragment := ts.get("/p/"+id+"/board", "HX-Request", "true").expectStatus(200).Body

	// The shell is the whole app element, including the inlined theme style:
	// this is the sse:theme swap target (§4.5).
	if !strings.Contains(page, shell) {
		t.Error("the shell fragment is not byte-identical inside the page")
	}
	if !strings.Contains(page, status) {
		t.Errorf("the status fragment is not byte-identical inside the page:\n%s", status)
	}
	if !strings.Contains(page, boardFragment) {
		t.Error("the board fragment is not byte-identical inside the page")
	}
}

// TestAuditHtmxTargets — every hx-target in a rendered FULL page must resolve
// to an element in that page.
//
// htmx resolves a trigger's swap target by walking up from the element to the
// closest ancestor carrying hx-target; absent one, the element itself is the
// target. A target that matches nothing makes the request abort with
// htmx:targetError before anything is swapped — the page looks inert. This
// caught the T-0084 class of bug, where the shell carried hx-target="#app"
// (intended for the theme trigger) with no element ever bearing id="app", and
// every descendant SSE refresh trigger inherited it and died on it.
//
// Relative forms ("this", "closest …", "find …") are skipped: they resolve
// against the element carrying the attribute, not the page.
func TestAuditHtmxTargets(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	pages := []string{
		"/",
		"/projects",
		"/p/" + id + "/board",
		"/p/" + id + "/report?include-backlog=1",
		"/p/" + id + "/check",
		"/p/" + id + "/settings",
		"/settings",
		"/settings/theme",
		"/settings/themes",
		"/settings/themes/micro-manager",
		"/about",
		"/p/" + id + "/item/T-0001",
		"/p/" + id + "/new",
	}

	targetRe := regexp.MustCompile(`hx-target="([^"]+)"`)
	for _, path := range pages {
		body := ts.get(path).expectStatus(200).Body
		for _, m := range targetRe.FindAllStringSubmatch(body, -1) {
			target := m[1]
			if target == "this" || strings.HasPrefix(target, "closest ") || strings.HasPrefix(target, "find ") {
				continue
			}
			if !hxTargetResolves(body, target) {
				t.Errorf("%s: hx-target=%q matches no element in the page; "+
					"every trigger that inherits it aborts with htmx:targetError", path, target)
			}
		}
	}
}

// hxTargetResolves is a string-level stand-in for querySelector that covers
// the selectors this implementation actually writes: #id, and [attr='v'] /
// [attr=\"v\"]. Anything else is an unknown form and passes, so the check
// never fails on a selector it cannot reason about — only on one it can prove
// matches nothing.
func hxTargetResolves(page, target string) bool {
	// The hx-target attribute VALUE itself contains the selector text, so a
	// bare Contains search would match the very attribute it came from (a card
	// with hx-target="[data-testid='item-panel-root']" "satisfies" that
	// selector even when no such element exists). Strip every hx-target
	// attribute before searching for the element it names.
	html := regexp.MustCompile(`hx-target="[^"]*"`).ReplaceAllString(page, "")

	if strings.HasPrefix(target, "#") {
		rest := strings.TrimPrefix(target, "#")
		// The leading [\\s>] keeps data-testid="app" from satisfying #app: a
		// real id attribute is preceded by whitespace or a tag boundary.
		re := regexp.MustCompile(`[\s>]id="` + regexp.QuoteMeta(rest) + `"`)
		return re.MatchString(html) || regexp.MustCompile(`[\s>]id='`+regexp.QuoteMeta(rest)+`'`).MatchString(html)
	}
	for _, re := range []string{
		`^\[([a-zA-Z][a-zA-Z0-9-]*)="([^"]*)"\]$`,
		`^\[([a-zA-Z][a-zA-Z0-9-]*)='([^']*)'\]$`,
	} {
		if m := regexp.MustCompile(re).FindStringSubmatch(target); m != nil {
			attr, val := m[1], m[2]
			return strings.Contains(html, attr+`="`+val+`"`) || strings.Contains(html, attr+`='`+val+`'`)
		}
	}
	return true // unknown selector form: do not fail what we cannot reason about
}

// TestAuditNoInheritedSwap — an element must never rely on an ANCESTOR's
// hx-swap. hx-swap is inherited the same way hx-target is, and an inherited
// outerHTML turns every innerHTML-into-a-container swap into a container-
// REPLACING swap: the dialog-root div was destroyed by the confirm-dialog
// GET this way, and the item-panel container by the card click before the
// card anchor got its own hx-swap. The app root's shell swap needs
// outerHTML, which is exactly why it also carries hx-disinherit="*": the
// boundary means descendants see the default innerHTML again.
//
// The rule enforced here: every element with a trigger attribute must either
// declare its own hx-swap or resolve to the default innerHTML. An inherited
// non-default swap is a bug.
func TestAuditNoInheritedSwap(t *testing.T) {
	ts := newTestServer(t, "clean-full")
	id := projectIDOf(t, ts, ts.Dirs[0])

	pages := []string{
		"/",
		"/projects",
		"/p/" + id + "/board",
		"/p/" + id + "/report?include-backlog=1",
		"/p/" + id + "/check",
		"/p/" + id + "/settings",
		"/settings",
		"/settings/theme",
		"/settings/themes",
		"/settings/themes/micro-manager",
		"/about",
		"/p/" + id + "/item/T-0001",
		"/p/" + id + "/new",
	}

	// A rendered page's elements with their own hx-swap / hx-disinherit /
	// trigger attribute, indexed by document order.
	type el struct {
		tag        string
		swap       string
		disinherit string
		trigger    bool
		ownSwap    bool
	}
	for _, path := range pages {
		body := ts.get(path).expectStatus(200).Body
		for _, bad := range inheritedSwaps(t, body) {
			t.Errorf("%s: %s", path, bad)
		}
	}
}

// inheritedSwaps returns a description of every trigger-bearing element in
// page whose effective hx-swap is inherited from an ancestor. It walks the
// rendered HTML with a stack-based scan good enough for the templates this
// implementation writes: quoted attributes, void elements, and <style>/
// <script> raw-text content.
func inheritedSwaps(t *testing.T, page string) []string {
	t.Helper()
	var out []string

	attrRe := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9-]*)="([^"]*)"`)
	tagRe := regexp.MustCompile(`<(/)?([a-zA-Z][a-zA-Z0-9]*)([^>]*)>`)
	void := map[string]bool{
		"area": true, "base": true, "br": true, "col": true, "embed": true,
		"hr": true, "img": true, "input": true, "link": true, "meta": true,
		"source": true, "track": true, "wbr": true,
	}

	type frame struct {
		tag  string
		swap string // own hx-swap, "" if none
		di   string // own hx-disinherit, "" if none
	}
	var stack []frame
	triggers := []string{"hx-get", "hx-post", "hx-delete", "hx-put", "hx-patch"}

	i := 0
	for i < len(page) {
		loc := tagRe.FindStringSubmatchIndex(page[i:])
		if loc == nil {
			break
		}
		_, end := i+loc[0], i+loc[1]
		// RE2 reports an optional group that matches empty as -1 (not as
		// start==end), so the closing-slash group needs an explicit check.
		closing := ""
		if loc[2] >= 0 {
			closing = page[i+loc[2] : i+loc[3]]
		}
		tagName := page[i+loc[4] : i+loc[5]]
		attrs := page[i+loc[6] : i+loc[7]]

		if tagName == "style" || tagName == "script" {
			if closing == "" {
				// Skip to the matching close; the content may contain '>'.
				if c := strings.Index(page[end:], "</"+tagName+">"); c >= 0 {
					end = end + c + len(tagName) + 3
				}
			}
			i = end
			continue
		}

		if closing != "" {
			for len(stack) > 0 && stack[len(stack)-1].tag != tagName {
				stack = stack[:len(stack)-1]
			}
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i = end
			continue
		}

		var swap, di string
		trigger := false
		for _, m := range attrRe.FindAllStringSubmatch(attrs, -1) {
			switch m[1] {
			case "hx-swap":
				swap = m[2]
			case "hx-disinherit":
				di = m[2]
			}
			for _, tr := range triggers {
				if m[1] == tr {
					trigger = true
				}
			}
		}

		if trigger {
			// Effective swap: own, else nearest ancestor whose hx-disinherit
			// does not cover hx-swap, else the htmx default innerHTML.
			eff := "innerHTML"
			for s := len(stack) - 1; s >= 0; s-- {
				f := stack[s]
				if covers(f.di, "hx-swap") {
					break // hx-disinherit boundary: stop walking up
				}
				if f.swap != "" {
					eff = f.swap
					break
				}
			}
			if eff != "innerHTML" && swap == "" {
				out = append(out, fmt.Sprintf("<%s> relies on an ancestor's hx-swap=%q; it must declare its own or the swap inherits outerHTML and replaces its container",
					tagName, eff))
			}
		}

		if swap != "" || di != "" {
			stack = append(stack, frame{tag: tagName, swap: swap, di: di})
		} else if !void[tagName] {
			stack = append(stack, frame{tag: tagName})
		}
		i = end
	}
	return out
}

// covers reports whether an hx-disinherit value covers attribute name: "*" or
// a space-separated list containing it.
func covers(di, name string) bool {
	if di == "" {
		return false
	}
	for _, w := range strings.Fields(di) {
		if w == "*" || w == name {
			return true
		}
	}
	return false
}
