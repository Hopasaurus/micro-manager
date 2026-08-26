package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Version-2 (board.md) tests: parsing, validation, and the five core
// operations (--add, --start, --pause, --finish, --move) generalized to a
// stage-based board. The fixture below exercises a custom stage (review),
// a per-stage WIP cap, tickler_stages, and needs_reason together, per M1's
// checklist.

const v2Board = `---
doc: board
version: 2
project: V2 Tests
next_id: T-0011
updated: 2026-08-20
stages: someday,ready,blocked,working,review
wip.working: 2
tickler_stages: someday->ready
needs_reason: blocked
---

# Board

- [ ] [T-0001] Fix the deploy script | stage:ready | prio:high | tags:infra,ci | detail:details/T-0001.md | created:2026-07-20 | owner:dana
- [ ] [T-0002] Second | stage:ready | prio:med | created:2026-07-21
- [ ] [T-0003] Waiting | stage:blocked | prio:low | created:2026-07-22 | reason:on the vendor
- [ ] [T-0005] Someday item | stage:someday | created:2026-07-23 | tickler:mon@08:00
- [ ] [T-0006] In progress | stage:working | created:2026-07-24 | started:2026-07-25
- [ ] [T-0007] In review | stage:review | created:2026-07-25
`

const v2Done = `---
doc: done
version: 2
---

# Done

## 2026-07

- [x] [T-0009] Already closed | created:2026-07-01 | done:2026-07-10 | outcome:shipped
`

const v2Detail = `---
doc: detail
id: T-0001
title: Fix the deploy script
updated: 2026-07-20
---

# T-0001 — Fix the deploy script

## Context

Deploys fail on a cold cache.
`

// newV2Dir builds a board.md-based directory on disk, with no version-1
// defaults (unlike newDir, which always seeds backlog.md/working.01.md).
func newV2Dir(t *testing.T, files map[string]string) string {
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
	return dir
}

func v2Dir(t *testing.T) (string, *Store) {
	t.Helper()
	dir := newV2Dir(t, map[string]string{
		"board.md":             v2Board,
		"done.md":              v2Done,
		"details/T-0001.md":    v2Detail,
		"details/_template.md": "---\ndoc: detail\n---\n\n## Context\n",
	})
	s := mustOpen(t, dir)
	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Fatalf("fixture should start clean:\n%s", violationMessages(vs))
	}
	return dir, s
}

// ---------------------------------------------------------------------------
// Parsing and Directory
// ---------------------------------------------------------------------------

func TestParseBoardReadsStagesAndItems(t *testing.T) {
	b, vs := parseBoard("board.md", []byte(v2Board))
	if len(vs) != 0 {
		t.Fatalf("unexpected parse violations: %v", vs)
	}
	if len(b.Items) != 6 {
		t.Fatalf("items = %d, want 6", len(b.Items))
	}
	want := []Stage{"someday", "ready", "blocked", "working", "review"}
	if len(b.stageCfg.Stages) != len(want) {
		t.Fatalf("stages = %v, want %v", b.stageCfg.Stages, want)
	}
	for i, s := range want {
		if b.stageCfg.Stages[i] != s {
			t.Errorf("stages[%d] = %s, want %s", i, b.stageCfg.Stages[i], s)
		}
	}
	if got, ok := b.stageCfg.WipLimits["working"]; !ok || got != 2 {
		t.Errorf("wip.working = %d,%v want 2,true", got, ok)
	}
	if got, ok := b.stageCfg.TicklerDestOf("someday"); !ok || got != "ready" {
		t.Errorf("tickler_stages[someday] = %s,%v want ready,true", got, ok)
	}
	if !b.stageCfg.StageNeedsReason("blocked") {
		t.Error("blocked should be in needs_reason")
	}
	// review is a fully ordinary declared stage with no special config.
	if !b.stageCfg.IsStage("review") {
		t.Error("review should be a declared stage")
	}
}

