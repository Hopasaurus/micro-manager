/*
  The capability probe (spec-pi-mm-plugin.md §4.2).

  Two things are worth asserting here and they pull in opposite directions:
  that a listed operation is FOUND — otherwise the gate hides tools the build
  has — and that an unreadable answer is UNKNOWN rather than empty, because
  "known: false, so expose everything" is what keeps the gate an optimisation
  over exit 2 instead of a second, weaker version of it.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  capabilities,
  hasOperation,
  parseOperations,
  probeCapabilities,
  resetCapabilities,
  UNKNOWN_CAPABILITIES,
} from "./capabilities.ts";

/** The Go build's own help, trimmed to the shape that matters. */
const REAL_HELP = `mm — micro-manager, a plain-markdown todo system

usage: mm --OPERATION [SUBJECT] [modifiers]

Operations:
  --init                    create a directory (needs --project NAME)
  --add TITLE               add a backlog item
  --block ID --reason T     move to Blocked with a reason
  --unblock ID              move back to Ready and drop the reason
  --search QUERY            substring or regex match over titles, tags, details
  --archive                 roll old month groups out of done.md
  --tick                    fire due someday schedules (tickler)
  --help, --version

Global modifiers:
  --dir PATH                act on this directory
  --json                    one JSON object on stdout, on success and failure

Environment:
  MM_REPORT_PERIOD          default --report period
`;

test("the Operations section is what names an operation", () => {
  const caps = parseOperations(REAL_HELP);
  assert.equal(caps.known, true);
  for (const op of ["init", "add", "block", "unblock", "search", "archive", "tick"]) {
    assert.ok(caps.operations.has(op), `--${op} was listed and should be found`);
  }
  // Modifiers are not operations, even though they are spelled the same way.
  assert.equal(caps.operations.has("dir"), false);
  assert.equal(caps.operations.has("json"), false);
  /*
    …and neither is a mention in prose. "MM_REPORT_PERIOD  default --report
    period" is the reason this parse is section-scoped: a whole-text scan would
    register mm_report against a build that has no --report at all.
  */
  assert.equal(caps.operations.has("report"), false);
});

test("a help text with no operations list is unknown, not empty", () => {
  for (const help of ["", "usage: mm [options]\n", "some other build entirely\n"]) {
    const caps = parseOperations(help);
    assert.equal(caps.known, false, JSON.stringify(help));
    // Unknown means every gate opens: exit 2 is the backstop (§4.3).
    assert.equal(hasOperation(caps, "tick"), true);
    assert.equal(hasOperation(caps, "not-an-operation-at-all"), true);
  }
});

test("a known build gates on what it listed", () => {
  const caps = parseOperations(REAL_HELP);
  assert.equal(hasOperation(caps, "tick"), true);
  assert.equal(hasOperation(caps, "report"), false, "not listed means not exposed");
});

test("an Operations section that yields nothing counts as unknown", () => {
  // A section header followed by prose rather than switches: the plugin cannot
  // read this build's help, which is not the same as the build having no
  // operations.
  const caps = parseOperations("Operations:\n  everything, obviously\n");
  assert.deepEqual(caps, UNKNOWN_CAPABILITIES);
});

/* --------------------------------------------------------- the subprocess */

function shim(body: string): { command: string; restore: () => void } {
  const dir = mkdtempSync(join(tmpdir(), "mm-caps-"));
  const path = join(dir, "mm");
  writeFileSync(path, `#!/bin/sh\n${body}\n`);
  chmodSync(path, 0o755);
  return { command: path, restore: () => {} };
}

test("the probe reads a real subprocess's help", async () => {
  const { command } = shim(`printf '%b' ${JSON.stringify(REAL_HELP)}`);
  const caps = await probeCapabilities(command, 5_000);
  assert.equal(caps.known, true);
  assert.ok(caps.operations.has("tick"));
});

test("a build that prints its help to stderr is still read", async () => {
  const { command } = shim(`printf '%b' ${JSON.stringify(REAL_HELP)} >&2`);
  const caps = await probeCapabilities(command, 5_000);
  assert.equal(caps.known, true, "guessing which stream help goes to would hide every tool");
});

test("a probe that cannot run leaves the answer unknown", async () => {
  const caps = await probeCapabilities(join(tmpdir(), "definitely-not-mm"), 2_000);
  assert.deepEqual(caps, UNKNOWN_CAPABILITIES);
});

test("a probe that never answers is bounded, and unknown", async () => {
  // exec, so the shell IS the sleep: killing a shell that merely started one
  // leaves the child holding this process's stdout pipe open, and the test run
  // then waits out the sleep it just proved it does not wait for.
  const { command } = shim("exec sleep 30");
  const started = Date.now();
  const caps = await probeCapabilities(command, 250);
  assert.deepEqual(caps, UNKNOWN_CAPABILITIES);
  assert.ok(Date.now() - started < 5_000, "the probe must not wait for a hung mm (§2.2)");
});

test("the answer is cached for the session, and droppable", async () => {
  resetCapabilities();
  const { command } = shim(`printf '%b' ${JSON.stringify(REAL_HELP)}`);
  const first = await capabilities(command);
  const second = await capabilities(command);
  assert.equal(first, second, "one probe per session; the second call is the cache");
  resetCapabilities();
  const third = await capabilities(command);
  assert.notEqual(first, third, "reset re-probes, which is what /reload needs");
  resetCapabilities();
});
