package mm

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func at(t *testing.T, s string) Timestamp {
	t.Helper()
	ts, err := ParseTimestamp(s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func loadList(t *testing.T, path string, kind ListKind) *ProjectList {
	t.Helper()
	l, err := LoadProjectList(path, kind)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	return l
}

// spec-gui.md §10: lastOpened is a TIMESTAMP - ISO 8601, extended format, with a
// UTC designator - because these files are synchronised between machines.
func TestTimestamp(t *testing.T) {
	ts, err := ParseTimestamp("2026-07-29T09:14:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := ts.String(); got != "2026-07-29T09:14:00Z" {
		t.Errorf("round trip = %q", got)
	}

	for _, bad := range []string{
		"2026-07-29",                // a DATE is not a TIMESTAMP
		"2026-07-29T09:14:00+02:00", // an offset, not UTC
		"2026-07-29 09:14:00Z",      // no T
		"20260729T091400Z",          // basic format
		"1785490440",                // epoch seconds
		"",
	} {
		if _, err := ParseTimestamp(bad); !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("ParseTimestamp(%q) should fail, got %v", bad, err)
		}
	}

	local := time.Date(2026, 7, 29, 11, 14, 30, 500, time.FixedZone("CEST", 2*3600))
	if got := NewTimestamp(local).String(); got != "2026-07-29T09:14:30Z" {
		t.Errorf("NewTimestamp did not convert to UTC seconds: %q", got)
	}
	if !(Timestamp{}).IsZero() || (Timestamp{}).String() != "" {
		t.Error("the zero timestamp should render as empty")
	}
}

func TestMissingListFileIsEmpty(t *testing.T) {
	l := loadList(t, filepath.Join(t.TempDir(), "recent.json"), ListRecent)
	if l.Exists || len(l.Entries) != 0 {
		t.Errorf("a missing file should be an empty list: %+v", l)
	}
	if got := l.Display(10); len(got) != 0 {
		t.Errorf("Display on an empty list = %v", got)
	}
}

// §10 rule 1: opening a project moves it to the FRONT, updating lastOpened. An
// entry already present is moved, never duplicated.
func TestTouchMovesToTheFrontWithoutDuplicating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	l := loadList(t, path, ListRecent)

	l.Touch(ListEntry{ProjectID: "aaa", Path: "/a", Name: "A"}, at(t, "2026-07-01T00:00:00Z"), 100)
	l.Touch(ListEntry{ProjectID: "bbb", Path: "/b", Name: "B"}, at(t, "2026-07-02T00:00:00Z"), 100)
	l.Touch(ListEntry{ProjectID: "aaa", Path: "/a"}, at(t, "2026-07-03T00:00:00Z"), 100)

	if len(l.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(l.Entries), l.Entries)
	}
	if l.Entries[0].ProjectID != "aaa" {
		t.Errorf("front is %q, want the just-opened aaa", l.Entries[0].ProjectID)
	}
	if l.Entries[0].LastOpened.String() != "2026-07-03T00:00:00Z" {
		t.Errorf("lastOpened = %q", l.Entries[0].LastOpened)
	}
	if l.Entries[0].Name != "A" {
		t.Error("re-opening dropped the name the entry already had")
	}
}

// recentMaxStored trims what is retained; recentCount only limits what is shown.
// Shrinking the display count MUST NOT discard stored history (§9.2, §10 rule 2).
func TestRetentionAndDisplayAreIndependent(t *testing.T) {
	l := loadList(t, filepath.Join(t.TempDir(), "recent.json"), ListRecent)
	for i := 0; i < 8; i++ {
		l.Touch(ListEntry{ProjectID: string(rune('a' + i)), Path: "/p" + string(rune('a'+i))},
			at(t, "2026-07-01T00:00:00Z"), 5)
	}
	if len(l.Entries) != 5 {
		t.Errorf("retained %d entries, want maxStored 5", len(l.Entries))
	}
	if got := l.Display(2); len(got) != 2 {
		t.Errorf("Display(2) returned %d", len(got))
	}
	if len(l.Entries) != 5 {
		t.Error("Display trimmed the stored list")
	}
	if got := l.Display(0); len(got) != 0 {
		t.Errorf("Display(0) returned %d entries", len(got))
	}
	if got := l.Display(99); len(got) != 5 {
		t.Errorf("Display(99) returned %d, want everything there is", len(got))
	}
}

