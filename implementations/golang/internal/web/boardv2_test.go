package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The board, generalized to a stage-based directory (spec-gui.md §5.5):
// dynamic columns from stages:, data-stage/data-has-reason on the card,
// data-needs-reason/data-wip-used/data-wip-limit on a capped column, and the
// tickler badge no longer someday-specific. T-0230 (M4).

func TestBoardV2ColumnsAndOrder(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	want := []string{
		"board-column-someday", "board-column-ready", "board-column-blocked",
		"board-column-working", "board-column-review", "board-column-done",
	}
	columnRE := regexp.MustCompile(`data-testid="(board-column-(?:someday|ready|blocked|working|review|done))"`)
	var got []string
	for _, m := range columnRE.FindAllStringSubmatch(body, -1) {
		got = append(got, m[1])
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("columns are\n  %v\nwant\n  %v (stages: order, done always last)", got, want)
	}

	for _, col := range want {
		suffixes := []string{"-header", "-title", "-count", "-body"}
		if col != "board-column-working" {
			suffixes = append(suffixes, "-add")
		}
		for _, suffix := range suffixes {
			if !hasTestid(body, col+suffix) {
				t.Errorf("%s is missing", col+suffix)
			}
		}
	}
	if hasTestid(body, "board-column-working-add") {
		t.Error("board-column-working-add MUST NOT exist, same rule as version 1")
	}

	// A column carrying wip.<slug> (working: 2) shows both attributes; one
	// with none (review) shows neither.
	if !strings.Contains(body, `data-testid="board-column-working" class="mm-column mm-column--working"`) {
		t.Errorf("board-column-working markup:\n%s", body)
	}
	if !regexp.MustCompile(`data-testid="board-column-working"[^>]*data-wip-used="1"[^>]*data-wip-limit="2"`).MatchString(body) {
		t.Errorf("board-column-working should carry data-wip-used=1 data-wip-limit=2:\n%s", body)
	}
	if regexp.MustCompile(`data-testid="board-column-review"[^>]*data-wip-`).MatchString(body) {
		t.Error("board-column-review has no cap and must carry neither wip attribute")
	}

	// needs_reason: blocked.
	if !regexp.MustCompile(`data-testid="board-column-blocked"[^>]*data-needs-reason="true"`).MatchString(body) {
		t.Errorf("board-column-blocked should carry data-needs-reason=true:\n%s", body)
	}
	if !regexp.MustCompile(`data-testid="board-column-ready"[^>]*data-needs-reason="false"`).MatchString(body) {
		t.Errorf("board-column-ready should carry data-needs-reason=false:\n%s", body)
	}
}

func TestBoardV2ItemCardAttributes(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	// T-0004 sits on blocked, with a reason.
	if !regexp.MustCompile(`data-item-id="T-0004"[^>]*data-item-state="board"[^>]*data-stage="blocked"`).MatchString(body) {
		t.Errorf("T-0004 card attributes:\n%s", body)
	}
	if !regexp.MustCompile(`data-item-id="T-0004"[^>]*data-has-reason="true"`).MatchString(body) {
		t.Errorf("T-0004 should carry data-has-reason=true:\n%s", body)
	}
	if strings.Contains(body, `data-item-id="T-0004"`) && regexp.MustCompile(`data-item-id="T-0004"[^>]*data-blocked=`).MatchString(body) {
		t.Error("a version-2 card must not carry the retired data-blocked attribute")
	}
	if !hasTestid(body, "item-T-0004-reason") {
		t.Error("T-0004's reason text should render on the card")
	}

	// T-0002 has no reason.
	if !regexp.MustCompile(`data-item-id="T-0002"[^>]*data-has-reason="false"`).MatchString(body) {
		t.Errorf("T-0002 should carry data-has-reason=false:\n%s", body)
	}

	// T-0009 sits on the custom "review" stage.
	if !regexp.MustCompile(`data-item-id="T-0009"[^>]*data-stage="review"`).MatchString(body) {
		t.Errorf("T-0009 should carry data-stage=review:\n%s", body)
	}

	// Done items carry no stage at all.
	if regexp.MustCompile(`data-item-id="T-0006"[^>]*data-stage=`).MatchString(body) {
		t.Error("a done item must not carry data-stage")
	}
}

// §5.1.4: tickler eligibility generalizes to any tickler_stages source, not
// only someday - but this fixture's only tickler-carrying item (T-0005) IS on
// someday, so this proves the generalized code path still renders the badge,
// not merely that the old someday-specific path still does.
func TestBoardV2TicklerBadgeRendersOnAnyEligibleStage(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "item-T-0005-tickler") {
		t.Errorf("T-0005 should carry its tickler badge:\n%s", body)
	}
}

