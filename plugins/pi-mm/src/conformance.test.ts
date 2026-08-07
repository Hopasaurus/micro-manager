/*
  Conformance (spec-pi-mm-plugin.md §10).

  §10 defines two tiers and three prohibitions, and this file is where the
  plugin is measured against them as a WHOLE rather than a module at a time.
  Everything here is a claim the spec makes about the plugin, phrased as
  something a reader can check:

    Minimally conforming  §2.1, §3.2, §3.3, §4.1–§4.3 for a named tool set;
                          §6; §8.1; §8.3; §9's global config.
    Fully conforming      additionally mm_edit, mm_move, mm_note, mm_remove
                          with §8.2, mm_check, the /mm command surface of §5,
                          and §7's branching state.
    And MUST NOT          add tools that change the meaning of the operations
                          defined here, write the data files, or run `mm` with
                          any switch this spec does not name.

  What this file does NOT do is re-test the modules. The behaviours §10 rests
  on are each owned by the file that implements them, and duplicating their
  assertions here would produce two things to keep in step:

    runner.test.ts        argv construction, envelope parsing, the exit-code
                          mapping of §4.3, the timeout of §4.1, MM_DIR
    board.test.ts         resolution order, ambiguity, the pin, §7's branch
                          reconstruction
    tools-remove.test.ts  §8.2's three guards, one by one
    context.test.ts       §6's injection rules
    config.test.ts        §9's two files and their trust rule
    mm-contract.test.ts   all of the above against the REAL mm

  So what is left for this file is the part no single module can answer: which
  tools exist, what they collectively send to `mm`, and what the plugin does
  when nobody has asked it to do anything.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import micromanager from "./index.ts";
import { clearPin, setPin } from "./board.ts";
import { resetCapabilities } from "./capabilities.ts";
import { parseCommand, plan } from "./command.ts";
import { resetContext } from "./context.ts";
import { resetPresence } from "./presence.ts";
import { readTools, type ToolContext, type ToolDefinition, type ToolDeps } from "./tools-read.ts";
import { recommendedTools } from "./tools-recommended.ts";
import { removeTools } from "./tools-remove.ts";
import { workflowTools } from "./tools-workflow.ts";
import { writeTools } from "./tools-write.ts";
import type { RunOptions, RunOutcome } from "./runner.ts";

/* ------------------------------------------------------------- the tiers */

/** §10's minimally conforming set, verbatim. */
const MINIMAL = [
  "mm_init",
  "mm_find",
  "mm_status",
  "mm_next",
  "mm_list",
  "mm_show",
  "mm_board",
  "mm_describe",
  "mm_add",
  "mm_start",
  "mm_pause",
  "mm_finish",
];

/** What §10 adds for full conformance. */
const FULL_EXTRA = ["mm_edit", "mm_move", "mm_note", "mm_remove", "mm_check"];

/** §4.2's recommended table — SHOULD, and gated on the build (capabilities.ts). */
const RECOMMENDED = [
  "mm_add_many",
  "mm_block",
  "mm_unblock",
  "mm_search",
  "mm_report",
  "mm_tick",
  "mm_archive",
];

const BOARD = "/boards/conformance";

const STATUS_ENVELOPE = {
  ok: true,
  operation: "status",
  directory: { path: BOARD, project: "Conformance", wipLimit: 1, wipUsed: 0 },
  result: { counts: {}, wip: { limit: 1, used: 0 }, slots: [], next: null, path: BOARD },
  changes: [],
  warnings: [],
  errors: [],
};

/**
 * What `result` is for each operation, in the shape mm really uses.
 *
 * Three operations answer with a LIST and the rest with an object, so one
 * generic envelope cannot stand in for all of them — and a sweep whose
 * fixtures were the wrong shape would be testing the fixtures.
 */
