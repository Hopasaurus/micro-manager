package mm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The recent and favorites lists (spec-gui.md §10).
//
// Both live in $XDG_CONFIG_HOME/micro-manager/, in SEPARATE files so that the
// high-frequency writes of one cannot clobber the other. They are shared with
// the TUI byte for byte (§13), which is why they are the library's and not a
// front end's.
//
// Neither list may contain a secret: paths only, and no item content (§10 rule 7).

// ListSchemaVersion is the schemaVersion this implementation writes.
const ListSchemaVersion = 1

// ListKind selects which of the two files is being handled. Favorites carry
// order and an optional label; recent does not.
type ListKind string

const (
	ListRecent    ListKind = "recent"
	ListFavorites ListKind = "favorites"
)

// Timestamp is a point in time: ISO 8601, extended format, with an explicit UTC
// designator - 2026-07-29T09:14:00Z (spec-gui.md §8.2, spec-file-format.md
// §3.3.1).
//
// Unlike Date this DOES carry a time zone, and the zone is always UTC. These
// files are synchronised between machines, so a local offset would make two
// machines disagree about the order of the same list.
type Timestamp struct {
	t time.Time // always UTC, second granularity
}

// NewTimestamp converts an instant to the stored form: UTC, truncated to the
// second, because the format has no place to put anything finer.
func NewTimestamp(t time.Time) Timestamp {
	return Timestamp{t: t.UTC().Truncate(time.Second)}
}

// ParseTimestamp accepts exactly the extended UTC form. An offset such as
// +02:00 is rejected rather than converted: accepting it would let one machine
// write a form another has to normalise, and the files are compared as text.
func ParseTimestamp(s string) (Timestamp, error) {
	t, err := time.Parse("2006-01-02T15:04:05Z", s)
	if err != nil {
		return Timestamp{}, fmt.Errorf(
			"%w: %q is not an ISO 8601 UTC timestamp (YYYY-MM-DDTHH:MM:SSZ)",
			ErrInvalidArgument, s)
	}
	return Timestamp{t: t.UTC()}, nil
}

func (ts Timestamp) String() string {
	if ts.IsZero() {
		return ""
	}
	return ts.t.Format("2006-01-02T15:04:05Z")
}

func (ts Timestamp) IsZero() bool            { return ts.t.IsZero() }
func (ts Timestamp) Before(o Timestamp) bool { return ts.t.Before(o.t) }
func (ts Timestamp) Time() time.Time         { return ts.t }

// ListEntry is one project in one of the two lists.
type ListEntry struct {
	ProjectID  string
	Path       string
	Name       string
	LastOpened Timestamp

	// ThemeName is carried so Home can show a project in its own colours before
	// opening it. It is a name, never a theme.
	ThemeName string

	// Order and Label are favorites-only (§10). Label overrides the displayed
	// name; Order is the user's own arrangement.
	Order int
	Label string

	// Extra holds per-entry keys this implementation does not know, so that a
	// newer front end's fields survive a write by this one.
	Extra map[string]any
}

// Display returns what to show: the label when a favorite has one, else the
// project name, else the last path element - never an empty string, because a
// row the user cannot identify is worse than a row with an ugly name.
func (e ListEntry) Display() string {
	switch {
	case e.Label != "":
		return e.Label
	case e.Name != "":
		return e.Name
	default:
		return filepath.Base(e.Path)
	}
}

// Missing reports whether the entry's path no longer resolves.
//
// It is a QUESTION, not a cleanup: §10 rule 5 requires such an entry to be
// rendered with data-missing="true" and never silently removed. A project on an
// unmounted drive is not a deleted project.
func (e ListEntry) Missing() bool {
	if e.Path == "" {
		return true
	}
	fi, err := os.Stat(e.Path)
	return err != nil || !fi.IsDir()
}

// ProjectList is one of the two files.
type ProjectList struct {
	Path    string
	Kind    ListKind
	Entries []ListEntry
	Exists  bool

	raw map[string]any // everything else in the document, preserved
}

// LoadProjectList reads a list file. A missing file is an empty list, not an
// error: neither list exists until something is opened.
func LoadProjectList(path string, kind ListKind) (*ProjectList, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectList{Path: path, Kind: kind, raw: map[string]any{}}, nil
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrIO, path, err)
	}
	l, err := ParseProjectList(path, kind, data)
	if err != nil {
		return nil, err
	}
	l.Exists = true
	return l, nil
}

