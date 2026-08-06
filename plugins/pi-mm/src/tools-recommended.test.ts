/*
  The recommended tools (spec-pi-mm-plugin.md §4.2, second table).

  The envelopes below were captured from the real `mm` while this was written,
  so what the formatters are asserted against is what the CLI actually emits.
  Four behaviours carry a rule rather than a preference, and each has a test of
  its own:

    - mm_block refuses without a reason, because I5 requires one.
    - mm_archive's I1/I2 coverage warning survives VERBATIM (§4.2).
    - mm_tick and mm_archive can preview, and a preview says so — a model must
      not read "would archive 2 items" as two items archived.
    - Nothing here runs on its own: the tools are functions, and the assertion
      that no event handler calls them lives in conformance.test.ts (§8.3).
*/

import test from "node:test";
import assert from "node:assert/strict";

import { PIN_DETAILS_KEY, clearPin, setPin } from "./board.ts";
import { RECOMMENDED_OPS, recommendedTools } from "./tools-recommended.ts";
import type { ToolDefinition, ToolDeps } from "./tools-read.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";

const BOARD = "/boards/rb";

function fakeRun(table: Record<string, RunOutcome>) {
  const calls: RunOptions[] = [];
  const run = async (opts: RunOptions): Promise<RunOutcome> => {
    calls.push(opts);
    const op = opts.args[0] ?? "";
    const answer = table[op];
    if (!answer) throw new Error(`test has no answer for ${op}`);
    return answer;
  };
  return { run: run as unknown as ToolDeps["run"], calls };
}

const ok = (envelope: unknown): RunOutcome => ({ ok: true, envelope: envelope as never });

const STATUS = ok({
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Runner Board", wipLimit: 1, wipUsed: 0 },
  result: { counts: {}, wip: { limit: 1, used: 0 }, slots: [], next: null },
  changes: [],
  warnings: [],
  errors: [],
});

function tools(table: Record<string, RunOutcome>, extra: Partial<ToolDeps> = {}) {
  const runner = fakeRun({ "--status": STATUS, ...table });
  const deps: ToolDeps = {
    health: async () => ({ ok: true, version: "mm 0.1.0" }),
    run: runner.run,
    ...extra,
  };
  const byName = new Map<string, ToolDefinition>();
  for (const tool of recommendedTools(deps)) byName.set(tool.name, tool);
  return { byName, calls: runner.calls };
}

function pinned() {
  setPin({ path: BOARD, project: "Runner Board", source: "pin" });
}

const textOf = (result: { content: ReadonlyArray<{ text: string }> }) => result.content[0]?.text ?? "";

/** The argv one call reached `mm` with, minus the resolution that preceded it. */
const argvOf = (calls: readonly RunOptions[]) => calls[calls.length - 1]?.args ?? [];

/* ---------------------------------------------------------------- shape */

test("every recommended tool names the operation it needs (§4.2)", () => {
  const { byName } = tools({});
  assert.deepEqual(
    [...byName.keys()],
    ["mm_block", "mm_unblock", "mm_search", "mm_report", "mm_tick", "mm_archive"],
  );
  for (const tool of byName.values()) {
    // Without an entry, index.ts registers nothing — the gate of §4.2 is
    // keyed by this map, so a tool missing from it is a tool nobody sees.
    assert.ok(RECOMMENDED_OPS[tool.name], `${tool.name} has no operation to gate on`);
    assert.ok(tool.description.length > 20, `${tool.name} needs a description the model can use`);
    assert.ok(tool.promptGuidelines?.length, `${tool.name} carries its §6 guidelines`);
  }
});

test("without mm, every recommended tool returns the remedy and runs nothing (§2.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const runner = fakeRun({});
  const deps: ToolDeps = {
    health: async () => ({ ok: false, message: "no `mm` on PATH — build it with go build…" }),
    run: runner.run,
  };
  for (const tool of recommendedTools(deps)) {
    const result = await tool.execute("id", { id: "T-0001", reason: "x", query: "x" });
    assert.equal(result.isError, true, `${tool.name} must fail loudly`);
    assert.match(textOf(result), /no `mm` on PATH/);
  }
  assert.equal(runner.calls.length, 0, "no subprocess is spawned when mm is absent");
});