function resultFor(op: string): unknown {
  switch (op) {
    case "--list":
    case "--check":
    case "--search":
      return [];
    case "--find":
      return { directories: [], partial: false, skipped: [] };
    case "--show":
      return { item: { id: "T-0001", title: "an item", state: "backlog" } };
    case "--init":
      return { path: BOARD, project: "Conformance" };
    case "--tick":
      return { fired: [], errors: [] };
    case "--archive":
      return { cutoff: "2026-08", months: [], items: 0, files: [], detailsMoved: [], detailOrphans: [] };
    case "--report":
      return { project: "Conformance", period: { label: "all", source: "switch" }, done: [] };
    case "--add-many":
      return [{ id: "T-0001", title: "an item", state: "backlog", section: "Ready" }];
    case "--remove":
      return { item: { id: "T-0001", title: "an item", state: "backlog" }, detailDeleted: false };
    default:
      return { id: "T-0001", title: "an item", state: "backlog", section: "Ready", position: 1 };
  }
}

const envelopeFor = (op: string) => ({
  ok: true,
  operation: op.replace(/^--/, ""),
  directory: { path: BOARD, project: "Conformance" },
  result: resultFor(op),
  changes: [],
  warnings: [],
  errors: [],
});

/** Every tool the plugin can register, with a runner that records the argv. */
function everyTool(answer?: RunOutcome) {
  const calls: RunOptions[] = [];
  const run = (async (opts: RunOptions): Promise<RunOutcome> => {
    calls.push(opts);
    const op = opts.args[0] ?? "";
    // Resolution asks --status first, and must succeed even when the case
    // under test is a failing operation.
    if (op === "--status") return { ok: true, envelope: STATUS_ENVELOPE as never };
    return answer ?? { ok: true, envelope: envelopeFor(op) as never };
  }) as unknown as ToolDeps["run"];

  const deps: ToolDeps = { health: async () => ({ ok: true, version: "mm 0.1.0" }), run };
  const byName = new Map<string, ToolDefinition>();
  for (const tool of [
    ...readTools(deps),
    ...writeTools(deps),
    ...workflowTools(deps),
    ...removeTools(deps),
    ...recommendedTools(deps),
  ]) {
    byName.set(tool.name, tool);
  }
  return { byName, calls };
}

test("the required tools of §10's two tiers all exist", () => {
  const { byName } = everyTool();
  for (const name of MINIMAL) {
    assert.ok(byName.has(name), `${name} is required for MINIMAL conformance (§10)`);
  }
  for (const name of FULL_EXTRA) {
    assert.ok(byName.has(name), `${name} is required for FULL conformance (§10)`);
  }
  for (const name of RECOMMENDED) {
    assert.ok(byName.has(name), `${name} is recommended by §4.2`);
  }
  // §10: "MUST NOT add tools that change the meaning of the operations defined
  // here." A tool this list has never heard of is exactly that, so the surface
  // is closed rather than merely covered.
  assert.deepEqual(
    [...byName.keys()].sort(),
    [...MINIMAL, ...FULL_EXTRA, ...RECOMMENDED].sort(),
    "the plugin registers a tool the spec does not define",
  );
});

test("every tool carries what pi and the model need of it", () => {
  const { byName } = everyTool();
  for (const tool of byName.values()) {
    assert.ok(tool.description.length > 20, `${tool.name}: the model reads this description`);
    assert.ok(tool.parameters, `${tool.name}: parameters are typebox schemas (§4.1)`);
    assert.ok(tool.promptGuidelines?.length, `${tool.name}: §6's guidelines say when to use it`);
    for (const line of tool.promptGuidelines ?? []) {
      // pi appends the bullets flat: "this tool" names nothing the model can
      // act on, so each one has to spell an mm_* tool out.
      assert.match(line, /\bmm_[a-z*]+/, `${tool.name}: a guideline must name a tool`);
    }
  }
});

/* ------------------------------------- §10: only the switches this spec names */

/**
 * Every switch the plugin is allowed to hand `mm`.
 *
 * §10: "MUST NOT run `mm` with any switch this spec does not name." The lists
 * are the plugin spec's own — §4.2 and Appendix A for the tools, §3.3 for
 * `--init`, Appendix B for the modifiers — plus, for the recommended tools,
 * the `spec-tools.md` sections their §4.2 rows cite.
 */
