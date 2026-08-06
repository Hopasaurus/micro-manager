/*
  Board resolution and the pin (spec-pi-mm-plugin.md §3.2, §3.2.1, §7).

  Which board is this session operating on? The answer is resolved ONCE through
  `mm` — never by scanning directories here, because `mm` is what implements
  "refuse rather than guess" (§3.2) — and then pinned, so that every later call
  passes `--dir <pinned>` and mid-session re-resolution cannot flip boards when
  two are nearby.

  The pin is the plugin's only durable state, and it lives where pi's own
  state-management pattern puts extension state: in tool-result `details`, with
  `session_start` reconstructing it from the active branch (§7). That is what
  makes a fork that changes the board not leak into the other branch — the
  in-memory value below is a cache of what the branch says, never the record.
*/

import { run, type Envelope, type RunOutcome } from "./runner.ts";

/** How the board in use was arrived at (§3.2.1: the board in use is always knowable). */
export type PinSource = "pin" | "MM_DIR" | "discovery";

export interface Pin {
  readonly path: string;
  readonly project?: string;
  readonly source: PinSource;
}

/**
 * The key the pin travels under in tool-result `details`.
 *
 * One key, spelled once: `session_start` looks for exactly this, and a tool
 * that spelled it differently would write state nothing reads back.
 */
export const PIN_DETAILS_KEY = "mmBoard";

/** The tools whose results carry a pin worth reconstructing from (§7). */
export const PIN_BEARING_TOOLS = ["mm_status", "mm_board", "mm_init"] as const;

let current: Pin | undefined;

export function pinnedBoard(): Pin | undefined {
  return current;
}

export function setPin(pin: Pin): void {
  current = pin;
}

export function clearPin(): void {
  current = undefined;
}

/**
 * Reads a board out of an envelope.
 *
 * `--init` is the one operation that reports its board in `result` rather than
 * in `directory`: there is no directory to describe until it has made one.
 */
export function boardFromEnvelope(envelope: Envelope, source: PinSource): Pin | undefined {
  const dir = envelope.directory;
  if (dir?.path) {
    return { path: dir.path, source, ...(dir.project ? { project: dir.project } : {}) };
  }
  const result = envelope.result as { path?: string; project?: string } | null | undefined;
  if (result?.path) {
    return { path: result.path, source, ...(result.project ? { project: result.project } : {}) };
  }
  return undefined;
}

export type Resolution =
  | { readonly ok: true; readonly pin: Pin }
  | {
      readonly ok: false;
      /** `ambiguous` needs a pin; `none` means no board was found at all. */
      readonly reason: "ambiguous" | "none" | "failed";
      readonly message: string;
      /** Candidate boards, for the ambiguous case (§3.2). */
      readonly candidates: readonly BoardListing[];
    };

export interface BoardListing {
  readonly path: string;
  readonly project?: string;
  /** True for the board this session is operating on (§3.2.1). */
  readonly inUse: boolean;
}

export interface ResolveOptions {
  /**
   * The runner to drive. Injectable because resolution IS a pair of mm calls:
   * a caller that could not substitute one could not test the thing this
   * module exists to do, and the tool layer already holds a runner of its own.
   */
  readonly run?: typeof run;
  readonly command?: string;
  readonly timeoutMs?: number;
  readonly signal?: AbortSignal;
  readonly cwd?: string;
  /** Force a fresh resolution even when a pin exists (`/mm board` with a path). */
  readonly path?: string;
}

/**
 * Resolves the board, pinning what it finds.
 *
 * The order is §3.2's: an existing pin wins; otherwise `mm` is asked with no
 * `--dir` at all, which lets IT apply `MM_DIR` and then its own discovery. The
 * plugin never reads `MM_DIR` to decide — it only reads it afterwards to say
 * which rule must have fired, because `mm` does not report that and §3.2.1
 * requires the answer to be knowable.
 */