/* ------------------------------------------------------- block / unblock */

const BLOCKED = ok({
  ok: true,
  operation: "block",
  directory: { path: BOARD, project: "Runner Board" },
  result: {
    id: "T-0002",
    title: "second thing",
    state: "backlog",
    section: "Blocked",
    position: 1,
    tags: ["ci"],
    blocked: "waiting on vendor",
  },
  changes: [{ kind: "moved", id: "T-0002", file: "backlog.md" }],
  warnings: [],
  errors: [],
});

test("mm_block carries the reason, and says where the item landed", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--block": BLOCKED });
  const result = await byName.get("mm_block")!.execute("id", {
    id: "T-0002",
    reason: "waiting on vendor",
  });

  assert.equal(result.isError, undefined);
  assert.deepEqual(argvOf(calls), ["--block", "T-0002", "--reason", "waiting on vendor"]);
  assert.match(textOf(result), /blocked T-0002 {2}second thing/);
  assert.match(textOf(result), /blocked: waiting on vendor/);
  assert.deepEqual(result.details[PIN_DETAILS_KEY], {
    path: BOARD,
    project: "Runner Board",
    source: "pin",
  });
});

test("mm_block without a reason refuses before spawning (I5)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({});
  const result = await byName.get("mm_block")!.execute("id", { id: "T-0002" });
  assert.equal(result.isError, true);
  assert.match(textOf(result), /reason/);
  assert.match(textOf(result), /I5/, "the message is about the invariant, not about a switch");
  assert.equal(calls.length, 0);
});

test("mm_unblock returns to the top of Ready unless told otherwise", async (t) => {
  t.after(clearPin);
  pinned();
  const unblocked = ok({
    ok: true,
    operation: "unblock",
    directory: { path: BOARD },
    result: { id: "T-0002", title: "second thing", state: "backlog", section: "Ready", position: 1 },
    changes: [],
    warnings: [],
    errors: [],
  });

  const plain = tools({ "--unblock": unblocked });
  await plain.byName.get("mm_unblock")!.execute("id", { id: "T-0002" });
  assert.deepEqual(argvOf(plain.calls), ["--unblock", "T-0002"], "no --top: that is mm's default");

  const atEnd = tools({ "--unblock": unblocked });
  const result = await atEnd.byName.get("mm_unblock")!.execute("id", { id: "T-0002", end: true });
  assert.deepEqual(argvOf(atEnd.calls), ["--unblock", "T-0002", "--end"]);
  assert.match(textOf(result), /unblocked T-0002/);
});

/* ---------------------------------------------------------------- search */

test("mm_search reports state and location for every hit (§5.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const hits = ok({
    ok: true,
    operation: "search",
    directory: { path: BOARD },
    result: [
      {
        at: { file: "backlog.md", line: 17 },
        field: "title",
        item: {
          id: "T-0001",
          title: "first thing",
          state: "backlog",
          section: "Ready",
          position: 1,
          prio: "high",
          tags: ["infra"],
        },
        text: "first thing",
      },
      {
        at: { file: "done.md", line: 14 },
        field: "detail",
        item: { id: "T-0004", title: "old thing", state: "done", outcome: "shipped", done: "2026-05-04" },
        text: "…the deploy script…",
      },
    ],
    changes: [],
    warnings: [],
    errors: [],
  });

  const { byName, calls } = tools({ "--search": hits });
  const result = await byName.get("mm_search")!.execute("id", {
    query: "thing",
    fields: ["title", "detail"],
    state: "backlog",
    regex: true,
    limit: 5,
  });

  assert.deepEqual(argvOf(calls), [
    "--search",
    "thing",
    "--field",
    "title",
    "--field",
    "detail",
    "--state",
    "backlog",
    "--regex",
    "--limit",
    "5",
  ]);
  const body = textOf(result);
  assert.match(body, /2 hits:/);
  // State per hit: one is in Ready, the other is closed — a hit whose state is
  // invisible is one you have to call mm_show about before you can use it.
  assert.match(body, /T-0001 {2}first thing.*ready #1, title match, backlog\.md:17/);
  assert.match(body, /T-0004 {2}old thing.*done \(shipped, 2026-05-04\), detail match, done\.md:14/);
});

