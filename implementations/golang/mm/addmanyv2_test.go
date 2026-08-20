package mm

import "testing"

// AddMany, generalized to a stage-based board (spec-tools.md §5.2.1). The
// line grammar (ParseAddLine) is directory-agnostic, so it accepts stage:/
// reason:/tickler_dest: alongside version 1's section:/blocked: - whichever
// the directory that receives the batch actually uses.

func TestParseAddLineV2Fields(t *testing.T) {
	req, err := ParseAddLine(0, "Second | stage:review | reason:needs eyes | tickler_dest:ready")
	if err != nil {
		t.Fatal(err)
	}
	if req.Stage != "review" || req.Reason != "needs eyes" || req.TicklerDest != "ready" {
		t.Errorf("req = %+v, want stage:review reason:%q tickler_dest:ready", req, "needs eyes")
	}
}

func TestAddManyV2AppendsInOrder(t *testing.T) {
	_, s := v2Dir(t)

	items, res, err := s.AddMany([]AddRequest{
		{Title: "Eight", Stage: "ready"},
		{Title: "Nine", Stage: "review"},
		{Title: "Ten", Stage: "blocked", Reason: "waiting on design"},
	}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("added %d items, want 3", len(items))
	}
	for i, want := range []ID{"T-0011", "T-0012", "T-0013"} {
		if items[i].ID != want {
			t.Errorf("item %d has id %s, want %s", i, items[i].ID, want)
		}
	}
	if len(res.Files) != 1 {
		t.Fatalf("wrote %v, want just board.md", res.Files)
	}
	if items[0].Stage != "ready" || items[1].Stage != "review" || items[2].Stage != "blocked" {
		t.Fatalf("stages = %s, %s, %s", items[0].Stage, items[1].Stage, items[2].Stage)
	}
	if items[2].Reason != "waiting on design" {
		t.Errorf("Reason = %q", items[2].Reason)
	}

	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.NextID != "T-0014" {
		t.Errorf("next_id = %s, want T-0014 (one bump per item)", d.NextID)
	}
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("directory should still be clean:\n%s", violationMessages(vs))
	}
}

// §5.2.1: "--top inserts the batch at the top, still in the input's order" -
// per stage, since a batch may span several.
func TestAddManyV2TopKeepsInputOrderPerStage(t *testing.T) {
	_, s := v2Dir(t)

	if _, _, err := s.AddMany([]AddRequest{
		{Title: "Eight", Stage: "ready", Top: true},
		{Title: "Nine", Stage: "ready", Top: true},
	}, today); err != nil {
		t.Fatal(err)
	}

	items, err := s.List(Filter{Stage: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	// List is on-disk order (never re-sorted), which is exactly the property
	// under test: --top puts the batch first, still in the input's order.
	want := []ID{"T-0011", "T-0012", "T-0001", "T-0002"}
	seq := idsOfValues(items)
	if len(seq) != len(want) {
		t.Fatalf("ready = %v, want %v", seq, want)
	}
	for i, id := range want {
		if seq[i] != id {
			t.Fatalf("ready sequence = %v, want %v", seq, want)
		}
	}
}

func TestAddManyV2IsAllOrNothing(t *testing.T) {
	dir, s := v2Dir(t)
	before := readMigFile(t, dir, "board.md")

	_, _, err := s.AddMany([]AddRequest{
		{Title: "Eight", Stage: "ready"},
		{Title: "Nine", Stage: "not-a-stage"},
	}, today)
	if err == nil {
		t.Fatal("an undeclared stage should fail the whole batch")
	}
	if got := readMigFile(t, dir, "board.md"); got != before {
		t.Error("board.md changed even though the batch failed")
	}
}

func TestAddManyV2RequiresReasonForNeedsReasonStage(t *testing.T) {
	_, s := v2Dir(t)

	_, _, err := s.AddMany([]AddRequest{
		{Title: "No reason", Stage: "blocked"},
	}, today)
	if err == nil {
		t.Fatal("blocked needs_reason with no reason: should fail")
	}
}

func TestAddManyV2DetailBodyRefused(t *testing.T) {
	_, s := v2Dir(t)

	_, _, err := s.AddMany([]AddRequest{
		{Title: "Eight", Stage: "ready", DetailBody: "notes"},
	}, today)
	if err == nil {
		t.Fatal("--add-many must refuse a detail body the same way it does on version 1")
	}
}
