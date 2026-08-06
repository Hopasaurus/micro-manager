/*
  The write tools (spec-pi-mm-plugin.md §4.2, §3.3, §3.4).

  A write tool is judged on three things and these test all three: the argv it
  builds (which is what actually reaches the board), what it refuses before
  spending a subprocess, and what it says afterwards. The formatted text is
  asserted only where the spec fixes it — mm_add reports the assigned ID,
  mm_init says the board is pinned.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { PIN_DETAILS_KEY, clearPin, pinnedBoard, setPin } from "./board.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";
import type { ToolDefinition, ToolDeps } from "./tools-read.ts";
import { writeTools } from "./tools-write.ts";

const BOARD = "/boards/rb";

const STATUS_ENVELOPE = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Runner Board", wipLimit: 1, wipUsed: 0 },
  result: { counts: { ready: 1 }, wip: { limit: 1, used: 0 }, path: BOARD, project: "Runner Board" },
};

/** mm --add, as the Go CLI really answers it. */
const ADD_ENVELOPE = {
  ok: true,
  operation: "add",
  directory: { path: BOARD, project: "Runner Board" },
  result: {
    id: "T-0004",
    title: "probe add",
    state: "backlog",
    section: "Ready",
    position: 3,
    prio: "high",
    tags: ["infra"],
    created: "2026-08-06",
  },
  changes: [{ kind: "created", id: "T-0004", file: "backlog.md" }],
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
const fail = (kind: string, exit: number, message: string): RunOutcome =>
  ({ ok: false, failure: { kind, exit, message, errors: [] } }) as never;

function tools(table: Record<string, RunOutcome>, extra: Partial<ToolDeps> = {}) {
  const runner = fakeRun(table);
  const deps: ToolDeps = {
    health: async () => ({ ok: true, version: "mm 0.1.0" }),
    run: runner.run,
    ...extra,
  };
  const byName = new Map<string, ToolDefinition>();
  for (const tool of writeTools(deps)) byName.set(tool.name, tool);
  return { byName, calls: runner.calls };
}

const pinned = () => setPin({ path: BOARD, project: "Runner Board", source: "pin" });
const textOf = (r: { content: ReadonlyArray<{ text: string }> }) => r.content[0]?.text ?? "";
/** The argv of the operation, skipping the --status resolution call. */
const opArgs = (calls: RunOptions[]) => calls.find((c) => c.args[0] !== "--status")?.args ?? [];

/* ---------------------------------------------------------------- shape */

test("the required write set is registered", () => {
  const { byName } = tools({});
  for (const name of ["mm_add", "mm_edit", "mm_move", "mm_note", "mm_init", "mm_describe"]) {
    assert.ok(byName.has(name), `${name} is required by §4.2`);
  }
  assert.equal(byName.size, 6, "mm_start/pause/finish are T-0180, mm_remove is T-0181");
});

test("without mm, nothing is spawned and every tool says why (§2.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const runner = fakeRun({});
  const deps: ToolDeps = {
    health: async () => ({ ok: false, message: "no `mm` on PATH — build it with go build…" }),
    run: runner.run,
  };
  for (const tool of writeTools(deps)) {
    const result = await tool.execute("id", { id: "T-0001", title: "x", text: "x", project: "p" });
    assert.equal(result.isError, true, `${tool.name} must refuse`);
    assert.match(textOf(result), /no `mm` on PATH/);
  }
  assert.equal(runner.calls.length, 0);
});

/* ---------------------------------------------------------------- add */

test("mm_add builds the argv and leads with the assigned ID (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE), "--add": ok(ADD_ENVELOPE) });

  const result = await byName.get("mm_add")!.execute("id", {
    title: "probe add",
    section: "someday",
    prio: "high",
    tags: ["infra", "ci"],
    top: true,
    detail_text: "the long form",
  });
  assert.deepEqual(opArgs(calls), [
    "--add",
    "probe add",
    "--section",
    "someday",
    "--prio",
    "high",
    "--tag",
    "infra",
    "--tag",
    "ci",
    "--top",
    "--detail-text",
    "the long form",
  ]);
  // The ID is the handle for everything after, so it leads.
  assert.match(textOf(result), /^added T-0004 {2}probe add/);
  assert.match(textOf(result), /backlog\.md/, "the write says what it touched");
  assert.equal((result.details[PIN_DETAILS_KEY] as { path?: string }).path, BOARD);
});

