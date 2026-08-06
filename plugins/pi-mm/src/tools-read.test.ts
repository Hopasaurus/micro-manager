/*
  The required read tools (spec-pi-mm-plugin.md §4.2, Appendix A).

  What each test checks is the CONTRACT rather than the wording: the argv that
  reached `mm`, whether the result is an error, and that `details` carries the
  envelope and the pin. Where the spec fixes a behaviour in words — on-disk
  order, "nothing ready" is a result, no boards is a result, violations are
  results — that is asserted directly, because those are the four places a
  plausible implementation goes wrong.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { PIN_DETAILS_KEY, clearPin, setPin } from "./board.ts";
import { readTools, type ToolDefinition, type ToolDeps } from "./tools-read.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";

/* --------------------------------------------------------------- doubles */

const BOARD = "/boards/rb";

/** Envelopes as the Go mm really writes them (captured while building this). */
const STATUS_ENVELOPE = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, projectId: "755a218e39da", project: "Runner Board", wipLimit: 1, wipUsed: 1 },
  result: {
    counts: { blocked: 0, done: 2, ready: 1, someday: 3 },
    next: { id: "T-0002", title: "two", state: "backlog", section: "Ready", position: 1 },
    oldestReady: { id: "T-0002", title: "two", state: "backlog", section: "Ready", position: 1 },
    slots: [
      {
        file: "working.01.md",
        occupied: true,
        item: { id: "T-0001", title: "one", state: "working", slot: 1 },
      },
    ],
    wip: { limit: 1, used: 1 },
    path: BOARD,
    project: "Runner Board",
  },
  changes: [],
  warnings: [],
  errors: [],
};

/**
 * A fake runner that records every call and answers from a table keyed by the
 * operation switch. Anything unlisted is a bug in the test, not a fallback.
 */
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
const fail = (
  kind: string,
  exit: number,
  message: string,
  envelope?: unknown,
): RunOutcome =>
  ({
    ok: false,
    failure: { kind, exit, message, errors: [], ...(envelope ? { envelope } : {}) },
  }) as never;

function tools(table: Record<string, RunOutcome>, extra: Partial<ToolDeps> = {}) {
  const runner = fakeRun(table);
  const deps: ToolDeps = {
    health: async () => ({ ok: true, version: "mm 0.1.0" }),
    run: runner.run,
    ...extra,
  };
  const byName = new Map<string, ToolDefinition>();
  for (const tool of readTools(deps)) byName.set(tool.name, tool);
  return { byName, calls: runner.calls };
}

function pinned() {
  setPin({ path: BOARD, project: "Runner Board", source: "pin" });
}

const textOf = (result: { content: ReadonlyArray<{ text: string }> }) => result.content[0]?.text ?? "";

/* ---------------------------------------------------------------- shape */

test("the required read set is registered, and only reads", () => {
  const { byName } = tools({});
  for (const name of ["mm_status", "mm_next", "mm_list", "mm_show", "mm_board", "mm_check", "mm_find"]) {
    assert.ok(byName.has(name), `${name} is required by §4.2`);
  }
  assert.equal(byName.size, 7, "the write tools are T-0179 onward");
  for (const tool of byName.values()) {
    assert.ok(tool.description.length > 20, `${tool.name} needs a description the model can use`);
    assert.ok(tool.promptGuidelines?.length, `${tool.name} carries its §6 guidelines`);
  }
});

/* -------------------------------------------------------------- gating */

test("without mm, every tool returns the remedy and runs nothing (§2.1, §4.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const runner = fakeRun({});
  const deps: ToolDeps = {
    health: async () => ({ ok: false, message: "no `mm` on PATH — build it with go build…" }),
    run: runner.run,
  };
  for (const tool of readTools(deps)) {
    const result = await tool.execute("id", tool.name === "mm_show" ? { id: "T-0001" } : {});
    assert.equal(result.isError, true, `${tool.name} must fail loudly`);
    assert.match(textOf(result), /no `mm` on PATH/);
  }
  assert.equal(runner.calls.length, 0, "no subprocess is spawned when mm is absent");
});

/* -------------------------------------------------------------- reading */

