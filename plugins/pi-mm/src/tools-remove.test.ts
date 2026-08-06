/*
  mm_remove and the double guard (spec-pi-mm-plugin.md §8.2).

  Three locks, and a test per way of getting past one you should not: no
  confirmation, a user who says no, and a UI that is not there. What each
  asserts is that NOTHING RAN — a guard that lets the subprocess through and
  regrets it afterwards is not a guard.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { clearPin, setPin } from "./board.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";
import { removeTools } from "./tools-remove.ts";
import type { ToolContext, ToolDefinition, ToolDeps } from "./tools-read.ts";

const BOARD = "/boards/rb";

const STATUS_ENVELOPE = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Runner Board" },
  result: { counts: { ready: 1 }, wip: { limit: 1, used: 0 } },
};

const SHOW_ENVELOPE = {
  ok: true,
  operation: "show",
  directory: { path: BOARD },
  result: {
    item: {
      id: "T-0004",
      title: "a duplicate",
      state: "backlog",
      section: "Ready",
      position: 3,
      prio: "high",
    },
  },
};

const REMOVE_ENVELOPE = {
  ok: true,
  operation: "remove",
  directory: { path: BOARD },
  result: { item: { id: "T-0004", title: "a duplicate" } },
  changes: [{ kind: "removed", id: "T-0004", file: "backlog.md" }],
  warnings: ["details/T-0004.md was left with no item; the directory now fails I9"],
};

function fakeRun(table: Record<string, RunOutcome>) {
  const calls: RunOptions[] = [];
  const run = async (opts: RunOptions): Promise<RunOutcome> => {
    calls.push(opts);
    const answer = table[opts.args[0] ?? ""];
    if (!answer) throw new Error(`test has no answer for ${opts.args[0]}`);
    return answer;
  };
  return { run: run as unknown as ToolDeps["run"], calls };
}

const ok = (envelope: unknown): RunOutcome => ({ ok: true, envelope: envelope as never });

function tool(table: Record<string, RunOutcome>, extra: Partial<ToolDeps> = {}) {
  const runner = fakeRun(table);
  const deps: ToolDeps = {
    health: async () => ({ ok: true, version: "mm 0.1.0" }),
    run: runner.run,
    ...extra,
  };
  const tools = removeTools(deps);
  return { tool: tools[0] as ToolDefinition, calls: runner.calls, all: tools };
}

/** A pi context with a UI that answers `answer`, recording what it was asked. */
function uiCtx(answer: boolean) {
  const asked: Array<{ title: string; message: string }> = [];
  const ctx: ToolContext = {
    hasUI: true,
    ui: {
      confirm: async (title, message) => {
        asked.push({ title, message });
        return answer;
      },
    },
  };
  return { ctx, asked };
}

const pinned = () => setPin({ path: BOARD, project: "Runner Board", source: "pin" });
const textOf = (r: { content: ReadonlyArray<{ text: string }> }) => r.content[0]?.text ?? "";
const ops = (calls: RunOptions[]) => calls.map((c) => c.args[0]);

test("mm_remove is one tool, and it is the only one here", () => {
  const { all } = tool({});
  assert.deepEqual(
    all.map((t) => t.name),
    ["mm_remove"],
  );
});

/* ------------------------------------------------------------- guard 1 */

test("without confirmed:true it refuses, and NOTHING runs (§8.2 step 1)", async (t) => {
  t.after(clearPin);
  pinned();
  for (const params of [{ id: "T-0004" }, { id: "T-0004", confirmed: false }]) {
    const { tool: mmRemove, calls } = tool({});
    const result = await mmRemove.execute("id", params);
    assert.equal(result.isError, true);
    assert.match(textOf(result), /needs confirmed:true/);
    // The refusal names the alternative, or it just gets retried with the flag
    // flipped — which is the failure mode the guard exists to prevent.
    assert.match(textOf(result), /mm_finish with outcome cancelled/);
    assert.deepEqual(calls, [], "no subprocess: not even the read");
  }
});

test("the schema defaults confirmed to false", () => {
  const { tool: mmRemove } = tool({});
  const schema = mmRemove.parameters as { properties?: { confirmed?: { default?: unknown } } };
  // A missing parameter must be a refusal, which is what the default encodes.
  assert.equal(schema.properties?.confirmed?.default, false);
});

/* ------------------------------------------------------------- guard 2 */

test("with a UI it asks, naming the item AND its home (§8.2 step 2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok(SHOW_ENVELOPE),
    "--remove": ok(REMOVE_ENVELOPE),
  });
  const { ctx, asked } = uiCtx(true);

  const result = await mmRemove.execute("id", { id: "T-0004", confirmed: true }, undefined, undefined, ctx);
  assert.equal(asked.length, 1, "the user is asked exactly once");
  assert.match(asked[0]!.message, /T-0004 {2}a duplicate/, "the item is named");
  assert.match(asked[0]!.message, /ready #3/, "…and its home");
  assert.match(asked[0]!.message, /mm_finish with outcome cancelled/, "…and the alternative");
  assert.equal(result.isError, undefined);
  assert.match(textOf(result), /removed T-0004/);
  // The read comes before the question; --force only on the far side of it.
  // (No --status: the board was already pinned, and a pinned board is never
  // re-resolved — T-0177's property, visible here.)
  assert.deepEqual(ops(calls), ["--show", "--remove"]);
  assert.deepEqual(calls[1]?.args, ["--remove", "T-0004", "--force"]);
});

