/*
  The recommended tools (spec-pi-mm-plugin.md §4.2, second table):
  mm_block, mm_unblock, mm_search, mm_report, mm_tick, mm_archive.

  "SHOULD provide when the CLI does" — so unlike the required set these are
  registered conditionally, against what the installed build actually has
  (capabilities.ts; index.ts does the gating). Everything else about them is
  the same as the required set: one tool, one operation, one subprocess (§4.1),
  the same gates, the same exit-code mapping (§4.3).

  Two of the six carry rules of their own, and both are about restraint:

    §4.2  mm_tick is "the tickler service … expose only when the installed mm
          has it, and NEVER auto-run it; a bare mm_tick//mm tick run is the only
          trigger". There is no event handler in this file and there must never
          be one — §8.3 and §2.2 say the same thing about the whole plugin, and
          the tickler is where the temptation lives.
    §4.2  mm_archive "MUST surface the I1/I2 coverage warning verbatim; never
          run on a schedule from the plugin". The warning is `mm`'s own, in the
          envelope's `warnings`, and passes through `withWarnings` untouched —
          the plugin does not paraphrase the one sentence that says part of the
          record has left the validated set.

  On `--dry-run`: mm_tick and mm_archive expose it, and nothing else does.
  Those two are the operations whose signatures in `spec-tools.md` §5.3 name it
  (`--tick [--dry-run]`, and §5.3.1's "dry-run it first" for an operation that
  takes data OUT of the validated set). A preview is also what makes "never
  auto-run" liveable: the agent can show what a run WOULD do without doing it.
*/

import { Type } from "typebox";

import {
  archiveReport,
  itemLine,
  reportBody,
  searchHits,
  tickReport,
  withWarnings,
  type ArchiveResult,
  type ItemView,
  type ReportResult,
  type SearchHit,
  type TickResult,
} from "./format.ts";
import type { Pin } from "./board.ts";
import type { Envelope } from "./runner.ts";
import {
  operate,
  pinDetailsFor,
  ready,
  resultOf,
  text,
  type ToolDefinition,
  type ToolDeps,
} from "./tools-read.ts";

/**
 * Which `mm` operation each tool needs (§4.2: "expose only when the installed
 * mm has the op").
 *
 * The map is here, beside the tools, rather than in index.ts: a tool added
 * below without an entry would be registered unconditionally, and this is the
 * file where that omission is visible.
 */
export const RECOMMENDED_OPS: Readonly<Record<string, string>> = {
  mm_block: "block",
  mm_unblock: "unblock",
  mm_search: "search",
  mm_report: "report",
  mm_tick: "tick",
  mm_archive: "archive",
};

function stepDetails(step: { envelope: Envelope; pin: Pin }): Record<string, unknown> {
  return { ...pinDetailsFor(step.pin), envelope: step.envelope };
}

/**
 * The header a preview run carries.
 *
 * `mm --dry-run` prefixes its own human output with "would:"; a tool result
 * needs the same thing said once, at the top, so a model cannot read a preview
 * as a thing that happened.
 */
const DRY_RUN_HEAD = "dry run — nothing was written";

/** The files an operation touched, for the one-line report. */
function changedFiles(envelope: Envelope): string {
  const files = [...new Set((envelope.changes ?? []).map((c) => c.file).filter(Boolean))];
  return files.length ? `  (${files.join(", ")})` : "";
}

