/*
  The extension factory (spec-pi-mm-plugin.md §2.1, §2.2, §3.1).

  What a skeleton has to get right is mostly what it does NOT do, so that is
  what these assert: it registers, it reports, and it touches nothing. The
  stubs below are the smallest shape of pi's API the factory actually uses —
  deliberately not the real ExtensionAPI, because a test that needed a running
  agent would not run here at all.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import micromanager, { STATUS_KEY, health } from "./index.ts";
import { resetCapabilities } from "./capabilities.ts";
import { resetPresence } from "./presence.ts";

/**
 * Both session-scoped probes, dropped.
 *
 * `mm --version` (presence) and `mm --help` (capabilities) are each cached for
 * the session, so a test that changed what is on PATH without dropping them
 * would be asserting against the previous test's shim.
 */
function resetProbes(): void {
  resetPresence();
  resetCapabilities();
}

type Handler = (event: { reason?: string }, ctx: FakeContext) => unknown;

interface FakeContext {
  ui: {
    setStatus: (key: string, text: string) => void;
    notify: (text: string, level: string) => void;
  };
}

/** Records what the factory registered, which is all a skeleton can be judged on. */
function fakePi() {
  const events = new Map<string, Handler[]>();
  const tools: unknown[] = [];
  const commands: string[] = [];
  return {
    api: {
      on(event: string, handler: Handler) {
        const list = events.get(event) ?? [];
        list.push(handler);
        events.set(event, list);
      },
      registerTool(def: unknown) {
        tools.push(def);
      },
      registerCommand(name: string) {
        commands.push(name);
      },
    },
    events,
    tools,
    commands,
    async fire(event: string, payload: { reason?: string }, ctx: FakeContext) {
      for (const handler of events.get(event) ?? []) await handler(payload, ctx);
    },
  };
}

function fakeCtx() {
  const status: Array<[string, string]> = [];
  const notes: Array<[string, string]> = [];
  const ctx: FakeContext = {
    ui: {
      setStatus: (key, text) => status.push([key, text]),
      notify: (text, level) => notes.push([text, level]),
    },
  };
  return { ctx, status, notes };
}

/** Puts a shim named `mm` on PATH for the duration of one test. */
function withMmOnPath(body: string): () => void {
  const dir = mkdtempSync(join(tmpdir(), "mm-path-"));
  const path = join(dir, "mm");
  writeFileSync(path, `#!/bin/sh\n${body}\n`);
  chmodSync(path, 0o755);
  const previous = process.env["PATH"];
  process.env["PATH"] = `${dir}:${previous ?? ""}`;
  return () => {
    process.env["PATH"] = previous;
  };
}

test("the factory registers the lifecycle and the tool surface built so far", () => {
  const pi = fakePi();
  micromanager(pi.api as never);

  assert.ok(pi.events.has("session_start"), "the mm check runs at session start (§2.1)");
  assert.ok(pi.events.has("session_shutdown"), "shutdown is stated, not omitted (§7)");

  // §4.2's whole REQUIRED set: read (T-0178), write (T-0179), workflow
  // (T-0180) and mm_remove (T-0181). The RECOMMENDED set is deliberately not
  // here: whether to register it is a question about the installed mm, so it
  // is answered at session_start (§4.2, and the two tests below).
  const names = pi.tools.map((t) => (t as { name: string }).name).sort();
  assert.deepEqual(names, [
    "mm_add",
    "mm_board",
    "mm_check",
    "mm_describe",
    "mm_edit",
    "mm_find",
    "mm_finish",
    "mm_init",
    "mm_list",
    "mm_move",
    "mm_next",
    "mm_note",
    "mm_pause",
    "mm_remove",
    "mm_show",
    "mm_start",
    "mm_status",
  ]);
  // §5's one command, with the operation as its first argument.
  assert.deepEqual(pi.commands, ["mm"]);
});

test("with mm present, the status line names the build it will drive", async () => {
  const restore = withMmOnPath('echo "mm 1.4.0"');
  resetProbes();
  try {
    const pi = fakePi();
    const { ctx, status, notes } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);

    assert.deepEqual(status, [[STATUS_KEY, "mm 1.4.0"]]);
    assert.equal(notes.length, 0, "a working plugin says nothing at startup");
  } finally {
    restore();
    resetProbes();
  }
});

/**
 * A shim that answers `--version` and `--help` differently.
 *
 * Both probes run at session start, and what the recommended set does depends
 * entirely on the second one, so a shim that echoed the same thing for every
 * argument could not express the case under test.
 */
function mmShim(help: string): string {
  return [
    'case "$1" in',
    '  --version) echo "mm 1.4.0" ;;',
    // %b, not %s: JSON.stringify escapes the newlines, and this is what turns
    // them back into a multi-line help text.
    `  --help) printf '%b' ${JSON.stringify(help)} ;;`,
    "esac",
  ].join("\n");
}

/** The recommended tools of §4.2, in the order they are registered. */
const RECOMMENDED = ["mm_block", "mm_unblock", "mm_search", "mm_report", "mm_tick", "mm_archive"];

