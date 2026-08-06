/*
  The write tools (spec-pi-mm-plugin.md §4.2, §3.3, §3.4, Appendix A):
  mm_add, mm_edit, mm_move, mm_note, mm_init, mm_describe.

  Same rules as the read set, and two more that only matter once a tool
  changes something:

    §4.1  One tool, one operation, one TRANSACTION. "start and finish" is two
          tool calls, because a chain hides which step failed — and because
          each mutation is independently validated before it commits.
    §8.3  Nothing on its own initiative. These run when the agent or the user
          asks, never from an event handler.

  Two of them depend on `mm` switches a minimally conforming build need not
  have (§3.3): `--init --prefix`, `--init --description`, and `--describe`.
  The plugin does NOT sniff for them — it runs the operation and lets exit 2
  say so, which is the degradation §4.3 already defines and one fewer thing to
  keep in step with the CLI. It never writes `structure.md` itself (§3.4).
*/

import { Type } from "typebox";

import { boardStatusLine, boardFromEnvelope, pinDetails, setPin, type Pin } from "./board.ts";
import { markBoardChanged } from "./context.ts";
import { itemDetail, itemLine, withWarnings, type ItemView } from "./format.ts";
import type { Envelope } from "./runner.ts";
import {
  operate,
  pinDetailsFor,
  ready,
  resultOf,
  text,
  type ToolDefinition,
  type ToolDeps,
  type ToolResult,
} from "./tools-read.ts";

/** `mm --init --prefix P`: one to four uppercase ASCII letters (format §3.3.2). */
const ID_PREFIX = /^[A-Z]{1,4}$/;

/**
 * The changed files a mutation reports, as one line.
 *
 * §4.1 puts the change set in `details` for a front end to render; this is the
 * one-line version for the model, so a write says what it touched without
 * anyone parsing JSON.
 */
function changedFiles(envelope: Envelope): string {
  const files = [...new Set((envelope.changes ?? []).map((c) => c.file).filter(Boolean))];
  return files.length ? `  (${files.join(", ")})` : "";
}

