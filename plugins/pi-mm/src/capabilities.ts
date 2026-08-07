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

    mm --help --json  →  result.operations  →  the set of operation names

  ONE subprocess, TWO readings of it. A build that implements spec-tools.md
  §3.4.1 answers with the envelope, whose lists are generated from the parser's
  own tables — the authoritative answer, and the reason that section exists. An
  older build ignores `--json` and prints prose, so the same output is then read
  as the `Operations:` section of a help page. Neither costs a second probe: the
  fallback parses what the first one already returned.

  Why `--help` at all rather than a probe per operation: it is one subprocess
  instead of one per tool, and a CLI's own help is the list it maintains.

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
  /** The operation names the build reported, without their leading dashes. */
  readonly operations: ReadonlySet<string>;
  /** False when the answer could not be read at all. */
  readonly known: boolean;
  /**
   * How it was learned: the §3.4.1 envelope, or the prose help of an older
   * build. Carried because it is the one thing worth saying in a diagnostic —
   * a wrong answer from "prose" is a parsing problem, and a wrong answer from
   * "json" is the build lying about itself.
   */
  readonly source?: "json" | "prose";
}

/** Nothing was learned: every gate opens (see the header). */
export const UNKNOWN_CAPABILITIES: Capabilities = { operations: new Set(), known: false };

/**
 * Reads the capability list out of `mm --help --json` (spec-tools.md §3.4.1).
 *
 * The lists there are generated from the parser's tables rather than from the
 * help prose, which is the whole point: a help text that grows a section or
 * re-indents a line cannot change what this believes about the build.
 *
 * Anything unexpected returns undefined so the caller falls back to the prose
 * reader — an older `mm` ignores `--json` on `--help` and prints a page, and
 * that is not an error, just an older build.
 */
export function parseCapabilityEnvelope(stdout: string): Capabilities | undefined {
  const text = stdout.trim();
  if (!text.startsWith("{")) return undefined;
  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    return undefined;
  }
  const result = (parsed as { result?: { operations?: unknown } } | null)?.result;
  const listed = result?.operations;
  if (!Array.isArray(listed)) return undefined;
  const operations = new Set(listed.filter((op): op is string => typeof op === "string"));
  // An envelope that reported no operations at all is not a build with none;
  // it is an answer this cannot use, and unknown is the safe reading of that.
  if (operations.size === 0) return undefined;
  return { operations, known: true, source: "json" };
}

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
  return operations.size > 0 ? { operations, known: true, source: "prose" } : UNKNOWN_CAPABILITIES;
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
  const outcome = await spawnText(command, ["--help", "--json"], timeoutMs);
  // Anything other than a clean run leaves the answer unknown. There is no
  // second attempt: a plugin that retried a probe would be spending the user's
  // session start on a question whose fallback is already correct.
  if (outcome.kind !== "exit") return UNKNOWN_CAPABILITIES;
  // Some CLIs print help to stderr. Both are read rather than assuming which,
  // because the cost of guessing wrong is hiding every recommended tool.
  const text = outcome.stdout || outcome.stderr;
  // The structured answer first, the prose page second — one output, read the
  // better way when the build offers it.
  return parseCapabilityEnvelope(text) ?? parseOperations(text);
}