export async function resolveBoard(opts: ResolveOptions = {}): Promise<Resolution> {
  if (!opts.path && current) return { ok: true, pin: current };

  const exec = opts.run ?? run;
  const outcome = await exec({
    args: ["--status"],
    ...(opts.path ? { dir: opts.path } : {}),
    ...(opts.command ? { command: opts.command } : {}),
    ...(opts.timeoutMs !== undefined ? { timeoutMs: opts.timeoutMs } : {}),
    ...(opts.signal ? { signal: opts.signal } : {}),
    ...(opts.cwd ? { cwd: opts.cwd } : {}),
  });

  if (outcome.ok) {
    const source: PinSource = opts.path ? "pin" : sourceFor(outcome.envelope);
    const pin = boardFromEnvelope(outcome.envelope, source);
    if (!pin) {
      return {
        ok: false,
        reason: "failed",
        message: "mm reported no board path; nothing to pin.",
        candidates: [],
      };
    }
    current = pin;
    return { ok: true, pin };
  }

  // §3.2: when mm refuses to choose, surface the candidates and require a pin.
  // The plugin MUST NOT guess among them, so this is a failure with a list —
  // never a silently chosen first entry.
  const ambiguous =
    outcome.failure.kind === "not-found" &&
    outcome.failure.errors.some((e) => e.code === "Ambiguous");
  const candidates = ambiguous ? await listBoards(opts) : [];
  return {
    ok: false,
    reason: ambiguous ? "ambiguous" : outcome.failure.kind === "not-found" ? "none" : "failed",
    message: outcome.failure.message,
    candidates,
  };
}

/**
 * Every board `mm --find` can see, with the one in use marked (§3.2.1).
 *
 * An empty list is a valid result — "no boards located" — never an error.
 */
export async function listBoards(opts: ResolveOptions = {}): Promise<BoardListing[]> {
  const exec = opts.run ?? run;
  const outcome = await exec({
    args: ["--find"],
    ...(opts.command ? { command: opts.command } : {}),
    ...(opts.timeoutMs !== undefined ? { timeoutMs: opts.timeoutMs } : {}),
    ...(opts.signal ? { signal: opts.signal } : {}),
    ...(opts.cwd ? { cwd: opts.cwd } : {}),
  });
  if (!outcome.ok) return [];
  const result = outcome.envelope.result as
    | { directories?: Array<{ path?: string; project?: string }> }
    | undefined;
  const inUse = current?.path;
  return (result?.directories ?? [])
    .filter((d): d is { path: string; project?: string } => Boolean(d.path))
    .map((d) => ({
      path: d.path,
      ...(d.project ? { project: d.project } : {}),
      inUse: d.path === inUse,
    }));
}

/**
 * Which rule produced a board `mm` resolved on its own.
 *
 * `mm` does not say, so this infers it the only honest way available: if
 * `MM_DIR` names the very directory that came back, that is what it was;
 * anything else was discovery. Naming MM_DIR whenever it is merely SET would
 * be a guess, and a wrong one whenever discovery overrode nothing.
 */
function sourceFor(envelope: Envelope): PinSource {
  const fromEnv = process.env["MM_DIR"];
  const path = envelope.directory?.path;
  if (fromEnv && path && samePath(fromEnv, path)) return "MM_DIR";
  return "discovery";
}

function samePath(a: string, b: string): boolean {
  return trimSlash(a) === trimSlash(b);
}

function trimSlash(p: string): string {
  return p.length > 1 && p.endsWith("/") ? p.slice(0, -1) : p;
}

/**
 * Rebuilds the pin from the active branch (§7).
 *
 * Entries are pi's session entries; the shape is read defensively because a
 * transcript can hold anything an older build wrote. The LAST pin-bearing
 * result wins: a session that re-pinned mid-branch means the later one.
 */
export function pinFromBranch(entries: readonly unknown[]): Pin | undefined {
  let found: Pin | undefined;
  for (const entry of entries) {
    const message = (entry as { type?: string; message?: Record<string, unknown> }).message;
    if ((entry as { type?: string }).type !== "message" || !message) continue;
    if (message["role"] !== "toolResult") continue;
    const tool = message["toolName"];
    if (typeof tool !== "string" || !PIN_BEARING_TOOLS.includes(tool as never)) continue;
    const details = message["details"] as Record<string, unknown> | undefined;
    const pin = details?.[PIN_DETAILS_KEY];
    if (isPin(pin)) found = pin;
  }
  return found;
}

function isPin(value: unknown): value is Pin {
  if (!value || typeof value !== "object") return false;
  const v = value as Record<string, unknown>;
  return typeof v["path"] === "string" && typeof v["source"] === "string";
}

/**
 * The persistent status line (§3.2.1): what board the agent is operating on,
 * at a glance. Cleared — an empty string — when nothing is resolved, because a
 * stale board name is worse than none.
 */
export function boardStatusLine(pin: Pin | undefined): string {
  if (!pin) return "";
  return pin.project ? `${pin.project} — ${pin.path}` : pin.path;
}

/** The pin as it travels in `details` (§7). */
export function pinDetails(pin: Pin | undefined): Record<string, unknown> {
  return pin ? { [PIN_DETAILS_KEY]: pin } : {};
}

export type { RunOutcome };
