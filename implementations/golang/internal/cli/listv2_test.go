package cli

import (
	"strings"
	"testing"
)

// --list --stage SLUG: mm.Filter has carried Stage since T-0227, but the CLI
// never read it into a request - found while updating this suite for T-0236.

func TestListCLIFiltersByStage(t *testing.T) {
	r, _ := v2Project(t)
	r.run("--add", "On ready", "--stage", "ready")
	r.run("--add", "On someday", "--stage", "someday")

	got := r.run("--list", "--stage", "someday")
	if got.Code != ExitOK {
		t.Fatalf("list: %s", got)
	}
	if !strings.Contains(got.Stdout, "On someday") {
		t.Errorf("missing the someday item:\n%s", got.Stdout)
	}
	if strings.Contains(got.Stdout, "On ready") {
		t.Errorf("--list --stage someday should not include a ready item:\n%s", got.Stdout)
	}
}
