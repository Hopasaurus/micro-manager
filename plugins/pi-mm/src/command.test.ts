/*
  The /mm command (spec-pi-mm-plugin.md §5).

  The command's whole design claim is that it runs the TOOLS rather than a
  parallel implementation, so most of these tests check the mapping from a
  typed line to a tool call — the only part that is actually new — plus the
  three §5 rules that would be easy to break: it prints what the tool returned,
  it does not run when no board is found, and /mm remove confirms the same way.
*/

import test from "node:test";
import assert from "node:assert/strict";

import { helpText, parseCommand, plan, runCommand, tokenize } from "./command.ts";
import { contextEnabled, resetSettings } from "./settings.ts";
import type { ToolContext, ToolDefinition, ToolResult } from "./tools-read.ts";

/* --------------------------------------------------------------- parsing */

test("a quoted title is one argument", () => {
  assert.deepEqual(tokenize('add "Fix the deploy script" --prio high'), [
    "add",
    "Fix the deploy script",
    "--prio",
    "high",
  ]);
  // An empty quoted string is an argument too — it is how a user says "this
  // one, deliberately blank" rather than "I forgot".
  assert.deepEqual(tokenize('edit T-1 --title ""'), ["edit", "T-1", "--title", ""]);
});

test("flags accumulate, switches do not take values", () => {
  const p = parseCommand("add Something --tag infra --tag ci --top --prio high");
  assert.equal(p.op, "add");
  assert.deepEqual(p.words, ["Something"]);
  assert.deepEqual(p.flags.get("tag"), ["infra", "ci"]);
  assert.deepEqual(p.flags.get("prio"), ["high"]);
  assert.ok(p.switches.has("top"));
});

test("--key=value is the same as --key value", () => {
  const p = parseCommand("list --section=someday --limit=10");
  assert.deepEqual(p.flags.get("section"), ["someday"]);
  assert.deepEqual(p.flags.get("limit"), ["10"]);
});

/* ---------------------------------------------------------------- plans */

test("every §5 op maps to its tool", () => {
  const cases: Array<[string, string]> = [
    ["status", "mm_status"],
    ["next", "mm_next"],
    ["check", "mm_check"],
    ["find", "mm_find"],
    ["board", "mm_board"],
    ["list", "mm_list"],
    ["show T-1", "mm_show"],
    ['add "x"', "mm_add"],
    ["edit T-1 --title y", "mm_edit"],
    ["move T-1 --top", "mm_move"],
    ["start T-1", "mm_start"],
    ["pause T-1", "mm_pause"],
    ["finish T-1", "mm_finish"],
    ["note T-1 something", "mm_note"],
    ["remove T-1", "mm_remove"],
    ["init --project x", "mm_init"],
    ["describe hello", "mm_describe"],
  ];
  for (const [line, tool] of cases) {
    const decided = plan(parseCommand(line));
    assert.ok("tool" in decided, `${line} should reach a tool`);
    assert.equal("tool" in decided && decided.tool, tool, line);
  }
});

test("the parameters a tool gets are the ones that were typed", () => {
  const add = plan(parseCommand('add "Fix the deploy script" --prio high --tag infra --tag ci --top'));
  assert.deepEqual("tool" in add && add.params, {
    title: "Fix the deploy script",
    prio: "high",
    tags: ["infra", "ci"],
    top: true,
  });

  const edit = plan(parseCommand("edit T-1 --set owner=dana --set sprint=12 --untag infra"));
  assert.deepEqual("tool" in edit && edit.params, {
    id: "T-1",
    untags: ["infra"],
    set: ["owner=dana", "sprint=12"],
  });

  // note takes the rest of the line as its text, unquoted: a human typing a
  // sentence should not have to think about quoting it.
  const note = plan(parseCommand("note T-1 the hose is in the shed"));
  assert.deepEqual("tool" in note && note.params, { id: "T-1", text: "the hose is in the shed" });
});

test("a missing subject asks for it instead of guessing", () => {
  for (const line of ["show", "edit", "move", "start", "pause", "finish", "note T-1", "remove", "init"]) {
    const decided = plan(parseCommand(line));
    assert.ok("say" in decided, `${line} should say what is missing`);
  }
});

test("an unknown op prints help rather than picking an operation", () => {
  const decided = plan(parseCommand("stat"));
  assert.ok("say" in decided);
  assert.match("say" in decided ? decided.say : "", /unknown: \/mm stat/);
  assert.match("say" in decided ? decided.say : "", /\/mm status/, "the help follows");
});

