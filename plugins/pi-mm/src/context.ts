/*
  Per-turn context injection (spec-pi-mm-plugin.md §6).

  Before every turn, a compact block naming the board and its state goes into
  the system prompt, so the agent starts knowing what is in flight without
  spending a tool call:

      [mm] /srv/boards/todos — Sample One — wip 1/4 · ready 3 · blocked 1 · someday 5
           in work:  T-0018  Migrate the build cache
           next:     T-0169  Spec: tickler fields and grammar (high)

  Every rule in §6 is about restraint, and each one is load-bearing:

    - DATA ONLY. No "please remember", no advice. Behaviour belongs in the
      tools' promptGuidelines, where it is attached to the tool it is about;
      prose here would be re-read every single turn.
    - ≤ 8 LINES, configurable down. A board that overflows is truncated with an
      explicit `…`, so the agent can tell "that is all of it" from "there is
      more" — and the full picture is one mm_status away.
    - NO DONE ITEMS, no archives. What is finished is not what is in flight.
    - REFRESH AFTER ANY MUTATION. The block must never describe a board the
      plugin just changed; between mutations it may be cached, because it is a
      snapshot and the agent's own tool calls are what is authoritative.
    - NOTHING AT ALL when no board is resolved or `mm` is absent. An empty
      injection is better than a false one.
*/

import { pinnedBoard, type Pin } from "./board.ts";
import { itemLine, type ItemView, type StatusResult } from "./format.ts";
import type { Envelope, RunOutcome, run as runFn } from "./runner.ts";
import { contextEnabled } from "./settings.ts";

/** §6's hard ceiling. `contextLines` may lower it, never raise it. */
export const MAX_LINES = 8;

/**
 * Builds the block from one `--status` envelope.
 *
 * Exported so the shape can be asserted without a subprocess: what goes into a
 * system prompt every turn deserves a test that reads it.
 */
export function contextBlock(pin: Pin, envelope: Envelope, maxLines = 6): string {
  const limit = Math.max(1, Math.min(MAX_LINES, maxLines));
  const result = (envelope.result ?? {}) as StatusResult;
  const directory = envelope.directory;
  const project = directory?.project ?? pin.project;
  const counts = result.counts ?? {};
  const wip = result.wip ?? {};

  // Head: which board, and the whole board in numbers. `done` is a count, not
  // a listing — §6 bars done ITEMS, and the number is what says whether the
  // board is moving.
  const head =
    `[mm] ${pin.path}` +
    (project ? ` — ${project}` : "") +
    ` — wip ${wip.used ?? 0}/${wip.limit ?? 0}` +
    ` · ready ${counts.ready ?? 0} · blocked ${counts.blocked ?? 0} · someday ${counts.someday ?? 0}`;

  const lines: string[] = [head];

  // §3.4/§6: the description MAY ride along, one line, still counted.
  if (directory?.description) {
    lines.push(`     ${oneLine(directory.description, 100)}`);
  }

  const working = (result.slots ?? []).filter((s) => s.occupied && s.item);
  for (const slot of working) {
    lines.push(`     in work:  ${itemLine(slot.item as ItemView)}`);
  }
  if (working.length === 0) {
    lines.push("     in work:  nothing");
  }
  if (result.next) {
    lines.push(`     next:     ${itemLine(result.next)}`);
  }

  if (lines.length <= limit) return lines.join("\n");
  // Truncated EXPLICITLY: the agent must be able to tell a short board from a
  // clipped one, or it will reason about work it cannot see.
  return [...lines.slice(0, limit - 1), "     …"].join("\n");
}

/* ------------------------------------------------------------- the cache */

interface Cached {
  readonly path: string;
  readonly block: string;
}

let cached: Cached | undefined;
let stale = true;

/**
 * Marks the cached block out of date.
 *
 * Called whenever an operation reports changes — `mm` itself says what it
 * touched, so nothing here has to know which operations mutate. That is the
 * §6 rule "MUST refresh after any plugin mutation" implemented from the one
 * source that cannot be wrong about it.
 */
export function markBoardChanged(envelope: Envelope): void {
  if ((envelope.changes ?? []).length > 0) stale = true;
}

/** Drops the cache outright, for session start and pin changes. */
export function resetContext(): void {
  cached = undefined;
  stale = true;
}

export interface ContextDeps {
  readonly health: () => Promise<{ ok: true; version: string } | { ok: false; message: string }>;
  readonly run: typeof runFn;
  readonly timeoutMs?: number;
  readonly contextLines?: number;
  /**
   * §6: never inject a board the project has not been trusted to read. The
   * caller passes pi's own answer; the plugin does not decide trust.
   */
  readonly allowed?: boolean;
}

/**
 * The block for this turn, or nothing.
 *
 * Nothing is the answer whenever there is any doubt: injection turned off, no
 * `mm`, no board, an untrusted project, or a status read that failed. §6 is
 * explicit that an empty injection beats a false one, so every failure path
 * here returns undefined rather than a guess or a stale line.
 */
export async function turnContext(deps: ContextDeps): Promise<string | undefined> {
  if (deps.allowed === false) return undefined;
  if (!contextEnabled()) return undefined;

  const pin = pinnedBoard();
  // Deliberately NOT resolving here. Resolution is a board-changing decision a
  // turn boundary should not make on the user's behalf, and §6 says to inject
  // nothing when no board is resolved — not to go looking for one.
  if (!pin) return undefined;

  if (!stale && cached && cached.path === pin.path) return cached.block;

  const state = await deps.health();
  if (!state.ok) return undefined;

  const outcome: RunOutcome = await deps.run({
    args: ["--status"],
    dir: pin.path,
    ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
  });
  if (!outcome.ok) return undefined;

  const block = contextBlock(pin, outcome.envelope, deps.contextLines ?? 6);
  cached = { path: pin.path, block };
  stale = false;
  return block;
}

function oneLine(s: string, limit: number): string {
  const flat = s.replace(/\s+/g, " ").trim();
  return flat.length > limit ? `${flat.slice(0, limit - 1)}…` : flat;
}
