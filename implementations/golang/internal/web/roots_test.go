package web

import (
	"path/filepath"
	"testing"
)

func TestExpandRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MM_SCAN_TEST_ROOT", filepath.Join(home, "work"))

	got := ExpandRoots([]string{"~/code", "$MM_SCAN_TEST_ROOT/projects", "$MM_SCAN_TEST_UNSET/nope"}, home)
	want := []string{filepath.Join(home, "code"), filepath.Join(home, "work", "projects")}
	if len(got) != len(want) {
		t.Fatalf("ExpandRoots() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("root %d = %q, want %q", i, got[i], want[i])
		}
	}
}
