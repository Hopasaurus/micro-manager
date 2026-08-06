/*
  The workflow tools (spec-pi-mm-plugin.md §4.2, §4.3).

  The case that matters most is the one the spec singles out: at the WIP limit,
  the agent must be told what is IN the slots so it can act. That is a fact
  about what reaches the model, so it is asserted on the text.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { PIN_DETAILS_KEY, clearPin, setPin } from "./board.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";
import type { ToolDefinition, ToolDeps } from "./tools-read.ts";
import { workflowTools } from "./tools-workflow.ts";

const BOARD = "/boards/rb";

const STATUS_ENVELOPE = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Runner Board", wipLimit: 1, wipUsed: 1 },
  result: { counts: { ready: 1 }, wip: { limit: 1, used: 1 } },
};

/** mm --start, as the Go CLI answers it on success. */
const START_ENVELOPE = {
  ok: true,
  operation: "start",
  directory: { path: BOARD, project: "Runner Board" },
  result: {
    id: "T-0001",
    title: "alpha",
    state: "working",
    slot: 1,
    created: "2026-08-06",
    started: "2026-08-06",
  },
  changes: [{ kind: "moved", id: "T-0001", file: "working.01.md" }],
};

/**
 * mm --start at the limit. The message is the CLI's own, newlines and all:
 * §4.3 asks the plugin to surface the remedy the CLI names, and this is it.
 */
const WIP_MESSAGE =
  "wip limit reached (1/1)\n  slot 01  T-0001  one\nfinish one, pause one, or raise the limit with --wip";

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
const fail = (kind: string, exit: number, message: string, errors: unknown[] = []): RunOutcome =>
  ({ ok: false, failure: { kind, exit, message, errors } }) as never;

function tools(table: Record<string, RunOutcome>, extra: Partial<ToolDeps> = {}) {
  const runner = fakeRun(table);
  const deps: ToolDeps = {
    health: async () => ({ ok: true, version: "mm 0.1.0" }),
    run: runner.run,
    ...extra,
  };
  const byName = new Map<string, ToolDefinition>();
  for (const tool of workflowTools(deps)) byName.set(tool.name, tool);
  return { byName, calls: runner.calls };
}

const pinned = () => setPin({ path: BOARD, project: "Runner Board", source: "pin" });
const textOf = (r: { content: ReadonlyArray<{ text: string }> }) => r.content[0]?.text ?? "";
const opArgs = (calls: RunOptions[]) => calls.find((c) => c.args[0] !== "--status")?.args ?? [];

test("the workflow set is registered, and nothing else", () => {
  const { byName } = tools({});
  assert.deepEqual([...byName.keys()].sort(), ["mm_finish", "mm_pause", "mm_start"]);
});

test("without mm, nothing is spawned and every tool says why (§2.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const runner = fakeRun({});
  const deps: ToolDeps = {
    health: async () => ({ ok: false, message: "no `mm` on PATH — build it with go build…" }),
    run: runner.run,
  };
  for (const tool of workflowTools(deps)) {
    const result = await tool.execute("id", { id: "T-0001" });
    assert.equal(result.isError, true);
    assert.match(textOf(result), /no `mm` on PATH/);
  }
  assert.equal(runner.calls.length, 0);
});

/* --------------------------------------------------------------- start */

test("mm_start reports the slot the item landed in", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE), "--start": ok(START_ENVELOPE) });

  const result = await byName.get("mm_start")!.execute("id", { id: "T-0001" });
  assert.deepEqual(opArgs(calls), ["--start", "T-0001"]);
  assert.match(textOf(result), /^started T-0001 {2}alpha → slot 01/);
  assert.equal((result.details[PIN_DETAILS_KEY] as { path?: string }).path, BOARD);
});

test("mm_start passes a named slot", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE), "--start": ok(START_ENVELOPE) });
  await byName.get("mm_start")!.execute("id", { id: "T-0001", slot: 2 });
  assert.deepEqual(opArgs(calls), ["--start", "T-0001", "--slot", "2"]);
});

