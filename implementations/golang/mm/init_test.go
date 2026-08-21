package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Version 2: what --init actually produces today (T-0241).

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
	if d.Version != 2 {
		t.Fatalf("directory version = %d, want 2 (spec-tools.md §5.1.1 describes no other output)", d.Version)
	}
	if d.Project != "Acme Rewrite" {
		t.Errorf("project = %q", d.Project)
	}
	if d.NextID != "T-0001" {
		t.Errorf("next_id = %q, want T-0001", d.NextID)
	}
	// §5.1.1: absent --wip-limit means UNCAPPED, not version 1's "one slot"
	// default - the one place a fresh init deliberately does not reproduce
	// version 1's out-of-the-box behavior. A version-2 cap lives in
	// StageCfg.WipLimits, never the version-1-only WipLimit/WipUsed fields.
	if _, capped := d.StageCfg.WipLimits["working"]; capped {
		t.Errorf("working is capped by default, want uncapped: %+v", d.StageCfg.WipLimits)
	}

	for _, name := range []string{"board.md", "done.md", "details/_template.md", "structure.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	for _, absent := range []string{"backlog.md", "working.01.md"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); !os.IsNotExist(err) {
			t.Errorf("a version-2 init must not write %s", absent)
		}
	}
	b := readFile(t, dir, "board.md")
	if strings.Contains(b, "## Ready") || strings.Contains(b, "## Blocked") || strings.Contains(b, "## Someday") {
		t.Errorf("board.md must carry no version-1 sections:\n%s", b)
	}

	// And the directory is immediately usable, through the real public API -
	// no bypass needed, since a fresh Init is version 2 and the guarded
	// methods only refuse version 1 (T-0236).
	it, _, err := s.Add(AddRequest{Title: "First real item"}, today)
	if err != nil {
		t.Errorf("add into a fresh directory: %v", err)
	}
	if it.Stage != "ready" {
		t.Errorf("a bare add lands on %q, want ready", it.Stage)
	}
	if _, _, err := s.Start(it.ID, StartRequest{}, today); err != nil {
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
	b := readFile(t, dir, "board.md")
	for _, want := range []string{"next_id: X-001", "id_prefix: X", "id_width: 3"} {
		if !strings.Contains(b, want) {
			t.Errorf("board.md lacks %q:\n%s", want, b)
		}
	}
	// The template stays generic: it is exempt from I9 and copied verbatim.
	if tmpl := readFile(t, dir, "details/_template.md"); !strings.Contains(tmpl, "T-XXXX") {
		t.Errorf("the detail template must stay grammar-free:\n%s", tmpl)
	}

	// The directory is immediately usable in its declared grammar.
	first, _, err := s.Add(AddRequest{Title: "First custom item"}, today)
	if err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := s.Add(AddRequest{Title: "Second"}, today); err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := s.Start(first.ID, StartRequest{}, today); err != nil {
		t.Errorf("start %s: %v", first.ID, err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after use:\n%s", violationMessages(vs))
	}
}

func TestInitDefaultWritesNoGrammarKeys(t *testing.T) {
	// Rule 6: absent keys = the grammar default, byte-identical.
	dir := filepath.Join(t.TempDir(), "micro-manager")
	if _, _, err := Init(dir, InitRequest{Project: "Plain"}, today); err != nil {
		t.Fatalf("init: %v", err)
	}
	b := readFile(t, dir, "board.md")
	if strings.Contains(b, "id_prefix") || strings.Contains(b, "id_width") {
		t.Errorf("default init must not declare a grammar:\n%s", b)
	}
	if strings.Contains(b, "stages:") || strings.Contains(b, "wip.working") {
		t.Errorf("default init must not declare stages or a WIP cap:\n%s", b)
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
		{"width above the 15 cap", InitRequest{Project: "P", IDWidth: 16}},
	}
	for _, tc := range cases {
		dir := filepath.Join(t.TempDir(), "micro-manager")
		if _, _, err := Init(dir, tc.req, today); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", tc.name, err)
		}
	}
}

