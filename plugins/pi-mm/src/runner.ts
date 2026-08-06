/*
  The mm runner (spec-pi-mm-plugin.md §4.1, §4.3, Appendix C).

  One subprocess per tool call, always `--json`, `--dir` once the board is
  pinned, bounded by a timeout, and every exit code turned into a typed outcome
  a tool can render without knowing how `mm` reports things.

  Three rules that shape the whole file:

    §4.1  One tool, one operation, one subprocess. Nothing here chains
          operations or runs `mm` twice — a chain hides which step failed.
    §4.3  The exit codes are `spec-tools.md` §10's, and the plugin MUST NOT
          reinterpret them. What this adds is the REMEDY: the words that tell
          an agent what to do next. Where the CLI already names one — the
          occupied slots at a WIP limit — it is passed through verbatim rather
          than re-derived.
    §8.4  Never retry a failed mutation automatically. There is no loop here,
          and that is deliberate: the board's state is the user's to re-confirm.
*/

import { spawn } from "node:child_process";

import { presenceMessage } from "./presence.ts";

/** §9's `timeoutMs` default. */
export const DEFAULT_TIMEOUT_MS = 30_000;

/** One error out of the envelope's `errors[]` (`spec-tools.md` §9.2). */
export interface MmError {
  readonly code: string;
  readonly message: string;
  readonly id?: string;
  readonly file?: string;
}

/** One change out of the envelope's `changes[]`. */
export interface MmChange {
  readonly kind?: string;
  readonly id?: string;
  readonly file?: string;
  readonly before?: string;
  readonly after?: string;
}

/** The board identity the envelope carries on every operation that has one. */
export interface MmDirectory {
  readonly path?: string;
  readonly projectId?: string;
  readonly project?: string;
  readonly description?: string;
  readonly nextId?: string;
  readonly wipLimit?: number;
  readonly wipUsed?: number;
}

/**
 * The `--json` envelope (`spec-tools.md` §9.2), as read rather than as hoped
 * for: every field is optional here because a runner that assumed a shape
 * would turn one missing key into a crash instead of an error.
 */
export interface Envelope {
  readonly ok?: boolean;
  readonly operation?: string;
  readonly directory?: MmDirectory;
  readonly result?: unknown;
  readonly changes?: readonly MmChange[];
  readonly warnings?: readonly string[];
  readonly errors?: readonly MmError[];
}

/**
 * Why a run failed.
 *
 * The first six are `mm`'s exit codes 1–6. The last three are the ways a run
 * fails without `mm` ever answering, and they are kept distinct because the
 * remedies differ: install it, wait less, or file a bug.
 */
export type FailureKind =
  | "invariant" // 1
  | "usage" // 2
  | "not-found" // 3
  | "precondition" // 4
  | "concurrent" // 5
  | "io" // 6
  | "spawn" // mm never started
  | "timeout" // mm never finished
  | "envelope"; // mm answered with something that is not the envelope

export interface RunFailure {
  readonly kind: FailureKind;
  /** The process exit code, or null when there was not one. */
  readonly exit: number | null;
  /** The formatted explanation, remedy included: what a tool result says. */
  readonly message: string;
  /** The envelope's errors, verbatim, for `details` and for machine callers. */
  readonly errors: readonly MmError[];
  /** The parsed envelope when there was one — failures carry it too (§9.2). */
  readonly envelope?: Envelope;
}

export type RunOutcome =
  | { readonly ok: true; readonly envelope: Envelope }
  | { readonly ok: false; readonly failure: RunFailure };

export interface RunOptions {
  /** The operation and its modifiers. `--json` and `--dir` are added here. */
  readonly args: readonly string[];
  /** The pinned board (§3.2). Omitted before a pin exists. */
  readonly dir?: string;
  readonly timeoutMs?: number;
  /** pi hands every tool an abort signal; a cancelled turn kills the child. */
  readonly signal?: AbortSignal;
  /** The binary. Overridable for tests; production always uses `mm`. */
  readonly command?: string;
  readonly cwd?: string;
}

/**
 * Assembles the argv for one run.
 *
 * Exported because the argv is a contract worth asserting directly: §10 says
 * the plugin MUST NOT run `mm` with a switch the spec does not name, and a test
 * that reads the array is how that stays true.
 */
export function buildArgs(args: readonly string[], dir?: string): string[] {
  for (const arg of args) {
    // A caller adding these itself is a bug in the plugin rather than a user
    // error: the runner owns both, and two of either would mean two callers
    // disagree about which board is being written.
    if (arg === "--json" || arg === "--dir" || arg.startsWith("--dir=")) {
      throw new Error(`runner owns ${arg}; do not pass it in args`);
    }
  }
  const out = [...args, "--json"];
  if (dir) out.push("--dir", dir);
  return out;
}

/**
 * Runs one `mm` operation.
 *
 * Never throws for anything `mm` does — a failure is a value, because every
 * caller is a tool that has to report rather than crash. It throws only for the
 * programming error `buildArgs` catches.
 */
