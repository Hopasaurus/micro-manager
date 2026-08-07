# Not a board

This directory is a fixture for the **emptiness test** of
`spec-file-format.md` Appendix B, and it exists to be *ignored*.

Its name matches — `micro-manager` is one of the six recognized names — but it
holds neither `backlog.md` nor `done.md`, so it was never a board. Discovery
skips it silently: `find.sh` does not list it, `mm --find` does not report it,
`check.sh --all` and `mm --check --all` do not count it, and directory
resolution cannot land on it.

That is what a source repository sharing the name looks like from the outside,
and this repository is itself the reason the rule exists: `plugins/pi-mm/` is
named as it is precisely to stay out of discovery's way.

Naming it explicitly is still an error, because then the user asked about it:

```bash
./check.sh sample-data/not-a-board/micro-manager   # exit 1, "not a micro-manager directory"
```

A directory holding exactly ONE of the two files is a different thing — a board
that lost a file — and is discovered and reported, never skipped.