// §10 rule 3: favorites are user-ordered and reorderable by drag.
func TestFavoritesOrdering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	l := loadList(t, path, ListFavorites)
	for _, id := range []string{"a", "b", "c", "d"} {
		l.Add(ListEntry{ProjectID: id, Path: "/" + id})
	}
	l.Add(ListEntry{ProjectID: "b", Path: "/b"}) // toggling on twice is one entry
	if len(l.Entries) != 4 {
		t.Fatalf("want 4 favorites, got %d", len(l.Entries))
	}

	if err := l.Reorder("d", 0); err != nil {
		t.Fatal(err)
	}
	if got := ids(l); got != "d,a,b,c" {
		t.Errorf("after moving d to the front: %s", got)
	}
	if err := l.Reorder("d", 99); err != nil {
		t.Fatal(err)
	}
	if got := ids(l); got != "a,b,c,d" {
		t.Errorf("a drop past the end should clamp to the end: %s", got)
	}
	for i, e := range l.Entries {
		if e.Order != i {
			t.Errorf("entry %d carries order %d", i, e.Order)
		}
	}
	if err := l.Reorder("nope", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

// §10 rule 5: an entry whose path no longer resolves is rendered as missing and
// MUST NOT be silently removed. A project on an unmounted drive is not deleted.
func TestMissingEntriesSurvive(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "here")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "favorites.json")

	l := loadList(t, path, ListFavorites)
	l.Add(ListEntry{ProjectID: "here", Path: real})
	l.Add(ListEntry{ProjectID: "gone", Path: filepath.Join(dir, "unmounted")})
	if err := l.Save(false); err != nil {
		t.Fatal(err)
	}

	reloaded := loadList(t, path, ListFavorites)
	if len(reloaded.Entries) != 2 {
		t.Fatalf("a missing entry was dropped: %+v", reloaded.Entries)
	}
	if reloaded.Entries[0].Missing() {
		t.Error("an existing directory reported Missing")
	}
	if !reloaded.Entries[1].Missing() {
		t.Error("a vanished directory did not report Missing")
	}

	// A file where a directory should be is missing too.
	fileEntry := ListEntry{Path: filepath.Join(dir, "favorites.json")}
	if !fileEntry.Missing() {
		t.Error("a plain file is not a project directory")
	}
}

// The same preservation rule as config and theme, and for the same reason: two
// front ends over one set of files (§9.4).
func TestListUnknownKeysSurviveAWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	writeJSON(t, path, `{
	  "schemaVersion": 1,
	  "generatedBy": "some other front end",
	  "entries": [
	    { "projectId": "aaa", "path": "/a", "name": "A", "order": 0,
	      "lastOpened": "2026-07-01T00:00:00Z", "pinnedColumn": 3 }
	  ]
	}`)

	l := loadList(t, path, ListFavorites)
	l.Add(ListEntry{ProjectID: "bbb", Path: "/b", Name: "B"})
	if err := l.Save(false); err != nil {
		t.Fatal(err)
	}

	var got map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["generatedBy"] == nil {
		t.Error("an unknown top-level key was dropped")
	}
	entries, _ := got["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	first, _ := entries[0].(map[string]any)
	if first["pinnedColumn"] == nil {
		t.Error("an unknown per-entry key was dropped")
	}
	if first["lastOpened"] != "2026-07-01T00:00:00Z" {
		t.Errorf("lastOpened was rewritten: %v", first["lastOpened"])
	}
}

// A recent file is read newest first and a favorites file in the user's order,
// whatever order the entries happen to sit in on disk.
func TestLoadOrdersByKind(t *testing.T) {
	dir := t.TempDir()
	recent := filepath.Join(dir, "recent.json")
	writeJSON(t, recent, `{"entries":[
	  {"projectId":"old","path":"/old","lastOpened":"2026-01-01T00:00:00Z"},
	  {"projectId":"new","path":"/new","lastOpened":"2026-07-01T00:00:00Z"}
	]}`)
	if got := ids(loadList(t, recent, ListRecent)); got != "new,old" {
		t.Errorf("recent order = %s, want newest first", got)
	}

	favorites := filepath.Join(dir, "favorites.json")
	writeJSON(t, favorites, `{"entries":[
	  {"projectId":"second","path":"/2","order":1},
	  {"projectId":"first","path":"/1","order":0}
	]}`)
	if got := ids(loadList(t, favorites, ListFavorites)); got != "first,second" {
		t.Errorf("favorites order = %s, want the user's order", got)
	}
}

// A missing projectId is derived rather than treated as a broken entry: a hand
// written favorites file with only paths in it is a reasonable thing to have.
func TestProjectIDIsDerivedWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	writeJSON(t, path, `{"entries":[{"path":"/home/u/code/micro-manager"}]}`)

	l := loadList(t, path, ListFavorites)
	want, err := ProjectID("/home/u/code/micro-manager")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 1 || l.Entries[0].ProjectID != want {
		t.Errorf("entries = %+v, want a derived id %s", l.Entries, want)
	}
}