// ParseProjectList builds a list from content rather than from disk, which is
// what a JSON API needs when a client PUTs a whole list back (spec-gui.md §4.2,
// GET·PUT /recent and /favorites). The parse is the same one LoadProjectList
// runs, so a document this accepts is a document that loads.
func ParseProjectList(path string, kind ListKind, data []byte) (*ProjectList, error) {
	l := &ProjectList{Path: path, Kind: kind, raw: map[string]any{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return l, nil
	}
	if err := json.Unmarshal(data, &l.raw); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalidArgument, path, err)
	}

	arr, _ := l.raw["entries"].([]any)
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		e := ListEntry{Extra: map[string]any{}}
		for _, key := range sortedKeys(obj) {
			v := obj[key]
			switch key {
			case "projectId":
				e.ProjectID, _ = v.(string)
			case "path":
				e.Path, _ = v.(string)
			case "name":
				e.Name, _ = v.(string)
			case "themeName":
				e.ThemeName, _ = v.(string)
			case "label":
				e.Label, _ = v.(string)
			case "order":
				e.Order, _ = jsonInt(v)
			case "lastOpened":
				if s, ok := v.(string); ok {
					// A malformed timestamp loses the ordering hint and nothing
					// else. Dropping the entry would lose a favorite over a typo.
					ts, err := ParseTimestamp(s)
					if err == nil {
						e.LastOpened = ts
					}
				}
			default:
				e.Extra[key] = v
			}
		}
		if e.Path == "" {
			continue // an entry with no path names nothing
		}
		if e.ProjectID == "" {
			if id, err := ProjectID(e.Path); err == nil {
				e.ProjectID = id
			}
		}
		l.Entries = append(l.Entries, e)
	}
	l.sortForKind()
	return l, nil
}

// WriteProjectList writes a list file's content atomically, which is what a PUT
// of a whole list needs (spec-gui.md §4.2). The document is parsed first so a
// client cannot write a file the service itself could not load; unknown keys are
// preserved because they were never decoded away.
func WriteProjectList(path string, kind ListKind, data []byte) error {
	if _, err := ParseProjectList(path, kind, data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrIO, filepath.Dir(path), err)
	}
	return writeFileAtomic(path, data)
}

// sortForKind puts the file in the order the list is defined to be in: recent by
// lastOpened newest first, favorites by the user's own order.
func (l *ProjectList) sortForKind() {
	switch l.Kind {
	case ListFavorites:
		sort.SliceStable(l.Entries, func(i, j int) bool {
			return l.Entries[i].Order < l.Entries[j].Order
		})
		for i := range l.Entries {
			l.Entries[i].Order = i
		}
	default:
		sort.SliceStable(l.Entries, func(i, j int) bool {
			return l.Entries[j].LastOpened.Before(l.Entries[i].LastOpened)
		})
	}
}

// Find returns an entry by project id.
func (l *ProjectList) Find(projectID string) (ListEntry, bool) {
	for _, e := range l.Entries {
		if e.ProjectID == projectID {
			return e, true
		}
	}
	return ListEntry{}, false
}

// Display returns the first count entries, which is what ui.recentCount and
// ui.favoritesCount control. Zero means show none; a negative count, or one
// larger than the list, returns what there is.
//
// This is deliberately separate from retention: shrinking the display count MUST
// NOT discard stored history (spec-gui.md §9.2).
func (l *ProjectList) Display(count int) []ListEntry {
	if count < 0 || count > len(l.Entries) {
		count = len(l.Entries)
	}
	return append([]ListEntry(nil), l.Entries[:count]...)
}

// Touch records that a project was opened (§10 rule 1).
//
// An entry already present is MOVED to the front, never duplicated, keeping the
// fields it already had unless this call carries better ones. maxStored trims
// the tail; zero or less leaves the list untrimmed.
func (l *ProjectList) Touch(e ListEntry, now Timestamp, maxStored int) {
	if e.ProjectID == "" && e.Path != "" {
		if id, err := ProjectID(e.Path); err == nil {
			e.ProjectID = id
		}
	}
	if prev, ok := l.Find(e.ProjectID); ok {
		if e.Name == "" {
			e.Name = prev.Name
		}
		if e.ThemeName == "" {
			e.ThemeName = prev.ThemeName
		}
		if e.Label == "" {
			e.Label = prev.Label
		}
		if len(e.Extra) == 0 {
			e.Extra = prev.Extra
		}
		l.Remove(e.ProjectID)
	}
	e.LastOpened = now
	l.Entries = append([]ListEntry{e}, l.Entries...)
	if maxStored > 0 && len(l.Entries) > maxStored {
		l.Entries = l.Entries[:maxStored]
	}
	if l.Kind == ListFavorites {
		l.renumber()
	}
}

