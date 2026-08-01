package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T-0114: the library honours the directory's declared ID grammar
// (spec-file-format.md §3.3.2). These tests build a non-default directory by
// hand; they deliberately add nothing to testdata/, because the check.sh
// lockstep (T-0116) is what will exercise both validators over one shared
// non-default corpus.

// customBacklog mirrors sampleBacklog in the MM/3 grammar: same item numbers
// and positions, next_id at MM-011, both keys declared.
const customBacklog = `---
doc: backlog
version: 1
project: Sample One
next_id: MM-011
id_prefix: MM
id_width: 3
updated: 2026-07-29
---

# Backlog

Prose that must be ignored.

## Ready

- [ ] [MM-001] First | prio:med | tags:example | created:2026-07-29
- [ ] [MM-005] Second | prio:high | created:2026-07-29

## Blocked

- [ ] [MM-002] Waiting | prio:low | created:2026-07-29 | blocked:on a thing

## Someday

- [ ] [MM-003] Maybe | prio:low | created:2026-07-29
`

// customDone mirrors sampleDone in the MM/3 grammar.
const customDone = `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [MM-010] Newest | created:2026-07-01 | done:2026-07-23 | outcome:shipped
- [x] [MM-009] Older | created:2026-06-01 | done:2026-07-02 | outcome:cancelled

## 2026-06

<!-- a comment -->

- [x] [MM-008] Last month | created:2026-05-01 | done:2026-06-30 | outcome:obsolete
`

