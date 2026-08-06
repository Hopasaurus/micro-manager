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
import { resetPresence } from "./presence.ts";

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

test("the factory registers lifecycle handlers and nothing else yet", () => {
  const pi = fakePi();
  micromanager(pi.api as never);

  assert.ok(pi.events.has("session_start"), "the mm check runs at session start (§2.1)");
  assert.ok(pi.events.has("session_shutdown"), "shutdown is stated, not omitted (§7)");
  // The tool surface is T-0178 onward. A skeleton that registered half a tool
  // would be worse than one that registers none.
  assert.equal(pi.tools.length, 0);
  assert.equal(pi.commands.length, 0);
});

test("with mm present, the status line names the build it will drive", async () => {
  const restore = withMmOnPath('echo "mm 1.4.0"');
  resetPresence();
  try {
    const pi = fakePi();
    const { ctx, status, notes } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);

    assert.deepEqual(status, [[STATUS_KEY, "mm 1.4.0"]]);
    assert.equal(notes.length, 0, "a working plugin says nothing at startup");
  } finally {
    restore();
    resetPresence();
  }
});

test("with mm missing, it degrades loudly and says what to install", async () => {
  // An empty directory as the whole PATH: nothing to find, which is the
  // condition §2.1 is about.
  const empty = mkdtempSync(join(tmpdir(), "mm-empty-path-"));
  const previous = process.env["PATH"];
  process.env["PATH"] = empty;
  resetPresence();
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
    resetPresence();
  }
});

test("session_start never throws, whatever mm does", async () => {
  // Gracefully: an mm that exits non-zero must not take the session down with
  // it. A guest that throws out of a lifecycle handler is not a guest.
  const restore = withMmOnPath("exit 9");
  resetPresence();
  try {
    const pi = fakePi();
    const { ctx, notes } = fakeCtx();
    micromanager(pi.api as never);
    await pi.fire("session_start", { reason: "startup" }, ctx);
    assert.equal(notes.length, 1);
  } finally {
    restore();
    resetPresence();
  }
});

test("health() is what every later tool gates on", async () => {
  const restore = withMmOnPath('echo "mm 1.4.0"');
  resetPresence();
  try {
    const ok = await health();
    assert.equal(ok.ok, true);
    assert.equal(ok.ok && ok.version, "mm 1.4.0");
  } finally {
    restore();
    resetPresence();
  }

  const empty = mkdtempSync(join(tmpdir(), "mm-empty-path-"));
  const previous = process.env["PATH"];
  process.env["PATH"] = empty;
  resetPresence();
  try {
    const bad = await health();
    assert.equal(bad.ok, false);
    // The message a tool result carries is the same one the user saw at
    // startup: one explanation, not two dialects of it.
    assert.match(!bad.ok ? bad.message : "", /no `mm` on PATH/);
  } finally {
    process.env["PATH"] = previous;
    resetPresence();
  }
});

/*
  §8.1's conformance smoke test, worth having from the first commit: "a test
  that greps the plugin's source for the board filenames and finds none".

  Two greps, because the filename one alone is both too weak and too strong —
  too weak (a plugin could read backlog.md through a path it assembled) and too
  strong (prose describing the board is not a file access). So:

    1. no plugin module imports node:fs at all. You cannot read or write a
       board without it, and the plugin's one legitimate way out to the world
       is spawning mm.
    2. no board filename appears inside a STRING LITERAL. Comments may say what
       a board is made of; code may not name its files.
*/
test("the plugin reaches the board only through mm (§8.1)", async () => {
  const { readdirSync, readFileSync } = await import("node:fs");
  const dir = new URL(".", import.meta.url).pathname;
  const names = ["backlog\\.md", "done\\.md", "working\\.[0-9N]", "details/"];

  let checked = 0;
  for (const name of readdirSync(dir)) {
    if (!name.endsWith(".ts") || name.endsWith(".test.ts")) continue;
    checked += 1;
    const source = readFileSync(join(dir, name), "utf8");

    assert.doesNotMatch(
      source,
      /from\s+["']node:fs(\/promises)?["']|require\(["']node:fs/,
      `${name} imports node:fs; the plugin never touches board files (§2.1, §8.1)`,
    );
    for (const needle of names) {
      const inAString = new RegExp(`["'\`][^"'\`\n]*${needle}`);
      assert.doesNotMatch(
        source,
        inAString,
        `${name} names a board file in a string literal; go through mm (§2.1, §8.1)`,
      );
    }
  }
  // A smoke test that silently covered nothing is the failure mode this file
  // cannot afford (the same reason the Go side logs its fixture counts).
  assert.ok(checked >= 2, `only ${checked} plugin module(s) were scanned`);
});