func TestInitWipLimit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")

	s, _, err := Init(dir, InitRequest{Project: "Capped", Wip: 3}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	b := readFile(t, dir, "board.md")
	if !strings.Contains(b, "wip.working: 3") {
		t.Errorf("board.md lacks wip.working: 3:\n%s", b)
	}
	d, _ := s.Directory()
	if limit := d.StageCfg.WipLimits["working"]; limit != 3 {
		t.Errorf("working's wip limit = %d, want 3", limit)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}

	// No working.NN.md files exist at all in version 2 - there is nothing to
	// number or pad a width for.
	for _, name := range []string{"working.01.md", "working.001.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("a version-2 init must not write %s", name)
		}
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "board.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := Init(dir, InitRequest{Project: "Nope"}, today)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("want ErrAlreadyExists, got %v", err)
	}
	if got := readFile(t, dir, "board.md"); got != "mine\n" {
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
	if len(res.Files) != 4 { // board, done, template, structure
		t.Errorf("files = %v, want 4", res.Files)
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

// §5.1.1: a supplied Description makes structure.md's SHOULD a MUST, even
// against an explicit NoStructure - there is nowhere else for it to live.
func TestInitDescriptionOverridesNoStructure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	_, _, err := Init(dir, InitRequest{
		Project: "Described", NoStructure: true, Description: "Gardening tasks for the back yard.",
	}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	s := readFile(t, dir, "structure.md")
	if !strings.Contains(s, "Gardening tasks for the back yard.") {
		t.Errorf("structure.md missing the supplied description:\n%s", s)
	}
}

func TestInitDescriptionSeedsStructure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	_, _, err := Init(dir, InitRequest{Project: "Garden", Description: "Gardening tasks."}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	s := readFile(t, dir, "structure.md")
	if !strings.Contains(s, "Gardening tasks.") {
		t.Errorf("structure.md missing the supplied description:\n%s", s)
	}
	if strings.Contains(s, "A todo directory in plain Markdown") {
		t.Errorf("the default prose should have been replaced, not kept alongside:\n%s", s)
	}
}

// ---------------------------------------------------------------------------
// Version 1 (initV1): the prior --init output, kept reachable only from this
// package's own tests (T-0241) - version-1 directories still exist and the
// CLI/library still support them fully, they are simply no longer what a
// fresh --init produces.

func TestInitV1CreatesAValidDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro-manager")

	s, res, err := initV1(dir, InitRequest{Project: "Acme Rewrite"}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if len(res.Changes) == 0 {
		t.Error("init reported no changes")
	}

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
	if d.WipLimit != 1 || d.WipUsed != 0 {
		t.Errorf("wip = %d/%d, want 0/1", d.WipUsed, d.WipLimit)
	}

	for _, name := range []string{"backlog.md", "done.md", "working.01.md",
		"details/_template.md", "structure.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("missing %s", name)
		}
	}
	b := readFile(t, dir, "backlog.md")
	for _, h := range []string{"## Ready", "## Blocked", "## Someday"} {
		if !strings.Contains(b, h) {
			t.Errorf("backlog.md has no %s", h)
		}
	}
	if w := readFile(t, dir, "working.01.md"); strings.Contains(strings.ToLower(w), "nothing in progress") {
		t.Errorf("the idle slot should not carry prose an operation cannot maintain:\n%s", w)
	}

	if _, _, err := testAddV1(s, AddRequest{Title: "First real item"}, today); err != nil {
		t.Errorf("add into a fresh directory: %v", err)
	}
	if _, _, err := testStartV1(s, "T-0001", StartRequest{}, today); err != nil {
		t.Errorf("start in a fresh directory: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after use:\n%s", violationMessages(vs))
	}
}

func TestInitV1DeclaredGrammar(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "micro-manager")
	s, _, err := initV1(dir, InitRequest{Project: "Custom", IDPrefix: "X", IDWidth: 3}, today)
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
	if tmpl := readFile(t, dir, "details/_template.md"); !strings.Contains(tmpl, "T-XXXX") {
		t.Errorf("the detail template must stay grammar-free:\n%s", tmpl)
	}

	if _, _, err := testAddV1(s, AddRequest{Title: "First custom item"}, today); err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := testAddV1(s, AddRequest{Title: "Second"}, today); err != nil {
		t.Errorf("add: %v", err)
	}
	if _, _, err := testStartV1(s, "X-001", StartRequest{}, today); err != nil {
		t.Errorf("start X-001: %v", err)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations after use:\n%s", violationMessages(vs))
	}
}

func TestInitV1DefaultWritesNoGrammarKeys(t *testing.T) {
	// Rule 6: absent keys = spec version 1, byte-identical.
	dir := filepath.Join(t.TempDir(), "micro-manager")
	if _, _, err := initV1(dir, InitRequest{Project: "Plain"}, today); err != nil {
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

func TestInitV1ValidatesGrammar(t *testing.T) {
	cases := []struct {
		name string
		req  InitRequest
	}{
		{"lowercase prefix", InitRequest{Project: "P", IDPrefix: "x"}},
		{"mixed-case prefix", InitRequest{Project: "P", IDPrefix: "Tt"}},
		{"five-letter prefix", InitRequest{Project: "P", IDPrefix: "ABCDE"}},
		{"negative width", InitRequest{Project: "P", IDWidth: -1}},
		{"width above the 15 cap", InitRequest{Project: "P", IDWidth: 16}},
	}
	for _, tc := range cases {
		dir := filepath.Join(t.TempDir(), "micro-manager")
		if _, _, err := initV1(dir, tc.req, today); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", tc.name, err)
		}
	}
}

func TestInitV1WipAndSlotWidth(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")

	s, _, err := initV1(dir, InitRequest{Project: "Wide", Wip: 3, SlotWidth: 3}, today)
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

func TestInitV1RefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "backlog.md"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := initV1(dir, InitRequest{Project: "Nope"}, today)
	if !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("want ErrAlreadyExists, got %v", err)
	}
	if got := readFile(t, dir, "backlog.md"); got != "mine\n" {
		t.Errorf("the existing file was touched: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "done.md")); !os.IsNotExist(err) {
		t.Error("a refused init left files behind")
	}
}

func TestInitV1ValidatesItsArguments(t *testing.T) {
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
		_, _, err := initV1(dir, c.req, today)
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("%s: want ErrInvalidArgument, got %v", c.name, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s: the directory was created anyway", c.name)
		}
	}
}

func TestInitV1DryRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")

	_, res, err := initV1(dir, InitRequest{Project: "Dry", Wip: 2, DryRun: true}, today)
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

func TestInitV1NoStructure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mm")
	s, _, err := initV1(dir, InitRequest{Project: "Bare", NoStructure: true}, today)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "structure.md")); !os.IsNotExist(err) {
		t.Error("structure.md should not have been written")
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("violations:\n%s", violationMessages(vs))
	}
}