test("mm_search caps the hits it will ask for, and refuses an empty query", async (t) => {
  t.after(clearPin);
  pinned();
  const empty = ok({ ok: true, operation: "search", directory: { path: BOARD }, result: [], changes: [], warnings: [], errors: [] });

  const capped = tools({ "--search": empty });
  const result = await capped.byName.get("mm_search")!.execute("id", { query: "x", limit: 9_000 });
  assert.deepEqual(argvOf(capped.calls).slice(-2), ["--limit", "200"]);
  assert.equal(textOf(result), "no matches.", "no hits is a result, not an error");

  const blank = tools({});
  const refused = await blank.byName.get("mm_search")!.execute("id", { query: "   " });
  assert.equal(refused.isError, true);
  assert.equal(blank.calls.length, 0);
});

/* ---------------------------------------------------------------- report */

test("mm_report states the period it resolved and where it came from (§5.1.11)", async (t) => {
  t.after(clearPin);
  pinned();
  const report = ok({
    ok: true,
    operation: "report",
    directory: { path: BOARD },
    result: {
      project: "Runner Board",
      period: { label: "2026-W31", since: "2026-07-27", until: "2026-08-02", source: "default" },
      done: [
        { id: "T-0001", title: "first thing", state: "done", outcome: "shipped", prio: "high", tags: ["infra"] },
      ],
      groups: [
        {
          key: "shipped",
          items: [{ id: "T-0001", title: "first thing", state: "done", outcome: "shipped" }],
        },
      ],
      wip: [{ id: "T-0007", title: "in flight", state: "working", slot: 1 }],
      next: [{ id: "T-0009", title: "up next", state: "backlog", section: "Ready" }],
    },
    changes: [],
    warnings: [],
    errors: [],
  });

  const { byName, calls } = tools({ "--report": report });
  const result = await byName.get("mm_report")!.execute("id", {
    period: "last-week",
    group_by: "outcome",
    include_wip: true,
    include_backlog: true,
  });

  assert.deepEqual(argvOf(calls), [
    "--report",
    "--period",
    "last-week",
    "--group-by",
    "outcome",
    "--include-wip",
    "--include-backlog",
  ]);
  const body = textOf(result);
  // "A report whose period is invisible is a report you cannot check."
  assert.match(body, /2026-W31 \(2026-07-27\.\.2026-08-02\) — from default/);
  assert.match(body, /1 closed/);
  assert.match(body, /shipped:/);
  assert.match(body, /in progress:\n {2}T-0007/);
  assert.match(body, /next:\n {2}T-0009/);
});

/* ------------------------------------------------------------------ tick */

const TICKED = ok({
  ok: true,
  operation: "tick",
  directory: { path: BOARD },
  result: {
    fired: [
      { id: "T-0003", kind: "move", tickled: "2026-08-06" },
      { id: "T-0011", kind: "spawn", spawned: "T-0042", tickled: "2026-08-06" },
    ],
    errors: [{ id: "T-0013", error: "tickler: \"whenever\" is not a schedule" }],
  },
  changes: [{ kind: "moved", id: "T-0003", file: "backlog.md" }],
  warnings: [],
  errors: [],
});

test("mm_tick reports both kinds of fire, and what errored (§5.3.3)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--tick": TICKED });
  const result = await byName.get("mm_tick")!.execute("id", {});

  assert.deepEqual(argvOf(calls), ["--tick"], "no --dry-run unless it was asked for");
  const body = textOf(result);
  assert.match(body, /2 fired:/);
  // The two fires are different events: one MOVED, the other SPAWNED a new id.
  assert.match(body, /T-0003 {2}moved to ready \(tickled 2026-08-06\)/);
  assert.match(body, /T-0011 {2}spawned T-0042/);
  // "A failing item never aborts the run" — so the run succeeded AND has to
  // say the part that did not.
  assert.equal(result.isError, undefined);
  assert.match(body, /error: T-0013: tickler: "whenever" is not a schedule/);
});

