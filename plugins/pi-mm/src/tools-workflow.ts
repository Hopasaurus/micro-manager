/*
  The workflow tools (spec-pi-mm-plugin.md §4.2, §4.3):
  mm_start, mm_pause, mm_finish.

  These move an item between the three homes an item can have — the backlog, a
  working slot, and done.md — and they are where the WIP limit is felt, so two
  rules matter more here than anywhere else:

    §4.1  ONE TOOL, ONE OPERATION. "start and finish" is two tool calls. A
          chain would hide which step failed, and each step is an independent
          transaction that either commits or does not.
    §4.3  At the WIP limit, the plugin MUST surface the remedy the CLI names —
          which slots are occupied — "so the agent can act (finish, pause, or
          --wip)". That remedy arrives inside the error message and is passed
          through rather than rebuilt; see the note on mm_start.

  §8.3 applies as ever: nothing here runs on its own initiative. The tool
  guidelines below tell the agent WHEN to call these, which is a suggestion the
  model acts on, never something an event handler does behind its back.
*/

import { Type } from "typebox";

import { itemLine, withWarnings, type ItemView } from "./format.ts";
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

/** Where an item ended up, in the words the board uses. */
function landed(item: ItemView): string {
  if (item.state === "working") {
    return item.slot ? `slot ${String(item.slot).padStart(2, "0")}` : "a working slot";
  }
  if (item.state === "done") return `done.md${item.outcome ? ` (${item.outcome})` : ""}`;
  const section = item.section ? item.section.toLowerCase() : "the backlog";
  return item.position ? `${section} #${item.position}` : section;
}

/** The files a transition touched, for the one-line report. */
function changedFiles(envelope: Envelope): string {
  const files = [...new Set((envelope.changes ?? []).map((c) => c.file).filter(Boolean))];
  return files.length ? `  (${files.join(", ")})` : "";
}

export function workflowTools(deps: ToolDeps): ToolDefinition[] {
  return [
    {
      name: "mm_start",
      label: "Start item",
      description:
        "Move a backlog item into a working slot. Fails when every slot is occupied — the WIP limit is the point of the slots — and the failure names what is in them.",
      promptSnippet: "Move a backlog item into a working slot",
      promptGuidelines: [
        "Use mm_start when you begin work on an item, so the board shows what is actually in progress.",
        "When mm_start reports the WIP limit, do not retry it: finish or pause one of the items it names, or ask the user whether to raise the limit.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID, e.g. T-0042 (a bare number also works)" }),
        slot: Type.Optional(
          Type.Integer({ minimum: 1, description: "A specific slot; omitted means the lowest idle one" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_start needs an id.", {}, true);

        const args = ["--start", id];
        if (typeof params["slot"] === "number") args.push("--slot", String(params["slot"]));

        const step = await operate(deps, args, signal);
        /*
          The WIP-limit failure needs nothing added here. mm's own message is

              wip limit reached (1/1)
                slot 01  T-0001  one
              finish one, pause one, or raise the limit with --wip

          — the remedy §4.3 asks for, already naming the occupied slots. The
          runner passes it through verbatim, so re-deriving a slot listing from
          the envelope would be a worse copy of something already correct. The
          guideline above is what stops the agent retrying it.
        */
        if (!step.ok) return step.result;

        const item = resultOf<ItemView>(step.envelope) ?? {};
        const body = `started ${itemLine(item)} → ${landed(item)}${changedFiles(step.envelope)}`;
        return text(withWarnings(body, step.envelope), {
          ...pinDetailsFor(step.pin),
          envelope: step.envelope,
        });
      },
    },

    {
      name: "mm_pause",
      label: "Pause item",
      description:
        "Return a working item to the backlog, freeing its slot. The slot's notes are preserved into the item's detail file — they exist nowhere else.",
      promptSnippet: "Return a working item to the backlog and free its slot",
      promptGuidelines: [
        "Use mm_pause to put work down without finishing it; the slot's notes are kept, so nothing is lost by pausing.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID" }),
        section: Type.Optional(
          Type.Union([Type.Literal("ready"), Type.Literal("blocked"), Type.Literal("someday")], {
            description: "Where it goes back to (default: the top of Ready)",
          }),
        ),
        end: Type.Optional(
          Type.Boolean({ description: "Append to the section instead of returning to its top" }),
        ),
        blocked: Type.Optional(
          Type.String({ description: "Why it cannot continue; required when pausing into blocked" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_pause needs an id.", {}, true);

        const args = ["--pause", id];
        if (typeof params["section"] === "string") args.push("--section", params["section"]);
        if (params["end"] === true) args.push("--end");
        if (typeof params["blocked"] === "string" && params["blocked"]) {
          args.push("--blocked", params["blocked"]);
        }

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        const body = `paused ${itemLine(item)} → ${landed(item)}${changedFiles(step.envelope)}`;
        return text(withWarnings(body, step.envelope), {
          ...pinDetailsFor(step.pin),
          envelope: step.envelope,
        });
      },
    },

    {
      name: "mm_finish",
      label: "Finish item",
      description:
        "Close an item into done.md with an outcome. Works from a working slot and from the backlog directly; outcome cancelled is how work is abandoned without deleting the record.",
      promptSnippet: "Close an item into done.md with an outcome",
      promptGuidelines: [
        "Use mm_finish when an item is done, and pass outcome cancelled or obsolete rather than mm_remove when work is being abandoned — the record is the point.",
        "Put what was learned in closing_note; it lands in the item's detail file, which outlives the conversation.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID" }),
        outcome: Type.Optional(
          Type.Union(
            [Type.Literal("shipped"), Type.Literal("cancelled"), Type.Literal("obsolete")],
            { description: "How it ended (default: shipped)" },
          ),
        ),
        closing_note: Type.Optional(
          Type.String({ description: "A closing note, appended to the item's detail file" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_finish needs an id.", {}, true);

        const args = ["--finish", id];
        if (typeof params["outcome"] === "string") args.push("--outcome", params["outcome"]);
        // --closing-note, not --note: --note is the operation that adds one
        // (spec-tools.md §5.1.10), and passing the wrong one here would be a
        // second operation rather than a modifier.
        if (typeof params["closing_note"] === "string" && params["closing_note"]) {
          args.push("--closing-note", params["closing_note"]);
        }

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        const body = `finished ${itemLine(item)} → ${landed(item)}${changedFiles(step.envelope)}`;
        return text(withWarnings(body, step.envelope), {
          ...pinDetailsFor(step.pin),
          envelope: step.envelope,
        });
      },
    },
  ];
}
