/*
  What a tool result SAYS (spec-pi-mm-plugin.md §4.1).

  "The tool result's `content` is the formatted outcome the model reads
  (compact, terminal-friendly, no raw JSON); `details` carries the parsed
  envelope." So this module turns envelopes into lines, and nothing else knows
  how a board looks written down.

  Compact is the whole design rule. Every line here is read by a model on every
  call, and the same text is what a human sees from `/mm` (§5: "the command
  MUST print the same formatted results the tools return"). Two renderings
  would be two things to keep in step.
*/

import type { Envelope, MmDirectory } from "./runner.ts";
import type { BoardListing, Pin } from "./board.ts";

/** One item as `mm` reports it in `result` (`spec-tools.md` §9.2). */
export interface ItemView {
  readonly id?: string;
  readonly title?: string;
  readonly state?: string;
  readonly section?: string;
  readonly slot?: number;
  readonly position?: number;
  readonly prio?: string;
  readonly tags?: readonly string[];
  readonly detail?: string;
  readonly created?: string;
  readonly started?: string;
  readonly done?: string;
  readonly outcome?: string;
  readonly blocked?: string;
  readonly tickler?: string;
  /**
   * Fields the format does not register — its extension point (§9 of the
   * format spec). They are carried, never dropped: a view that hid them would
   * make the plugin lossy about a format whose whole point is that a tool it
   * has never heard of can add a key.
   */
  readonly extra?: Readonly<Record<string, string>>;
  readonly file?: string;
  readonly line?: number;
}

/**
 * One item on one line: `T-0042  Fix the deploy script (high, infra,ci)`.
 *
 * The ID leads because it is the handle every other tool takes. Priority and
 * tags are parenthesised and omitted when absent rather than printed empty —
 * `(med)` on every item would be noise on the majority of a board.
 */
export function itemLine(item: ItemView): string {
  const bits: string[] = [];
  if (item.prio && item.prio !== "med") bits.push(item.prio);
  if (item.tags?.length) bits.push(item.tags.join(","));
  if (item.blocked) bits.push(`blocked: ${item.blocked}`);
  if (item.tickler) bits.push(`wakes ${item.tickler}`);
  const suffix = bits.length ? `  (${bits.join(", ")})` : "";
  return `${item.id ?? "?"}  ${item.title ?? ""}${suffix}`;
}

/** Where an item lives, for `mm_show`: the state, and the slot when working. */
export function itemLocation(item: ItemView): string {
  if (item.state === "working") {
    return item.slot ? `working, slot ${String(item.slot).padStart(2, "0")}` : "working";
  }
  if (item.state === "done") {
    return item.outcome ? `done (${item.outcome}${item.done ? `, ${item.done}` : ""})` : "done";
  }
  const section = item.section ? item.section.toLowerCase() : "backlog";
  return item.position ? `${section} #${item.position}` : section;
}

/** Every field of one item, for `mm_show`. Absent fields are not printed. */
export function itemDetail(item: ItemView): string {
  const lines = [`${item.id ?? "?"}  ${item.title ?? ""}`, `  where:   ${itemLocation(item)}`];
  const field = (label: string, value: string | undefined) => {
    if (value) lines.push(`  ${label.padEnd(8)} ${value}`);
  };
  field("prio:", item.prio);
  field("tags:", item.tags?.length ? item.tags.join(",") : undefined);
  field("created:", item.created);
  field("started:", item.started);
  field("done:", item.done);
  field("outcome:", item.outcome);
  field("blocked:", item.blocked);
  field("wakes:", item.tickler);
  field("detail:", item.detail);
  for (const [key, value] of Object.entries(item.extra ?? {})) {
    lines.push(`  ${`${key}:`.padEnd(8)} ${value}`);
  }
  return lines.join("\n");
}

/** The board's identity line: project, path, and description when it has one. */
export function boardIdentity(pin: Pin, directory?: MmDirectory): string {
  const project = directory?.project ?? pin.project;
  const head = project ? `${project} — ${pin.path}` : pin.path;
  // §3.4: the description is the first paragraph of structure.md, carried in
  // the envelope. It is absent until the CLI grows --describe (T-0187), and an
  // absent description is a valid state rather than an error.
  const description = directory?.description;
  return description ? `${head}\n${truncate(description, 200)}` : head;
}