test("mm_add without a title refuses before spawning", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });
  const result = await byName.get("mm_add")!.execute("id", { title: "   " });
  assert.equal(result.isError, true);
  assert.equal(calls.length, 0);
});

/* --------------------------------------------------------------- edit */

test("mm_edit reaches unregistered keys through --set (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--edit": ok({ ok: true, operation: "edit", directory: { path: BOARD }, result: { id: "T-0004", title: "renamed" } }),
  });

  await byName.get("mm_edit")!.execute("id", {
    id: "T-0004",
    title: "renamed",
    prio: "low",
    tags: ["ci"],
    untags: ["infra"],
    set: ["owner=dana", "sprint=12"],
    unset: ["blocked"],
  });
  assert.deepEqual(opArgs(calls), [
    "--edit",
    "T-0004",
    "--title",
    "renamed",
    "--prio",
    "low",
    "--tag",
    "ci",
    "--untag",
    "infra",
    "--set",
    "owner=dana",
    "--set",
    "sprint=12",
    "--unset",
    "blocked",
  ]);
});

test("mm_edit refuses a --set that is not KEY=VALUE, and an empty edit", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });

  const bad = await byName.get("mm_edit")!.execute("id", { id: "T-0004", set: ["owner"] });
  assert.equal(bad.isError, true);
  assert.match(textOf(bad), /KEY=VALUE/);

  const nothing = await byName.get("mm_edit")!.execute("id", { id: "T-0004" });
  assert.equal(nothing.isError, true);
  assert.match(textOf(nothing), /needs something to change/);
  assert.equal(calls.length, 0, "neither reached mm");
});

/* --------------------------------------------------------------- move */

test("mm_move takes exactly one destination", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--move": ok({ ok: true, operation: "move", directory: { path: BOARD }, result: { id: "T-0004", title: "probe add", position: 1 } }),
  });

  const two = await byName.get("mm_move")!.execute("id", { id: "T-0004", top: true, position: 3 });
  assert.equal(two.isError, true);
  assert.match(textOf(two), /one destination; got position and top/);
  assert.equal(calls.length, 0);

  const none = await byName.get("mm_move")!.execute("id", { id: "T-0004" });
  assert.equal(none.isError, true);
  assert.match(textOf(none), /needs a destination or a section/);

  await byName.get("mm_move")!.execute("id", { id: "T-0004", section: "someday", top: true });
  assert.deepEqual(opArgs(calls), ["--move", "T-0004", "--section", "someday", "--top"]);
});

/* --------------------------------------------------------------- note */

test("mm_note appends one dated entry and says where it landed", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--note": ok({
      ok: true,
      operation: "note",
      directory: { path: BOARD },
      result: { id: "T-0004", title: "probe add", detail: "details/T-0004.md" },
      changes: [{ kind: "created", id: "T-0004", file: "details/T-0004.md" }],
    }),
  });

  const result = await byName.get("mm_note")!.execute("id", { id: "T-0004", text: "a dated note" });
  assert.deepEqual(opArgs(calls), ["--note", "T-0004", "a dated note"]);
  assert.match(textOf(result), /noted on T-0004/);

  const empty = await byName.get("mm_note")!.execute("id", { id: "T-0004", text: "  " });
  assert.equal(empty.isError, true, "a note with no text is a refusal, not an empty entry");
});

/* --------------------------------------------------------------- init */

