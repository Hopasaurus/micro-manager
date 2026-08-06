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

/* ------------------------------------------- the recommended set (§4.2) */

/** One hit of `mm --search` (`spec-tools.md` §5.2, §9.2). */
export interface SearchHit {
  readonly item?: ItemView;
  /** Which field matched: title, tags, or detail. */
  readonly field?: string;
  readonly at?: { readonly file?: string; readonly line?: number };
  readonly text?: string;
}

/**
 * Search hits, one line each.
 *
 * §5.2 asks a search to report "state and location per hit", and both are on
 * the line rather than in `details`: a hit whose state you cannot see is one
 * you have to call `mm_show` about before you know whether it is still open.
 */
export function searchHits(hits: readonly SearchHit[]): string {
  if (hits.length === 0) return "no matches.";
  const lines = hits.map((hit) => {
    const item = hit.item ?? {};
    const where = hit.at?.file ? `${hit.at.file}${hit.at.line ? `:${hit.at.line}` : ""}` : "";
    const tail = [itemLocation(item), hit.field ? `${hit.field} match` : "", where]
      .filter(Boolean)
      .join(", ");
    return `${itemLine(item)}  — ${tail}`;
  });
  return [`${hits.length} hit${hits.length === 1 ? "" : "s"}:`, ...lines].join("\n");
}

/** `mm --report`'s result (`spec-tools.md` §5.1.11). */
export interface ReportResult {
  readonly project?: string;
  readonly period?: {
    readonly label?: string;
    readonly since?: string;
    readonly until?: string;
    /** Where the period came from: a switch, the environment, or the default. */
    readonly source?: string;
  };
  readonly done?: readonly ItemView[];
  readonly groups?: ReadonlyArray<{ readonly key?: string; readonly items?: readonly ItemView[] }>;
  readonly wip?: readonly ItemView[];
  readonly next?: readonly ItemView[];
}

/**
 * A report.
 *
 * The period line is not decoration: §5.1.11 requires EVERY output mode to
 * state the resolved period and where it came from, because "a report whose
 * period is invisible is a report you cannot check" — and a model reading a
 * bare list of items has no other way to know which week it is looking at.
 */
export function reportBody(report: ReportResult): string {
  const period = report.period ?? {};
  const span = period.since && period.until ? ` (${period.since}..${period.until})` : "";
  const from = period.source ? ` — from ${period.source}` : "";
  const head = `${report.project ? `${report.project} — ` : ""}${period.label ?? "period"}${span}${from}`;

  const lines = [head];
  const done = report.done ?? [];
  lines.push(`${done.length} closed`);
  if (report.groups?.length) {
    for (const group of report.groups) {
      lines.push(`${group.key ?? "(ungrouped)"}:`);
      for (const item of group.items ?? []) lines.push(`  ${itemLine(item)}`);
    }
  } else {
    for (const item of done) lines.push(`  ${itemLine(item)}`);
  }
  if (report.wip?.length) {
    lines.push("in progress:");
    for (const item of report.wip) lines.push(`  ${itemLine(item)}`);
  }
  if (report.next?.length) {
    lines.push("next:");
    for (const item of report.next) lines.push(`  ${itemLine(item)}`);
  }
  return lines.join("\n");
}

/** `mm --tick`'s result (`spec-tools.md` §5.3.3). */
export interface TickResult {
  readonly fired?: ReadonlyArray<{
    readonly id?: string;
    /** `move` for a one-shot, `spawn` for a recurring prototype. */
    readonly kind?: string;
    readonly spawned?: string;
    readonly tickled?: string;
  }>;
  readonly errors?: ReadonlyArray<{ readonly id?: string; readonly error?: string }>;
}

/**
 * What a tickler run did.
 *
 * Both halves are always reported, because §5.3.3 requires it: "a failing item
 * never aborts the run", so a run that fired three and failed one is a success
 * whose failure still has to be said out loud.
 */
export function tickReport(result: TickResult): string {
  const fired = result.fired ?? [];
  const errors = result.errors ?? [];
  const lines: string[] = [];

  if (fired.length === 0) {
    lines.push("nothing was due.");
  } else {
    lines.push(`${fired.length} fired:`);
    for (const fire of fired) {
      // The two kinds of fire are different events, not one with a flag: a
      // one-shot MOVES the item and consumes its schedule; a recurring one
      // leaves the prototype where it is and spawns a new item with a new ID.
      const what =
        fire.kind === "spawn"
          ? `spawned ${fire.spawned ?? "a new item"}`
          : "moved to ready";
      lines.push(`  ${fire.id ?? "?"}  ${what}${fire.tickled ? ` (tickled ${fire.tickled})` : ""}`);
    }
  }
  for (const failure of errors) {
    lines.push(`  error: ${failure.id ?? "?"}: ${failure.error ?? "unknown"}`);
  }
  return lines.join("\n");
}

/** `mm --archive`'s result (`spec-tools.md` §5.3.1). */
export interface ArchiveResult {
  readonly cutoff?: string;
  readonly months?: readonly string[];
  readonly items?: number;
  readonly files?: readonly string[];
  readonly detailsMoved?: ReadonlyArray<{
    readonly id?: string;
    readonly from?: string;
    readonly to?: string;
  }>;
  readonly detailOrphans?: readonly string[];
}

/**
 * What an archive run moved.
 *
 * Every filename here comes from the result, never from a literal: the plugin
 * does not know what an archive file is called and must not appear to (§8.1).
 * The cost of the run — that archived items leave the ID pool and stop being
 * covered by I1/I2 — is `mm`'s own warning and rides along verbatim through
 * `withWarnings` (§4.2), which is why it is not restated here.
 */
export function archiveReport(result: ArchiveResult): string {
  const items = result.items ?? 0;
  const cutoff = result.cutoff ? ` (cutoff ${result.cutoff})` : "";
  if (items === 0) return `nothing to archive${cutoff}.`;

  const months = result.months ?? [];
  const files = result.files ?? [];
  const lines = [
    `archived ${items} item${items === 1 ? "" : "s"}` +
      (months.length ? ` from ${months.join(", ")}` : "") +
      (files.length ? ` into ${files.join(", ")}` : "") +
      cutoff,
  ];
  for (const move of result.detailsMoved ?? []) {
    lines.push(`  ${move.id ?? "?"}: ${move.from ?? "?"} → ${move.to ?? "?"}`);
  }
  for (const orphan of result.detailOrphans ?? []) {
    lines.push(`  orphaned: ${orphan}`);
  }
  return lines.join("\n");
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