const ALLOWED_SWITCHES = new Set([
  // Operations: §4.2's two tables, and Appendix B's list.
  "--init", "--find", "--status", "--next", "--list", "--show", "--describe",
  "--add", "--edit", "--move", "--start", "--pause", "--finish", "--note",
  "--remove", "--check", "--block", "--unblock", "--search", "--report",
  "--tick", "--archive", "--add-many",
  // Modifiers, Appendix B.
  "--json", "--dir", "--force", "--top", "--end", "--position", "--section",
  "--state", "--prio", "--tag", "--untag", "--set", "--limit", "--detail",
  "--detail-text", "--outcome", "--closing-note", "--before",
  // §3.3 names these five on mm_init by name.
  "--project", "--slots", "--slot-width", "--prefix", "--description",
  // Named by the spec-tools.md operation each tool's §4.2 row points at:
  // --title, --unset and --after (§5.1.5, §5.1.7), --blocked (§5.1.2's reason field),
  // --slot (§5.1.8), --reason (§5.2's --block ID --reason TEXT), --field and
  // --regex (§5.2's --search), the period and content switches of §5.1.11,
  // --age (§5.3.1's second spelling of the cutoff), and --dry-run, which
  // §5.3's signatures give to --tick and --archive.
  "--title", "--unset", "--after", "--blocked", "--slot", "--reason", "--field", "--regex",
  "--period", "--since", "--until", "--group-by", "--include-wip",
  "--include-backlog", "--include-archives", "--age", "--dry-run",
]);

/**
 * Operations no tool may ever run.
 *
 * Each is a real `mm` operation (`spec-tools.md` §5.2, §5.3) that the plugin
 * spec does not give a tool — and each would be a quiet disaster from an
 * agent: renumbering IDs, rewriting a directory's format, or changing the WIP
 * limit the slots exist to enforce.
 */
const FORBIDDEN_OPERATIONS = ["--fix", "--migrate", "--stats", "--export", "--wip", "--top-up", "--subtask"];

/** A maximal call for every tool: every parameter it declares, at once. */
const MAXIMAL_CALLS: Record<string, Record<string, unknown>> = {
  mm_status: {},
  mm_next: {},
  mm_find: {},
  mm_check: {},
  mm_board: { path: BOARD },
  mm_list: { section: "ready", state: "all", prio: "high", tag: "infra", limit: 10 },
  mm_show: { id: "T-0001", detail: true },
  mm_init: {
    project: "Conformance",
    dir: BOARD,
    slots: 2,
    slot_width: 2,
    prefix: "C",
    description: "a board",
  },
  mm_describe: { text: "a board" },
  mm_add: {
    title: "an item",
    section: "ready",
    prio: "high",
    tags: ["infra", "ci"],
    top: true,
    blocked: "",
    detail_text: "long form",
  },
  mm_edit: {
    id: "T-0001",
    title: "an item",
    prio: "low",
    tags: ["infra"],
    untags: ["ci"],
    set: ["owner=dana"],
    unset: ["owner"],
    blocked: "waiting",
  },
  mm_move: { id: "T-0001", section: "someday", position: 2 },
  mm_note: { id: "T-0001", text: "a finding" },
  mm_start: { id: "T-0001", slot: 1 },
  mm_pause: { id: "T-0001", section: "ready", end: true, blocked: "waiting" },
  mm_finish: { id: "T-0001", outcome: "shipped", closing_note: "shipped in v2.1" },
  mm_remove: { id: "T-0001", confirmed: true },
  mm_add_many: {
    items: ["One | prio:high", "Two | tags:infra"],
    section: "ready",
    prio: "med",
    tags: ["captured"],
    top: true,
  },
  mm_block: { id: "T-0001", reason: "waiting on the vendor" },
  mm_unblock: { id: "T-0001", end: true },
  mm_search: { query: "deploy", fields: ["title", "tags", "detail"], state: "backlog", regex: true, limit: 10 },
  mm_report: {
    period: "last-week",
    since: "2026-07-01",
    until: "2026-07-31",
    group_by: "outcome",
    include_wip: true,
    include_backlog: true,
    include_archives: true,
  },
  mm_tick: { dry_run: true },
  mm_archive: { before: "2026-01", dry_run: true },
};

