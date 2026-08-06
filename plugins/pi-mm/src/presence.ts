/*
  Is there a conforming `mm` on PATH? (spec-pi-mm-plugin.md §2.1)

  The plugin drives the CLI for every board interaction and never touches the
  files, so `mm` missing is not an edge case — it is the whole plugin being
  unavailable. §2.1 fixes what must then happen: tools return a "no mm on PATH"
  error, context injection is skipped, and the plugin SAYS WHAT TO INSTALL
  rather than pretending.

  This module owns exactly that question. It runs one probe, caches the answer
  for the session, and hands back a value every other module can branch on. It
  does not run `mm` for anything else — that is the runner's job (T-0176).
*/

import { spawn } from "node:child_process";

/** How long a probe may take before it counts as absent. */
export const PROBE_TIMEOUT_MS = 5_000;

/**
 * The result of asking whether `mm` is usable.
 *
 * `reason` distinguishes the three ways it can fail, because they need
 * different words to the user: the binary is not there at all, it is there and
 * failed, or it took too long to answer.
 */
export type Presence =
  | { readonly ok: true; readonly version: string }
  | {
      readonly ok: false;
      readonly reason: "not-found" | "failed" | "timeout";
      readonly detail: string;
    };

/** What the plugin runs to answer the question. */
const PROBE_ARGS = ["--version"] as const;

/**
 * What one probe spawn produced.
 *
 * The three shapes are the three ways running a program can end, and they are
 * kept apart because each needs different words: it was not there, it never
 * answered, or it answered.
 */
export type ProbeOutcome =
  | { readonly kind: "exit"; readonly code: number | null; readonly stdout: string; readonly stderr: string }
  | { readonly kind: "timeout" }
  | { readonly kind: "error"; readonly enoent: boolean; readonly detail: string };

/**
 * Runs `mm` once and collects its output.
 *
 * The plugin asks `mm` two questions that are not board interactions — "are you
 * there?" (`--version`) and "what can you do?" (`--help`, capabilities.ts) —
 * and neither goes through the runner, which owns `--json` and `--dir` and
 * insists on an envelope. This is the shared half of both: spawn, bound it,
 * never hold the host process open, and never throw.
 */
export function spawnText(
  command: string,
  args: readonly string[],
  timeoutMs: number,
): Promise<ProbeOutcome> {
  return new Promise((resolve) => {
    let settled = false;
    const done = (outcome: ProbeOutcome) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(outcome);
    };

    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(command, [...args], { stdio: ["ignore", "pipe", "pipe"] });
    } catch (err) {
      // spawn can throw synchronously (EACCES on some platforms) rather than
      // emitting "error"; both paths must produce the same answer.
      resolve({ kind: "error", enoent: true, detail: describe(err) });
      return;
    }

    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      done({ kind: "timeout" });
    }, timeoutMs);
    // A probe must never hold the host process open (§2.2: the plugin is a
    // guest).
    timer.unref?.();

    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr?.on("data", (chunk) => {
      stderr += String(chunk);
    });
    child.on("error", (err: NodeJS.ErrnoException) => {
      done({ kind: "error", enoent: err.code === "ENOENT", detail: describe(err) });
    });
    child.on("close", (code) => {
      done({ kind: "exit", code, stdout, stderr });
    });
  });
}

/**
 * The one message the plugin gives a user whose `mm` is missing or broken.
 *
 * It names the remedy, because "mm not found" alone leaves someone stuck: the
 * CLI is a Go build from this repository, not an npm package pi could fetch.
 * §2.1's "degrade loudly but gracefully" is this text plus the fact that
 * nothing else in the plugin pretends to work.
 */
export function presenceMessage(p: Presence): string {
  // The CLI's own --version line, verbatim: it already names itself, and
  // prefixing it again reads as "mm mm 1.4.0".
  if (p.ok) return p.version;
  const head =
    p.reason === "not-found"
      ? "no `mm` on PATH"
      : p.reason === "timeout"
        ? "`mm` did not answer `--version` in time"
        : "`mm --version` failed";
  return (
    `${head} — the micro-manager plugin needs the mm CLI to do anything at all ` +
    `(it never edits board files itself). Build it from the micro-manager ` +
    `repository with \`go build -o bin/mm ./cmd/mm\` in implementations/golang ` +
    `and put it on PATH, then \`/reload\`.` +
    (p.detail ? ` (${p.detail})` : "")
  );
}

/**
 * Runs the probe once.
 *
 * Exported for tests and for a deliberate re-probe (a user who installs `mm`
 * mid-session and reloads); everything else goes through the cache below.
 */
export async function probe(
  command = "mm",
  timeoutMs = PROBE_TIMEOUT_MS,
): Promise<Presence> {
  const outcome = await spawnText(command, PROBE_ARGS, timeoutMs);
  if (outcome.kind === "timeout") {
    return { ok: false, reason: "timeout", detail: `${timeoutMs}ms` };
  }
  if (outcome.kind === "error") {
    return {
      ok: false,
      reason: outcome.enoent ? "not-found" : "failed",
      detail: outcome.detail,
    };
  }
  if (outcome.code === 0) {
    const version = firstLine(outcome.stdout) || firstLine(outcome.stderr);
    return { ok: true, version: version || "unknown version" };
  }
  const said = firstLine(outcome.stderr);
  return {
    ok: false,
    reason: "failed",
    detail: `exit ${outcome.code}${said ? `: ${said}` : ""}`,
  };
}

/**
 * The session's cached answer.
 *
 * One probe per session: `mm` does not appear and disappear mid-session, and a
 * subprocess per tool call to ask a question already answered is the kind of
 * cost a guest should not impose. `reset()` exists for `/reload` and for tests.
 */
let cached: Promise<Presence> | undefined;

export function presence(command = "mm"): Promise<Presence> {
  cached ??= probe(command);
  return cached;
}

export function resetPresence(): void {
  cached = undefined;
}

function firstLine(s: string): string {
  return s.split("\n")[0]?.trim() ?? "";
}

function describe(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}
