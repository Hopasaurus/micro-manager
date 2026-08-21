package cli

import (
	"strings"
	"testing"
)

// --block/--unblock generalized to version 2 (found while migrating this
// suite's v1 fixtures for T-0236): runBlock/runUnblock only ever set
// MoveRequest.Section/Blocked, so on a v2 directory the move landed nowhere
// - Store.Move's v2 dispatch reads Stage, not Section, and both were left
// empty. Fixed to set Stage/Reason too, mirroring the GUI's already-
// generalized block/unblock (T-0230, internal/web/item.go's operate()).

func TestBlockUnblockCLIV2(t *testing.T) {
	r, dir := v2Project(t)
	r.run("--add", "Something")

	if got := r.run("--block", "T-0001", "--reason", "waiting on ops"); got.Code != ExitOK {
		t.Fatalf("block: %s", got)
	}
	board := readFileAt(t, dir, "board.md")
	if !strings.Contains(board, "stage:blocked") || !strings.Contains(board, "reason:waiting on ops") {
		t.Errorf("block did not land on stage:blocked with its reason:\n%s", board)
	}

	if got := r.run("--unblock", "T-0001"); got.Code != ExitOK {
		t.Fatalf("unblock: %s", got)
	}
	board = readFileAt(t, dir, "board.md")
	if !strings.Contains(board, "stage:ready") {
		t.Errorf("unblock did not land on stage:ready:\n%s", board)
	}
}
