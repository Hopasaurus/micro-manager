/*
  Per-turn context injection (spec-pi-mm-plugin.md §6).

  This block goes into the system prompt on every single turn, so what is
  asserted here is mostly what it must NOT contain and when it must NOT appear.
  A rule broken in this file is a rule broken thousands of times over.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { clearPin, setPin } from "./board.ts";
import { MAX_LINES, contextBlock, markBoardChanged, resetContext, turnContext } from "./context.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";
import { setContextEnabled, resetSettings } from "./settings.ts";

const BOARD = "/srv/boards/todos";
const PIN = { path: BOARD, project: "Sample One", source: "pin" as const };

const STATUS = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Sample One", wipLimit: 4, wipUsed: 1 },
  result: {
    counts: { ready: 3, blocked: 1, someday: 5, done: 87 },
    next: { id: "T-0169", title: "Spec: tickler fields and grammar", prio: "high" },
    slots: [
      { file: "working.01.md", occupied: true, item: { id: "T-0018", title: "Migrate the build cache", slot: 1 } },
      { file: "working.02.md", occupied: false, item: null },
    ],
    wip: { limit: 4, used: 1 },
  },
};

function fakeRun(outcomes: RunOutcome[]) {
  const calls: RunOptions[] = [];
  let i = 0;
  const run = async (opts: RunOptions): Promise<RunOutcome> => {
    calls.push(opts);
    return outcomes[Math.min(i++, outcomes.length - 1)] as RunOutcome;
  };
  return { run: run as never, calls };
}

const ok = (envelope: unknown): RunOutcome => ({ ok: true, envelope: envelope as never });
const healthy = async () => ({ ok: true as const, version: "mm 0.1.0" });

/* ---------------------------------------------------------------- shape */

test("the block is the one §6 shows", () => {
  const block = contextBlock(PIN, STATUS, 6);
  const lines = block.split("\n");
  assert.match(lines[0]!, /^\[mm\] \/srv\/boards\/todos — Sample One — wip 1\/4 · ready 3 · blocked 1 · someday 5$/);
  assert.match(lines[1]!, /^ {5}in work: {2}T-0018 {2}Migrate the build cache$/);
  assert.match(lines[2]!, /^ {5}next: {5}T-0169 {2}Spec: tickler fields and grammar {2}\(high\)$/);
  assert.equal(lines.length, 3);
});

test("it carries no done items and no commentary (§6)", () => {
  const block = contextBlock(PIN, STATUS, 8);
  // The done COUNT is not in the head either — what is finished is not what is
  // in flight, and the head is already the longest line.
  assert.doesNotMatch(block, /done/i);
  // No advice: behaviour belongs in the tools' promptGuidelines, where it is
  // attached to the tool it is about rather than re-read every turn.
  assert.doesNotMatch(block, /please|remember|should|use mm_/i);
});

test("an empty board still says what it is", () => {
  const block = contextBlock(PIN, {
    ok: true,
    directory: { path: BOARD, project: "Sample One" },
    result: { counts: {}, wip: { limit: 1, used: 0 }, slots: [] },
  });
  assert.match(block, /wip 0\/1 · ready 0 · blocked 0 · someday 0/);
  assert.match(block, /in work: {2}nothing/);
  assert.doesNotMatch(block, /next:/, "no next item is no line, not an empty one");
});

test("the board description rides along when it is set (§3.4)", () => {
  const block = contextBlock(
    PIN,
    { ...STATUS, directory: { ...STATUS.directory, description: "A small-file todo system." } },
    8,
  );
  assert.match(block, /A small-file todo system\./);
  // Still data, still counted: it is a line like any other.
  assert.ok(block.split("\n").length <= 8);
});

test("an overflowing board is truncated with an explicit … (§6)", () => {
  const busy = {
    ...STATUS,
    result: {
      ...STATUS.result,
      slots: Array.from({ length: 10 }, (_, n) => ({
        file: `working.0${n}.md`,
        occupied: true,
        item: { id: `T-00${n}`, title: `item ${n}`, slot: n + 1 },
      })),
    },
  };
  const block = contextBlock(PIN, busy, 6);
  const lines = block.split("\n");
  assert.equal(lines.length, 6, "the limit is obeyed");
  // Explicit, because the agent must be able to tell a short board from a
  // clipped one — otherwise it reasons about work it cannot see.
  assert.equal(lines.at(-1), "     …");
});

