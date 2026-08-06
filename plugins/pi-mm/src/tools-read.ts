/*
  The required read tools (spec-pi-mm-plugin.md §4.2, Appendix A):
  mm_status, mm_next, mm_list, mm_show, mm_board, mm_check, mm_find.

  One tool, one `mm` operation, one subprocess (§4.1) — no tool here chains
  two, and none of them writes. Every result is `content` (the formatted
  outcome a model reads) plus `details` (the parsed envelope, and the pin, for
  §7's state reconstruction).

  The gates, in order, before any of them runs its operation:

    1. `mm` on PATH at all (§2.1) — otherwise an error naming the remedy.
    2. a resolved board (§3.2) — otherwise an error naming the cause, never a
       fake empty board (§4.1). mm_find is the exception: listing boards is
       exactly what you do when you have none.
*/

import { Type } from "typebox";

import {
  boardStatusLine,
  listBoards,
  pinDetails,
  pinnedBoard,
  resolveBoard,
  setPin,
  type Pin,
} from "./board.ts";
import {
  boardIdentity,
  boardList,
  checkReport,
  itemDetail,
  itemList,
  resolutionNote,
  statusSummary,
  withWarnings,
  type CheckResult,
  type ItemView,
  type StatusResult,
} from "./format.ts";
import { markBoardChanged } from "./context.ts";
import { run, type Envelope, type RunOutcome } from "./runner.ts";

/** What a tool needs from the world, injected so tests can drive it. */
export interface ToolDeps {
  /** The health gate of §2.1 — index.ts's `health()` in production. */
  readonly health: () => Promise<{ ok: true; version: string } | { ok: false; message: string }>;
  readonly run: typeof run;
  readonly command?: string;
  readonly timeoutMs?: number;
  /** Called whenever the pin changes, so the TUI status line follows it (§3.2.1). */
  readonly onPin?: (line: string) => void;
  /**
   * §9's `confirmRemove`: the master switch for mm_remove's interactive prompt
   * (§8.2 step 2). It never weakens the schema guard — `confirmed: true` is
   * required either way. Defaults to on; the config reader is T-0183.
   */
  readonly confirmRemove?: boolean;
}

/**
 * The slice of pi's ExtensionContext the tools use.
 *
 * Narrow on purpose: a tool that took the whole context could reach anything,
 * and what these actually need is "is there a user, and may I ask them?".
 */
export interface ToolContext {
  readonly hasUI?: boolean;
  readonly ui?: {
    confirm?: (title: string, message: string) => Promise<boolean> | boolean;
  };
}

/** The tool-result shape pi expects, narrowed to what these tools produce. */
export interface ToolResult {
  readonly content: ReadonlyArray<{ readonly type: "text"; readonly text: string }>;
  readonly details: Record<string, unknown>;
  readonly isError?: boolean;
}

export interface ToolDefinition {
  readonly name: string;
  readonly label: string;
  readonly description: string;
  readonly promptSnippet?: string;
  readonly promptGuidelines?: readonly string[];
  readonly parameters: unknown;
  execute(
    toolCallId: string,
    params: Record<string, unknown>,
    signal?: AbortSignal,
    // pi's own tool signature. onUpdate streams progress, which nothing here
    // needs; ctx is what mm_remove asks the user through (§8.2).
    onUpdate?: unknown,
    ctx?: ToolContext,
  ): Promise<ToolResult>;
}

export const text = (body: string, details: Record<string, unknown> = {}, isError = false): ToolResult => ({
  content: [{ type: "text", text: body }],
  details,
  ...(isError ? { isError: true } : {}),
});

/**
 * The environment gate, on its own (§2.1).
 *
 * Tools that validate their parameters first would otherwise answer "mm_move
 * needs a destination" to a user whose real problem is that the plugin cannot
 * work at all — two round trips to reach the actionable message. So every tool
 * asks this before it inspects what it was called with: the environment
 * failure is the more fundamental one, and it is the same answer whatever the
 * arguments were.
 */
export async function ready(deps: ToolDeps): Promise<ToolResult | undefined> {
  const state = await deps.health();
  return state.ok ? undefined : text(state.message, {}, true);
}