func TestBoardV2ActionsGeneralizeStartPauseBlockUnblock(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	// T-0001 (ready): start enabled, pause/unblock disabled, block enabled.
	if !hasEnabledAction(body, "T-0001", "start") {
		t.Error("T-0001 (ready) should be startable")
	}
	if hasEnabledAction(body, "T-0001", "pause") {
		t.Error("T-0001 (ready) should not be pausable")
	}
	if !hasEnabledAction(body, "T-0001", "block") {
		t.Error("T-0001 (ready) should be blockable")
	}

	// T-0003 (working): pause enabled, start disabled.
	if hasEnabledAction(body, "T-0003", "start") {
		t.Error("T-0003 (working) should not be startable")
	}
	if !hasEnabledAction(body, "T-0003", "pause") {
		t.Error("T-0003 (working) should be pausable")
	}

	// T-0004 (blocked): unblock enabled, block disabled.
	if hasEnabledAction(body, "T-0004", "block") {
		t.Error("T-0004 (blocked) should not be blockable again")
	}
	if !hasEnabledAction(body, "T-0004", "unblock") {
		t.Error("T-0004 (blocked) should be unblockable")
	}
}

// wip.working: 2 already holds T-0003; starting a second item should still
// be allowed (used < limit), proving the check reads the stage's own cap,
// not version 1's single directory-wide WipLimit (which stays 0 for a
// version-2 directory).
func TestBoardV2StartDisabledOnlyAtItsOwnStageCap(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	if !hasEnabledAction(body, "T-0001", "start") {
		t.Errorf("T-0001 should still be startable (working: 1/2 used):\n%s", body)
	}
}

func TestStatusBarV2ShowsStageCounts(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	body := ts.get("/p/" + id + "/board").expectStatus(http.StatusOK).Body

	if !hasTestid(body, "status-count-someday") || !hasTestid(body, "status-count-review") {
		t.Errorf("status bar should carry one status-count-<slug> per declared stage:\n%s", body)
	}
	if strings.Contains(body, `data-ready=`) {
		t.Error("the version-2 status bar must not carry the retired data-ready/blocked/someday attributes")
	}
	if !regexp.MustCompile(`data-testid="status-wip"[^>]*data-wip-used="1"[^>]*data-wip-limit="2"`).MatchString(body) {
		t.Errorf("status-wip should reflect the working stage's own cap (1/2):\n%s", body)
	}
}

// The mutation endpoints (item.go's operate()), generalized: block/unblock/
// move set BOTH the version-1 (Section/Blocked) and version-2 (Stage/Reason)
// request fields unconditionally - Store.Move/Pause dispatch on the
// directory's actual version and read only the pair that applies, so one
// call is correct for either, and these prove the version-2 half actually
// works end to end through the HTTP layer, not just that board.go renders
// the resulting state correctly.

func TestItemMutationV2Block(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	got := ts.post("/p/"+id+"/items/T-0002/block", "reason=needs+a+decision",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").
		expectStatus(http.StatusOK).Body
	if !regexp.MustCompile(`data-item-id="T-0002"[^>]*data-stage="blocked"`).MatchString(got) {
		t.Errorf("T-0002 should now be on stage:blocked:\n%s", got)
	}
	if !hasTestid(got, "item-T-0002-reason") {
		t.Error("T-0002's new reason should render")
	}
}

func TestItemMutationV2Unblock(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	got := ts.post("/p/"+id+"/items/T-0004/unblock", "",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").
		expectStatus(http.StatusOK).Body
	if !regexp.MustCompile(`data-item-id="T-0004"[^>]*data-stage="ready"`).MatchString(got) {
		t.Errorf("T-0004 should now be on stage:ready:\n%s", got)
	}
}

func TestItemMutationV2MoveToArbitraryStage(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	got := ts.post("/p/"+id+"/items/T-0001/move", "stage=review",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").
		expectStatus(http.StatusOK).Body
	if !regexp.MustCompile(`data-item-id="T-0001"[^>]*data-stage="review"`).MatchString(got) {
		t.Errorf("T-0001 should now be on the custom stage:review:\n%s", got)
	}
}

func TestItemMutationV2PauseToStage(t *testing.T) {
	ts, id := boardServer(t, "clean-v2-full")
	got := ts.post("/p/"+id+"/items/T-0003/pause", "stage=someday",
		"HX-Request", "true", "Content-Type", "application/x-www-form-urlencoded").
		expectStatus(http.StatusOK).Body
	if !regexp.MustCompile(`data-item-id="T-0003"[^>]*data-stage="someday"`).MatchString(got) {
		t.Errorf("T-0003 should now be on stage:someday:\n%s", got)
	}
}

// hasEnabledAction reports whether an item's action button for op is present
// and not disabled.
func hasEnabledAction(body, id, op string) bool {
	re := regexp.MustCompile(`data-testid="item-` + id + `-action-` + op + `"([^>]*)>`)
	m := re.FindStringSubmatch(body)
	if m == nil {
		return false
	}
	return !strings.Contains(m[1], "disabled")
}