test("a user who says no stops it, and mm never sees --remove", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok(SHOW_ENVELOPE),
  });
  const { ctx } = uiCtx(false);

  const result = await mmRemove.execute("id", { id: "T-0004", confirmed: true }, undefined, undefined, ctx);
  // Declining is not an error — the user answered the question they were
  // asked, and the answer was no.
  assert.equal(result.isError, undefined);
  assert.match(textOf(result), /was not removed: the user declined/);
  assert.equal(result.details["declined"], true);
  assert.deepEqual(ops(calls), ["--show"], "nothing was removed");
});

test("confirmRemove:false skips the prompt but never the schema guard (§9)", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool(
    { "--status": ok(STATUS_ENVELOPE), "--show": ok(SHOW_ENVELOPE), "--remove": ok(REMOVE_ENVELOPE) },
    { confirmRemove: false },
  );
  const { ctx, asked } = uiCtx(false);

  const done = await mmRemove.execute("id", { id: "T-0004", confirmed: true }, undefined, undefined, ctx);
  assert.equal(asked.length, 0, "the prompt is off");
  assert.match(textOf(done), /removed T-0004/);

  // …and with the prompt off, an unconfirmed call is still refused: the
  // config switch governs the dialog, not the assertion.
  const { tool: strict, calls: strictCalls } = tool({}, { confirmRemove: false });
  const refused = await strict.execute("id", { id: "T-0004" }, undefined, undefined, ctx);
  assert.equal(refused.isError, true);
  assert.deepEqual(strictCalls, []);
  assert.ok(calls.length > 0);
});

test("headless: no prompt, and the explicit confirmed is the whole guard (§8.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok(SHOW_ENVELOPE),
    "--remove": ok(REMOVE_ENVELOPE),
  });

  // No ctx at all (print/JSON mode), and a ctx that says it has no UI: both
  // mean there is nobody to ask, and inventing an answer would be worse.
  for (const ctx of [undefined, { hasUI: false } as ToolContext]) {
    const result = await mmRemove.execute("id", { id: "T-0004", confirmed: true }, undefined, undefined, ctx);
    assert.equal(result.isError, undefined);
    assert.match(textOf(result), /removed T-0004/);
  }
  assert.equal(calls.filter((c) => c.args[0] === "--remove").length, 2);
});

/* ------------------------------------------------------------- guard 3 */

test("--force is passed only on the far side of both plugin guards", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok(SHOW_ENVELOPE),
    "--remove": ok(REMOVE_ENVELOPE),
  });
  await mmRemove.execute("id", { id: "T-0004", confirmed: true });
  const removeCall = calls.find((c) => c.args[0] === "--remove");
  assert.deepEqual(removeCall?.args, ["--remove", "T-0004", "--force"]);
  // §4.2 gives this tool id and confirmed only — no --with-detail, which is a
  // switch the spec does not name for it.
  assert.ok(!removeCall?.args.includes("--with-detail"));
});

test("the orphaned detail file mm warns about is surfaced", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok(SHOW_ENVELOPE),
    "--remove": ok(REMOVE_ENVELOPE),
  });
  const result = await mmRemove.execute("id", { id: "T-0004", confirmed: true });
  // mm reports it; the tool carries it rather than swallowing it, because the
  // board is now failing I9 and the user is the one who can decide about it.
  assert.match(textOf(result), /warning: .*was left with no item/);
});

/* -------------------------------------------------------------- gating */

test("an unknown item fails at the read, before anything is removed", async (t) => {
  t.after(clearPin);
  pinned();
  const { tool: mmRemove, calls } = tool({
    "--status": ok(STATUS_ENVELOPE),
    "--show": { ok: false, failure: { kind: "not-found", exit: 3, message: "T-9999 not found", errors: [] } } as never,
  });
  const result = await mmRemove.execute("id", { id: "T-9999", confirmed: true });
  assert.equal(result.isError, true);
  assert.match(textOf(result), /not found/);
  assert.deepEqual(ops(calls), ["--show"]);
});

test("without mm, it refuses like everything else (§2.1)", async () => {
  const runner = fakeRun({});
  const [mmRemove] = removeTools({
    health: async () => ({ ok: false, message: "no `mm` on PATH — build it with go build…" }),
    run: runner.run,
  });
  const result = await mmRemove!.execute("id", { id: "T-0004", confirmed: true });
  assert.equal(result.isError, true);
  assert.match(textOf(result), /no `mm` on PATH/);
  assert.equal(runner.calls.length, 0);
});