// validateCustomDir is validateDir over the MM/3 corpus.
func validateCustomDir(t *testing.T, files map[string]string) []Violation {
	t.Helper()
	dir := newDir(t, map[string]string{"backlog.md": customBacklog, "done.md": customDone})
	for k, v := range files {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := mustOpen(t, dir)
	vs, err := s.Validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	return vs
}

// The declaration rides on the directory model: readers MUST read it before
// interpreting any ID (rule 4), and a front end needs it to render and to
// resolve ID arguments.
func TestDirectoryReportsDeclaredGrammar(t *testing.T) {
	s := mustOpen(t, newDir(t, map[string]string{
		"backlog.md": customBacklog, "done.md": customDone}))
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.IDPrefix != "MM" || d.IDWidth != 3 {
		t.Errorf("grammar = %q/%d, want MM/3", d.IDPrefix, d.IDWidth)
	}
	if d.NextID != "MM-011" {
		t.Errorf("next_id = %q", d.NextID)
	}

	// A directory without the keys reports the defaults - the format's rule 6
	// promise that absent keys are byte-identical to spec version 1.
	s2 := mustOpen(t, newDir(t, nil))
	d2, err := s2.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d2.IDPrefix != "T" || d2.IDWidth != 4 {
		t.Errorf("default grammar = %q/%d, want T/4", d2.IDPrefix, d2.IDWidth)
	}
}

// Grammar is the typed accessor front ends parse ID arguments against. It is
// the directory's declared grammar with the same defaults Directory reports.
func TestStoreGrammar(t *testing.T) {
	s := mustOpen(t, newDir(t, map[string]string{
		"backlog.md": customBacklog, "done.md": customDone}))
	g, err := s.Grammar()
	if err != nil {
		t.Fatal(err)
	}
	if g != (IDGrammar{Prefix: "MM", Width: 3}) {
		t.Errorf("grammar = %+v, want MM/3", g)
	}
	// Parsing against the declared grammar is what a front end does with an ID
	// argument: MM-001 resolves, T-0001 does not.
	if id, err := g.ParseID("MM-001"); err != nil || id != "MM-001" {
		t.Errorf("ParseID(MM-001) = %q, %v", id, err)
	}
	if _, err := g.ParseID("T-0001"); err == nil {
		t.Error("ParseID(T-0001) should fail against the MM/3 grammar")
	}

	s2 := mustOpen(t, newDir(t, nil))
	g2, err := s2.Grammar()
	if err != nil {
		t.Fatal(err)
	}
	if g2 != DefaultIDGrammar() {
		t.Errorf("default grammar = %+v, want T/4", g2)
	}
}

// Allocation zero-pads to the declared width and next_id increments in the
// declared space.
func TestAddAllocatesInDeclaredGrammar(t *testing.T) {
	s := mustOpen(t, newDir(t, map[string]string{
		"backlog.md": customBacklog, "done.md": customDone}))

	a, _, err := s.Add(AddRequest{Title: "First new"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "MM-011" {
		t.Errorf("first add = %s, want MM-011", a.ID)
	}
	b, _, err := s.Add(AddRequest{Title: "Second new"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID != "MM-012" {
		t.Errorf("second add = %s, want MM-012", b.ID)
	}
	if _, err := s.Get("MM-011"); err != nil {
		t.Errorf("MM-011 should resolve: %v", err)
	}
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != "MM-013" {
		t.Errorf("next_id = %q, want MM-013", d.NextID)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after adds:\n%s", violationMessages(vs))
	}
}

// The task's verify: an add/start/finish/search round trip with MM-001-style
// IDs.
func TestCustomGrammarLifecycle(t *testing.T) {
	dir := newDir(t, map[string]string{
		"backlog.md": customBacklog, "done.md": customDone})
	s := mustOpen(t, dir)

	it, _, err := s.Add(AddRequest{Title: "Round trip", Prio: PrioHigh}, today)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Start(it.ID, StartRequest{}, today); err != nil {
		t.Fatalf("start: %v", err)
	}
	got, err := s.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != StateWorking {
		t.Errorf("state = %q, want working", got.State)
	}
	if _, _, err := s.Finish(it.ID, FinishRequest{Note: "shipped"}, today); err != nil {
		t.Fatalf("finish: %v", err)
	}
	hits, err := s.Search(SearchRequest{Query: "Round trip"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatalf("search should find the finished item, got %v", hits)
	}
	for _, h := range hits {
		if h.Item.ID != "MM-011" {
			t.Errorf("search hit for a foreign id: %s", h.Item.ID)
		}
	}
	// Finish wrote its note into a detail file named in the declared grammar.
	if _, err := os.Stat(filepath.Join(dir, "details/MM-011.md")); err != nil {
		t.Errorf("detail file should be details/MM-011.md: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after lifecycle:\n%s", violationMessages(vs))
	}
}

// A directory declaring MM/3 rejects mm-001 (case deviation), M-001 (wrong
// prefix) and MM-1 / MM-0001 (wrong width) - matching is exact, and one
// grammar per directory means a foreign ID is a malformed line.
func TestCustomGrammarRejectsForeignIDs(t *testing.T) {
	g := IDGrammar{Prefix: "MM", Width: 3}
	for _, line := range []string{
		"- [ ] [mm-001] Lowercase | prio:med",
		"- [ ] [M-001] Short prefix | prio:med",
		"- [ ] [MM-1] Short digits | prio:med",
		"- [ ] [MM-0001] Long digits | prio:med",
		"- [ ] [T-001] Default grammar | prio:med",
	} {
		if isItemLineG(line, g) {
			t.Errorf("isItemLineG(%q) should be false", line)
		}
		if _, err := parseItemLineG("backlog.md", 19, line, g); err == nil {
			t.Errorf("parseItemLineG(%q) should fail", line)
		}
	}
	// Inside a real directory the same lines are reported, not read.
	for _, pair := range [][2]string{
		{"- [ ] [MM-001] First", "- [ ] [mm-001] First"}, // case deviation
		{"- [ ] [MM-001] First", "- [ ] [M-001] First"},  // wrong prefix
		{"- [ ] [MM-001] First", "- [ ] [MM-1] First"},   // wrong width
	} {
		vs := validateCustomDir(t, map[string]string{
			"backlog.md": strings.Replace(customBacklog, pair[0], pair[1], 1)})
		if !hasViolation(vs, "format", "malformed item line") {
			t.Errorf("replacing %q with %q should be reported:\n%s", pair[0], pair[1], violationMessages(vs))
		}
	}
}

// I2 runs in the declared counter space: next_id must be an ID of the declared
// grammar, and the below-next_id comparison uses its digits.
func TestValidatorUsesDeclaredGrammarForNextID(t *testing.T) {
	// A next_id in the default grammar inside a MM/3 directory is I2.
	vs := validateCustomDir(t, map[string]string{
		"backlog.md": strings.Replace(customBacklog, "next_id: MM-011", "next_id: T-0011", 1)})
	if !hasViolation(vs, "I2", "next_id is not a MM-### id") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}

	// next_id below an item is still I2, compared digit-wise in the declared
	// space - MM-009 is a bigger number than T-0009 would be, and the check
	// must not care about the prefix.
	vs = validateCustomDir(t, map[string]string{
		"backlog.md": strings.Replace(customBacklog, "next_id: MM-011", "next_id: MM-009", 1)})
	if !hasViolation(vs, "I2", "is at or above next_id (MM-009)") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

// Working-file frontmatter carries the same grammar: an id from another
// grammar in a slot is an I4 finding, not a silent misread.
func TestWorkingFileUsesDeclaredGrammar(t *testing.T) {
	busy := strings.Replace(busySlot, "id: T-0042", "id: MM-007", 1)
	vs := validateCustomDir(t, map[string]string{
		"working.01.md": strings.Replace(busy, "detail: details/T-0042.md", "detail: null", 1)})
	if len(vs) != 0 {
		t.Errorf("a MM/3 working file should be clean, got:\n%s", violationMessages(vs))
	}

	// The default-grammar ID in the same slot is rejected.
	vs = validateCustomDir(t, map[string]string{"working.01.md": busySlot})
	if !hasViolation(vs, "I4", "id is not a MM-### id: T-0042") {
		t.Errorf("got:\n%s", violationMessages(vs))
	}
}

// A width outside the RECOMMENDED 3-6 range warns and never fails: the
// directory is valid, --check passes, and Add still works.
func TestWidthOutsideRecommendedWarnsNeverFails(t *testing.T) {
	backlog2 := `---
doc: backlog
version: 1
project: Narrow
next_id: X-05
id_prefix: X
id_width: 2
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [X-01] One | prio:med | created:2026-07-29
- [ ] [X-04] Four | prio:med | created:2026-07-29

## Blocked

## Someday
`
	done2 := `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [X-03] Closed | created:2026-06-01 | done:2026-07-02 | outcome:shipped
`
	dir := newDir(t, map[string]string{"backlog.md": backlog2, "done.md": done2})
	s := mustOpen(t, dir)

	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("width 2 must not fail --check:\n%s", violationMessages(vs))
	}
	_, warns, err := s.ValidateWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "3-6") {
		t.Errorf("want one width warning, got %v", warns)
	}

	it, _, err := s.Add(AddRequest{Title: "Fifth"}, today)
	if err != nil {
		t.Fatalf("add must not be blocked by a warning: %v", err)
	}
	if it.ID != "X-05" {
		t.Errorf("add = %s, want X-05", it.ID)
	}
}

// A width above the shared 15 cap (§3.3.2 rule 3, T-0120) is a format
// violation, never a warning: at 16 digits the narrowest readers silently
// round, so every implementation refuses uniformly. The default grammar
// stands in, so the rest of the directory still parses and the declaration is
// the only finding.
func TestWidthAboveCapIsAViolation(t *testing.T) {
	backlog16 := `---
doc: backlog
version: 1
project: Wide
next_id: T-0005
id_width: 16
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-0001] One | prio:med | created:2026-07-29

## Blocked

- [ ] [T-0002] Two | created:2026-07-29 | blocked:on a thing

## Someday

- [ ] [T-0003] Maybe | created:2026-07-29
`
	dir := newDir(t, map[string]string{
		"backlog.md": backlog16,
		"done.md": `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [T-0004] Closed | created:2026-06-01 | done:2026-07-02 | outcome:shipped
`,
	})
	s := mustOpen(t, dir)

	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("want exactly the width violation, got:\n%s", violationMessages(vs))
	}
	if vs[0].Invariant != invFormat || !strings.Contains(vs[0].Message, "one to fifteen") {
		t.Errorf("want a format violation naming the cap, got: %s", vs[0])
	}
	if vs[0].At.File != "backlog.md" {
		t.Errorf("violation should point at backlog.md, got %s", vs[0].At)
	}

	// A violation, not a warning: the width never reaches the warn stream.
	_, warns, err := s.ValidateWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warns {
		if strings.Contains(w.Message, "3-6") {
			t.Errorf("width 16 is a violation, not a warning: %s", w)
		}
	}

	// The default grammar stood in, so the broken directory still opens, lists
	// and reads - rule 3's default-stands-in promise.
	if _, err := s.List(Filter{State: StateAll}); err != nil {
		t.Errorf("list: %v", err)
	}
	if _, err := s.Get("T-0001"); err != nil {
		t.Errorf("get under the standing-in grammar: %v", err)
	}
}

// 15 is the inclusive upper edge of the cap: the widest honorable width.
// Every 15-digit ID fits under 2^53, so the directory validates cleanly and
// the width is merely warned about, exactly like width 2.
func TestWidthAtTheCapIsAccepted(t *testing.T) {
	backlog15 := `---
doc: backlog
version: 1
project: Wide
next_id: T-000000000000005
id_width: 15
updated: 2026-07-29
---

# Backlog

## Ready

- [ ] [T-000000000000001] One | prio:med | created:2026-07-29

## Blocked

- [ ] [T-000000000000002] Two | created:2026-07-29 | blocked:on a thing

## Someday

- [ ] [T-000000000000003] Maybe | created:2026-07-29
`
	dir := newDir(t, map[string]string{
		"backlog.md": backlog15,
		"done.md": `---
doc: done
version: 1
updated: 2026-07-29
---

# Done

## 2026-07

- [x] [T-000000000000004] Closed | created:2026-06-01 | done:2026-07-02 | outcome:shipped
`,
	})
	s := mustOpen(t, dir)

	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Errorf("width 15 must not fail --check:\n%s", violationMessages(vs))
	}
	_, warns, err := s.ValidateWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0].Message, "3-6") {
		t.Errorf("want one width warning, got %v", warns)
	}
	// The counter space at width 15 is 10^15 - 1, which fits an int exactly.
	g := IDGrammar{Prefix: "T", Width: 15}
	if g.Cap() != 999999999999999 {
		t.Errorf("Cap() at width 15 = %d, want 999999999999999", g.Cap())
	}
}

