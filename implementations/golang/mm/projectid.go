package mm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ProjectIDLength is the number of hex characters in a project id
// (spec-gui.md §3.1).
const ProjectIDLength = 12

// ProjectID derives the identifier a URI addresses a directory by
// (spec-gui.md §3.1):
//
//	projectId = lowercase(hex(sha256(canonicalPath)))[0:12]
//
// The algorithm is fixed by the spec rather than left to the implementation, so
// that one test fixture computes the same id against every implementation. Do
// not "improve" it.
//
// The path need not exist. A favorite on an unmounted drive still has an id —
// spec-gui.md §10 rule 5 requires it to be rendered as missing rather than
// silently dropped, and it cannot be rendered without one.
//
// The id is derived, never parsed. Nothing reconstructs a path from it; a front
// end resolves an id by looking it up among the directories it knows about.
func ProjectID(path string) (string, error) {
	canon, err := CanonicalPath(path)
	if err != nil {
		return "", err
	}
	return projectIDOf(canon), nil
}

// projectIDOf hashes an already-canonical path. It touches no filesystem, which
// is what lets the cross-implementation test vectors assert on paths that do not
// exist on the machine running the test.
func projectIDOf(canonicalPath string) string {
	sum := sha256.Sum256([]byte(nfc(canonicalPath)))
	return hex.EncodeToString(sum[:])[:ProjectIDLength]
}

// nfc normalises to Unicode NFC, the form spec-gui.md §3.1 hashes.
//
// This is not cosmetic. macOS returns decomposed (NFD) filenames from a
// directory listing while a path typed into a config file is usually composed,
// so without this one directory yields two ids in a single process — and the
// same directory yields different ids on macOS and Linux, which defeats the
// point of fixing the algorithm.
//
// It applies to the HASH INPUT ONLY. The canonical path is left exactly as the
// filesystem reported it, because on a byte-exact filesystem a normalised path
// may not open the file it names.
func nfc(s string) string { return norm.NFC.String(s) }

// CanonicalPath resolves a directory path to the form ProjectID hashes: absolute,
// symlinks resolved, no trailing separator (spec-gui.md §3.1).
//
// It is exported because a front end needs it for more than ids: two entries in
// the favorites file that reach one directory by different routes are one
// project, and comparing raw strings would not say so.
//
// A path that does not resolve is not an error — it is made absolute and
// returned. Discovery, recent and favorites all carry entries for directories
// that are not present right now.
func CanonicalPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidArgument)
	}
	canon := canonical(path)
	// filepath.Abs cleans, so a trailing separator is already gone; strip one
	// anyway for a caller that reaches this with a pre-canonicalised string,
	// and never strip the root itself down to nothing.
	if len(canon) > 1 {
		canon = strings.TrimRight(canon, string(filepath.Separator))
		if canon == "" {
			canon = string(filepath.Separator)
		}
	}
	return canon, nil
}