test("the recommended set is registered against the installed build (§4.2)", async () => {
  // A build with three of the six operations. --tick and --archive are absent,
  // which is precisely the case §4.2 legislates: "expose only when the
  // installed mm has it".
  const restore = withMmOnPath(
    mmShim(
      "Operations:\n  --add TITLE   add a backlog item\n  --block ID    move to Blocked\n" +
        "  --unblock ID  move back to Ready\n  --search Q    match over titles\n\n" +
        "Global modifiers:\n  --json        one JSON object\n  --report-ish  not an operation\n",
    ),
  );
  resetProbes();
  try {
    const pi = fakePi();
    const { ctx } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);

    const names = pi.tools.map((t) => (t as { name: string }).name);
    assert.deepEqual(
      names.filter((n) => RECOMMENDED.includes(n)),
      ["mm_block", "mm_unblock", "mm_search"],
    );
    // The Global modifiers section is not a source of operations: a modifier
    // that happens to read like one must not register a tool.
    assert.equal(names.includes("mm_report"), false);
  } finally {
    restore();
    resetProbes();
  }
});

test("an unreadable --help exposes the recommended set rather than hiding it (§4.2, §4.3)", async () => {
  // Unknown is not absent. Hiding a tool because a probe could not be read is
  // a worse failure than exposing one that answers exit 2, which the runner
  // already reports as the build's age.
  const restore = withMmOnPath(mmShim("mm — some other build's help, with no operations list\n"));
  resetProbes();
  try {
    const pi = fakePi();
    const { ctx } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);

    const names = pi.tools.map((t) => (t as { name: string }).name);
    for (const tool of RECOMMENDED) {
      assert.ok(names.includes(tool), `${tool} should be exposed when capabilities are unknown`);
    }
    // …and registering twice must not duplicate: session_start can fire again.
    await pi.fire("session_start", { reason: "reload" }, ctx);
    const again = pi.tools.map((t) => (t as { name: string }).name);
    assert.equal(
      again.filter((n) => n === "mm_tick").length,
      1,
      "a reload re-probes; it must not register the same tool twice",
    );
  } finally {
    restore();
    resetProbes();
  }
});

test("with mm missing, it degrades loudly and says what to install", async () => {
  // An empty directory as the whole PATH: nothing to find, which is the
  // condition §2.1 is about.
  const empty = mkdtempSync(join(tmpdir(), "mm-empty-path-"));
  const previous = process.env["PATH"];
  process.env["PATH"] = empty;
  resetProbes();
  try {
    const pi = fakePi();
    const { ctx, status, notes } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);

    assert.deepEqual(status, [[STATUS_KEY, "mm unavailable"]]);
    assert.equal(notes.length, 1, "loudly: exactly one notification, not a silent failure");
    const [text, level] = notes[0]!;
    assert.match(text, /micro-manager/);
    assert.match(text, /go build/, "…and it says how to get mm");
    assert.equal(level, "warning");
  } finally {
    process.env["PATH"] = previous;
    resetProbes();
  }
});

test("session_start never throws, whatever mm does", async () => {
  // Gracefully: an mm that exits non-zero must not take the session down with
  // it. A guest that throws out of a lifecycle handler is not a guest.
  const restore = withMmOnPath("exit 9");
  resetProbes();
  try {
    const pi = fakePi();
    const { ctx, notes } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);
    assert.equal(notes.length, 1);
  } finally {
    restore();
    resetProbes();
  }
});

test("health() is what every later tool gates on", async () => {
  const restore = withMmOnPath('echo "mm 1.4.0"');
  resetProbes();
  try {
    const ok = await health();
    assert.equal(ok.ok, true);
    assert.equal(ok.ok && ok.version, "mm 1.4.0");
  } finally {
    restore();
    resetProbes();
  }

  const empty = mkdtempSync(join(tmpdir(), "mm-empty-path-"));
  const previous = process.env["PATH"];
  process.env["PATH"] = empty;
  resetProbes();
  try {
    const bad = await health();
    assert.equal(bad.ok, false);
    // The message a tool result carries is the same one the user saw at
    // startup: one explanation, not two dialects of it.
    assert.match(!bad.ok ? bad.message : "", /no `mm` on PATH/);
  } finally {
    process.env["PATH"] = previous;
    resetProbes();
  }
});

/*
  §8.1's conformance smoke test: "a test that greps the plugin's source for the
  board filenames and finds none".

  The rule has been narrowed twice, both times because it fired on prose rather
  than on an access — a parameter description saying "working.NN.md", a result
  line saying an item landed in "done.md". Naming a board file is how you TALK
  about a board; opening one is the thing that must never happen. So what is
  asserted is the mechanism rather than the vocabulary:

    1. No plugin module imports node:fs, WITH ONE EXEMPTION. You cannot read or
       write a board without it, and the plugin's one way out to the world is
       spawning mm — whose argv the runner's tests pin. The exemption is
       config.ts, which reads the plugin's OWN config (§9); it is granted by
       name, and paid for by rule 3.
    2. No board filename appears as a PATH: a bare filename literal
       ("backlog.md"), or one after a separator ("details/T-0042.md"). A
       filename inside a sentence is prose and is allowed.
    3. The exempt module opens nothing but its own config file. An exemption
       nobody checks is just a hole.
*/
test("the plugin reaches the board only through mm (§8.1)", async () => {
  const { readdirSync, readFileSync } = await import("node:fs");
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
  assert.ok(checked >= 8, `only ${checked} plugin module(s) were scanned`);
});