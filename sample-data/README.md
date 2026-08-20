# sample-data

A free corpus of real micro-manager directories, used by more than one
implementation's test suite (currently `implementations/golang`; `check.sh`
at the repository root and the other in-progress implementations are meant
to validate against it too, per each implementation's own conformance plan)
and by `find.sh`/`mm --find`'s discovery tests.

| Directory | Purpose |
|---|---|
| `sample1`, `sample2`, `sample3` | Ordinary boards exercising the four naming conventions and a range of item shapes |
| `hidden`, `symbol`, `symbol-hidden` | The dotfile and non-ASCII directory-name variants (`.micro-manager`, `µmanager`, `.µmanager`) — spec-file-format.md Appendix B's naming rule needs all four spellings covered somewhere |
| `not-a-board` | A directory that looks like one by name but holds neither `backlog.md` nor `board.md` — the emptiness test, meant to be *skipped* by discovery |
| `legacy-v1` | The live Go board's frozen pre-migration snapshot (see its own `README.md`) |

## Every directory here is version 1, on purpose (T-0230, M4)

`spec-file-format.md`'s version-2 redesign (`board.md`, `stage:`) is real,
shipped, and in day-to-day use at `implementations/golang/micro-manager`
(T-0229). Nothing in this corpus has been migrated to it, and that is a
decision, not an oversight: `check.sh` has no version-2 support at all yet
(it is a from-scratch bash/awk parser of `backlog.md`/`working.NN.md`,
unrelated to the Go implementation's own v2 code), and none of
`implementations/{typescript,python,erlang}` has version-2 support either —
most have barely started. Migrating this shared corpus now would break every
one of those the moment it starts reading here, for no benefit: the point of
a shared fixture corpus is that everything reading it agrees about what it
contains.

Revisit this once `check.sh` and at least one other implementation can read
`board.md`. Until then, a fixture added here should stay version 1 too,
matching its neighbors — an implementation wanting version-2 coverage should
add its own fixtures elsewhere (`implementations/golang/mm/testdata/`
already holds that implementation's own version-2-specific cases) rather
than converting a fixture every other implementation still depends on
reading as version 1.
