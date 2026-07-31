# Session 004 — retrospective

    Date:    2026-07-31
    Scope:   everything after session 003's retrospective was written —
             the Appendix B fix, two commits and a merge, then phase 3
             items T-0060, T-0061 and T-0062.
    Outcome: 3 items shipped. 1,177 lines of Go and 299 of templates added
             to internal/web; 479 tests, 100 of them in the web layer.
             `/` and `/projects` exist, so the service is navigable without
             typing an id into the URL.
    Open:    a second spec inconsistency, found and deliberately not acted
             on (§3). Phase 3 has two items left.

---

## 1. What happened

Seven requests, all short:

| Request | What it produced |
|---|---|
| "update the spec with the one line fix" | `--mm-density-*` added to Appendix B; two records of it as unresolved corrected |
| "Commit the working tree changes" | two commits — the GUI work, and your dev container, separately |
| "do the merge and make a note…" | `main` fast-forwarded, both branches deleted, a memory written |
| "how can I start the mm-ui…" | a walkthrough, and one gap found while writing it |
| "Start phase-3" | report view, check view, home and projects |
| this | the retrospective |

This is the first session stretch that began with **housekeeping rather than
code** — a spec edit, a commit, a merge, a preference recorded — and the
housekeeping is where two of the four findings came from.

## 2. What the session found

### The gap found by writing documentation

Answering "how do I start it and see a project" surfaced something no test had:
`registry.touch` had existed and been tested since T-0048, and **nothing called
it**. The recent list was never written, so the project switcher rendered empty
for ever and `/` did not exist to show it either.

Nothing was failing. The unit test passed, because it called `touch` directly.
The defect was the absence of a caller, which is invisible to a test suite that
tests what exists rather than what is reachable.

**Writing the instructions is what found it** — the moment the answer had to be
"you cannot, you have to type the id" was the moment the gap became obvious.
That is a third way of finding defects, alongside session 002's "running it" and
session 003's "looking at it": *explaining it*.

### A data race, surfaced as a flake

`Server.addr` was written by Echo's `ListenerAddrFunc` and read by whoever
wanted the address, with no synchronisation. It appeared as an intermittent
failure in a test that had passed a hundred times. It is now behind a mutex and
`go test -race` is clean.

The proximate trigger was mine — a leftover `mm-ui` holding port 7717 — but
chasing it found the real defect underneath, which would have stayed hidden.

### Port 0 could not mean "ephemeral"

`New` turned port 0 into 7717, so a test asking the OS for a free port silently
got the real one. Two meanings had been folded into one value: "unset, use the
default" and "any port will do". The default now lives only in `DefaultConfig`,
where §9.2 puts it, and `cmd/mm-ui` always passes a real port.

### `data-period-source` always said `config`

§5.7 requires the report to distinguish `switch`, `config` and `default`.
Reading the **merged** configuration cannot: `report.period` is `last-week`
whether the user chose it or the built-in supplied it. The server now keeps the
config *file* alongside the merged view and asks what was actually written.

This is a general shape worth remembering: **a merged view answers "what is the
value", never "who set it"**, and any UI that reports provenance needs the
unmerged source.

### And one the browser found

The scan-root heading was uppercased by CSS. That heading is a filesystem path,
and paths are case-sensitive on most systems this runs on — shouting one at the
user misrepresents it. Uppercasing stays on the Home list titles, which are
words.

## 3. The second spec inconsistency

There are **three exclude lists** for project discovery and they disagree:

| Source | Excludes |
|---|---|
| `find.sh`, the reference tool | `.git .hg .svn .claude node_modules vendor target dist build .venv venv` |
| `mm.DefaultDiscoveryOptions` | the same eleven |
| `spec-gui.md` §9.2 `scan.excludes` | `node_modules .git target vendor .venv dist` — six |

The config default omits `.claude`, `.hg`, `.svn`, `build` and `venv`. The GUI
follows §9.2, because that is the sentence binding the config file, and the
visible result is that `/projects` lists `.claude/skills/micro-manager` — the
*skill* directory — as a project while `mm --find` does not.

**The two front ends now disagree about which projects exist**, which is exactly
what §9.5 exists to prevent. The fix is one line, in the same place and of the
same kind as session 003's Appendix B omission.

It was surfaced rather than applied, and that is now the settled pattern: session
003 asked, the answer came back in one round trip, and the same question is worth
one more.

## 4. Efficiency

### Where the session lost time — my side

**Fixture assumptions, again.** Session 003 recorded this exact finding and it
recurred: tests written against `clean-full` assuming a free WIP slot, and a
report test asserting `data-from` on a period (`all`) that is unbounded and
legitimately has none. **The lesson did not stick because it was written as
advice rather than as a habit.** The habit is: read the fixture, or assert on
what the code returns, before writing the expectation.

**Two servers on one port.** A leftover `mm-ui` from an earlier check held 7717
and produced a confusing test failure. Killing background processes before
starting another is one command and I skipped it twice.

**Rebuilding after editing embedded assets.** Session 003 recorded that the
binary serves the JavaScript it was built with. I hit the same wall once more
this stretch, on CSS.

### Where the session was efficient — worth repeating

- **Moving `Report.Markdown()` into the library.** §5.7 requires the GUI's copy
  button to produce the CLI's exact text. One renderer makes that structural; a
  test comparing two renderings makes it checked. The CLI's output did not
  change by a byte.
- **Verifying in the browser after each view.** Two of the last three sessions'
  most embarrassing defects were CSS-level and invisible to Go tests. Ten
  seconds of looking is now part of finishing a view.
- **A scratch config home for anything that writes lists.** Favourites and
  recent are real files; the demo ran against a throwaway directory.

### What you could do differently

1. **Answer §3.** Same shape as last time, same one-line fix, and it is
   currently making the CLI and the GUI disagree.
2. **Consider whether `mm.DefaultDiscoveryOptions` and `find.sh` are the
   authority.** They already agree with each other and disagree with the spec.
   If the eleven-name list is the intended one, §9.2 is simply behind.
3. **Keep the requests short.** "Start phase-3" produced three items with no
   further steering, because the items carry their own requirements. Four
   sessions have now confirmed this.

## 5. State, and what is next

Phase 3 is three of five done, in the order the plan set:

```
T-0060  report view    shipped
T-0061  check view     shipped
T-0062  home/projects  shipped
T-0063  settings, both scopes      next
T-0064  theme editor, library, import/export
```

Phase 4 — the JSON API, SSE, test mode, accessibility, `/about`, and the
contract audit — is untouched, plus T-0071 for the CLI switches.

Uncommitted: 28 files of working tree, all of it phase 3. `main` is at
`4da65c5`, which carries phases 0–2.