/** An envelope's `result`, read as the shape the caller expects. */
export function resultOf<T>(envelope: Envelope): T | undefined {
  return (envelope.result ?? undefined) as T | undefined;
}

/**
 * Runs one operation against the resolved board.
 *
 * Every read tool is this plus a formatter, which is the point: the gates, the
 * `--dir`, the error mapping and the `details` shape are decided once.
 */
export async function operate(
  deps: ToolDeps,
  args: readonly string[],
  signal: AbortSignal | undefined,
): Promise<{ ok: true; envelope: Envelope; pin: Pin } | { ok: false; result: ToolResult }> {
  const state = await deps.health();
  if (!state.ok) return { ok: false, result: text(state.message, {}, true) };

  const resolution = await resolveBoard({
    run: deps.run,
    ...(deps.command ? { command: deps.command } : {}),
    ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
    ...(signal ? { signal } : {}),
  });
  if (!resolution.ok) {
    const body =
      resolution.reason === "ambiguous"
        ? [resolution.message, "", boardList(resolution.candidates), "", "Pin one with mm_board."].join("\n")
        : resolution.message;
    return { ok: false, result: text(body, { reason: resolution.reason }, true) };
  }
  deps.onPin?.(boardStatusLine(resolution.pin));

  const outcome: RunOutcome = await deps.run({
    args,
    dir: resolution.pin.path,
    ...(deps.command ? { command: deps.command } : {}),
    ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
    ...(signal ? { signal } : {}),
  });
  if (!outcome.ok) {
    // A failed mutation can still have written something before it failed —
    // mm reports what it touched either way, so the cache is invalidated from
    // the same source in both branches (§6).
    if (outcome.failure.envelope) markBoardChanged(outcome.failure.envelope);
    return {
      ok: false,
      result: text(
        outcome.failure.message,
        {
          ...pinDetails(resolution.pin),
          error: { kind: outcome.failure.kind, exit: outcome.failure.exit, errors: outcome.failure.errors },
          ...(outcome.failure.envelope ? { envelope: outcome.failure.envelope } : {}),
        },
        true,
      ),
    };
  }
  markBoardChanged(outcome.envelope);
  return { ok: true, envelope: outcome.envelope, pin: resolution.pin };
}

/** The pin as `details` carries it (§7), for tool files that assemble their own. */
export function pinDetailsFor(pin: Pin): Record<string, unknown> {
  return pinDetails(pin);
}

/** `details` for a successful read: the envelope, plus the pin (§4.1, §7). */
function details(envelope: Envelope, pin: Pin): Record<string, unknown> {
  return { ...pinDetails(pin), envelope };
}