export function recommendedTools(deps: ToolDeps): ToolDefinition[] {
  return [
    {
      name: "mm_block",
      label: "Block item",
      description:
        "Move an item into the board's ## Blocked section with a reason. The reason is required — an item in Blocked that does not say why is an invariant violation (I5).",
      promptSnippet: "Move an item to blocked, with the reason it cannot proceed",
      promptGuidelines: [
        "Use mm_block when work cannot proceed for an external reason, and say what is being waited on — the reason is what makes the block reviewable later.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID, e.g. T-0042 (a bare number also works)" }),
        reason: Type.String({ description: "Why it cannot proceed. Required (I5)." }),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        const reason = String(params["reason"] ?? "").trim();
        if (!id) return text("mm_block needs an id.", {}, true);
        // Caught here rather than in the CLI so the message is about the
        // invariant instead of about a missing switch, and costs no subprocess.
        if (!reason) {
          return text("mm_block needs a reason; every blocked item must say why (I5).", {}, true);
        }

        const step = await operate(deps, ["--block", id, "--reason", reason], signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        return text(
          withWarnings(`blocked ${itemLine(item)}${changedFiles(step.envelope)}`, step.envelope),
          stepDetails(step),
        );
      },
    },

    {
      name: "mm_unblock",
      label: "Unblock item",
      description:
        "Move a blocked item back to ## Ready and drop its blocked reason. It returns to the TOP of Ready by default — something that has just become possible is usually the next thing to pick up.",
      promptSnippet: "Return a blocked item to ready and drop its reason",
      promptGuidelines: [
        "Use mm_unblock when the thing an item was waiting on has happened; it returns to the top of Ready unless end is set.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID" }),
        end: Type.Optional(
          Type.Boolean({ description: "Append to Ready instead of returning to its top" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_unblock needs an id.", {}, true);

        const args = ["--unblock", id];
        if (params["end"] === true) args.push("--end");

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        return text(
          withWarnings(`unblocked ${itemLine(item)}${changedFiles(step.envelope)}`, step.envelope),
          stepDetails(step),
        );
      },
    },

    {
      name: "mm_search",
      label: "Search board",
      description:
        "Find items by substring or regular expression over titles, tags and detail bodies. Each hit reports the item, which field matched, and the file and line it was found at.",
      promptSnippet: "Search the board's titles, tags and detail bodies",
      promptGuidelines: [
        "Use mm_search to find out whether something is already on the board before adding it again.",
      ],
      parameters: Type.Object({
        query: Type.String({ description: "Substring, or a regular expression when regex is set" }),
        fields: Type.Optional(
          Type.Array(
            Type.Union([Type.Literal("title"), Type.Literal("tags"), Type.Literal("detail")]),
            { description: "Restrict the search to these fields (default: all three)" },
          ),
        ),
        state: Type.Optional(
          Type.Union([Type.Literal("backlog"), Type.Literal("working"), Type.Literal("done")], {
            description: "Only items in this state",
          }),
        ),
        regex: Type.Optional(Type.Boolean({ description: "Treat the query as a regular expression" })),
        limit: Type.Optional(
          Type.Integer({ minimum: 1, maximum: 200, description: "Cap the number of hits (default 50)" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const query = String(params["query"] ?? "").trim();
        if (!query) return text("mm_search needs a query.", {}, true);

        const args = ["--search", query];
        for (const field of asStrings(params["fields"])) args.push("--field", field);
        if (typeof params["state"] === "string") args.push("--state", params["state"]);
        if (params["regex"] === true) args.push("--regex");
        // Capped like mm_list, and for the same reason: an unbounded result is
        // a context bill the user pays.
        const asked = typeof params["limit"] === "number" ? params["limit"] : 50;
        args.push("--limit", String(Math.max(1, Math.min(200, Math.trunc(asked)))));

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const hits = resultOf<SearchHit[]>(step.envelope) ?? [];
        return text(withWarnings(searchHits(hits), step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_report",
      label: "Report",
      description:
        "What closed in a period, newest first, with the period it resolved and where that came from. Defaults to the last COMPLETE week — a report over a period that is still accumulating gives a different answer every time it is run.",
      promptSnippet: "Summarise what closed in a period",
      promptGuidelines: [
        "Use mm_report for 'what did we get done' questions; report the period it names, because the default is last week rather than this one.",
      ],
      parameters: Type.Object({
        period: Type.Optional(
          Type.String({
            description:
              "A period token: last-week, this-week, YYYY-Www, last-N-days, all, last-month, this-month, YYYY-MM",
          }),
        ),
        since: Type.Optional(Type.String({ description: "Start date, YYYY-MM-DD (inclusive)" })),
        until: Type.Optional(Type.String({ description: "End date, YYYY-MM-DD (default: today)" })),
        group_by: Type.Optional(
          Type.Union(
            [
              Type.Literal("outcome"),
              Type.Literal("tag"),
              Type.Literal("day"),
              Type.Literal("none"),
            ],
            { description: "How to group the closed items (default: none, a flat list)" },
          ),
        ),
        include_wip: Type.Optional(
          Type.Boolean({ description: "Append what is currently in the working slots" }),
        ),
        include_backlog: Type.Optional(
          Type.Boolean({ description: "Append the top of ## Ready as what is next" }),
        ),
        include_archives: Type.Optional(
          Type.Boolean({ description: "Also read archived months, which are outside the ID pool" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const args = ["--report"];
        // §5.1.11's precedence is the CLI's to apply, not this tool's: it
        // passes what it was given and lets mm decide which wins, so the
        // period a report states is always the one mm resolved.
        if (typeof params["period"] === "string" && params["period"]) {
          args.push("--period", params["period"]);
        }
        if (typeof params["since"] === "string" && params["since"]) args.push("--since", params["since"]);
        if (typeof params["until"] === "string" && params["until"]) args.push("--until", params["until"]);
        if (typeof params["group_by"] === "string") args.push("--group-by", params["group_by"]);
        if (params["include_wip"] === true) args.push("--include-wip");
        if (params["include_backlog"] === true) args.push("--include-backlog");
        if (params["include_archives"] === true) args.push("--include-archives");

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const report = resultOf<ReportResult>(step.envelope) ?? {};
        return text(withWarnings(reportBody(report), step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_tick",
      label: "Run the tickler",
      description:
        "Run the tickler once: every ## Someday item whose tickler: schedule is due today fires. One-shot items move to Ready and consume their schedule; recurring items stay as prototypes and spawn a new Ready item. Never runs on its own — calling this tool is the only trigger.",
      promptSnippet: "Fire the board's due someday schedules, once",
      promptGuidelines: [
        "Only run mm_tick when the user asks for it; it is a mutation of the board and nothing schedules it.",
        "Run mm_tick with dry_run first when you want to show what is due without firing it.",
      ],
      parameters: Type.Object({
        dry_run: Type.Optional(
          Type.Boolean({ description: "Report what would fire and write nothing" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const args = ["--tick"];
        const preview = params["dry_run"] === true;
        if (preview) args.push("--dry-run");

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const result = resultOf<TickResult>(step.envelope) ?? {};
        const body = preview ? `${DRY_RUN_HEAD}\n${tickReport(result)}` : tickReport(result);
        return text(withWarnings(body, step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_archive",
      label: "Archive done months",
      description:
        "Roll month groups older than a cutoff out of the board's done file into a per-year archive, with their detail files. Archived items LEAVE THE ID POOL and stop being covered by invariants I1 and I2 — the warning saying so is part of every run's output.",
      promptSnippet: "Roll old closed months out into a per-year archive",
      promptGuidelines: [
        "Only run mm_archive when the user asks for it, and run it with dry_run first: it moves items out of the validated set, and the warning it prints is the cost of the run.",
      ],
      parameters: Type.Object({
        before: Type.Optional(
          Type.String({
            description: "Archive months older than this YYYY-MM; the named month stays",
          }),
        ),
        age: Type.Optional(
          Type.Integer({
            minimum: 0,
            description: "The same cutoff as a policy: keep each month for this many days",
          }),
        ),
        dry_run: Type.Optional(
          Type.Boolean({ description: "Report what would move and write nothing" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const before = typeof params["before"] === "string" ? params["before"].trim() : "";
        const age = typeof params["age"] === "number" ? Math.trunc(params["age"]) : undefined;
        // §5.3.1: "Two ways to express it, mutually exclusive." The CLI refuses
        // both too; refusing here names both and spends no subprocess.
        if (before && age !== undefined) {
          return text("mm_archive takes before or age, not both — they are two spellings of one cutoff.", {}, true);
        }

        const args = ["--archive"];
        if (before) args.push("--before", before);
        if (age !== undefined) args.push("--age", String(age));
        const preview = params["dry_run"] === true;
        if (preview) args.push("--dry-run");

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const result = resultOf<ArchiveResult>(step.envelope) ?? {};
        const body = preview ? `${DRY_RUN_HEAD}\n${archiveReport(result)}` : archiveReport(result);
        // withWarnings is what carries mm's I1/I2 coverage warning through
        // verbatim (§4.2). Nothing here rewrites or summarises it.
        return text(withWarnings(body, step.envelope), stepDetails(step));
      },
    },
  ];
}

/** Reads an array-of-strings parameter, ignoring anything that is not one. */
function asStrings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((v): v is string => typeof v === "string" && v.length > 0);
}