test("/mm remove asserts the confirmation a typed command IS (§8.2)", () => {
  const decided = plan(parseCommand("remove T-0042"));
  // A human typing the command is the user asking; the tool still runs its
  // interactive prompt, so a typed removal is confirmed twice.
  assert.deepEqual("tool" in decided && decided.params, { id: "T-0042", confirmed: true });
});

test("/mm tick and /mm archive say they are not built rather than half-running", () => {
  for (const op of ["tick", "archive"]) {
    const decided = plan(parseCommand(op));
    assert.ok("say" in decided);
    assert.match("say" in decided ? decided.say : "", /not available yet/);
  }
});

test("/mm context is session state, and reports itself (§5, §6)", (t) => {
  t.after(resetSettings);
  resetSettings();
  assert.equal(contextEnabled(), true, "on by default");

  assert.match(sayOf(plan(parseCommand("context off"))), /context injection off/);
  assert.equal(contextEnabled(), false);
  assert.match(sayOf(plan(parseCommand("context"))), /is off for this session/);

  assert.match(sayOf(plan(parseCommand("context on"))), /context injection on/);
  assert.equal(contextEnabled(), true);

  assert.match(sayOf(plan(parseCommand("context maybe"))), /takes on or off/);
});

test("help lists the surface, and an empty line is help", () => {
  assert.equal(sayOf(plan(parseCommand(""))), helpText());
  assert.match(helpText(), /\/mm remove ID/);
  assert.match(helpText(), /\/mm context \[on\|off\]/);
});

function sayOf(decided: ReturnType<typeof plan>): string {
  return "say" in decided ? decided.say : "";
}

/* ------------------------------------------------------------- dispatch */

/** A tool double that records what it was called with. */
function fakeTool(name: string, result: ToolResult) {
  const calls: Array<{ params: Record<string, unknown>; ctx?: ToolContext }> = [];
  const tool: ToolDefinition = {
    name,
    label: name,
    description: "x".repeat(30),
    parameters: {},
    async execute(_id, params, _signal, _onUpdate, ctx) {
      calls.push({ params, ...(ctx ? { ctx } : {}) });
      return result;
    },
  };
  return { tool, calls };
}

const said = (text: string, isError = false): ToolResult => ({
  content: [{ type: "text", text }],
  details: {},
  ...(isError ? { isError: true } : {}),
});

test("the command prints exactly what the tool returned (§5)", async () => {
  const status = fakeTool("mm_status", said("Runner Board — /boards/rb\nwip 1/1 · ready 2"));
  const out = await runCommand(new Map([["mm_status", status.tool]]), "status");
  assert.equal(out.isError, false);
  // Same bytes: the command is not a second renderer.
  assert.equal(out.text, "Runner Board — /boards/rb\nwip 1/1 · ready 2");
});

test("a tool error is surfaced as an error, with its text", async () => {
  const start = fakeTool("mm_start", said("wip limit reached (1/1)\n  slot 01  T-0001  one", true));
  const out = await runCommand(new Map([["mm_start", start.tool]]), "start T-0002");
  assert.equal(out.isError, true);
  assert.match(out.text, /slot 01 {2}T-0001 {2}one/);
});

test("no board found stops the command with the resolution failure (§5)", async () => {
  // The tool is the one that resolves, so "MUST NOT run when no board is
  // found" is inherited rather than re-implemented: the tool refuses, and the
  // command prints the refusal.
  const list = fakeTool("mm_list", said("no board: mm found none here. mm_init creates one.", true));
  const out = await runCommand(new Map([["mm_list", list.tool]]), "list");
  assert.equal(out.isError, true);
  assert.match(out.text, /no board/);
});

test("the command's ctx reaches the tool, which is how /mm remove confirms", async () => {
  const remove = fakeTool("mm_remove", said("removed T-0042"));
  const ctx: ToolContext = { hasUI: true, ui: { confirm: async () => true } };
  await runCommand(new Map([["mm_remove", remove.tool]]), "remove T-0042", ctx);

  assert.deepEqual(remove.calls[0]?.params, { id: "T-0042", confirmed: true });
  // Handed through, not re-created: the dialog the user sees is the tool's,
  // and there is exactly one implementation of the guard.
  assert.equal(remove.calls[0]?.ctx, ctx);
});

test("a tool the build does not have says so", async () => {
  const out = await runCommand(new Map(), "status");
  assert.equal(out.isError, true);
  assert.match(out.text, /mm_status is not available in this build/);
});