export async function run(opts: RunOptions): Promise<RunOutcome> {
  const command = opts.command ?? "mm";
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const argv = buildArgs(opts.args, opts.dir);

  const raw = await spawnOnce(command, argv, timeoutMs, opts.signal, opts.cwd);

  if (raw.kind === "spawn-error") {
    return {
      ok: false,
      failure: {
        kind: "spawn",
        exit: null,
        // The same words the session-start check uses, so a user who ignored
        // the warning at startup meets one explanation, not two.
        message: presenceMessage({
          ok: false,
          reason: raw.enoent ? "not-found" : "failed",
          detail: raw.detail,
        }),
        errors: [],
      },
    };
  }

  if (raw.kind === "timeout") {
    return {
      ok: false,
      failure: {
        kind: "timeout",
        exit: null,
        message:
          `mm did not finish within ${timeoutMs}ms and was stopped. ` +
          `Nothing was retried — re-run the operation if you still want it ` +
          `(the board is unchanged unless mm had already committed).`,
        errors: [],
      },
    };
  }

  const envelope = parseEnvelope(raw.stdout);
  if (!envelope) {
    return {
      ok: false,
      failure: {
        kind: "envelope",
        exit: raw.code,
        message:
          `mm exited ${raw.code} without the --json envelope it promises ` +
          `(spec-tools.md §9.2). Output: ${excerpt(raw.stdout || raw.stderr)}`,
        errors: [],
      },
    };
  }

  if (raw.code === 0) return { ok: true, envelope };

  const errors = envelope.errors ?? [];
  return {
    ok: false,
    failure: {
      kind: kindFor(raw.code, errors),
      exit: raw.code,
      message: explain(raw.code, errors),
      errors,
      envelope,
    },
  };
}

/**
 * The exit code's name (Appendix C). An exit the table does not have is `io`:
 * "the tool could not do its job" is the honest reading of an unknown code,
 * and inventing a seventh kind would be reinterpreting the taxonomy §4.3
 * forbids reinterpreting.
 */
function kindFor(code: number | null, errors: readonly MmError[]): FailureKind {
  switch (code) {
    case 1:
      return "invariant";
    case 2:
      return "usage";
    case 3:
      return "not-found";
    case 4:
      return "precondition";
    case 5:
      return "concurrent";
    case 6:
      return "io";
    default:
      return errors.length > 0 ? "io" : "envelope";
  }
}

/**
 * The message a tool shows, per Appendix C's "plugin surface" column.
 *
 * The CLI's own message always leads. Where the spec asks for something the
 * CLI cannot know — that a missing operation is a build's age rather than a
 * plugin bug, that a stale read must be re-read — that sentence is appended.
 */
function explain(code: number | null, errors: readonly MmError[]): string {
  const said = errors.map((e) => e.message).filter(Boolean).join("; ");
  const body = said || `mm exited ${code}`;
  switch (code) {
    case 1:
      return `${body}\nThe board violates an invariant. Run mm_check to see every finding.`;
    case 2:
      // §4.3: "Report 'operation not available in this mm', not a plugin bug."
      return `${body}\nThis operation is not available in the installed mm build.`;
    case 3:
      return errors.some((e) => e.code === "Ambiguous")
        ? `${body}\nMore than one board matches. Pin one with mm_board PATH.`
        : body;
    case 4:
      // The CLI names the remedy itself (the occupied slots at a WIP limit),
      // so anything added here would be a worse copy of it.
      return body;
    case 5:
      return `${body}\nThe board changed since it was read. Re-read with mm_status or mm_list, then decide again — nothing was retried.`;
    default:
      return body;
  }
}

/** What one spawn produced. */
type Raw =
  | { kind: "exit"; code: number | null; stdout: string; stderr: string }
  | { kind: "timeout" }
  | { kind: "spawn-error"; enoent: boolean; detail: string };

function spawnOnce(
  command: string,
  argv: readonly string[],
  timeoutMs: number,
  signal: AbortSignal | undefined,
  cwd: string | undefined,
): Promise<Raw> {
  return new Promise((resolve) => {
    let settled = false;
    const done = (raw: Raw) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      signal?.removeEventListener("abort", onAbort);
      resolve(raw);
    };

    let child: ReturnType<typeof spawn>;
    try {
      child = spawn(command, [...argv], {
        stdio: ["ignore", "pipe", "pipe"],
        // The environment passes through untouched: MM_DIR is part of the
        // board resolution order (§3.2), and a runner that scrubbed it would
        // quietly change which board a user's shell means.
        ...(cwd ? { cwd } : {}),
      });
    } catch (err) {
      resolve({ kind: "spawn-error", enoent: isEnoent(err), detail: describe(err) });
      return;
    }

    const timer = setTimeout(() => {
      child.kill("SIGKILL");
      done({ kind: "timeout" });
    }, timeoutMs);
    timer.unref?.();

    const onAbort = () => {
      child.kill("SIGKILL");
      done({ kind: "timeout" });
    };
    signal?.addEventListener("abort", onAbort, { once: true });

    let stdout = "";
    let stderr = "";
    child.stdout?.on("data", (chunk) => {
      stdout += String(chunk);
    });
    child.stderr?.on("data", (chunk) => {
      stderr += String(chunk);
    });
    child.on("error", (err) => {
      done({ kind: "spawn-error", enoent: isEnoent(err), detail: describe(err) });
    });
    child.on("close", (code) => {
      done({ kind: "exit", code, stdout, stderr });
    });
  });
}

/**
 * Reads the envelope out of stdout.
 *
 * §9.2 promises one JSON object and nothing else, on success AND on failure,
 * so anything else is a failure of that promise rather than something to
 * salvage: no scanning for the first `{`, no partial parse.
 */
function parseEnvelope(stdout: string): Envelope | undefined {
  const text = stdout.trim();
  if (!text) return undefined;
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return undefined;
    return parsed as Envelope;
  } catch {
    return undefined;
  }
}

function excerpt(s: string, limit = 200): string {
  const t = s.trim();
  if (!t) return "(nothing)";
  return t.length > limit ? `${t.slice(0, limit)}…` : t;
}

function isEnoent(err: unknown): boolean {
  return (err as NodeJS.ErrnoException | undefined)?.code === "ENOENT";
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
