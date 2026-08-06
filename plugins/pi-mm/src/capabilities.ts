/*
  What can the installed `mm` do? (spec-pi-mm-plugin.md §4.2)

  The required tools are always registered: a build that cannot run one of them
  is not a conforming `mm`, and exit 2 is the honest answer if it happens
  anyway (§4.3). The RECOMMENDED set is different — §4.2 says to provide it
  "when the CLI does", and of `mm_tick`: "expose only when the installed mm has
  it". A tool the model can see is a tool it will try; offering `mm_archive`
  against a build that has never heard of `--archive` spends a turn to learn
  what one probe already knows.

  So this module asks once, at session start, and caches for the session:

    mm --help  →  the Operations section  →  the set of operation names

  Why `--help` rather than a probe per operation: it is one subprocess instead
  of six, and a CLI's own help is the list it maintains — spec-tools.md §3.4
  requires `--help` to name the operations, and the Go build's usage text has a
  test asserting it matches the parser exactly.

  UNKNOWN IS NOT ABSENT. A help text this cannot read leaves `known: false`,
  and `hasOperation` then answers yes to everything. Hiding a tool because a
  probe was unreadable would be a worse failure than exposing one that answers
  exit 2 — which §4.3 already renders as "not available in the installed mm
  build". The gate is an optimisation over that fallback, never a substitute
  for it.
*/

import { spawnText } from "./presence.ts";

/** How long the probe may take before the answer counts as unknown. */
export const CAPABILITY_TIMEOUT_MS = 5_000;

export interface Capabilities {
  /** The operation names `mm --help` listed, without their leading dashes. */
  readonly operations: ReadonlySet<string>;
  /** False when the help text could not be read as a list of operations. */
  readonly known: boolean;
}

/** Nothing was learned: every gate opens (see the header). */
export const UNKNOWN_CAPABILITIES: Capabilities = { operations: new Set(), known: false };

/**
 * Reads the operation names out of `mm --help`.
 *
 * Scoped to the Operations section rather than scanning the whole text,
 * because the rest of the page names modifiers and environment variables with
 * the same spelling — `--dir`, `--json`, and a mention of `--report` inside a
 * description of `MM_REPORT_PERIOD`. Section-scoped, an operation is listed
 * because it IS one.
 */
export function parseOperations(help: string): Capabilities {
  const lines = help.split("\n");
  const start = lines.findIndex((line) => /^operations:\s*$/i.test(line.trim()));
  if (start < 0) return UNKNOWN_CAPABILITIES;

  const operations = new Set<string>();
  for (const line of lines.slice(start + 1)) {
    // A new section header — "Global modifiers:", "Environment:" — ends the
    // list. Blank lines inside it do not, since a long list may be grouped.
    if (/^\S/.test(line)) break;
    const match = /^\s+--([a-z][a-z0-9-]*)/.exec(line);
    if (match?.[1]) operations.add(match[1]);
  }
  // A section that yielded nothing is a help text shaped differently from what
  // this can read, which is the unknown case rather than "no operations".
  return operations.size > 0 ? { operations, known: true } : UNKNOWN_CAPABILITIES;
}

/**
 * Does the installed build have this operation?
 *
 * `true` whenever the answer is unknown — see the header. The argument is the
 * operation's name without dashes: `hasOperation(caps, "tick")`.
 */
export function hasOperation(caps: Capabilities, operation: string): boolean {
  return caps.known ? caps.operations.has(operation) : true;
}

/**
 * The session's cached answer.
 *
 * One probe per session, like presence.ts's: an `mm` does not grow an
 * operation mid-session, and a subprocess per registration decision is a cost
 * a guest should not impose. `resetCapabilities()` exists for `/reload` and
 * for tests.
 */
let cached: Promise<Capabilities> | undefined;

export function capabilities(command = "mm", timeoutMs = CAPABILITY_TIMEOUT_MS): Promise<Capabilities> {
  cached ??= probeCapabilities(command, timeoutMs);
  return cached;
}

export function resetCapabilities(): void {
  cached = undefined;
}

/** Runs the probe once. Exported for tests and for a deliberate re-probe. */
export async function probeCapabilities(
  command = "mm",
  timeoutMs = CAPABILITY_TIMEOUT_MS,
): Promise<Capabilities> {
  const outcome = await spawnText(command, ["--help"], timeoutMs);
  // Anything other than a clean run leaves the answer unknown. There is no
  // second attempt: a plugin that retried a probe would be spending the user's
  // session start on a question whose fallback is already correct.
  if (outcome.kind !== "exit") return UNKNOWN_CAPABILITIES;
  // Some CLIs print help to stderr. Both are read rather than assuming which,
  // because the cost of guessing wrong is hiding every recommended tool.
  return parseOperations(outcome.stdout || outcome.stderr);
}