export function readTools(deps: ToolDeps): ToolDefinition[] {
  return [
    {
      name: "mm_status",
      label: "Board status",
      description:
        "The micro-manager board's one-screen status: which directory, WIP n/N, per-section counts, what is in a working slot, and the top of Ready.",
      promptSnippet: "Read the board's status: WIP, counts, what is in work, what is next",
      promptGuidelines: [
        "Use mm_status to see the board's state before deciding what to work on, and again after a Concurrent error to re-read.",
        "Prefer the mm_* tools over shelling out to the mm CLI with bash.",
      ],
      parameters: Type.Object({}),
      async execute(_id, _params, signal) {
        const step = await operate(deps, ["--status"], signal);
        if (!step.ok) return step.result;
        const result = resultOf<StatusResult>(step.envelope) ?? {};
        const body = [
          boardIdentity(step.pin, step.envelope.directory),
          statusSummary(result),
        ].join("\n");
        return text(withWarnings(body, step.envelope), details(step.envelope, step.pin));
      },
    },

    {
      name: "mm_next",
      label: "Next item",
      description:
        "The top of the board's ## Ready section — the item to start next. Reports that nothing is ready rather than failing when the section is empty.",
      promptSnippet: "Read the next item to work on from the board",
      promptGuidelines: [
        "Use mm_next when the user asks what to work on next; it reads the top of Ready without re-sorting it.",
      ],
      parameters: Type.Object({}),
      async execute(_id, _params, signal) {
        const step = await operate(deps, ["--next"], signal);
        if (!step.ok) {
          // §4.2: a non-zero exit for an empty Ready is "nothing ready", an
          // explicit result — not an error the agent has to interpret.
          if (step.result.details["error"] && isEmptyReady(step.result)) {
            return text("nothing ready.", { ...step.result.details, empty: true });
          }
          return step.result;
        }
        const item = resultOf<ItemView>(step.envelope);
        const body = item?.id ? itemList([item], "nothing ready.") : "nothing ready.";
        return text(withWarnings(body, step.envelope), details(step.envelope, step.pin));
      },
    },

    {
      name: "mm_list",
      label: "List items",
      description:
        "Items on the board in ON-DISK ORDER, filtered by section, state, priority or tag. The order of ## Ready is the user's own prioritisation and is never re-sorted.",
      promptSnippet: "List board items in on-disk order, with filters",
      promptGuidelines: [
        "Use mm_list to see items; the order it returns is the user's prioritisation, so do not re-rank it when reporting.",
      ],
      parameters: Type.Object({
        section: Type.Optional(
          Type.Union([Type.Literal("ready"), Type.Literal("blocked"), Type.Literal("someday")], {
            description: "Backlog section to filter by",
          }),
        ),
        state: Type.Optional(
          Type.Union(
            [
              Type.Literal("backlog"),
              Type.Literal("working"),
              Type.Literal("done"),
              Type.Literal("all"),
            ],
            { description: "Where the items live (default: backlog)" },
          ),
        ),
        prio: Type.Optional(
          Type.Union([Type.Literal("high"), Type.Literal("med"), Type.Literal("low")]),
        ),
        tag: Type.Optional(Type.String({ description: "Only items carrying this tag" })),
        limit: Type.Optional(
          Type.Integer({ minimum: 1, maximum: 200, description: "Cap the number of items (default 50)" }),
        ),
      }),
      async execute(_id, params, signal) {
        const args = ["--list"];
        if (typeof params["section"] === "string") args.push("--section", params["section"]);
        if (typeof params["state"] === "string") args.push("--state", params["state"]);
        if (typeof params["prio"] === "string") args.push("--prio", params["prio"]);
        if (typeof params["tag"] === "string") args.push("--tag", params["tag"]);
        // §4.2: default 50, capped at 200. The cap is applied here rather than
        // trusted from the model, because an unbounded list is a context bill
        // the user pays.
        const asked = typeof params["limit"] === "number" ? params["limit"] : 50;
        args.push("--limit", String(Math.max(1, Math.min(200, Math.trunc(asked)))));

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const items = resultOf<ItemView[]>(step.envelope) ?? [];
        return text(
          withWarnings(itemList(items, "no items match."), step.envelope),
          details(step.envelope, step.pin),
        );
      },
    },

    {
      name: "mm_show",
      label: "Show item",
      description:
        "Every field of one item and where it currently lives, optionally with the body of its detail file.",
      promptSnippet: "Read one board item in full, optionally with its detail file",
      promptGuidelines: [
        "Use mm_show before editing an item, so the change is made against what the board actually holds.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID, e.g. T-0042 (a bare number also works)" }),
        detail: Type.Optional(Type.Boolean({ description: "Include the detail file's body" })),
      }),
      async execute(_id, params, signal) {
        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_show needs an id.", {}, true);
        const args = ["--show", id];
        if (params["detail"] === true) args.push("--detail");

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        // --show wraps the item: { item, detail? } — the detail block is
        // present only with --detail and only when the item has a file.
        const shown = resultOf<{ item?: ItemView; detail?: { body?: string } }>(step.envelope) ?? {};
        const item = shown.item ?? {};
        const body = shown.detail?.body ?? "";
        const rendered = body ? `${itemDetail(item)}\n\n${body.trim()}` : itemDetail(item);
        return text(withWarnings(rendered, step.envelope), details(step.envelope, step.pin));
      },
    },

    {
      name: "mm_board",
      label: "Board in use",
      description:
        "Show or change the board this session operates on. With no path: the pinned board's identity, description and live status. With a path: pin that board for the session.",
      promptSnippet: "Show the board in use, or pin a different one",
      promptGuidelines: [
        "Use mm_board with no path to find out which board you are operating on, and with a path when the user names a different one.",
      ],
      parameters: Type.Object({
        path: Type.Optional(
          Type.String({ description: "Pin this board for the session; omit to show the current one" }),
        ),
      }),
      async execute(_id, params, signal) {
        const state = await deps.health();
        if (!state.ok) return text(state.message, {}, true);

        const path = typeof params["path"] === "string" ? params["path"].trim() : "";
        const resolution = await resolveBoard({
          run: deps.run,
          ...(path ? { path } : {}),
          ...(deps.command ? { command: deps.command } : {}),
          ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
          ...(signal ? { signal } : {}),
        });
        if (!resolution.ok) {
          const body =
            resolution.reason === "ambiguous"
              ? [resolution.message, "", boardList(resolution.candidates), "", "Pin one with mm_board."].join("\n")
              : resolution.message;
          return text(body, { reason: resolution.reason }, true);
        }
        setPin(resolution.pin);
        deps.onPin?.(boardStatusLine(resolution.pin));

        // One call answers "show me this board": what it is, what it says about
        // itself, and what is happening on it right now (§3.4).
        const outcome = await deps.run({
          args: ["--status"],
          dir: resolution.pin.path,
          ...(deps.command ? { command: deps.command } : {}),
          ...(signal ? { signal } : {}),
        });
        if (!outcome.ok) return text(outcome.failure.message, pinDetails(resolution.pin), true);
        const result = resultOf<StatusResult>(outcome.envelope) ?? {};
        const body = [
          boardIdentity(resolution.pin, outcome.envelope.directory),
          resolutionNote(resolution.pin),
          statusSummary(result),
        ].join("\n");
        return text(withWarnings(body, outcome.envelope), details(outcome.envelope, resolution.pin));
      },
    },

    {
      name: "mm_find",
      label: "Find boards",
      description:
        "Every micro-manager board this environment can see, with the one in use marked. No boards is a result, not an error.",
      promptSnippet: "List every board that can be located",
      promptGuidelines: [
        "Use mm_find when the board is ambiguous or unknown, then mm_board to pin the right one.",
      ],
      parameters: Type.Object({}),
      async execute(_id, _params, signal) {
        // No board gate here on purpose: listing boards is exactly what you do
        // when there is no board in use (§3.2.1).
        const state = await deps.health();
        if (!state.ok) return text(state.message, {}, true);

        const boards = await listBoards({
          run: deps.run,
          ...(deps.command ? { command: deps.command } : {}),
          ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
          ...(signal ? { signal } : {}),
        });
        return text(boardList(boards), {
          boards,
          ...pinDetails(pinnedBoard()),
        });
      },
    },

    {
      name: "mm_check",
      label: "Check board",
      description:
        "Validate the board against the ten invariants. Reports every violation as path:line, or that the board is clean.",
      promptSnippet: "Validate the board and report violations with their locations",
      promptGuidelines: [
        "Use mm_check when a tool reports an invariant violation, and report the findings verbatim rather than summarising them.",
      ],
      parameters: Type.Object({}),
      async execute(_id, _params, signal) {
        const step = await operate(deps, ["--check"], signal);
        if (!step.ok) {
          // Violations are results, not errors (spec-tools.md §5.1.12): the
          // exit code is what a script gates on, and a broken board is exactly
          // what this tool is for reporting.
          const envelope = step.result.details["envelope"] as Envelope | undefined;
          const results = envelope ? resultOf<CheckResult[]>(envelope) : undefined;
          if (results?.length) {
            return text(checkReport(results), { ...step.result.details, violations: true });
          }
          return step.result;
        }
        const results = resultOf<CheckResult[]>(step.envelope) ?? [];
        return text(
          withWarnings(checkReport(results), step.envelope),
          details(step.envelope, step.pin),
        );
      },
    },
  ];
}

/** Is this failure an empty ## Ready rather than something wrong? */
function isEmptyReady(result: ToolResult): boolean {
  const error = result.details["error"] as { exit?: number | null } | undefined;
  // spec-tools.md §5.2: --next exits non-zero when Ready is empty so a script
  // can stop; that is the only meaning of exit 3 for this operation.
  return error?.exit === 3;
}
