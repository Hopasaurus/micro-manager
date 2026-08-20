# A frozen version-1 board

This is a byte-for-byte copy of `implementations/golang/micro-manager` — this
repository's own task-tracking board — captured immediately before it was
migrated to version 2 (`board.md`), T-0229.

It exists to regression-test the `1 → 2` migration step against real data:
`spec-tools.md` §5.3.4's chain, `spec-file-format.md` Appendix C. Unlike
`sample1`-`sample3`, it was not written as a fixture; it is the directory the
step was actually designed against, with everything real usage produces —
long `reason:` values with embedded punctuation, tickled recurring items,
multiple blocked reasons — that a hand-built synthetic fixture would not
necessarily think to include.

`notes/add-columns.md` §7.1 (decision 21): version-1 fixtures are kept for as
long as version-1 support is kept, one paired decision, not two. This
directory retires only alongside an explicit future decision to drop the
`1 → 2` step from the maintained chain — not by attrition once every real
board has moved on.

**This directory is frozen.** It is never migrated, never edited to track the
live board's ongoing changes, and no tool should write to it. `README.md`
itself is the one file this repository's own `board.md`/`done.md`/`details/`
convention does not expect, which is deliberate: it keeps this directory from
being mistaken for a live board by a careless glance.
