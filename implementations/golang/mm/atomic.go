package mm

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Atomic file writing, and concurrent-modification detection.
//
// spec-tools.md §7 rule 3: each file is written to a temporary file in the same
// directory and renamed over the target, so a crash leaves either the old file
// or the new one, never a truncated one. rename(2) is atomic within a
// filesystem; the temp file must therefore be a sibling of the target, not in
// /tmp, or the rename degrades into a copy that can be interrupted halfway.

// stamp records what a file looked like when it was read, so a write can detect
// that somebody else changed it in the meantime (§7 rule 5).
//
// Size and mtime, not a hash: the check must be cheap enough to run on every
// file of every operation. It catches the realistic case - a person editing in
// another window, or the CLI running while the UI service has the directory
// open - rather than a deliberate attempt to defeat it.
type stamp struct {
	size    int64
	modTime int64 // UnixNano
	missing bool  // the file did not exist when read
}

func stampOf(path string) stamp {
	fi, err := os.Stat(path)
	if err != nil {
		return stamp{missing: true}
	}
	return stamp{size: fi.Size(), modTime: fi.ModTime().UnixNano()}
}

func (s stamp) equal(o stamp) bool {
	if s.missing || o.missing {
		return s.missing == o.missing
	}
	return s.size == o.size && s.modTime == o.modTime
}

// pendingWrite is one file's worth of a transaction.
type pendingWrite struct {
	path string
	data []byte
	was  stamp // as read, for the concurrent-modification check
}

// writeSet accumulates the writes of one operation so they can be validated,
// counted, and then applied together.
//
// It exists because an operation is a transaction over several files: --start
// touches a working file and backlog.md, and applying half of that leaves the
// directory invalid. This cannot make a multi-file update truly atomic on a
// POSIX filesystem - nothing can - so §7 rule 4 requires the ORDER to be chosen
// so that an interruption fails validation loudly rather than losing data. The
// caller controls that by the order it adds files.
type writeSet struct {
	writes  []pendingWrite
	deletes []string
}

// Add queues a file write. was is the stamp taken when the file was read; pass
// the zero stamp with missing set for a file being created.
func (ws *writeSet) Add(path string, data []byte, was stamp) {
	ws.writes = append(ws.writes, pendingWrite{path: path, data: data, was: was})
}

// Delete queues a file removal, applied after every write.
func (ws *writeSet) Delete(path string) { ws.deletes = append(ws.deletes, path) }

// Empty reports whether the operation would touch nothing at all. A no-op must
// write nothing: no rewrite, no mtime bump, no diff under version control.
func (ws *writeSet) Empty() bool { return len(ws.writes) == 0 && len(ws.deletes) == 0 }

// Paths lists what would be touched, for a dry run's report.
func (ws *writeSet) Paths() []string {
	out := make([]string, 0, len(ws.writes)+len(ws.deletes))
	for _, w := range ws.writes {
		out = append(out, w.path)
	}
	out = append(out, ws.deletes...)
	sort.Strings(out)
	return out
}

// checkUnchanged verifies that nothing has been modified since it was read.
//
// Run before the first write, so a conflict aborts the whole transaction rather
// than leaving it half applied.
func (ws *writeSet) checkUnchanged() error {
	for _, w := range ws.writes {
		if now := stampOf(w.path); !now.equal(w.was) {
			return fmt.Errorf("%w: %s changed on disk since it was read; re-read before retrying",
				ErrConcurrent, filepath.Base(w.path))
		}
	}
	return nil
}

// Commit applies the whole set: check for concurrent modification, then write
// each file atomically in the order it was added, then apply deletions.
func (ws *writeSet) Commit() error {
	if ws.Empty() {
		return nil
	}
	if err := ws.checkUnchanged(); err != nil {
		return err
	}
	for _, w := range ws.writes {
		if err := writeFileAtomic(w.path, w.data); err != nil {
			return err
		}
	}
	for _, p := range ws.deletes {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%w: removing %s: %v", ErrIO, p, err)
		}
	}
	return nil
}

// writeFileAtomic writes data to path via a sibling temporary file, an fsync,
// and a rename.
//
// The fsync is what makes the guarantee real on a crash rather than only on a
// process kill: without it the rename can be visible while the data is still in
// the page cache, and a power loss leaves an empty file where the old one was.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	// The format's nested directories are details/ and the details-YYYY/ an
	// archive writes (§5.6): creating the first file in one that never existed
	// must work, and for details-YYYY/ that is the normal case.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: creating %s: %v", ErrIO, dir, err)
	}
	f, err := os.CreateTemp(dir, ".mm-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: creating a temp file in %s: %v", ErrIO, dir, err)
	}
	tmp := f.Name()

	// From here on, every failure path must remove the temp file: leaving
	// .mm-*.tmp behind would litter the user's directory with debris that looks
	// like data loss.
	cleanup := func(e error) error {
		f.Close()
		os.Remove(tmp)
		return e
	}

	if _, err := f.Write(data); err != nil {
		return cleanup(fmt.Errorf("%w: writing %s: %v", ErrIO, tmp, err))
	}
	if err := f.Sync(); err != nil {
		return cleanup(fmt.Errorf("%w: syncing %s: %v", ErrIO, tmp, err))
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: closing %s: %v", ErrIO, tmp, err)
	}

	// Preserve the target's permissions; CreateTemp makes files 0600, which
	// would silently tighten a file the user had made group-readable.
	if fi, err := os.Stat(path); err == nil {
		if err := os.Chmod(tmp, fi.Mode().Perm()); err != nil {
			os.Remove(tmp)
			return fmt.Errorf("%w: chmod %s: %v", ErrIO, tmp, err)
		}
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("%w: renaming %s to %s: %v", ErrIO, tmp, path, err)
	}
	return syncDir(dir)
}

// syncDir flushes the directory entry so the rename itself survives a crash.
//
// A rename is atomic but not durable: without this, a power loss just after the
// rename can leave the directory pointing at the old inode. Not every platform
// supports it, and a failure here means the write succeeded but its durability
// is unproven - which is not worth failing the operation over.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer d.Close()
	_ = d.Sync()
	return nil
}
