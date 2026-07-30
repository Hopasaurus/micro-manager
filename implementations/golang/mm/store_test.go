package mm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// dirBacklog is sampleBacklog with a next_id above every ID in the fixture set.
// sampleBacklog alone declares next_id: T-0004, which is right for the parser
// tests but violates I2 once sampleDone's T-0008..T-0010 share the directory.
var dirBacklog = strings.Replace(sampleBacklog, "next_id: T-0004", "next_id: T-0011", 1)

// newDir builds a valid micro-manager directory on disk and returns its path.
func newDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	base := map[string]string{
		"backlog.md":    dirBacklog,
		"done.md":       sampleDone,
		"working.01.md": idleSlot,
	}
	for k, v := range files {
		base[k] = v
	}
	for name, body := range base {
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

func mustOpen(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return s
}

func TestOpen(t *testing.T) {
	dir := newDir(t, nil)
	s := mustOpen(t, dir)
	if !filepath.IsAbs(s.Path()) {
		t.Errorf("Path() should be absolute, got %q", s.Path())
	}

	if _, err := Open(filepath.Join(dir, "nope")); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	if _, err := Open(filepath.Join(dir, "backlog.md")); !errors.Is(err, ErrNotFound) {
		t.Errorf("opening a file should fail, got %v", err)
	}
}

func TestDirectory(t *testing.T) {
	dir := newDir(t, map[string]string{"working.02.md": busySlot})
	d, err := mustOpen(t, dir).Directory()
	if err != nil {
		t.Fatal(err)
	}
	if d.Project != "Sample One" {
		t.Errorf("Project = %q", d.Project)
	}
	if d.NextID != "T-0011" {
		t.Errorf("NextID = %q", d.NextID)
	}
	if d.WipLimit != 2 || d.WipUsed != 1 {
		t.Errorf("wip = %d/%d, want 1/2", d.WipUsed, d.WipLimit)
	}
	if len(d.Slots) != 2 || d.Slots[0].Occupied() || !d.Slots[1].Occupied() {
		t.Errorf("slots wrong: %+v", d.Slots)
	}
}

func TestListDefaultsToBacklogInOnDiskOrder(t *testing.T) {
	items, err := mustOpen(t, newDir(t, nil)).List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	// On-disk order, not sorted by priority: T-0005 is high but comes second
	// because that is where the user put it.
	want := []ID{"T-0001", "T-0005", "T-0002", "T-0003"}
	if got := idsOfValues(items); !sameIDs(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestListFilters(t *testing.T) {
	dir := newDir(t, map[string]string{"working.02.md": busySlot})
	s := mustOpen(t, dir)

	cases := []struct {
		name string
		f    Filter
		want []ID
	}{
		{"section", Filter{Section: SectionBlocked}, []ID{"T-0002"}},
		{"prio", Filter{Prio: PrioHigh}, []ID{"T-0005"}},
		{"tag", Filter{Tag: "example"}, []ID{"T-0001"}},
		{"blocked", Filter{Blocked: true}, []ID{"T-0002"}},
		{"limit", Filter{Limit: 2}, []ID{"T-0001", "T-0005"}},
		{"working", Filter{State: StateWorking}, []ID{"T-0042"}},
		{"done", Filter{State: StateDone}, []ID{"T-0010", "T-0009", "T-0008"}},
	}
	for _, c := range cases {
		got, err := s.List(c.f)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !sameIDs(idsOfValues(got), c.want) {
			t.Errorf("%s: got %v, want %v", c.name, idsOfValues(got), c.want)
		}
	}

	all, _ := s.List(Filter{State: StateAll})
	if len(all) != 8 {
		t.Errorf("StateAll should span every file, got %d", len(all))
	}
}

// An absent prio reads as med, so filtering by med must find it.
func TestListPrioUsesEffectiveValue(t *testing.T) {
	src := strings.Replace(dirBacklog,
		"- [ ] [T-0001] First | prio:med | tags:example | created:2026-07-29",
		"- [ ] [T-0001] First | tags:example | created:2026-07-29", 1)
	got, err := mustOpen(t, newDir(t, map[string]string{"backlog.md": src})).List(Filter{Prio: PrioMed})
	if err != nil {
		t.Fatal(err)
	}
	if !sameIDs(idsOfValues(got), []ID{"T-0001"}) {
		t.Errorf("absent prio should match med, got %v", idsOfValues(got))
	}
}

func TestGet(t *testing.T) {
	dir := newDir(t, map[string]string{"working.02.md": busySlot})
	s := mustOpen(t, dir)

	// Works wherever the item lives.
	for _, id := range []ID{"T-0001", "T-0042", "T-0010"} {
		it, err := s.Get(id)
		if err != nil {
			t.Errorf("Get(%s): %v", id, err)
			continue
		}
		if it.ID != id {
			t.Errorf("got %s", it.ID)
		}
	}
	if _, err := s.Get("T-9999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// A directory that already has violations must still open and list. Refusing to
// read a broken directory removes the tool exactly when it is needed.
func TestReadsWorkOnABrokenDirectory(t *testing.T) {
	broken := strings.Replace(dirBacklog, "prio:med", "prio:URGENT", 1)
	s := mustOpen(t, newDir(t, map[string]string{"backlog.md": broken}))

	items, err := s.List(Filter{})
	if err != nil {
		t.Fatalf("list should still work: %v", err)
	}
	if len(items) == 0 {
		t.Error("readable items should still be listed")
	}
	vs, err := s.Validate()
	if err != nil {
		t.Fatalf("validate should not error: %v", err)
	}
	if len(vs) == 0 {
		t.Error("the violation should be reported")
	}
}

func TestStoreIsSafeForConcurrentUse(t *testing.T) {
	s := mustOpen(t, newDir(t, nil))
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				if _, err := s.List(Filter{State: StateAll}); err != nil {
					done <- err
					return
				}
				if _, err := s.Validate(); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent use failed: %v", err)
		}
	}
}

func idsOfValues(items []Item) []ID {
	out := make([]ID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func sameIDs(a, b []ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