// Add reports Conflict when next_id reaches the declared cap, and the cap is
// named in the message (spec-tools.md §5.1.2). Width 1 keeps the counter
// space small enough to exhaust in a test.
func TestAddExhaustsDeclaredCounterSpace(t *testing.T) {
	backlog1 := `---
doc: backlog
version: 1
project: Tiny
next_id: X-8
id_prefix: X
id_width: 1
---

## Ready

## Blocked

## Someday
`
	done1 := `---
doc: done
version: 1
---

# Done

## 2026-07

- [x] [X-7] Closed | done:2026-07-02 | outcome:shipped
`
	s := mustOpen(t, newDir(t, map[string]string{"backlog.md": backlog1, "done.md": done1}))

	a, _, err := s.Add(AddRequest{Title: "Eighth"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "X-8" {
		t.Errorf("add = %s, want X-8", a.ID)
	}
	// next_id is now X-9, the cap: the next add is exhausted.
	_, _, err = s.Add(AddRequest{Title: "Ninth"}, today)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if !strings.Contains(err.Error(), "X-9") || !strings.Contains(err.Error(), "1-digit") ||
		!strings.Contains(err.Error(), "9 items") {
		t.Errorf("error should name the cap: %v", err)
	}
	// The directory is left valid: the cap is a state, not a broken write.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after exhaustion:\n%s", violationMessages(vs))
	}
}

// The declared grammar must not change what a no-op write does: parse and
// write back byte for byte, just like a default directory.
func TestCustomGrammarRoundTripsBytes(t *testing.T) {
	for path, body := range map[string]string{
		"backlog.md": customBacklog,
		"done.md":    customDone,
	} {
		name := strings.SplitN(path, "/", 2)[0]
		var b *backlogFile
		var d *doneFile
		if name == "backlog.md" {
			b, _ = parseBacklog(name, []byte(body))
		} else {
			d, _ = parseDoneG(name, []byte(body), IDGrammar{Prefix: "MM", Width: 3})
		}
		var got string
		if b != nil {
			got = string(b.Edit().Bytes())
		} else {
			got = string(d.Edit().Bytes())
		}
		if string(got) != body {
			t.Errorf("%s round trip changed the file:\n%s", path, firstDiff(body, got))
		}
	}
}

// ---------------------------------------------------------------------------
// T-0118 — per-task prefixes are NOT in v1.

// The T-0112 request offered "or even on a per task basis": two prefixes in
// one directory, each with its own counter. T-0113 decided against it for v1 —
// §3.3.2 rule 1 keeps ONE grammar per directory, "a directory containing IDs
// in more than one grammar is invalid" — so the per-prefix counter model, I2
// per counter, and per-prefix check.sh validation are all out of scope. What
// T-0118 closes out is the guard: the exact scenario the spike described,
// X-001 and Y-001 in one directory, is a FORMAT VIOLATION everywhere, and the
// write path cannot produce it.
func TestMixedPrefixesAreOneGrammarViolation(t *testing.T) {
	// The fixture is the verify scenario: an X/3 directory whose Someday holds
	// Y-001 — a perfectly well-formed ID in its own Y/3 grammar. The directory
	// is invalid, with the foreign prefix as its ONLY finding.
	s := mustOpen(t, "../testdata/broken-mixed-prefix")
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want exactly 1:\n%s", len(vs), violationMessages(vs))
	}
	if vs[0].At.File != "backlog.md" || vs[0].At.Line != 23 {
		t.Errorf("finding = %s:%d, want backlog.md:23", vs[0].At.File, vs[0].At.Line)
	}
	if !strings.Contains(vs[0].Message, "Y-001") {
		t.Errorf("finding should name the foreign ID: %s", vs[0].Message)
	}
}

// The library cannot WRITE the scenario either: allocation formats in the
// declared grammar only, and an operation whose subject does not parse in it
// is refused before anything is touched.
func TestMixedPrefixesCannotBeWritten(t *testing.T) {
	s := mustOpen(t, newDir(t, map[string]string{
		"backlog.md": customBacklog, "done.md": customDone})) // MM/3

	// A Y-001 subject is not an MM/3 ID, so no operation accepts it.
	g, err := s.Grammar()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ParseID("Y-001"); err == nil {
		t.Error("Y-001 should not parse in the MM/3 grammar")
	}
	if _, err := s.Get("Y-001"); err == nil {
		t.Error("Get(Y-001) should not resolve in an MM/3 directory")
	}

	// Allocation stays inside the declared grammar forever: it never produces
	// a second prefix, so the counter model does not need per-prefix state.
	a, _, err := s.Add(AddRequest{Title: "Allocated"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(a.ID), "MM-") {
		t.Errorf("allocation = %s, want an MM- ID", a.ID)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after add:\n%s", violationMessages(vs))
	}
}