test("mm_init creates a board and pins it for the session (§3.3)", async (t) => {
  t.after(clearPin);
  clearPin();
  const { byName, calls } = tools({
    "--init": ok({
      ok: true,
      operation: "init",
      // The one envelope that reports its board in `result`: there is no
      // directory to describe until this call has made one.
      result: { path: "/boards/garden", project: "gardening" },
      changes: [{ kind: "created", file: "backlog.md" }],
    }),
  });

  const result = await byName.get("mm_init")!.execute("id", {
    project: "gardening",
    dir: "/boards/garden",
    slots: 2,
    slot_width: 3,
    prefix: "G",
  });
  assert.deepEqual(calls[0]?.args, [
    "--init",
    "--project",
    "gardening",
    "--slots",
    "2",
    "--slot-width",
    "3",
    "--prefix",
    "G",
  ]);
  assert.equal(calls[0]?.dir, "/boards/garden", "--dir names where the new board goes");
  assert.match(textOf(result), /created gardening at \/boards\/garden/);
  assert.match(textOf(result), /ids are G-0001/);
  assert.match(textOf(result), /pinned for this session/);
  assert.equal(pinnedBoard()?.path, "/boards/garden");
  assert.equal((result.details[PIN_DETAILS_KEY] as { path?: string }).path, "/boards/garden");
});

test("mm_init validates the prefix against the format's grammar before running (§3.3)", async (t) => {
  t.after(clearPin);
  clearPin();
  const { byName, calls } = tools({});
  for (const prefix of ["g", "TOOLONG", "T1", "T-", "1"]) {
    const result = await byName.get("mm_init")!.execute("id", { project: "x", prefix });
    assert.equal(result.isError, true, `${prefix} should be refused`);
    // The message is about the GRAMMAR rather than about a switch: a bad
    // prefix is the user's mistake, not the build's age.
    assert.match(textOf(result), /one to four uppercase letters/);
  }
  assert.equal(calls.length, 0, "a bad prefix costs no subprocess");
});

test("mm_init needs no board, and does not resolve one first", async (t) => {
  t.after(clearPin);
  clearPin();
  const { byName, calls } = tools({
    "--init": ok({ ok: true, operation: "init", result: { path: "/boards/new", project: "New" } }),
  });
  await byName.get("mm_init")!.execute("id", { project: "New" });
  // It is what you call when there is no board; resolving one first would be
  // asking a question whose answer it is about to change.
  assert.deepEqual(
    calls.map((c) => c.args[0]),
    ["--init"],
  );
});

/* ----------------------------------------------------------- describe */

test("mm_describe goes through mm, and degrades on a build without it (§3.4, §4.3)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--describe": ok({
      ok: true,
      operation: "describe",
      directory: { path: BOARD },
      changes: [{ kind: "updated", file: "structure.md" }],
    }),
  });
  const result = await byName.get("mm_describe")!.execute("id", { text: "A small-file todo system." });
  assert.deepEqual(opArgs(calls), ["--describe", "A small-file todo system."]);
  assert.match(textOf(result), /description set/);

  // A minimally conforming build has no --describe. The plugin does not sniff
  // for the switch: it runs it and lets exit 2 say so — and it must NEVER fall
  // back to writing structure.md itself (§3.4, §8.1).
  const older = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--describe": fail(
      "usage",
      2,
      "unknown switch --describe\nThis operation is not available in the installed mm build.",
    ),
  });
  const degraded = await older.byName.get("mm_describe")!.execute("id", { text: "x" });
  assert.equal(degraded.isError, true);
  assert.match(textOf(degraded), /not available in the installed mm build/);
});

test("mm_describe with no text refuses", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });
  const result = await byName.get("mm_describe")!.execute("id", { text: "" });
  assert.equal(result.isError, true);
  assert.equal(calls.length, 0);
});

/* ------------------------------------------------------------ failures */

test("a failed mutation is reported as failed, with the remedy (§4.3)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--add": fail("invariant", 1, "backlog.md:11: T-0001 is already defined\nRun mm_check to see every finding."),
  });
  const result = await byName.get("mm_add")!.execute("id", { title: "x" });
  assert.equal(result.isError, true, "a partial success is the one state a transaction may not produce");
  assert.match(textOf(result), /mm_check/);
});