func TestDirectoryReportsVersion2AndStageConfig(t *testing.T) {
	_, s := v2Dir(t)
	d, err := s.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.Version != 2 {
		t.Errorf("version = %d, want 2", d.Version)
	}
	if d.StageUsed["working"] != 1 {
		t.Errorf("StageUsed[working] = %d, want 1", d.StageUsed["working"])
	}
}

func TestParseBoardRejectsItemWithoutStage(t *testing.T) {
	board := strings.Replace(v2Board,
		"- [ ] [T-0002] Second | stage:ready | prio:med | created:2026-07-21",
		"- [ ] [T-0002] Second | prio:med | created:2026-07-21", 1)
	_, vs := parseBoard("board.md", []byte(board))
	if !hasInvariant(vs, "I7") {
		t.Errorf("expected an I7 violation for a missing stage:, got %v", vs)
	}
}

func TestParseBoardIgnoresHeadings(t *testing.T) {
	board := strings.Replace(v2Board, "# Board\n", "# Board\n\n## Not A Section\n", 1)
	b, vs := parseBoard("board.md", []byte(board))
	if len(vs) != 0 {
		t.Fatalf("a heading in board.md must not be an error: %v", vs)
	}
	if len(b.Items) != 6 {
		t.Errorf("items = %d, want 6 (heading must not swallow items)", len(b.Items))
	}
}