/** A ctx that says yes, so mm_remove reaches the far side of its guards. */
const CONSENTING: ToolContext = { hasUI: true, ui: { confirm: async () => true } };

test("no tool runs mm with a switch this spec does not name (§10)", async (t) => {
  t.after(clearPin);
  setPin({ path: BOARD, project: "Conformance", source: "pin" });
  const { byName, calls } = everyTool();

  // Every tool is exercised at its widest, so a switch that only appears with
  // an unusual parameter still reaches the sweep.
  assert.deepEqual(
    Object.keys(MAXIMAL_CALLS).sort(),
    [...byName.keys()].sort(),
    "a tool with no maximal call would be swept over without being checked",
  );
  for (const [name, params] of Object.entries(MAXIMAL_CALLS)) {
    await byName.get(name)!.execute("id", params, undefined, undefined, CONSENTING);
  }

  const seen = new Set<string>();
  for (const call of calls) {
    for (const arg of call.args) {
      if (arg.startsWith("--")) seen.add(arg.split("=")[0] as string);
    }
  }
  assert.ok(seen.size > 20, `the sweep only saw ${seen.size} switches; it is not covering the surface`);
  for (const arg of seen) {
    assert.ok(ALLOWED_SWITCHES.has(arg), `${arg} is not a switch this spec names (§10)`);
  }
  for (const forbidden of FORBIDDEN_OPERATIONS) {
    assert.equal(seen.has(forbidden), false, `${forbidden} must never be run by a tool`);
  }
  t.diagnostic(`swept ${byName.size} tools, ${calls.length} invocations, ${seen.size} distinct switches`);
});

test("one tool call is one operation and one subprocess (§4.1)", async (t) => {
  t.after(clearPin);
  const operations = new Set(
    [...ALLOWED_SWITCHES].filter((s) => !s.startsWith("--dir") && OPERATION_SWITCHES.has(s)),
  );

  for (const [name, params] of Object.entries(MAXIMAL_CALLS)) {
    setPin({ path: BOARD, project: "Conformance", source: "pin" });
    const { byName, calls } = everyTool();
    await byName.get(name)!.execute("id", params, undefined, undefined, CONSENTING);

    /*
      Two tools legitimately spend a second subprocess, and neither chains two
      MUTATIONS — which is what §4.1's rule is about ("start and finish is two
      tool calls", "a chain hides which step failed"):

        mm_board  re-resolves, then reads the status of what it pinned (§3.4:
                  one call answers "show me this board").
        mm_remove reads the item before it deletes it, because §8.2 step 2 says
                  the prompt names the item AND ITS HOME — a confirmation
                  dialog that could not say what it was about would be a worse
                  guard than none.
    */
    const budget = name === "mm_board" || name === "mm_remove" ? 2 : 1;
    assert.ok(
      calls.length <= budget,
      `${name} spawned ${calls.length} subprocesses; §4.1 allows ${budget}`,
    );
    for (const call of calls) {
      const found = call.args.filter((arg) => operations.has(arg));
      assert.equal(
        found.length,
        1,
        `${name} sent ${found.length} operations in one invocation: ${call.args.join(" ")}`,
      );
      assert.equal(found[0], call.args[0], `${name}: the operation must lead the argv`);
    }
  }
});

/** The subset of ALLOWED_SWITCHES that are operations rather than modifiers. */
const OPERATION_SWITCHES = new Set([
  "--init", "--find", "--status", "--next", "--list", "--show", "--describe",
  "--add", "--add-many", "--edit", "--move", "--start", "--pause", "--finish",
  "--note", "--remove", "--check", "--block", "--unblock", "--search",
  "--report", "--tick", "--archive",
]);

/* ----------------------------------------------- §5: the command surface */