test("at the WIP limit, the agent is told what is IN the slots (§4.3)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--start": fail("precondition", 4, WIP_MESSAGE, [
      { code: "WipLimitReached", message: WIP_MESSAGE, id: "T-0002" },
    ]),
  });

  const result = await byName.get("mm_start")!.execute("id", { id: "T-0002" });
  assert.equal(result.isError, true);
  // The remedy is the slot listing. Losing it would leave the agent with a
  // limit and no way to act on it, which is exactly what §4.3 forbids.
  assert.match(textOf(result), /slot 01 {2}T-0001 {2}one/);
  assert.match(textOf(result), /finish one, pause one, or raise the limit/);
  const error = result.details["error"] as { exit?: number; errors?: Array<{ code?: string }> };
  assert.equal(error.exit, 4);
  assert.equal(error.errors?.[0]?.code, "WipLimitReached");
  // One attempt. §8.4: a failed mutation is never retried automatically.
  assert.equal(calls.filter((c) => c.args[0] === "--start").length, 1);
});

/* --------------------------------------------------------------- pause */

test("mm_pause returns the item and can name where", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--pause": ok({
      ok: true,
      operation: "pause",
      directory: { path: BOARD },
      result: { id: "T-0001", title: "alpha", state: "backlog", section: "Someday", position: 3 },
      changes: [{ kind: "moved", id: "T-0001", file: "backlog.md" }],
    }),
  });

  const result = await byName.get("mm_pause")!.execute("id", {
    id: "T-0001",
    section: "someday",
    end: true,
  });
  assert.deepEqual(opArgs(calls), ["--pause", "T-0001", "--section", "someday", "--end"]);
  assert.match(textOf(result), /^paused T-0001 {2}alpha → someday #3/);
});

test("mm_pause into blocked carries the reason", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--pause": ok({
      ok: true,
      operation: "pause",
      directory: { path: BOARD },
      result: { id: "T-0001", title: "alpha", state: "backlog", section: "Blocked", blocked: "waiting on review" },
    }),
  });
  await byName.get("mm_pause")!.execute("id", {
    id: "T-0001",
    section: "blocked",
    blocked: "waiting on review",
  });
  assert.deepEqual(opArgs(calls), [
    "--pause",
    "T-0001",
    "--section",
    "blocked",
    "--blocked",
    "waiting on review",
  ]);
});

/* -------------------------------------------------------------- finish */

test("mm_finish carries the outcome and the closing note (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--finish": ok({
      ok: true,
      operation: "finish",
      directory: { path: BOARD },
      result: {
        id: "T-0001",
        title: "alpha",
        state: "done",
        outcome: "shipped",
        done: "2026-08-06",
        detail: "details/T-0001.md",
      },
      changes: [
        { kind: "created", id: "T-0001", file: "details/T-0001.md" },
        { kind: "moved", id: "T-0001", file: "done.md" },
      ],
    }),
  });

  const result = await byName.get("mm_finish")!.execute("id", {
    id: "T-0001",
    outcome: "shipped",
    closing_note: "done and dusted",
  });
  // --closing-note, not --note: --note is the OPERATION that adds one, and
  // passing it here would be a second operation rather than a modifier.
  assert.deepEqual(opArgs(calls), [
    "--finish",
    "T-0001",
    "--outcome",
    "shipped",
    "--closing-note",
    "done and dusted",
  ]);
  assert.match(textOf(result), /^finished T-0001 {2}alpha → done\.md \(shipped\)/);
  assert.match(textOf(result), /details\/T-0001\.md/, "the closing note's home is named");
});

test("mm_finish defaults its outcome to mm's own", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--finish": ok({
      ok: true,
      operation: "finish",
      directory: { path: BOARD },
      result: { id: "T-0001", title: "alpha", state: "done", outcome: "shipped" },
    }),
  });
  await byName.get("mm_finish")!.execute("id", { id: "T-0001" });
  // No --outcome: the default is the CLI's (shipped), and repeating it here
  // would be a second place to change if it ever moved.
  assert.deepEqual(opArgs(calls), ["--finish", "T-0001"]);
});

test("each workflow tool refuses an empty id before spawning", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });
  for (const name of ["mm_start", "mm_pause", "mm_finish"]) {
    const result = await byName.get(name)!.execute("id", { id: "  " });
    assert.equal(result.isError, true, `${name} should refuse`);
  }
  assert.equal(calls.length, 0);
});

test("a wrong-state transition is reported as it came (§4.3)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--pause": fail("precondition", 4, "T-0002 is not in a working slot"),
  });
  const result = await byName.get("mm_pause")!.execute("id", { id: "T-0002" });
  assert.equal(result.isError, true);
  assert.match(textOf(result), /not in a working slot/);
});