// A malformed timestamp costs the ordering hint and nothing else. Dropping the
// entry would lose a favorite over a typo.
func TestMalformedTimestampDoesNotDropTheEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	writeJSON(t, path, `{"entries":[{"projectId":"a","path":"/a","lastOpened":"yesterday"}]}`)
	l := loadList(t, path, ListRecent)
	if len(l.Entries) != 1 {
		t.Fatalf("the entry was dropped: %+v", l.Entries)
	}
	if !l.Entries[0].LastOpened.IsZero() {
		t.Error("a malformed timestamp was accepted")
	}
}

func TestMalformedListFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	writeJSON(t, path, `{"entries": [`)
	if _, err := LoadProjectList(path, ListRecent); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("want ErrInvalidArgument, got %v", err)
	}
}

// UpdateProjectList re-reads before writing, so a concurrent writer loses one
// edit rather than the whole file (§10 rule 6).
func TestUpdateRereadsBeforeWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.json")
	if err := UpdateProjectList(path, ListFavorites, func(l *ProjectList) error {
		l.Add(ListEntry{ProjectID: "a", Path: "/a"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A stale handle, as a second front end would hold.
	stale := loadList(t, path, ListFavorites)

	if err := UpdateProjectList(path, ListFavorites, func(l *ProjectList) error {
		l.Add(ListEntry{ProjectID: "b", Path: "/b"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProjectList(path, ListFavorites, func(l *ProjectList) error {
		l.Add(ListEntry{ProjectID: "c", Path: "/c"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if got := ids(loadList(t, path, ListFavorites)); got != "a,b,c" {
		t.Errorf("entries = %s, want every update kept", got)
	}
	if len(stale.Entries) != 1 {
		t.Error("the stale handle was mutated behind the caller's back")
	}

	if err := UpdateProjectList(path, ListFavorites, func(*ProjectList) error {
		return errors.New("no")
	}); err == nil {
		t.Error("an error from the callback should abort the write")
	}
	if got := ids(loadList(t, path, ListFavorites)); got != "a,b,c" {
		t.Errorf("an aborted update still wrote: %s", got)
	}
}

func TestEntryDisplayName(t *testing.T) {
	cases := []struct {
		e    ListEntry
		want string
	}{
		{ListEntry{Label: "L", Name: "N", Path: "/p/dir"}, "L"},
		{ListEntry{Name: "N", Path: "/p/dir"}, "N"},
		{ListEntry{Path: "/p/dir"}, "dir"},
	}
	for _, c := range cases {
		if got := c.e.Display(); got != c.want {
			t.Errorf("Display() = %q, want %q", got, c.want)
		}
	}
}

func TestEntryForADirectory(t *testing.T) {
	dir := newDir(t, nil)
	d, err := mustOpen(t, dir).Directory()
	if err != nil {
		t.Fatal(err)
	}
	e := EntryFor(d, "Sample One — Dark")
	if e.ProjectID != d.ProjectID || e.Path != d.Path || e.Name != "Sample One" {
		t.Errorf("EntryFor = %+v", e)
	}
	if e.ThemeName != "Sample One — Dark" {
		t.Errorf("themeName = %q", e.ThemeName)
	}
}

// §10 rule 7: no secrets, paths only, and no item content.
func TestListsCarryNoItemContent(t *testing.T) {
	dir := newDir(t, nil)
	d, err := mustOpen(t, dir).Directory()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "recent.json")
	l := loadList(t, path, ListRecent)
	l.Touch(EntryFor(d, ""), at(t, "2026-07-30T12:00:00Z"), 100)
	data, err := l.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"T-000", "prio:", "backlog", "- [ ]"} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf("the list file carries item content: %q appears in\n%s", forbidden, data)
		}
	}
}

func TestListPathFor(t *testing.T) {
	sp := NewSystemPaths("/home/u/.config")
	if ListPathFor(sp, ListRecent) != sp.Recent || ListPathFor(sp, ListFavorites) != sp.Favorites {
		t.Error("ListPathFor returned the wrong file")
	}
	if listKindFromPath(sp.Favorites) != ListFavorites || listKindFromPath(sp.Recent) != ListRecent {
		t.Error("listKindFromPath misread a filename")
	}
}

func ids(l *ProjectList) string {
	out := make([]string, 0, len(l.Entries))
	for _, e := range l.Entries {
		out = append(out, e.ProjectID)
	}
	return strings.Join(out, ",")
}