test("every operation §5 defines reaches a tool (§10, full conformance)", () => {
  const { byName } = everyTool();
  // §5's grammar, one entry per line of it. A line that produced "unknown"
  // would be a command the spec promises and the plugin does not have.
  const lines = [
    "status", "next", "check", "find",
    "init --project Conformance",
    "board",
    "describe some text",
    "context",
    "list --section ready",
    "show T-0001",
    'add "an item"',
    "add-many\nan item\nanother item",
    "edit T-0001 --prio high",
    "move T-0001 --top",
    "start T-0001",
    "pause T-0001",
    "finish T-0001 --outcome shipped",
    "note T-0001 a finding",
    "remove T-0001",
    "tick",
    "archive --before 2026-01",
    // Not in §5's list, but §4.2 tools and so mirrored the same way.
    'block T-0001 "waiting"',
    "unblock T-0001",
    'search "deploy"',
    "report last-week",
  ];
  for (const line of lines) {
    const decided = plan(parseCommand(line));
    if ("say" in decided) {
      assert.doesNotMatch(decided.say, /^unknown:/, `/mm ${line} is not implemented`);
      continue;
    }
    assert.ok(byName.has(decided.tool), `/mm ${line} maps to ${decided.tool}, which does not exist`);
  }
});

test("an operation the spec does not define is refused, never guessed", () => {
  // §5's rule against helpfulness: `/mm stat` is a typo, and running --status
  // for it would be the plugin deciding what someone meant.
  for (const line of ["stat", "fix", "migrate", "wip 3", "stats"]) {
    const decided = plan(parseCommand(line));
    assert.ok("say" in decided, `/mm ${line} must not reach a tool`);
    assert.match("say" in decided ? decided.say : "", /^unknown:/);
  }
});

/* -------------------------------------------- §8.3: nothing on its own */

/** An `mm` shim that records every argv it is called with. */
function recordingShim(envelope: unknown): { dir: string; argv: () => string[][] } {
  const dir = mkdtempSync(join(tmpdir(), "mm-conformance-"));
  const path = join(dir, "mm");
  const log = join(dir, "argv");
  const json = JSON.stringify(envelope).replaceAll("'", "'\\''");
  writeFileSync(
    path,
    [
      "#!/bin/sh",
      `echo "$*" >> ${log}`,
      'case "$1" in',
      '  --version) echo "mm 9.9.9" ;;',
      "  --help) printf 'Operations:\\n  --status  x\\n' ;;",
      `  *) printf '%s' '${json}' ;;`,
      "esac",
    ].join("\n"),
  );
  chmodSync(path, 0o755);
  return {
    dir,
    argv: () => {
      try {
        return readFileSync(log, "utf8").split("\n").filter(Boolean).map((line) => line.split(" "));
      } catch {
        return [];
      }
    },
  };
}