test("mm_tick previews without pretending it ran", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--tick": TICKED });
  const result = await byName.get("mm_tick")!.execute("id", { dry_run: true });
  assert.deepEqual(argvOf(calls), ["--tick", "--dry-run"]);
  assert.match(textOf(result), /^dry run — nothing was written\n/);
});

test("a board with nothing due says so", async (t) => {
  t.after(clearPin);
  pinned();
  const quiet = ok({
    ok: true,
    operation: "tick",
    directory: { path: BOARD },
    result: { fired: [], errors: [] },
    changes: [],
    warnings: [],
    errors: [],
  });
  const { byName } = tools({ "--tick": quiet });
  const result = await byName.get("mm_tick")!.execute("id", {});
  assert.equal(textOf(result), "nothing was due.");
});

/* --------------------------------------------------------------- archive */

/** The Go build's own warnings, verbatim — this is the text §4.2 protects. */
const COVERAGE_WARNING =
  "2 archived items left the ID pool: an archive is outside the format spec and is not " +
  "validated, so I1 and I2 no longer see them (spec-file-format.md §10.5), and a report " +
  "over an archived period needs IncludeArchives to find them";

const ARCHIVED = ok({
  ok: true,
  operation: "archive",
  directory: { path: BOARD },
  result: {
    cutoff: "2026-08",
    months: ["2026-05"],
    items: 2,
    files: ["done-2026.md"],
    detailsMoved: [{ id: "T-0004", from: "details/T-0004.md", to: "details-2026/T-0004.md" }],
    detailOrphans: [],
  },
  changes: [],
  warnings: [COVERAGE_WARNING],
  errors: [],
});

test("mm_archive surfaces the I1/I2 coverage warning verbatim (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--archive": ARCHIVED });
  const result = await byName.get("mm_archive")!.execute("id", { before: "2026-08" });

  assert.deepEqual(argvOf(calls), ["--archive", "--before", "2026-08"]);
  const body = textOf(result);
  assert.match(body, /archived 2 items from 2026-05 into done-2026\.md \(cutoff 2026-08\)/);
  assert.match(body, /T-0004: details\/T-0004\.md → details-2026\/T-0004\.md/);
  // VERBATIM: the sentence saying part of the record has left the validated
  // set is mm's, and the plugin does not paraphrase it.
  assert.ok(
    body.includes(COVERAGE_WARNING),
    `the warning was altered on the way through:\n${body}`,
  );
});

test("mm_archive takes one cutoff, and previews", async (t) => {
  t.after(clearPin);
  pinned();
  const both = tools({});
  const refused = await both.byName.get("mm_archive")!.execute("id", { before: "2026-01", age: 30 });
  assert.equal(refused.isError, true);
  assert.match(textOf(refused), /two spellings of one cutoff/);
  assert.equal(both.calls.length, 0, "refused before spending a subprocess");

  const policy = tools({ "--archive": ARCHIVED });
  await policy.byName.get("mm_archive")!.execute("id", { age: 30, dry_run: true });
  assert.deepEqual(argvOf(policy.calls), ["--archive", "--age", "30", "--dry-run"]);
});

test("an archive with nothing to move is a result, not an error", async (t) => {
  t.after(clearPin);
  pinned();
  const empty = ok({
    ok: true,
    operation: "archive",
    directory: { path: BOARD },
    result: { cutoff: "2026-08", months: [], items: 0, files: [], detailsMoved: [], detailOrphans: [] },
    changes: [],
    warnings: [],
    errors: [],
  });
  const { byName } = tools({ "--archive": empty });
  const result = await byName.get("mm_archive")!.execute("id", {});
  assert.equal(result.isError, undefined);
  assert.equal(textOf(result), "nothing to archive (cutoff 2026-08).");
});