test("mm_status shows identity and the one-screen summary", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });
  const result = await byName.get("mm_status")!.execute("id", {});

  assert.equal(result.isError, undefined);
  const body = textOf(result);
  assert.match(body, /Runner Board — \/boards\/rb/);
  assert.match(body, /wip 1\/1 · ready 1 · blocked 0 · someday 3 · done 2/);
  assert.match(body, /in work: {2}T-0001 {2}one/);
  assert.match(body, /next: {5}T-0002 {2}two/);
  // §4.1: details carries the parsed envelope; §7: and the pin.
  assert.equal((result.details["envelope"] as { operation?: string }).operation, "status");
  assert.equal((result.details[PIN_DETAILS_KEY] as { path?: string }).path, BOARD);
  assert.deepEqual(calls[0]?.args, ["--status"]);
  assert.equal(calls[0]?.dir, BOARD, "every call passes --dir once pinned (§4.1)");
});

test("mm_next reports an empty Ready as a result, not an error (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const empty = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--next": fail("not-found", 3, "## Ready is empty"),
  });
  const result = await empty.byName.get("mm_next")!.execute("id", {});
  assert.equal(result.isError, undefined, "an empty backlog is not a failure");
  assert.match(textOf(result), /nothing ready/);
  assert.equal(result.details["empty"], true);

  const full = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--next": ok({ ok: true, operation: "next", directory: { path: BOARD }, result: { id: "T-0002", title: "two", prio: "high" } }),
  });
  const item = await full.byName.get("mm_next")!.execute("id", {});
  assert.match(textOf(item), /T-0002 {2}two {2}\(high\)/);
});

test("mm_next still surfaces a real failure", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--next": fail("io", 6, "reading backlog.md: permission denied"),
  });
  const result = await byName.get("mm_next")!.execute("id", {});
  assert.equal(result.isError, true, "exit 6 is not an empty Ready");
  assert.match(textOf(result), /permission denied/);
});

test("mm_list passes its filters and caps the limit at 200 (§4.2)", async (t) => {
  t.after(clearPin);
  pinned();
  const items = [
    { id: "T-0002", title: "two", prio: "high", tags: ["infra", "ci"] },
    { id: "T-0003", title: "three" },
  ];
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--list": ok({ ok: true, operation: "list", directory: { path: BOARD }, result: items }),
  });

  const result = await byName.get("mm_list")!.execute("id", {
    section: "ready",
    state: "backlog",
    prio: "high",
    tag: "infra",
    limit: 5000,
  });
  assert.deepEqual(calls[0]?.args, [
    "--list",
    "--section",
    "ready",
    "--state",
    "backlog",
    "--prio",
    "high",
    "--tag",
    "infra",
    "--limit",
    "200",
  ]);
  // On-disk order, verbatim: ## Ready's order is the user's prioritisation
  // (§4.2), so the tool must not re-rank what it was handed.
  const lines = textOf(result).split("\n");
  assert.match(lines[0] ?? "", /^T-0002/);
  assert.match(lines[1] ?? "", /^T-0003/);
});

test("mm_list defaults the limit to 50 and says so when nothing matches", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--list": ok({ ok: true, operation: "list", directory: { path: BOARD }, result: [] }),
  });
  const result = await byName.get("mm_list")!.execute("id", {});
  assert.deepEqual(calls[0]?.args, ["--list", "--limit", "50"]);
  assert.equal(result.isError, undefined);
  assert.match(textOf(result), /no items match/);
});

test("mm_show renders every field, and the detail body when asked", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--show": ok({
      ok: true,
      operation: "show",
      directory: { path: BOARD },
      result: {
        item: {
          id: "T-0003",
          title: "with a detail",
          state: "backlog",
          section: "Ready",
          position: 2,
          prio: "high",
          tags: ["infra"],
          detail: "details/T-0003.md",
          created: "2026-08-06",
        },
        detail: { body: "# T-0003 — with a detail\n\nthe long form body\n", path: "details/T-0003.md" },
      },
    }),
  });

  const result = await byName.get("mm_show")!.execute("id", { id: "3", detail: true });
  assert.deepEqual(calls[0]?.args, ["--show", "3", "--detail"]);
  const body = textOf(result);
  assert.match(body, /T-0003 {2}with a detail/);
  assert.match(body, /where: {3}ready #2/);
  assert.match(body, /prio: {4}high/);
  assert.match(body, /the long form body/);
});

test("mm_show without an id refuses before spawning anything", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName, calls } = tools({ "--status": ok(STATUS_ENVELOPE) });
  const result = await byName.get("mm_show")!.execute("id", { id: "  " });
  assert.equal(result.isError, true);
  assert.equal(calls.length, 0);
});

