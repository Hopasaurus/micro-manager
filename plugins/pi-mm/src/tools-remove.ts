/*
  mm_remove, and the double guard (spec-pi-mm-plugin.md §8.2).

  Removal is the one operation that destroys the record. §8.2 puts three
  independent locks on it, and the order is the point — each one is a different
  party asserting the same thing:

    1. `confirmed: true` in the schema, defaulting to FALSE, so a missing
       parameter is a refusal. This is the MODEL asserting it has the user's
       go-ahead.
    2. `ctx.ui.confirm` when there is a UI, naming the item AND ITS HOME. This
       is the USER, answering for themselves.
    3. `--force`, the CLI's own guard (spec-tools.md §5.1.6) — "the third line,
       not the first".

  Headless (`ctx.hasUI === false`) skips step 2 and the explicit `confirmed` is
  the whole guard: there is nobody to ask, and inventing an answer would be
  worse than either refusing everything or trusting the flag the model set
  deliberately.

  And the thing the guards exist to point AT: removal is for a typo, a
  duplicate, an item added to the wrong board. Abandoning work is
  `mm_finish --outcome cancelled`, which keeps the record. The tool says so
  when it refuses, because a refusal that does not name the alternative just
  gets retried.
*/

import { Type } from "typebox";

import { itemLine, itemLocation, withWarnings, type ItemView } from "./format.ts";
import {
  operate,
  pinDetailsFor,
  ready,
  resultOf,
  text,
  type ToolContext,
  type ToolDefinition,
  type ToolDeps,
} from "./tools-read.ts";

/** What the user is asked, when there is a user to ask. */
function confirmation(item: ItemView, id: string): { title: string; message: string } {
  const named = item.id ? itemLine(item) : id;
  // §8.2 step 2: "naming the item and its home". The home is what makes the
  // question answerable — removing something in a working slot or already in
  // done.md is a different decision from removing a backlog line.
  const home = item.id ? ` (${itemLocation(item)})` : "";
  return {
    title: "Remove this item?",
    message:
      `${named}${home}\n\n` +
      "It is deleted outright and the ID is retired, never reused. " +
      "To abandon the work while keeping the record, cancel this and use " +
      "mm_finish with outcome cancelled instead.",
  };
}

export function removeTools(deps: ToolDeps): ToolDefinition[] {
  return [
    {
      name: "mm_remove",
      label: "Remove item",
      description:
        "Delete an item outright. The ID is retired and never reused, and nothing is kept. Requires confirmed:true, and asks the user as well when there is a UI. To abandon work while keeping the record, use mm_finish with outcome cancelled instead.",
      promptSnippet: "Delete a board item outright (guarded; prefer mm_finish --outcome cancelled)",
      promptGuidelines: [
        "Use mm_remove only for a mistake — a typo, a duplicate, an item added to the wrong board. Work being abandoned is mm_finish with outcome cancelled, which keeps the record.",
        "Set confirmed:true on mm_remove only when the user has actually asked for the deletion in this conversation; it is an assertion about them, not about your own confidence.",
      ],
      parameters: Type.Object({
        id: Type.String({ description: "The item's ID, e.g. T-0042" }),
        confirmed: Type.Boolean({
          // The default is the guard. A model that omits the parameter has not
          // asserted anything, and the answer to "did the user ask for this?"
          // must never be assumed.
          default: false,
          description:
            "Set true ONLY when the user has asked for this item to be deleted. Omitting it refuses.",
        }),
      }),
      async execute(_toolCallId, params, signal, _onUpdate, ctx?: ToolContext) {
        const gate = await ready(deps);
        if (gate) return gate;

        const id = String(params["id"] ?? "").trim();
        if (!id) return text("mm_remove needs an id.", {}, true);

        // Guard 1, before anything is read or spawned: an unconfirmed removal
        // is refused outright, and the refusal names the alternative so it is
        // not simply retried with the flag flipped.
        if (params["confirmed"] !== true) {
          return text(
            `${id} was not removed: mm_remove needs confirmed:true, and only when the user ` +
              "has asked for the deletion. If the work is being abandoned rather than the " +
              "item being a mistake, use mm_finish with outcome cancelled — that keeps the record.",
            { confirmed: false },
            true,
          );
        }

        /*
          Read the item before asking about it (§8.2 step 2 names the item and
          its home). This is a second subprocess, and deliberately not the
          chaining §4.1 forbids: that rule is about chaining MUTATIONS, whose
          failure modes hide each other. A read that fails removes nothing and
          reports its own error — and a confirmation prompt that could not say
          what it was about would be a worse guard than none.
        */
        const shown = await operate(deps, ["--show", id], signal);
        if (!shown.ok) return shown.result;
        const item =
          resultOf<{ item?: ItemView }>(shown.envelope)?.item ??
          resultOf<ItemView>(shown.envelope) ??
          {};

        // Guard 2: the user, when there is one to ask. §9's confirmRemove is
        // the master switch for this prompt — it never weakens guard 1.
        const asking = deps.confirmRemove !== false;
        if (asking && ctx?.hasUI && ctx.ui?.confirm) {
          const { title, message } = confirmation(item, id);
          const answer = await ctx.ui.confirm(title, message);
          if (!answer) {
            return text(`${item.id ?? id} was not removed: the user declined.`, {
              declined: true,
              ...pinDetailsFor(shown.pin),
            });
          }
        }

        // Guard 3: the CLI's own. --force is passed only now, on the far side
        // of both of the plugin's guards.
        const step = await operate(deps, ["--remove", id, "--force"], signal);
        if (!step.ok) return step.result;

        const removed = resultOf<{ item?: ItemView; detailOrphan?: string }>(step.envelope) ?? {};
        const gone = removed.item ?? item;
        const lines = [`removed ${gone.id ?? id}  ${gone.title ?? ""}`.trimEnd()];
        // mm reports an orphaned detail file as a warning; withWarnings carries
        // it. The tool does not offer to delete it — that is --with-detail, a
        // switch §4.2 does not give this tool.
        return text(withWarnings(lines.join("\n"), step.envelope), {
          ...pinDetailsFor(step.pin),
          envelope: step.envelope,
        });
      },
    },
  ];
}
