# micro-manager

A todo system stored as **plain markdown**, plus the tooling to manipulate it.

Every data file — `backlog.md`, `working.NN.md`, `done.md`, `details/*.md` — is
readable by a human in a text editor and parseable by a machine with a handful
of regexes. Nothing needs a tool; a tool (`mm`, the CLI in this repository) is
just faster and harder to get wrong than editing by hand. `./check.sh` proves
that: it validates a directory in bash and awk alone, no runtime required.

This repository holds the format's specification, that reference validator,
and implementations of the tooling in progress — Go furthest along, others
starting.

## Quick start

Build and install the Go CLI to a per-user path (no `sudo` needed; `~/.local/bin`
is on `PATH` by default on most distributions):

```bash
make install
mm --version
```

(`make install PREFIX=/usr/local` for a system-wide install instead — see
`make help` for every target, and *Building and installing the CLI* below for
what `make install` does under the hood.)

Then:

```bash
mm --init --project "Example"                 # creates ./micro-manager
mm --add "Fix the deploy script" --prio high --tag infra
mm --status
```

```
# Example  (wip 0/1)

working.01.md: idle

ready:     1
blocked:   0
someday:   0
done:      0

next:   T-0001  Fix the deploy script  (created 2026-08-04)
oldest: T-0001  Fix the deploy script  (created 2026-08-04)
```

`mm --help` lists every operation; `mm --help --OPERATION` prints one
operation's own page. For the full day-to-day workflow (start, note, pause,
finish, block, report, WIP limits, scripting with `--json`/`--porcelain`), see
[`skills/micro-manager-cli/SKILL.md`](skills/micro-manager-cli/SKILL.md) — the
same document a coding agent uses (see *Using this with a coding agent* below).

## No tool required

The format's whole premise is that a tool is an accelerant, not a dependency.
Given only `bash` and `awk` (present on macOS and Linux by default):

```bash
chmod +x check.sh find.sh
./find.sh          # list every micro-manager directory below here
./check.sh --all   # validate all of them against the ten invariants
```

`check.sh` is **the reference validator** — the Go implementation's own
checker is cross-tested against it over the fixture corpus, and the two are
required to agree.

## Building and installing the CLI

```bash
make build          # implementations/golang/bin/mm
make install        # build, then install to $(PREFIX)/bin  (default: ~/.local)
make install-ui      # same, for the GUI service, mm-ui
make install-all      # both
make uninstall[-ui|-all]
```

Or by hand, exactly as `make install` does it (see
`implementations/golang/AGENTS.md` for why the output path must be explicit —
`go build -o mm ./cmd/mm` silently writes into the `mm/` *library* package
instead of failing):

```bash
cd implementations/golang
go build -o bin/mm ./cmd/mm
install -m 0755 bin/mm ~/.local/bin/mm   # or anywhere else on PATH
```

## Repository layout

```
check.sh                      reference validator for the ten invariants
find.sh                       discovery: finds micro-manager directories
Makefile                      make build / make install / make check / ...
Dockerfile, docker-*.sh       a development container with every toolchain
skills/
  micro-manager-cli/          portable agent-skill package: use mm elsewhere
project/
  spec-file-format.md         NORMATIVE: on-disk formats, tokens, invariants I1-I10
  spec-tools.md               library/CLI contract, transactions, errors, exit codes
  spec-gui.md                 web UI: DOM contract, routes, theming, binding
  spec-tui.md                 terminal UI
  SKILL.md                    how to use the format day to day (contributor-facing)
implementations/
  golang/                     the reference implementation — furthest along
    mm/                         the library
    cmd/mm/, cmd/mm-ui/          the CLI and the GUI service
    internal/                    CLI wrapper, web service
  python/mmx                  a small stopgap CLI, not a full implementation
  typescript/                 scaffold: library, CLI, server and web client
  erlang/                     placeholder
sample-data/                  example micro-manager directories, used as fixtures
```

## Implementations

| | Library | CLI | GUI service | TUI |
|---|---|---|---|---|
| **Go** (`implementations/golang`) | done | done (`mm`) | done (`mm-ui`, HTMX + Echo) | not started |
| Python (`implementations/python/mmx`) | — | a small stopgap, not a full implementation | — | — |
| TypeScript (`implementations/typescript`) | scaffold | scaffold | scaffold | — |
| Erlang (`implementations/erlang`) | placeholder | — | — | — |

The Go implementation is the one to reach for. Its `testdata/` fixture corpus
is the source other implementations validate against, and `check.sh` is the
oracle all of them are required to agree with.

## Specifications

| File | Covers |
|---|---|
| `project/spec-file-format.md` | On-disk formats, tokens, the ten invariants (I1–I10) |
| `project/spec-tools.md` | Library API, CLI surface, transactions, error taxonomy, exit codes |
| `project/spec-gui.md` | Web UI: DOM contract, routes, theming, discovery, binding |
| `project/spec-tui.md` | Terminal UI: keyboard model, colour degradation |

Precedence: the format spec wins any disagreement about what a file may
contain, then the tools spec on operation semantics, then the UI specs. A tool
that must violate the format spec to do its job is wrong; the data is not.

## Using this with a coding agent

Two different skill files exist, for two different audiences:

| File | For | Assumes |
|---|---|---|
| [`project/SKILL.md`](project/SKILL.md) | developing micro-manager itself | this repository is checked out (`check.sh`, `find.sh`, the specs) |
| [`skills/micro-manager-cli/SKILL.md`](skills/micro-manager-cli/SKILL.md) | using `mm` in *your own* projects | only that `mm` is on `PATH` |

See [`skills/micro-manager-cli/README.md`](skills/micro-manager-cli/README.md)
for installing the portable one into pi, Claude Code, Codex, or any other
harness implementing the [Agent Skills standard](https://agentskills.io/specification).

## Development container

A Docker image with every implementation's toolchain (Go, Node.js, Python,
Erlang/OTP, an editor for `$VISUAL`/`$EDITOR`, and the `pi` coding agent):

```bash
./docker-build.sh
./docker-run.sh                    # interactive shell, repo mounted at /workspace
./docker-run.sh ./check.sh --all   # or run a single command
```

## Contributing

Start with [`AGENTS.md`](AGENTS.md) — orientation, spec precedence, the ten
invariants that get broken most often, and the working rules for the Go
implementation. Every operation that mutates a directory validates it before
writing, so it should be impossible for `mm` to produce a directory its own
`--check` rejects; `check.sh` is the independent check on that claim.

## License

No license has been chosen for this repository yet.
