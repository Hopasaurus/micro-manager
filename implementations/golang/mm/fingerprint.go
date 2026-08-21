package mm

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Fingerprint is a cheap summary of a directory's on-disk state
// (spec-tools.md §2.4).
//
// It changes when the directory changes, and a UI polls it to decide whether to
// re-read. Equality means "no need to re-read" and NOTHING STRONGER: it is not a
// lock and not a correctness mechanism. Staleness is caught at write time by the
// size-and-mtime check of §7 rule 5, which is why this one is allowed to be as
// coarse as it is.
type Fingerprint string

// String renders the fingerprint for logging and for a JSON payload.
func (f Fingerprint) String() string { return string(f) }

// Fingerprint summarises the files this directory owns, without reading one of
// them (spec-tools.md §2.4 rule 1).
//
// Owned means the files of the format spec: backlog.md (version 1) or
// board.md (version 2), done.md and any done-YYYY.md archive, every
// working.NN.md, and every file in details/. The listing itself is part of
// the input, so adding a slot file or a detail file changes the value even
// before anything is written into it.
//
// theme.json and config.json are deliberately excluded. They sit outside the
// file-format spec (spec-gui.md §8.2), and a theme edit must not present itself
// to a client as a change to the data.
func (s *Store) Fingerprint() (Fingerprint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := readDirNames(s.path)
	if err != nil {
		return "", err
	}

	names := make([]string, 0, len(entries)+8)
	for _, name := range entries {
		if isOwnedFile(name) {
			names = append(names, name)
		}
	}
	for _, name := range detailNames(s.path) {
		names = append(names, "details/"+name)
	}
	sort.Strings(names)

	// name\0size\0mtime\n per file. The separator is NUL because it is the one
	// byte a filename cannot contain: with any printable separator, two
	// different listings could render to the same bytes.
	var b strings.Builder
	for _, name := range names {
		st := stampOf(filepath.Join(s.path, name))
		if st.missing {
			// Raced with a delete between listing and stat. Skipping it is
			// right: the next poll sees a directory without it, which is what
			// the caller needs to know.
			continue
		}
		b.WriteString(name)
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(st.size, 10))
		b.WriteByte(0)
		b.WriteString(strconv.FormatInt(st.modTime, 10))
		b.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(b.String()))
	return Fingerprint(hex.EncodeToString(sum[:])), nil
}

// isOwnedFile reports whether a name in the directory root belongs to the
// format spec. Templates in details/ are handled by detailNames; everything
// else - theme.json, config.json, structure.md, an editor's swap file - is not
// data and does not move the fingerprint.
func isOwnedFile(name string) bool {
	switch {
	case name == "backlog.md", name == "board.md", name == "done.md":
		return true
	case isDoneArchiveName(name):
		return true
	}
	// A plain working.md is a name needing migration (I10) rather than a file
	// the directory owns, and discoverWorkingFiles reports it as such. It is
	// still data, so a change to it must be visible.
	if name == "working.md" {
		return true
	}
	_, _, ok := isWorkingFileName(name)
	return ok
}

// isDoneArchiveName matches done-YYYY.md, the archive form of done.md
// (spec-file-format.md §7.3).
func isDoneArchiveName(name string) bool {
	const prefix, suffix = "done-", ".md"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
		return false
	}
	year := name[len(prefix) : len(name)-len(suffix)]
	if len(year) != 4 {
		return false
	}
	for _, r := range year {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
