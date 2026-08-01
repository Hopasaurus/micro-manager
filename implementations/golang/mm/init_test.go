package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesAValidDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro-manager")

	s, res, err := Init(dir, InitRequest{Project: "Acme Rewrite"}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if len(res.Changes) == 0 {
		t.Error("init reported no changes")
	}

	// The point of the operation: what it writes must pass its own checker.
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 0 {
		t.Fatalf("a fresh directory should be clean:\n%s", violationMessages(vs))
	}

	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.Project != "Acme Rewrite" {
		t.Errorf("project = %q", d.Project)
	}
	if d.NextID != "T-0001" {
		t.Errorf("next_id = %q, want T-0001", d.NextID)
	}
	if d.WipLimit != 1 || d.WipUsed != 0 {
		t.Errorf("wip = %d/%d, want 0/1", d.WipUsed, d.WipLimit)
	}

	for _, name := range []string{"backlog.md", "done.md", "working.01.md",
		"details/_template.md", "structure.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	// All three sections, present even though empty.
	b := readFile(t, dir, "backlog.md")
	for _, h := range []string{"## Ready", "## Blocked", "## Someday"} {
		if !strings.Contains(b, h) {
			t.Errorf("backlog.md has no %s", h)
		}
	}
	// A slot that says nothing about being empty, because an operation that
	// fills the frontmatter will not rewrite the prose.
	if w := readFile(t, dir, "working.01.md"); strings.Contains(strings.ToLower(w), "nothing in progress") {
		t.Errorf("the idle slot should not carry prose an operation cannot maintain:\n%s", w)
	}

	// And the directory is immediately usable.
	if _, _, err := s.Add(AddRequest{Title: "First real item"}, today); err != nil {
		t.Errorf("add into a fresh directory: %v", err)
	}
	if _, _, err := s.Start("T-0001", StartRequest{}, today); err != nil {
		t.Errorf("start in a fresh directory: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after use:\n%s", violationMessages(vs))
	}
}

func TestInitDeclaredGrammar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro-manager")
	s, _, err := Init(dir, InitRequest{Project: "Custom", IDPrefix: "X", IDWidth: 3}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("a fresh custom directory should be clean:\n%s", violationMessages(vs))
	}
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.IDPrefix != "X" || d.IDWidth != 3 || d.NextID != "X-001" {
		t.Errorf("grammar = %q/%d next %q, want X/3 next X-001", d.IDPrefix, d.IDWidth, d.NextID)
	}
	b := readFile(t, dir, "backlog.md")
	for _, want := range []string{"next_id: X-001", "id_prefix: X", "id_width: 3"} {
		if !strings.Contains(b, want) {
			t.Errorf("backlog.md lacks %q:\n%s", want, b)
		}
	}
	// The template stays generic: it is exempt from I9 and copied verbatim.
	if tmpl := readFile(t, dir, "details/_template.md"); !strings.Contains(tmpl, "T-XXXX") {
		t.Errorf("the detail template must stay grammar-free:\n%s", tmpl)
	}

	// The directory is immediately usable in its declared grammar.
	if _, _, err := s.Add(AddRequest{Title: "First custom item"}, today); err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := s.Add(AddRequest{Title: "Second"}, today); err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := s.Start("X-001", StartRequest{}, today); err != nil {
		t.Errorf("start X-001: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after use:\n%s", violationMessages(vs))
	}
}

func TestInitDefaultWritesNoGrammarKeys(t *testing.T) {
	// Rule 6: absent keys = spec version 1, byte-identical.
	dir := filepath.Join(t.TempDir(), "micro-manager")
	if _, _, err := Init(dir, InitRequest{Project: "Plain"}, today); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := readFile(t, dir, "backlog.md")
	if strings.Contains(b, "id_prefix") || strings.Contains(b, "id_width") {
		t.Errorf("default init must not declare a grammar:\n%s", b)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Directory(); d.NextID != "T-0001" {
		t.Errorf("next_id = %q, want T-0001", d.NextID)
	}
}

func TestInitValidatesGrammar(t *testing.T) {
	cases := []struct {
		name string
		req  InitRequest
	}{
		{"lowercase prefix", InitRequest{Project: "P", IDPrefix: "x"}},
		{"mixed-case prefix", InitRequest{Project: "P", IDPrefix: "Tt"}},
		{"five-letter prefix", InitRequest{Project: "P", IDPrefix: "ABCDE"}},
		{"negative width", InitRequest{Project: "P", IDWidth: -1}},
	}
	for _, tc := range cases {
		dir := filepath.Join(t.TempDir(), "micro-manager")
		if _, _, err := Init(dir, tc.req, today); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", tc.name, err)
		}
	}
}

func TestInitWipAndSlotWidth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")

	s, _, err := Init(dir, InitRequest{Project: "Wide", Wip: 3, SlotWidth: 3}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	for _, name := range []string{"working.001.md", "working.002.md", "working.003.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	d, _ := s.Directory()
	if d.WipLimit != 3 {
		t.Errorf("wip limit = %d, want 3 - the limit IS the file count", d.WipLimit)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Init(dir, InitRequest{Project: "Nope"}, today)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("want ErrAlreadyExists, got %v", err)
	}
	if got := readFile(t, dir, "backlog.md"); got != "mine\n" {
		t.Errorf("the existing file was touched: %q", got)
	}
	// Nothing else was created either.
	if _, err := os.Stat(filepath.Join(dir, "done.md")); !os.IsNotExist(err) {
		t.Error("a refused init left files behind")
	}
}

func TestInitValidatesItsArguments(t *testing.T) {
	cases := []struct {
		name string
		req  InitRequest
	}{
		{"no project", InitRequest{}},
		{"blank project", InitRequest{Project: "   "}},
		{"null project", InitRequest{Project: "null"}},
		{"comment marker", InitRequest{Project: "Acme #1"}},
		{"newline", InitRequest{Project: "Acme\nInc"}},
		{"negative wip", InitRequest{Project: "P", Wip: -1}},
		// 10 slots cannot be numbered in one digit without mixing widths (I10).
		{"width too narrow", InitRequest{Project: "P", Wip: 10, SlotWidth: 1}},
	}
	for _, c := range cases {
		dir := filepath.Join(t.TempDir(), "mm")
		_, _, err := Init(dir, c.req, today)
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", c.name, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s: the directory was created anyway", c.name)
		}
	}
}

func TestInitDryRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")

	_, res, err := Init(dir, InitRequest{Project: "Dry", Wip: 2, DryRun: true}, today)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if len(res.Files) != 6 { // backlog, done, two slots, template, structure
		t.Errorf("files = %v, want 6", res.Files)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("dry run created the directory")
	}
}

func TestInitNoStructure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	s, _, err := Init(dir, InitRequest{Project: "Bare", NoStructure: true}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "structure.md")); !os.IsNotExist(err) {
		t.Error("structure.md should not have been written")
	}
	// structure.md is documentation, not data: its absence changes nothing.
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}