func hasInvariant(vs []Violation, inv string) bool {
	for _, v := range vs {
		if v.Invariant == inv {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Validation: referential integrity and stage-shaped I3/I5/I7
// ---------------------------------------------------------------------------

func TestValidateRejectsUndeclaredStageOnItem(t *testing.T) {
	board := strings.Replace(v2Board, "stage:review", "stage:qa", 1)
	dir, s := v2Dir(t) // establish a valid dir first for mustOpen's side effects
	_ = dir
	if err := os.WriteFile(filepath.Join(s.Path(), "board.md"), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	vs, err := s.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if !hasInvariant(vs, "I7") {
		t.Errorf("expected I7 for an undeclared stage, got %v", violationMessages(vs))
	}
}

func TestValidateRejectsWipKeyForUndeclaredStage(t *testing.T) {
	board := strings.Replace(v2Board, "wip.working: 2", "wip.working: 2\nwip.qa: 1", 1)
	b, vs := parseBoard("board.md", []byte(board))
	if !hasInvariant(vs, "I7") {
		t.Errorf("expected I7 for wip.qa naming an undeclared stage, got %v", vs)
	}
	if _, ok := b.stageCfg.WipLimits["qa"]; ok {
		t.Error("an undeclared stage's wip cap must not be admitted into StageConfig")
	}
}

func TestValidateRequiresReasonWhereNeedsReasonLists(t *testing.T) {
	board := strings.Replace(v2Board, "| reason:on the vendor", "", 1)
	_, vs := parseBoard("board.md", []byte(board))
	m := &dirModel{path: "."}
	m.board, _ = parseBoard("board.md", []byte(board))
	_ = vs
	found := m.checkBoard()
	if !hasInvariant(found, "I5") {
		t.Errorf("expected I5 for a blocked item with no reason:, got %v", found)
	}
}

func TestValidateRequiresStartedOnWorkingStage(t *testing.T) {
	board := strings.Replace(v2Board, "| created:2026-07-24 | started:2026-07-25", "| created:2026-07-24", 1)
	m := &dirModel{path: "."}
	m.board, _ = parseBoard("board.md", []byte(board))
	found := m.checkBoard()
	if !hasInvariant(found, "I7") {
		t.Errorf("expected I7 for stage:working with no started:, got %v", found)
	}
}

func TestValidateTicklerOnlyOnASource(t *testing.T) {
	board := strings.Replace(v2Board, "stage:someday", "stage:review", 1)
	m := &dirModel{path: "."}
	m.board, _ = parseBoard("board.md", []byte(board))
	found := m.checkTicklerV2()
	if !hasInvariant(found, "I7") {
		t.Errorf("expected I7 for tickler: on a non-source stage, got %v", found)
	}
}

// ---------------------------------------------------------------------------
// Add
// ---------------------------------------------------------------------------

func TestAddV2DefaultsToReady(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Add(AddRequest{Title: "New thing"}, today)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if it.Stage != "ready" {
		t.Errorf("stage = %s, want ready", it.Stage)
	}
	if it.ID != "T-0011" {
		t.Errorf("id = %s, want T-0011 (allocated from next_id)", it.ID)
	}
}

func TestAddV2ToCustomStage(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Add(AddRequest{Title: "Needs review", Stage: "review"}, today)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if it.Stage != "review" {
		t.Errorf("stage = %s, want review", it.Stage)
	}
}

func TestAddV2RejectsUndeclaredStage(t *testing.T) {
	_, s := v2Dir(t)
	_, _, err := s.Add(AddRequest{Title: "x", Stage: "qa"}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestAddV2RequiresReasonForNeedsReasonStage(t *testing.T) {
	_, s := v2Dir(t)
	_, _, err := s.Add(AddRequest{Title: "x", Stage: "blocked"}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument (blocked needs a reason:)", err)
	}
	it, _, err := s.Add(AddRequest{Title: "x", Stage: "blocked", Reason: "waiting"}, today)
	if err != nil {
		t.Fatalf("add with reason: %v", err)
	}
	if it.Reason != "waiting" {
		t.Errorf("reason = %q, want %q", it.Reason, "waiting")
	}
}

func TestAddV2TicklerRequiresEligibleStage(t *testing.T) {
	_, s := v2Dir(t)
	_, _, err := s.Add(AddRequest{Title: "x", Stage: "ready", Tickler: "mon@08:00"}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument (ready is not tickler-eligible)", err)
	}
	it, _, err := s.Add(AddRequest{Title: "x", Stage: "someday", Tickler: "mon@08:00"}, today)
	if err != nil {
		t.Fatalf("add on someday: %v", err)
	}
	if it.Tickler != "mon@08:00" {
		t.Errorf("tickler = %q", it.Tickler)
	}
}

// ---------------------------------------------------------------------------
// Start / Pause / Finish / Move
// ---------------------------------------------------------------------------

func TestStartV2MovesToWorking(t *testing.T) {
	dir, s := v2Dir(t)
	it, _, err := s.Start("T-0002", StartRequest{}, today)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if it.Stage != "working" || it.Started != today {
		t.Errorf("stage/started = %s/%s, want working/%s", it.Stage, it.Started, today)
	}
	got := readFile(t, dir, "board.md")
	if !strings.Contains(got, "[T-0002] Second | stage:working") {
		t.Errorf("board.md was not rewritten in place:\n%s", got)
	}
}

// startV2 took no dryRun parameter at all and hardcoded a real commit -
// found while migrating internal/cli's dry-run test suite off v1 fixtures
// for T-0236, which caught mm --start --dry-run actually writing to disk on
// a version-2 directory. spec-tools.md §3.4: a dry run MUST write nothing.
func TestStartV2DryRunWritesNothing(t *testing.T) {
	dir, s := v2Dir(t)
	before := readFile(t, dir, "board.md")
	if _, _, err := s.Start("T-0002", StartRequest{DryRun: true}, today); err != nil {
		t.Fatalf("dry-run start: %v", err)
	}
	if got := readFile(t, dir, "board.md"); got != before {
		t.Errorf("a dry run wrote to board.md:\nbefore:\n%s\nafter:\n%s", before, got)
	}
}

func TestStartV2EnforcesWipLimit(t *testing.T) {
	_, s := v2Dir(t)
	// wip.working: 2, and T-0006 already occupies one slot.
	if _, _, err := s.Start("T-0002", StartRequest{}, today); err != nil {
		t.Fatalf("first start: %v", err)
	}
	_, _, err := s.Start("T-0001", StartRequest{}, today)
	if !errors.Is(err, ErrWipLimitReached) {
		t.Fatalf("err = %v, want ErrWipLimitReached", err)
	}
	var swe *StageWipLimitError
	if !errors.As(err, &swe) {
		t.Fatalf("error does not carry a StageWipLimitError")
	}
	if swe.Stage != "working" || swe.Limit != 2 || len(swe.Occupants) != 2 {
		t.Errorf("stage/limit/occupants = %s/%d/%d, want working/2/2",
			swe.Stage, swe.Limit, len(swe.Occupants))
	}
}

func TestPauseV2DefaultsToReadyAndKeepsReason(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Pause("T-0006", PauseRequest{}, today)
	if err != nil {
		t.Fatalf("pause: %v", err)
	}
	if it.Stage != "ready" {
		t.Errorf("stage = %s, want ready", it.Stage)
	}
	if it.Started.IsZero() {
		t.Error("started must survive a pause")
	}
}

func TestPauseV2ToNeedsReasonStageRequiresReason(t *testing.T) {
	_, s := v2Dir(t)
	_, _, err := s.Pause("T-0006", PauseRequest{Stage: "blocked"}, today)
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("err = %v, want ErrInvalidArgument", err)
	}
}

func TestFinishV2DropsStageAndTickler(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Finish("T-0005", FinishRequest{}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if it.Stage != "" {
		t.Errorf("stage = %q, want empty once done", it.Stage)
	}
	if it.Tickler != "" {
		t.Error("tickler: must not survive into done.md")
	}
	if it.Outcome != OutcomeShipped {
		t.Errorf("outcome = %s, want shipped (default)", it.Outcome)
	}
}

func TestFinishV2WorksDirectlyFromReady(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Finish("T-0001", FinishRequest{Outcome: OutcomeCancelled}, today)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if it.Outcome != OutcomeCancelled {
		t.Errorf("outcome = %s, want cancelled", it.Outcome)
	}
}

func TestMoveV2BetweenArbitraryStages(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Move("T-0007", MoveRequest{Stage: "working"}, today)
	if err != nil {
		t.Fatalf("move review -> working: %v", err)
	}
	if it.Stage != "working" {
		t.Errorf("stage = %s, want working", it.Stage)
	}
	// wip.working: 2 already has T-0006; this is the second, still within cap.
	d, _ := s.Directory()
	if d.StageUsed["working"] != 2 {
		t.Errorf("StageUsed[working] = %d, want 2", d.StageUsed["working"])
	}
}

func TestMoveV2KeepsReasonAfterLeavingNeedsReasonStage(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Move("T-0003", MoveRequest{Stage: "ready"}, today)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if it.Reason == "" {
		t.Error("reason: must NOT be dropped on leaving a needs_reason stage (decision 18)")
	}
}

func TestMoveV2DropsTicklerLeavingASource(t *testing.T) {
	_, s := v2Dir(t)
	it, _, err := s.Move("T-0005", MoveRequest{Stage: "review"}, today)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if it.Tickler != "" {
		t.Error("tickler: must be dropped leaving a tickler_stages source")
	}
}

func TestMoveV2RejectsWhenWipLimitFull(t *testing.T) {
	_, s := v2Dir(t)
	if _, _, err := s.Move("T-0001", MoveRequest{Stage: "working"}, today); err != nil {
		t.Fatalf("first move: %v", err)
	}
	_, _, err := s.Move("T-0002", MoveRequest{Stage: "working"}, today)
	if !errors.Is(err, ErrWipLimitReached) {
		t.Errorf("err = %v, want ErrWipLimitReached", err)
	}
}

// ---------------------------------------------------------------------------
// Round trip: a no-op operation writes nothing (spec-tools.md §7)
// ---------------------------------------------------------------------------

func TestV2NoOpMoveWritesNothing(t *testing.T) {
	dir, s := v2Dir(t)
	before := readFile(t, dir, "board.md")
	_, res, err := s.Move("T-0001", MoveRequest{Stage: "ready", Top: true}, today)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(res.Files) != 0 {
		t.Errorf("a move to where the item already is should write nothing, wrote %v", res.Files)
	}
	after := readFile(t, dir, "board.md")
	if before != after {
		t.Error("board.md changed on a no-op move")
	}
}

// ---------------------------------------------------------------------------
// regroupBoard: the standing physical layout after any real mutation
// (spec-file-format.md §5.1.6, tx.go's stage()).
// ---------------------------------------------------------------------------

// stageOrderIn returns the stage of every item line in file order, by
// scanning raw text — deliberately not going through the parser, so this
// proves what is actually ON DISK rather than what the model claims.
func stageOrderIn(t *testing.T, body string) []string {
	t.Helper()
	var order []string
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		for _, field := range strings.Split(line, " | ") {
			if v, ok := strings.CutPrefix(field, "stage:"); ok {
				order = append(order, v)
				break
			}
		}
	}
	return order
}

func TestMoveRegroupsBoardByStage(t *testing.T) {
	// v2Board's own file order is ready, ready, blocked, someday, working,
	// review - NOT stages: order (someday, ready, blocked, working, review)
	// - so any real change must visibly re-lay the whole file out.
	dir, s := v2Dir(t)
	if _, _, err := s.Move("T-0005", MoveRequest{Stage: "someday", Top: true}, today); err != nil {
		t.Fatalf("move: %v", err)
	}
	body := readFile(t, dir, "board.md")

	got := stageOrderIn(t, body)
	want := []string{"someday", "ready", "ready", "blocked", "working", "review"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("file order = %v, want stages: order %v", got, want)
	}

	// Exactly one blank line at every group boundary: the existing one
	// between the heading/prose and the first group (unchanged from the
	// fixture's own convention), plus one between each of the five stage
	// groups (someday/ready/blocked/working/review) and the next.
	if n := strings.Count(body, "\n\n- ["); n != 5 {
		t.Errorf("found %d blank-line-then-item boundaries, want 5:\n%s", n, body)
	}

	if vs, _ := s.Validate(); len(vs) != 0 {
		t.Errorf("regrouped board should still validate:\n%s", violationMessages(vs))
	}
}

// An empty stage contributes no group and no orphaned blank line.
func TestRegroupSkipsEmptyStagesCleanly(t *testing.T) {
	dir, s := v2Dir(t)
	// Empty out "review" by finishing its one occupant.
	if _, _, err := s.Finish("T-0007", FinishRequest{}, today); err != nil {
		t.Fatalf("finish: %v", err)
	}
	body := readFile(t, dir, "board.md")
	if strings.Contains(body, "\n\n\n") {
		t.Errorf("an emptied stage left a double-blank gap:\n%s", body)
	}
	if strings.HasSuffix(strings.TrimRight(body, "\n"), "\n") {
		t.Errorf("trailing blank line left after the last group:\n%q", body)
	}
}

// A stray item whose stage: is not among the declared stages (a
// pre-existing violation, I4) must survive a regroup, not be silently
// dropped - this must never turn "reported broken" into "quietly deleted."
func TestRegroupPreservesAnItemOnAnUndeclaredStage(t *testing.T) {
	dir := newV2Dir(t, map[string]string{
		"board.md": `---
doc: board
version: 2
project: Stray
next_id: T-0003
stages: ready,working
---

# Board

- [ ] [T-0001] Fine | stage:ready | created:2026-07-20
- [ ] [T-0002] Orphaned stage | stage:archived | created:2026-07-20
`,
		"done.md": "---\ndoc: done\nversion: 2\n---\n\n# Done\n",
	})
	s := mustOpen(t, dir)

	// Move the well-formed item, which is enough to trigger a regroup.
	if _, _, err := s.Move("T-0001", MoveRequest{Stage: "working"}, today); err != nil {
		t.Fatalf("move: %v", err)
	}
	body := readFile(t, dir, "board.md")
	if !strings.Contains(body, "[T-0002] Orphaned stage") {
		t.Fatalf("the stray item was dropped by the regroup:\n%s", body)
	}
}