test("the lifecycle consults §7, §9 and §6 — and mutates nothing (§2.2, §8.3)", async (t) => {
  const shim = recordingShim(STATUS_ENVELOPE);
  const previousPath = process.env["PATH"];
  process.env["PATH"] = `${shim.dir}:${previousPath ?? ""}`;
  resetPresence();
  resetCapabilities();
  resetContext();
  t.after(() => {
    process.env["PATH"] = previousPath;
    resetPresence();
    resetCapabilities();
    resetContext();
    clearPin();
  });

  const events = new Map<string, Array<(event: unknown, ctx: unknown) => unknown>>();
  const api = {
    on(event: string, handler: (event: unknown, ctx: unknown) => unknown) {
      const list = events.get(event) ?? [];
      list.push(handler);
      events.set(event, list);
    },
    registerTool() {},
    registerCommand() {},
  };
  micromanager(api as never);

  // §2.2 names the handlers a guest may have. One this does not know about is
  // a place a mutation could hide.
  assert.deepEqual(
    [...events.keys()].sort(),
    ["before_agent_start", "session_shutdown", "session_start"],
    "the plugin registered a lifecycle handler §2.2 does not sanction",
  );

  // The two questions §10's tiers require the factory to ASK: pi's own trust
  // answer for §9's project config, and the active branch for §7's pin.
  const asked = { trust: 0, branch: 0 };
  const ctx = {
    cwd: process.cwd(),
    isProjectTrusted: () => {
      asked.trust += 1;
      return true;
    },
    ui: { setStatus() {}, notify() {} },
    sessionManager: {
      getBranch: () => {
        asked.branch += 1;
        return [];
      },
    },
  };
  const fire = async (event: string, payload: unknown): Promise<unknown[]> => {
    const out: unknown[] = [];
    for (const handler of events.get(event) ?? []) out.push(await handler(payload, ctx));
    return out;
  };

  await fire("session_start", { reason: "startup" });
  assert.ok(asked.trust > 0, "§9: the project config's trust check is pi's answer, and must be asked for");
  assert.ok(asked.branch > 0, "§7: the pin is reconstructed from the active branch");

  // A turn boundary, with a board pinned — the case where an eager plugin
  // would be tempted to tick, auto-start, or tidy.
  setPin({ path: BOARD, project: "Conformance", source: "pin" });
  const [injected] = await fire("before_agent_start", { systemPrompt: "system" });
  const prompt = (injected as { systemPrompt?: string } | undefined)?.systemPrompt ?? "";
  // §6, minimal conformance: the block is appended to the prompt it was given,
  // never in place of it.
  assert.match(prompt, /^system\n/, "the injected block must not replace the system prompt");
  assert.match(prompt, /\[mm\] \/boards\/conformance/);
  await fire("before_agent_start", { systemPrompt: "system" });
  await fire("session_shutdown", {});

  const argv = shim.argv();
  assert.ok(argv.length > 0, "the shim was never called; this test proved nothing");
  const readOnly = new Set(["--version", "--help", "--status", "--find", "--list", "--show", "--next", "--check"]);
  for (const call of argv) {
    assert.ok(
      readOnly.has(call[0] ?? ""),
      `a lifecycle handler ran ${call.join(" ")}; §8.3 allows no mutation on the plugin's own initiative`,
    );
  }
  t.diagnostic(`lifecycle ran: ${argv.map((c) => c[0]).join(", ")}`);
});

/* ------------------------------------------------ §8.1: never the files */

