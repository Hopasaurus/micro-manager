package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Editor integration (spec-tools.md §3.5, §5.1.2).
//
// This is wrapper territory by definition: $EDITOR is process environment, and
// launching a child process that takes over the terminal is exactly the kind of
// thing a UI service must never inherit. The library creates the detail file;
// only this decides whether a human is shown it.
//
// The editor command is NOT run through a shell. `EDITOR="code --wait"` is
// common and must work, so the value is split on spaces — but passing it to
// `sh -c` would make a semicolon in the variable run arbitrary commands with the
// user's file as an argument.

// Editor is how the wrapper launches one, injected so tests do not spawn vi.
type Editor func(program string, args []string) error

// resolveEditor picks the editor, or "" when there is none.
//
// VISUAL wins where both are set (§3.5). The distinction is historical but
// real: EDITOR may be a line editor for a dumb terminal, VISUAL is the
// full-screen one, and this is a full-screen job.
func resolveEditor(visual, editor string) []string {
	for _, candidate := range []string{visual, editor} {
		if fields := strings.Fields(candidate); len(fields) > 0 {
			return fields
		}
	}
	return nil
}

// wantsEditor decides whether to open one at all.
//
// Every "no" here is a case where a child process on the terminal would be
// wrong rather than merely unwanted:
//
//   - --no-edit and --quiet: asked for explicitly.
//   - --json and --porcelain: a machine is reading stdout, and an editor would
//     take over the terminal in the middle of a pipeline.
//   - --dry-run: nothing was written, so there is no file to open.
//   - no terminal: the same reason, one level down. A CI job with EDITOR set
//     would otherwise hang forever waiting for a human.
func wantsEditor(in *Invocation, env Env, hasTerminal bool) bool {
	switch {
	case in.Bool("no-edit"), in.Quiet, in.JSON, in.Porcelain, in.DryRun:
		return false
	case !hasTerminal:
		return false
	}
	return len(resolveEditor(env.Visual, env.Editor)) > 0
}

// openEditor launches the editor on a path and waits for it.
//
// A failure to launch is NOT an operation failure: the item and its detail file
// are already written and validated. Losing that because a misconfigured
// $EDITOR could not start would be the tool destroying good work over a
// preference, so the caller reports and carries on.
func openEditor(env Env, path string) error {
	command := resolveEditor(env.Visual, env.Editor)
	if len(command) == 0 {
		return nil
	}
	program, args := command[0], append(command[1:], path)

	if env.Launch != nil {
		return env.Launch(program, args)
	}
	cmd := exec.Command(program, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Dir = filepath.Dir(path)
	return cmd.Run()
}
