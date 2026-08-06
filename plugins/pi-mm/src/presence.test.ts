/*
  The mm-on-PATH check (spec-pi-mm-plugin.md §2.1).

  Every case here is a real subprocess against a shim written to a temp
  directory, because what is being tested is exactly the part a fake would
  paper over: what happens when spawning a program goes wrong. A stub that
  "returns not-found" proves nothing about ENOENT.

  Tests run on plain node — `node --test src/*.test.ts` — with no test
  framework and no build step. The plugin's only runtime dependency is typebox
  (§3.1), and its tests keep the same discipline.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { presence, presenceMessage, probe, resetPresence } from "./presence.ts";

/** Writes an executable shim and returns its path. */
function shim(body: string): string {
  const dir = mkdtempSync(join(tmpdir(), "mm-presence-"));
  const path = join(dir, "mm");
  writeFileSync(path, `#!/bin/sh\n${body}\n`);
  chmodSync(path, 0o755);
  return path;
}

test("a working mm reports its version", async () => {
  const path = shim('echo "mm 1.4.0"');
  const p = await probe(path);
  assert.equal(p.ok, true);
  assert.equal(p.ok && p.version, "mm 1.4.0");
});

test("a version on stderr still counts", async () => {
  // Nothing in spec-tools.md §9 fixes which stream --version uses, so a build
  // that answers on stderr is conforming and must not read as broken.
  const path = shim('echo "mm 1.4.0" >&2');
  const p = await probe(path);
  assert.equal(p.ok, true);
  assert.equal(p.ok && p.version, "mm 1.4.0");
});

test("multi-line output keeps only the first line", async () => {
  const path = shim('printf "mm 1.4.0\\nbuilt from source\\n"');
  const p = await probe(path);
  assert.equal(p.ok && p.version, "mm 1.4.0");
});

test("no mm on PATH is not-found, not a crash", async () => {
  const p = await probe(join(tmpdir(), "definitely-not-a-real-binary-mm"));
  assert.equal(p.ok, false);
  assert.equal(!p.ok && p.reason, "not-found");
});

test("an mm that fails is failed, and carries why", async () => {
  const path = shim('echo "boom" >&2; exit 3');
  const p = await probe(path);
  assert.equal(p.ok, false);
  assert.equal(!p.ok && p.reason, "failed");
  assert.match(!p.ok ? p.detail : "", /exit 3/);
  assert.match(!p.ok ? p.detail : "", /boom/);
});

test("an mm that hangs is a timeout, not a hung session", async () => {
  const path = shim("sleep 30");
  const started = Date.now();
  const p = await probe(path, 150);
  assert.equal(p.ok, false);
  assert.equal(!p.ok && p.reason, "timeout");
  assert.ok(Date.now() - started < 5_000, "the probe returned without waiting for the child");
});

test("the message names the remedy, whichever way it failed", () => {
  for (const reason of ["not-found", "failed", "timeout"] as const) {
    const msg = presenceMessage({ ok: false, reason, detail: "" });
    // §2.1: "the plugin says what to install rather than pretending".
    assert.match(msg, /go build/, `${reason} should say how to get mm`);
    assert.match(msg, /PATH/, `${reason} should say where to put it`);
    assert.match(msg, /reload/, `${reason} should say how to pick it up`);
  }
  assert.equal(presenceMessage({ ok: true, version: "mm 1.4.0" }), "mm 1.4.0");
});

test("the answer is probed once per session and reset explicitly", async () => {
  // A counting shim: each run appends a line, so the file length is the number
  // of probes that actually happened.
  const dir = mkdtempSync(join(tmpdir(), "mm-presence-count-"));
  const counter = join(dir, "runs");
  const path = join(dir, "mm");
  writeFileSync(path, `#!/bin/sh\necho x >> ${counter}\necho "mm 1.4.0"\n`);
  chmodSync(path, 0o755);
  writeFileSync(counter, "");

  resetPresence();
  const first = await presence(path);
  const second = await presence(path);
  assert.equal(first.ok, true);
  assert.equal(second.ok, true);
  assert.equal(runs(counter), 1, "the second caller reuses the cached answer");

  resetPresence();
  await presence(path);
  assert.equal(runs(counter), 2, "a reset probes again — a reload is how a new mm is noticed");
  resetPresence();
});

function runs(file: string): number {
  return readFileSync(file, "utf8").split("\n").filter(Boolean).length;
}