/** How the board was arrived at (§3.2.1), in the words the spec uses. */
export function resolutionNote(pin: Pin): string {
  switch (pin.source) {
    case "pin":
      return "resolved: pinned for this session";
    case "MM_DIR":
      return "resolved: MM_DIR";
    default:
      return "resolved: mm's discovery";
  }
}

/** `mm --status`'s result, as `mm_status` and `mm_board` show it. */
export interface StatusResult {
  readonly counts?: { readonly ready?: number; readonly blocked?: number; readonly someday?: number; readonly done?: number };
  readonly next?: ItemView | null;
  readonly oldestReady?: ItemView | null;
  readonly slots?: ReadonlyArray<{ readonly file?: string; readonly occupied?: boolean; readonly item?: ItemView | null }>;
  readonly wip?: { readonly limit?: number; readonly used?: number };
  readonly path?: string;
  readonly project?: string;
}

/**
 * The status summary: WIP, counts, what is in work, what is next.
 *
 * This is the one screen §3.4 calls "what `mm --status` already is", and the
 * shape context injection will reuse (§6) — so it stays data, with no
 * commentary a prompt would have to carry every turn.
 */
export function statusSummary(result: StatusResult): string {
  const wip = `wip ${result.wip?.used ?? 0}/${result.wip?.limit ?? 0}`;
  const c = result.counts ?? {};
  const counts = [
    `ready ${c.ready ?? 0}`,
    `blocked ${c.blocked ?? 0}`,
    `someday ${c.someday ?? 0}`,
    `done ${c.done ?? 0}`,
  ].join(" · ");
  const lines = [`${wip} · ${counts}`];

  const working = (result.slots ?? []).filter((s) => s.occupied && s.item);
  if (working.length) {
    for (const slot of working) {
      lines.push(`in work:  ${itemLine(slot.item as ItemView)}`);
    }
  } else {
    lines.push("in work:  nothing");
  }
  lines.push(result.next ? `next:     ${itemLine(result.next)}` : "next:     nothing ready");
  if (result.oldestReady && result.oldestReady.id !== result.next?.id) {
    lines.push(`oldest:   ${itemLine(result.oldestReady)}`);
  }
  return lines.join("\n");
}

/** `mm --find`'s listing, with the board in use marked (§3.2.1). */
export function boardList(boards: readonly BoardListing[]): string {
  if (boards.length === 0) {
    // A result, never an error: a workspace with no boards is a fact about the
    // workspace, and mm_init is the answer to it.
    return "no boards located. mm_init creates one.";
  }
  const lines = boards.map((b) => {
    const mark = b.inUse ? "* " : "  ";
    return `${mark}${b.path}${b.project ? `  (${b.project})` : ""}`;
  });
  return [`${boards.length} board${boards.length === 1 ? "" : "s"}:`, ...lines].join("\n");
}

/** One violation of `mm --check`, as `path:line: message`. */
export interface Violation {
  readonly file?: string;
  readonly line?: number;
  readonly invariant?: string;
  readonly message?: string;
}

export interface CheckResult {
  readonly ok?: boolean;
  readonly path?: string;
  readonly project?: string;
  readonly violations?: readonly Violation[];
}

/** `mm --check`'s findings, each with its location (§4.2). */
export function checkReport(results: readonly CheckResult[]): string {
  const lines: string[] = [];
  let total = 0;
  for (const dir of results) {
    const violations = dir.violations ?? [];
    total += violations.length;
    for (const v of violations) {
      const where = v.file ? `${v.file}${v.line ? `:${v.line}` : ""}` : dir.path ?? "?";
      lines.push(`${where}: ${v.invariant ? `${v.invariant} ` : ""}${v.message ?? ""}`);
    }
  }
  if (total === 0) return "clean: no violations.";
  return [`${total} violation${total === 1 ? "" : "s"}:`, ...lines].join("\n");
}

/** A list of items, or a plain statement that there are none. */
export function itemList(items: readonly ItemView[], empty: string): string {
  if (items.length === 0) return empty;
  return items.map(itemLine).join("\n");
}

/** Warnings ride along under the result, never mixed into it. */
export function withWarnings(body: string, envelope: Envelope): string {
  const warnings = envelope.warnings ?? [];
  if (warnings.length === 0) return body;
  return [body, ...warnings.map((w) => `warning: ${w}`)].join("\n");
}

function truncate(s: string, limit: number): string {
  const t = s.trim();
  return t.length > limit ? `${t.slice(0, limit - 1)}…` : t;
}