/*
  §8.1's smoke test: "a test that greps the plugin's source for the board
  filenames and finds none is a conformance smoke-test worth having."

  It lived in index.test.ts until this file existed; §8.1 is a conformance
  rule, and this is where conformance is measured.

  The rule has been narrowed twice, both times because it fired on prose rather
  than on an access — a parameter description saying "working.NN.md", a result
  line saying an item landed in "done.md". Naming a board file is how you TALK
  about a board; opening one is the thing that must never happen. So what is
  asserted is the mechanism rather than the vocabulary:

    1. No plugin module imports node:fs, WITH ONE EXEMPTION. You cannot read or
       write a board without it, and the plugin's one way out to the world is
       spawning mm — whose argv the sweep above pins. The exemption is
       config.ts, which reads the plugin's OWN config (§9); it is granted by
       name, and paid for by rule 3.
    2. No board filename appears as a PATH: a bare filename literal
       ("backlog.md"), or one after a separator ("details/T-0042.md"). A
       filename inside a sentence is prose and is allowed.
    3. The exempt module opens nothing but its own config file. An exemption
       nobody checks is just a hole.
*/
test("the plugin reaches the board only through mm (§8.1)", async () => {
  const { readdirSync } = await import("node:fs");
  const dir = new URL(".", import.meta.url).pathname;
  // Each needle is a board FILE. The details one carries its directory,
  // because "details" alone is also pi's word for a tool result's payload —
  // which is how this rule fired on board.ts reading message["details"].
  const names = [
    "backlog\\.md",
    "done\\.md",
    "working\\.[0-9N]+\\.md",
    "details/[^\"'`\\s]*\\.md",
  ];
  /** The one module allowed to touch the filesystem, and what it may open. */
  const FS_EXEMPT = "config.ts";

  let checked = 0;
  for (const name of readdirSync(dir)) {
    if (!name.endsWith(".ts") || name.endsWith(".test.ts")) continue;
    checked += 1;
    const source = readFileSync(join(dir, name), "utf8");

    if (name !== FS_EXEMPT) {
      assert.doesNotMatch(
        source,
        /from\s+["']node:fs(\/promises)?["']|require\(["']node:fs/,
        `${name} imports node:fs; the plugin never touches board files (§2.1, §8.1)`,
      );
    }
    for (const needle of names) {
      const asAPath = new RegExp(`["'\`](${needle}|[^"'\`\n]*/${needle})["'\`]`);
      assert.doesNotMatch(
        source,
        asAPath,
        `${name} builds a path to a board file; go through mm (§2.1, §8.1)`,
      );
    }
  }

  /*
    Rule 3: the exemption, checked — at the point where a path is BUILT rather
    than where it is read. The read itself takes a variable, so asserting on
    the call would prove nothing; what constrains config.ts is that every path
    it constructs ends in its own filename.
  */
  const exempt = readFileSync(join(dir, FS_EXEMPT), "utf8");
  // Line-wise, because a nested call (homedir()) ends the naive paren match
  // early; every join in this module is one line.
  const built = exempt.split("\n").filter((line) => /\bjoin\(/.test(line));
  assert.ok(built.length > 0, "the exemption should be earning its keep");
  for (const line of built) {
    assert.match(
      line,
      /CONFIG_FILENAME/,
      `${FS_EXEMPT} builds a path that is not its own config file: ${line.trim()}`,
    );
  }
  assert.match(exempt, /CONFIG_FILENAME = "mm-plugin\.json"/);

  // A smoke test that silently covered nothing is the failure mode this file
  // cannot afford (the same reason the Go side logs its fixture counts).
  assert.ok(checked >= 10, `only ${checked} plugin module(s) were scanned`);
});

/* --------------------------------------- §4.3: the exit codes, end to end */

test("every exit code reaches a tool result as §4.3 requires", async (t) => {
  t.after(clearPin);
  // The runner's own mapping is runner.test.ts's; what is checked here is that
  // it survives the trip through a tool — that a failed operation is a failed
  // TOOL RESULT carrying the code's message, and never a quiet success.
  const cases: Array<[number, string, RegExp]> = [
    [1, "invariant", /mm_check/],
    [2, "usage", /not available in the installed mm build/],
    [3, "not-found", /no such item/],
    [4, "precondition", /wip limit reached/],
    [5, "concurrent", /Re-read with mm_status/],
    [6, "io", /permission denied/],
  ];

  for (const [exit, kind, expected] of cases) {
    setPin({ path: BOARD, project: "Conformance", source: "pin" });
    const messages: Record<number, string> = {
      1: "backlog.md:21: I1 duplicate id\nThe board violates an invariant. Run mm_check to see every finding.",
      2: "unknown switch\nThis operation is not available in the installed mm build.",
      3: "no such item: T-9999",
      4: "wip limit reached (1/1)",
      5: "the board changed\nRe-read with mm_status or mm_list, then decide again — nothing was retried.",
      6: "permission denied",
    };
    const { byName } = everyTool({
      ok: false,
      failure: {
        kind,
        exit,
        message: messages[exit] as string,
        errors: [{ code: kind, message: messages[exit] as string }],
        // A failure still carries an envelope (§9.2), and this one is not
        // empty — which is the case §4.3 legislates: "a mutation that fails
        // MUST be reported as failed even when some part of the envelope is
        // non-empty", because a partial success is the one state a transaction
        // may not produce.
        envelope: {
          ok: false,
          operation: "add",
          changes: [{ kind: "created", id: "T-0001", file: "somewhere" }],
          errors: [{ code: kind, message: messages[exit] as string }],
        },
      },
    } as never);

    const result = await byName.get("mm_add")!.execute("id", { title: "an item" });
    assert.equal(result.isError, true, `exit ${exit} must be reported as a failure (§4.3)`);
    assert.match(result.content[0]?.text ?? "", expected, `exit ${exit}`);
    // …and the code and its errors reach `details`, where a front end reads
    // them rather than re-parsing the message.
    const error = result.details["error"] as { kind?: string; exit?: number };
    assert.equal(error?.kind, kind, `exit ${exit}: details carries the failure kind`);
    assert.equal(error?.exit, exit);
  }
  t.diagnostic("covered: exits 1-6 through a tool result");
});