// Add appends to a favorites list, at the end of the user's order. It is a no-op
// when the project is already a favorite: toggling on twice is not two entries.
func (l *ProjectList) Add(e ListEntry) {
	if e.ProjectID == "" && e.Path != "" {
		if id, err := ProjectID(e.Path); err == nil {
			e.ProjectID = id
		}
	}
	if _, ok := l.Find(e.ProjectID); ok {
		return
	}
	e.Order = len(l.Entries)
	l.Entries = append(l.Entries, e)
}

// Remove drops an entry. Removing what is not there is not an error - a UI's
// remove button and a concurrent write may race, and the outcome is the same.
func (l *ProjectList) Remove(projectID string) {
	out := l.Entries[:0]
	for _, e := range l.Entries {
		if e.ProjectID != projectID {
			out = append(out, e)
		}
	}
	l.Entries = out
	if l.Kind == ListFavorites {
		l.renumber()
	}
}

// Reorder moves an entry to an index, which is what a favorites drag commits
// (§10 rule 3). Out-of-range indexes clamp rather than fail: a drop past the end
// of a list means the end of the list.
func (l *ProjectList) Reorder(projectID string, index int) error {
	from := -1
	for i, e := range l.Entries {
		if e.ProjectID == projectID {
			from = i
			break
		}
	}
	if from < 0 {
		return fmt.Errorf("%w: %s is not in %s", ErrNotFound, projectID, filepath.Base(l.Path))
	}
	if index < 0 {
		index = 0
	}
	if index > len(l.Entries)-1 {
		index = len(l.Entries) - 1
	}
	e := l.Entries[from]
	l.Entries = append(l.Entries[:from], l.Entries[from+1:]...)
	rest := append([]ListEntry(nil), l.Entries[index:]...)
	l.Entries = append(append(l.Entries[:index], e), rest...)
	l.renumber()
	return nil
}

func (l *ProjectList) renumber() {
	for i := range l.Entries {
		l.Entries[i].Order = i
	}
}

// Save writes the list atomically (§10 rule 6).
func (l *ProjectList) Save(dryRun bool) error {
	data, err := l.Bytes()
	if err != nil {
		return err
	}
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.Path), 0o755); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrIO, filepath.Dir(l.Path), err)
	}
	if err := writeFileAtomic(l.Path, data); err != nil {
		return err
	}
	l.Exists = true
	return nil
}

// Bytes renders the list as it would be written, unknown keys included.
func (l *ProjectList) Bytes() ([]byte, error) {
	if l.raw == nil {
		l.raw = map[string]any{}
	}
	l.raw["schemaVersion"] = ListSchemaVersion

	entries := make([]any, 0, len(l.Entries))
	for _, e := range l.Entries {
		obj := map[string]any{}
		for k, v := range e.Extra {
			obj[k] = v
		}
		obj["projectId"] = e.ProjectID
		obj["path"] = e.Path
		if e.Name != "" {
			obj["name"] = e.Name
		}
		if !e.LastOpened.IsZero() {
			obj["lastOpened"] = e.LastOpened.String()
		}
		if e.ThemeName != "" {
			obj["themeName"] = e.ThemeName
		}
		if l.Kind == ListFavorites {
			obj["order"] = e.Order
			if e.Label != "" {
				obj["label"] = e.Label
			}
		}
		entries = append(entries, obj)
	}
	l.raw["entries"] = entries
	return marshalJSONFile(l.raw)
}

// UpdateProjectList is read, modify, write against one list file.
//
// It re-reads immediately before writing rather than working from a list the
// caller has been holding, which is how §10 rule 6's "tolerate concurrent
// writers" is honoured at this granularity: two front ends may both be running,
// and the loser of a race should lose one edit rather than the whole file.
func UpdateProjectList(path string, kind ListKind, fn func(*ProjectList) error) error {
	l, err := LoadProjectList(path, kind)
	if err != nil {
		return err
	}
	if err := fn(l); err != nil {
		return err
	}
	return l.Save(false)
}

// EntryFor builds a list entry from an open directory, which is what a front end
// records when it opens a project.
func EntryFor(d Directory, themeName string) ListEntry {
	name := d.Project
	if name == "" {
		name = filepath.Base(d.Path)
	}
	return ListEntry{
		ProjectID: d.ProjectID,
		Path:      d.Path,
		Name:      name,
		ThemeName: themeName,
	}
}

// ListPathFor returns the file a kind lives in.
func ListPathFor(sp SystemPaths, kind ListKind) string {
	if kind == ListFavorites {
		return sp.Favorites
	}
	return sp.Recent
}

// listKindFromPath is a convenience for callers holding only a filename.
func listKindFromPath(path string) ListKind {
	if strings.Contains(filepath.Base(path), "favorite") {
		return ListFavorites
	}
	return ListRecent
}
