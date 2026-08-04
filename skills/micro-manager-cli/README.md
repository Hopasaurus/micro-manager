# Installing the micro-manager skill

This directory is a self-contained skill package for coding agents: a single
`SKILL.md` following the [Agent Skills standard](https://agentskills.io/specification)
— frontmatter (`name`, `description`, `compatibility`) plus instructions, no
other files required. Any agent harness that implements the standard can load
it directly from this directory; installing it is just putting a copy (or a
symlink) where that harness looks.

It is written for **using** `mm` in your own projects, and assumes the `mm`
binary is already on `PATH`. It is not the same file as this repository's own
`project/SKILL.md`, which is aimed at people developing micro-manager itself
(building the CLI, running `check.sh`, the fixture corpus) — if that's what
you're doing, use that one instead.

**A note on the folder name.** This package's `SKILL.md` declares
`name: micro-manager`, but its *source* directory in this repository is
`skills/micro-manager-cli/`, not `skills/micro-manager/`. That's deliberate:
`find.sh`/`check.sh` recognize a *todo* directory by name alone (`micro-manager`
and five spelling variants — see `project/SKILL.md`), so a folder literally
named `micro-manager` anywhere in this repository gets treated as one and
rejected as broken. When you install it into your own agent's skill directory
below, name the **destination** `micro-manager` (matching the skill's
identity) — that collision only exists inside this source repository.

## Prerequisite: `mm` on PATH

```bash
command -v mm && mm --version
```

If that fails, install the CLI first. Building it from this repository:

```bash
cd implementations/golang
go build -o bin/mm ./cmd/mm
sudo install -m 0755 bin/mm /usr/local/bin/mm   # or anywhere else on PATH
```

(See `implementations/golang/SKILL.md` for the full build/validate story.)

## Install for pi

Global, for every project:

```bash
mkdir -p ~/.pi/agent/skills
cp -r skills/micro-manager-cli ~/.pi/agent/skills/micro-manager
```

Project-only, committed alongside a specific repo:

```bash
mkdir -p .pi/skills
cp -r /path/to/skills/micro-manager-cli .pi/skills/micro-manager
```

Or load it for a single run without installing anything:

```bash
pi --skill /path/to/skills/micro-manager-cli/SKILL.md
```

pi discovers any directory containing a `SKILL.md` under these locations
automatically — no registration step beyond copying it there. See pi's own
`docs/skills.md` for the full discovery rules.

## Install for Claude Code

```bash
mkdir -p ~/.claude/skills
cp -r skills/micro-manager-cli ~/.claude/skills/micro-manager
```

Project-level instead of global:

```bash
mkdir -p .claude/skills
cp -r /path/to/skills/micro-manager-cli .claude/skills/micro-manager
```

## Install for Codex, or anything else implementing the Agent Skills standard

The same copy, into whatever directory that harness scans for skills — for
example:

```bash
mkdir -p ~/.codex/skills
cp -r skills/micro-manager-cli ~/.codex/skills/micro-manager
```

Consult that harness's own documentation for its exact skill directory; the
package itself (`SKILL.md`, unmodified) is what's portable.

## Symlink instead of copy, if you want updates to follow

A copy is a snapshot; a symlink stays current if this package is edited later
(this repository does the same thing internally for its own contributor-facing
skill: see `.claude/skills/micro-manager/SKILL.md`, a symlink to
`project/SKILL.md`).

```bash
ln -s "$(pwd)/skills/micro-manager-cli" ~/.claude/skills/micro-manager
```

## Verify it loaded

Start the agent in a directory that has (or is near) a `micro-manager`
directory and ask something that should trigger it, e.g. "what's next in the
backlog?" or "start T-0031". If the harness supports forced loading (pi:
`/skill:micro-manager` — the frontmatter name, not the folder name), use that
to confirm the file parses and is found before trusting automatic discovery.