test("mm_board with no path answers 'show me this board' (§3.4)", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName } = tools({
    "--status": ok({
      ...STATUS_ENVELOPE,
      directory: { ...STATUS_ENVELOPE.directory, description: "A small-file todo system." },
    }),
  });
  const body = textOf(await byName.get("mm_board")!.execute("id", {}));
  assert.match(body, /Runner Board — \/boards\/rb/);
  assert.match(body, /A small-file todo system\./, "the description rides in when the build reports one");
  assert.match(body, /resolved: pinned for this session/, "how it resolved is knowable (§3.2.1)");
  assert.match(body, /wip 1\/1/, "…and the live status summary");
});

test("mm_board with a path re-pins the session", async (t) => {
  t.after(clearPin);
  clearPin();
  const { byName, calls } = tools({
    "--status": ok({ ...STATUS_ENVELOPE, directory: { path: "/boards/other", project: "Other" } }),
  });
  const result = await byName.get("mm_board")!.execute("id", { path: "/boards/other" });
  assert.match(textOf(result), /Other — \/boards\/other/);
  assert.equal((result.details[PIN_DETAILS_KEY] as { path?: string }).path, "/boards/other");
  assert.equal(calls[0]?.dir, "/boards/other");
});

test("mm_find marks the board in use, and no boards is a result (§3.2.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const listing = tools({
    "--find": ok({
      ok: true,
      operation: "find",
      result: {
        directories: [
          { path: BOARD, project: "Runner Board" },
          { path: "/boards/two", project: "Two" },
        ],
      },
    }),
  });
  const body = textOf(await listing.byName.get("mm_find")!.execute("id", {}));
  assert.match(body, /2 boards:/);
  assert.match(body, /\* \/boards\/rb {2}\(Runner Board\)/, "the in-use board is marked");
  assert.match(body, /^ {2}\/boards\/two/m);

  const none = tools({ "--find": ok({ ok: true, operation: "find", result: { directories: [] } }) });
  const empty = await none.byName.get("mm_find")!.execute("id", {});
  assert.equal(empty.isError, undefined, "no boards is a result, never an error");
  assert.match(textOf(empty), /no boards located/);
});

test("mm_find needs no board, which is the point of it", async (t) => {
  t.after(clearPin);
  clearPin();
  const { byName, calls } = tools({ "--find": ok({ ok: true, result: { directories: [] } }) });
  const result = await byName.get("mm_find")!.execute("id", {});
  assert.equal(result.isError, undefined);
  // It must not have tried to resolve a board first: listing is what you do
  // when you have none.
  assert.deepEqual(calls.map((c) => c.args[0]), ["--find"]);
});

test("mm_check reports violations as results with their locations", async (t) => {
  t.after(clearPin);
  pinned();
  const violations = {
    ok: false,
    operation: "check",
    directory: { path: BOARD },
    result: [
      {
        ok: false,
        path: BOARD,
        violations: [
          { file: "backlog.md", line: 11, invariant: "I1", message: "T-0001 is already defined at backlog.md:9" },
        ],
      },
    ],
  };
  const { byName } = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--check": fail("invariant", 1, "1 problem", violations),
  });
  const result = await byName.get("mm_check")!.execute("id", {});
  // spec-tools.md §5.1.12: violations are results, not errors — the exit code
  // is what a script gates on, and reporting them IS this tool's job.
  assert.equal(result.isError, undefined);
  assert.match(textOf(result), /backlog\.md:11: I1 T-0001 is already defined/);

  const clean = tools({
    "--status": ok(STATUS_ENVELOPE),
    "--check": ok({ ok: true, operation: "check", directory: { path: BOARD }, result: [{ ok: true, path: BOARD, violations: [] }] }),
  });
  assert.match(textOf(await clean.byName.get("mm_check")!.execute("id", {})), /clean: no violations/);
});

/* ------------------------------------------------------------- plumbing */

test("a tool call updates the board status line (§3.2.1)", async (t) => {
  t.after(clearPin);
  pinned();
  const lines: string[] = [];
  const { byName } = tools({ "--status": ok(STATUS_ENVELOPE) }, { onPin: (line) => lines.push(line) });
  await byName.get("mm_status")!.execute("id", {});
  assert.deepEqual(lines, ["Runner Board — /boards/rb"]);
});

test("warnings ride under the result rather than inside it", async (t) => {
  t.after(clearPin);
  pinned();
  const { byName } = tools({
    "--status": ok({ ...STATUS_ENVELOPE, warnings: ["the period predates done.md"] }),
  });
  const body = textOf(await byName.get("mm_status")!.execute("id", {}));
  assert.match(body, /warning: the period predates done\.md$/m);
});