export function writeTools(deps: ToolDeps): ToolDefinition[] {
  return [
    {
      name: "mm_add",
      label: "Add item",
      description:
        "Add an item to the board's backlog and report the ID it was assigned. The ID is the handle every other tool takes.",
      promptSnippet: "Add a backlog item and get its assigned ID",
      promptGuidelines: [
        "Use mm_add to capture work; it appends to the bottom of the section unless top is set, because a new item is not automatically more urgent than what is queued.",
      ],
      parameters: Type.Object({
        title: Type.String({ description: "One line, and it must not contain a pipe (|)" }),
        section: Type.Optional(
          Type.Union([Type.Literal("ready"), Type.Literal("blocked"), Type.Literal("someday")], {
            description: "Which backlog section (default: ready)",
          }),
        ),
        prio: Type.Optional(
          Type.Union([Type.Literal("high"), Type.Literal("med"), Type.Literal("low")]),
        ),
        tags: Type.Optional(Type.Array(Type.String(), { description: "Tags, no spaces in each" })),
        top: Type.Optional(
          Type.Boolean({ description: "Insert at the top of the section instead of the bottom" }),
        ),
        blocked: Type.Optional(
          Type.String({ description: "Why it cannot start; implies section blocked" }),
        ),
        detail_text: Type.Optional(
          Type.String({ description: "Long-form description, written to the item's detail file" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const title = String(params["title"] ?? "").trim();
        if (!title) return text("mm_add needs a title.", {}, true);

        const args = ["--add", title];
        if (typeof params["section"] === "string") args.push("--section", params["section"]);
        if (typeof params["prio"] === "string") args.push("--prio", params["prio"]);
        for (const tag of asStrings(params["tags"])) args.push("--tag", tag);
        if (params["top"] === true) args.push("--top");
        if (typeof params["blocked"] === "string" && params["blocked"]) {
          args.push("--blocked", params["blocked"]);
        }
        if (typeof params["detail_text"] === "string" && params["detail_text"]) {
          args.push("--detail-text", params["detail_text"]);
        }

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        // §4.2: "The new item INCLUDING ITS ID — the handle for everything
        // after." Leading with it is the whole contract of this tool.
        const body = `added ${itemLine(item)}${changedFiles(step.envelope)}`;
        return text(withWarnings(body, step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_edit",
      label: "Edit item",
      description:
        "Change fields on an existing item in place — title, priority, tags, and any other key including ones this plugin does not know. It never rewrites the item wholesale; unnamed fields are left exactly as they are.",
      promptSnippet: "Change fields on one board item in place",
      promptGuidelines: [
        "Use mm_edit for fields; use mm_move for position, and mm_start/mm_pause/mm_finish for state.",
        "mm_edit changes only the fields you name — read the item with mm_show first if you need to see what is there.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID, e.g. T-0042" }),
        title: Type.Optional(Type.String()),
        prio: Type.Optional(
          Type.Union([Type.Literal("high"), Type.Literal("med"), Type.Literal("low")]),
        ),
        tags: Type.Optional(Type.Array(Type.String(), { description: "Tags to ADD" })),
        untags: Type.Optional(Type.Array(Type.String(), { description: "Tags to remove" })),
        set: Type.Optional(
          Type.Array(Type.String(), {
            description:
              "Arbitrary fields as KEY=VALUE, including keys the format does not register (the extension point)",
          }),
        ),
        unset: Type.Optional(Type.Array(Type.String(), { description: "Field keys to remove" })),
        blocked: Type.Optional(Type.String({ description: "The blocked reason" })),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_edit needs an id.", {}, true);

        const args = ["--edit", id];
        if (typeof params["title"] === "string") args.push("--title", params["title"]);
        if (typeof params["prio"] === "string") args.push("--prio", params["prio"]);
        for (const tag of asStrings(params["tags"])) args.push("--tag", tag);
        for (const tag of asStrings(params["untags"])) args.push("--untag", tag);
        // §4.2: "MUST reach unregistered keys via --set". Unregistered fields
        // are the format's extension point, and a tool that could not write one
        // would make the plugin less capable than the CLI it wraps.
        for (const pair of asStrings(params["set"])) {
          if (!pair.includes("=")) {
            return text(`mm_edit --set takes KEY=VALUE, got ${JSON.stringify(pair)}.`, {}, true);
          }
          args.push("--set", pair);
        }
        for (const key of asStrings(params["unset"])) args.push("--unset", key);
        if (typeof params["blocked"] === "string") args.push("--blocked", params["blocked"]);

        if (args.length === 2) return text("mm_edit needs something to change.", {}, true);

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        return text(
          withWarnings(`updated\n${itemDetail(item)}`, step.envelope),
          stepDetails(step),
        );
      },
    },

    {
      name: "mm_move",
      label: "Move item",
      description:
        "Reposition a backlog item, or move it between backlog sections. Exactly one destination: a position, the top, the end, or before/after another item.",
      promptSnippet: "Reposition a backlog item or move it between sections",
      promptGuidelines: [
        "Use mm_move to reorder or re-section an item; the order of ## Ready is the user's prioritisation, so move deliberately rather than tidying.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID" }),
        section: Type.Optional(
          Type.Union([Type.Literal("ready"), Type.Literal("blocked"), Type.Literal("someday")]),
        ),
        position: Type.Optional(Type.Integer({ minimum: 1, description: "1-based position" })),
        top: Type.Optional(Type.Boolean()),
        end: Type.Optional(Type.Boolean()),
        before: Type.Optional(Type.String({ description: "Place it before this item's ID" })),
        after: Type.Optional(Type.String({ description: "Place it after this item's ID" })),
        blocked: Type.Optional(
          Type.String({ description: "Reason, required when moving into blocked" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_move needs an id.", {}, true);

        const destinations = ["position", "top", "end", "before", "after"].filter((k) => {
          const v = params[k];
          return v !== undefined && v !== false && v !== "";
        });
        // The CLI refuses two destinations too; catching it here spends no
        // subprocess and names both, which is the more useful error.
        if (destinations.length > 1) {
          return text(`mm_move takes one destination; got ${destinations.join(" and ")}.`, {}, true);
        }
        if (destinations.length === 0 && typeof params["section"] !== "string") {
          return text("mm_move needs a destination or a section.", {}, true);
        }

        const args = ["--move", id];
        if (typeof params["section"] === "string") args.push("--section", params["section"]);
        if (typeof params["position"] === "number") args.push("--position", String(params["position"]));
        if (params["top"] === true) args.push("--top");
        if (params["end"] === true) args.push("--end");
        if (typeof params["before"] === "string" && params["before"]) args.push("--before", params["before"]);
        if (typeof params["after"] === "string" && params["after"]) args.push("--after", params["after"]);
        if (typeof params["blocked"] === "string") args.push("--blocked", params["blocked"]);

        const step = await operate(deps, args, signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        return text(withWarnings(`moved ${itemLine(item)}`, step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_note",
      label: "Add note",
      description:
        "Append a dated entry to an item's notes — the working slot's ## Notes when it is in one, otherwise its detail file, which is created if needed.",
      promptSnippet: "Append a dated note to a board item",
      promptGuidelines: [
        "Use mm_note to record a finding against the item it belongs to, rather than leaving it only in the conversation.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID" }),
        text: Type.String({ description: "The note. One entry; it is dated for you." }),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        const note = String(params["text"] ?? "").trim();
        if (!id) return text("mm_note needs an id.", {}, true);
        if (!note) return text("mm_note needs some text.", {}, true);

        const step = await operate(deps, ["--note", id, note], signal);
        if (!step.ok) return step.result;
        const item = resultOf<ItemView>(step.envelope) ?? {};
        const body = `noted on ${item.id ?? id}${changedFiles(step.envelope)}`;
        return text(withWarnings(body, step.envelope), stepDetails(step));
      },
    },

    {
      name: "mm_init",
      label: "Create board",
      description:
        "Create a new micro-manager board and pin it for this session. The ID prefix cannot be changed cleanly later, so it belongs here.",
      promptSnippet: "Create a new board and pin it for the session",
      promptGuidelines: [
        "Use mm_init to bootstrap a board when there is none; it pins the new board, so later mm_* calls act on it.",
      ],
      parameters: Type.Object({
        project: Type.String({ description: "The board's human name. Required." }),
        dir: Type.Optional(
          Type.String({ description: "Where to create it; omitted means mm's default" }),
        ),
        slots: Type.Optional(
          Type.Integer({ minimum: 1, description: "Working files, and so the WIP limit" }),
        ),
        slot_width: Type.Optional(
          Type.Integer({ minimum: 1, description: "Digits in a slot number, e.g. 2 for slot 01" }),
        ),
        prefix: Type.Optional(
          Type.String({ description: "ID prefix: one to four uppercase letters, e.g. G for G-0001" }),
        ),
        description: Type.Optional(
          Type.String({ description: "The board's description, written into structure.md" }),
        ),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const project = String(params["project"] ?? "").trim();
        if (!project) return text("mm_init needs a project name.", {}, true);

        const prefix = typeof params["prefix"] === "string" ? params["prefix"].trim() : "";
        // §3.3: validated against the format's grammar BEFORE invocation, so a
        // bad prefix costs no subprocess and gets a message about the grammar
        // rather than about a switch.
        if (prefix && !ID_PREFIX.test(prefix)) {
          return text(
            `prefix must be one to four uppercase letters (A-Z), got ${JSON.stringify(prefix)}.`,
            {},
            true,
          );
        }

        const args = ["--init", "--project", project];
        if (typeof params["slots"] === "number") args.push("--slots", String(params["slots"]));
        if (typeof params["slot_width"] === "number") {
          args.push("--slot-width", String(params["slot_width"]));
        }
        if (prefix) args.push("--prefix", prefix);
        if (typeof params["description"] === "string" && params["description"]) {
          args.push("--description", params["description"]);
        }

        // mm_init is the one tool that does NOT resolve a board first: it is
        // what you call when there is none, and --dir names where the new one
        // goes rather than which one to act on.
        const dir = typeof params["dir"] === "string" ? params["dir"].trim() : "";
        const outcome = await deps.run({
          args,
          ...(dir ? { dir } : {}),
          ...(deps.command ? { command: deps.command } : {}),
          ...(deps.timeoutMs !== undefined ? { timeoutMs: deps.timeoutMs } : {}),
          ...(signal ? { signal } : {}),
        });
        if (!outcome.ok) {
          return text(
            outcome.failure.message,
            { error: { kind: outcome.failure.kind, exit: outcome.failure.exit } },
            true,
          );
        }

        markBoardChanged(outcome.envelope);

        // §3.3: "A freshly created board is pinned for the session." Its
        // identity comes from `result`, not `directory` — there was no
        // directory to describe until this call made one.
        const pin = boardFromEnvelope(outcome.envelope, "pin");
        if (pin) {
          setPin(pin);
          deps.onPin?.(boardStatusLine(pin));
        }
        const created = resultOf<{ path?: string; project?: string }>(outcome.envelope) ?? {};
        const body = [
          `created ${created.project ?? project} at ${created.path ?? dir}`,
          prefix ? `ids are ${prefix}-0001…` : "",
          "pinned for this session",
        ]
          .filter(Boolean)
          .join("\n");
        return text(withWarnings(body, outcome.envelope), {
          ...pinDetails(pin),
          envelope: outcome.envelope,
        });
      },
    },

    {
      name: "mm_describe",
      label: "Describe board",
      description:
        "Write or replace the board's description — the first paragraph of its structure.md — through mm. The plugin never edits that file itself.",
      promptSnippet: "Set the board's description",
      promptGuidelines: [
        "Use mm_describe to record what a board is for; it replaces the first paragraph of structure.md and leaves the rest.",
      ],
      parameters: Type.Object({
        text: Type.String({ description: "The description. One paragraph." }),
      }),
      async execute(_id, params, signal) {
        const gate = await ready(deps);
        if (gate) return gate;

        const body = String(params["text"] ?? "").trim();
        if (!body) return text("mm_describe needs some text.", {}, true);

        const step = await operate(deps, ["--describe", body], signal);
        // A build without --describe answers exit 2, which the runner already
        // reports as "not available in the installed mm build" (§4.3). That is
        // the whole gate: the plugin does not sniff for the switch, and it
        // never falls back to writing structure.md itself (§3.4, §8.1).
        if (!step.ok) return step.result;
        return text(
          withWarnings(`description set${changedFiles(step.envelope)}`, step.envelope),
          stepDetails(step),
        );
      },
    },
  ];
}

function stepDetails(step: { envelope: Envelope; pin: Pin }): Record<string, unknown> {
  return { ...pinDetailsFor(step.pin), envelope: step.envelope };
}

/** Reads an array-of-strings parameter, ignoring anything that is not one. */
function asStrings(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((v): v is string => typeof v === "string" && v.length > 0);
}

export type { ToolResult };