test("the ceiling is 8 lines whatever it is asked for (§6)", () => {
  const busy = {
    ...STATUS,
    result: {
      ...STATUS.result,
      slots: Array.from({ length: 20 }, (_, n) => ({
        occupied: true,
        item: { id: `T-01${n}`, title: `item ${n}` },
      })),
    },
  };
  assert.ok(contextBlock(PIN, busy, 99).split("\n").length <= MAX_LINES);
  assert.equal(MAX_LINES, 8);
});

/* --------------------------------------------------------------- when */

test("nothing is injected without a board (§6)", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  clearPin();
  resetContext();
  const runner = fakeRun([ok(STATUS)]);
  assert.equal(await turnContext({ health: healthy, run: runner.run }), undefined);
  // …and it did not go looking for one: resolving a board is a decision a turn
  // boundary must not make on the user's behalf.
  assert.equal(runner.calls.length, 0);
});

test("nothing is injected without mm (§6)", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  setPin(PIN);
  resetContext();
  const runner = fakeRun([ok(STATUS)]);
  const block = await turnContext({
    health: async () => ({ ok: false, message: "no `mm` on PATH" }),
    run: runner.run,
  });
  assert.equal(block, undefined);
  assert.equal(runner.calls.length, 0);
});

test("nothing is injected when the status read fails", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  setPin(PIN);
  resetContext();
  const runner = fakeRun([
    { ok: false, failure: { kind: "io", exit: 6, message: "boom", errors: [] } } as never,
  ]);
  // An empty injection beats a false one — including a stale one.
  assert.equal(await turnContext({ health: healthy, run: runner.run }), undefined);
});

test("an untrusted project is never injected (§6, §9)", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  setPin(PIN);
  resetContext();
  const runner = fakeRun([ok(STATUS)]);
  assert.equal(
    await turnContext({ health: healthy, run: runner.run, allowed: false }),
    undefined,
  );
  assert.equal(runner.calls.length, 0, "not even read");
});

test("/mm context off turns it off for the session (§5)", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
    resetSettings();
  });
  setPin(PIN);
  resetContext();
  resetSettings();

  setContextEnabled(false);
  assert.equal(await turnContext({ health: healthy, run: fakeRun([ok(STATUS)]).run }), undefined);

  setContextEnabled(true);
  assert.ok(await turnContext({ health: healthy, run: fakeRun([ok(STATUS)]).run }));
});

/* -------------------------------------------------------------- caching */

test("the block is cached between turns, and refreshed after a mutation (§6)", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  setPin(PIN);
  resetContext();
  const runner = fakeRun([ok(STATUS)]);
  const deps = { health: healthy, run: runner.run };

  await turnContext(deps);
  await turnContext(deps);
  assert.equal(runner.calls.length, 1, "a snapshot may be reused between turns");

  // mm says what it touched, so nothing here has to know which operations
  // mutate — this is the rule implemented from the one source that cannot be
  // wrong about it.
  markBoardChanged({ ok: true, changes: [{ kind: "created", id: "T-0004", file: "backlog.md" }] });
  await turnContext(deps);
  assert.equal(runner.calls.length, 2, "the block must never describe a board the plugin just changed");

  // A read reports no changes, so it does not invalidate anything.
  markBoardChanged({ ok: true, changes: [] });
  await turnContext(deps);
  assert.equal(runner.calls.length, 2);
});

test("a different board is never served from the old board's cache", async (t) => {
  t.after(() => {
    clearPin();
    resetContext();
  });
  setPin(PIN);
  resetContext();
  const other = { ...STATUS, directory: { path: "/boards/other", project: "Other" } };
  const runner = fakeRun([ok(STATUS), ok(other)]);
  const deps = { health: healthy, run: runner.run };

  const first = await turnContext(deps);
  assert.match(first ?? "", /Sample One/);

  setPin({ path: "/boards/other", project: "Other", source: "pin" });
  const second = await turnContext(deps);
  assert.match(second ?? "", /\/boards\/other/);
  assert.equal(runner.calls.length, 2);
});
