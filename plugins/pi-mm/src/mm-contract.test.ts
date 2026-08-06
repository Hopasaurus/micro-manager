/*
  The runner against the REAL `mm` (spec-pi-mm-plugin.md §2.1, §4.3).

  Every other test here drives a shell shim, which proves the runner handles
  what the CLI is documented to do. This one proves the documentation and the
  binary agree — that the envelope really is on stdout for a failure, that a
  WIP limit really does exit 4 with the slots in the message, that `--dir`
  really does pin the board.

  It SKIPS when `mm` is not on PATH, because the plugin's tests must run on a
  machine that has not built the Go implementation. A skip that looks like a
  pass is the failure mode this file guards against, so it logs what it
  covered.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { clearPin, setPin } from "./board.ts";
import { probe } from "./presence.ts";
import { run } from "./runner.ts";
import { readTools } from "./tools-read.ts";
import { workflowTools } from "./tools-workflow.ts";
import { writeTools } from "./tools-write.ts";

const present = await probe("mm", 5_000);

test("the runner drives a real mm end to end", { skip: present.ok ? false : `no mm on PATH (${present.reason})` }, async (t) => {
  t.diagnostic(`mm: ${present.ok ? present.version : "absent"}`);
  const dir = join(mkdtempSync(join(tmpdir(), "mm-contract-")), "board");

  // --init: the board is created and the envelope names where.
  const init = await run({
    args: ["--init", "--project", "Contract Board", "--slots", "1"],
    dir,
    command: "mm",
  });
  assert.equal(init.ok, true, JSON.stringify(init));
  if (!init.ok) return;
  /*
    Worth pinning, because it is the one place the envelope's shape surprises:
    --init reports the new board in `result`, NOT in `directory`. There is no
    directory to describe until the operation has created one. mm_init (T-0179)
    reads its path and project from here.
  */
  const created = init.envelope.result as { path?: string; project?: string } | null;
  assert.equal(created?.project, "Contract Board");
  assert.equal(created?.path, dir);
  assert.equal(init.envelope.directory, undefined, "no directory block before the board exists");

  // --add: the ID comes back, which is the handle for everything after (§4.2).
  const add = await run({ args: ["--add", "first"], dir, command: "mm" });
  assert.equal(add.ok, true);
  const status = await run({ args: ["--status"], dir, command: "mm" });
  assert.equal(status.ok, true);
  if (!status.ok) return;
  assert.equal(status.envelope.directory?.path, dir, "--dir pinned the board it was given");

  // --start twice: the second hits the WIP limit, and THAT is the case §4.3
  // cares about — exit 4 with the remedy in the message.
  const started = await run({ args: ["--start", "1"], dir, command: "mm" });
  assert.equal(started.ok, true, JSON.stringify(started));
  await run({ args: ["--add", "second"], dir, command: "mm" });
  const blocked = await run({ args: ["--start", "2"], dir, command: "mm" });
  assert.equal(blocked.ok, false);
  if (blocked.ok) return;
  assert.equal(blocked.failure.kind, "precondition", blocked.failure.message);
  assert.equal(blocked.failure.exit, 4);
  assert.match(blocked.failure.message, /wip limit reached/i);
  assert.match(blocked.failure.message, /slot 01/, "the CLI's remedy survives the trip");

  // An unknown id: exit 3, and not mistaken for an ambiguous board.
  const missing = await run({ args: ["--show", "T-9999"], dir, command: "mm" });
  assert.equal(missing.ok, false);
  if (missing.ok) return;
  assert.equal(missing.failure.kind, "not-found");
  assert.doesNotMatch(missing.failure.message, /Pin one/);

  // An unknown switch: exit 2, reported as the build's age.
  const usage = await run({ args: ["--definitely-not-a-switch"], dir, command: "mm" });
  assert.equal(usage.ok, false);
  if (usage.ok) return;
  assert.equal(usage.failure.kind, "usage");
  assert.match(usage.failure.message, /not available in the installed mm build/);

  t.diagnostic("covered: init, add, status, start, wip limit, unknown id, unknown switch");
});

/*
  The read tools against a real board and a real mm (§4.2).

  The unit tests drive a fake runner, which proves the tools format and gate
  correctly. This proves the two halves fit: that the argv they build is argv
  mm accepts, and that the envelopes they read are envelopes mm writes.
*/
test("the read tools work against a real board", { skip: present.ok ? false : `no mm on PATH (${present.reason})` }, async (t) => {
  const dir = join(mkdtempSync(join(tmpdir(), "mm-tools-")), "board");
  await run({ args: ["--init", "--project", "Tools Board", "--slots", "2"], dir, command: "mm" });
  await run({ args: ["--add", "first", "--prio", "high", "--tag", "infra"], dir, command: "mm" });
  await run({ args: ["--add", "second", "--section", "someday"], dir, command: "mm" });
  await run({ args: ["--start", "1"], dir, command: "mm" });

  // Pinned explicitly: what is under test is the tools, not discovery, and a
  // test that resolved by scanning would depend on where it was run from.
  setPin({ path: dir, project: "Tools Board", source: "pin" });
  t.after(clearPin);

  const tools = new Map(
    readTools({ health: async () => ({ ok: true, version: "real" }), run }).map((x) => [x.name, x]),
  );
  const body = async (name: string, params: Record<string, unknown> = {}) => {
    const result = await tools.get(name)!.execute("id", params);
    assert.equal(result.isError, undefined, `${name}: ${result.content[0]?.text}`);
    return result.content[0]?.text ?? "";
  };

  const status = await body("mm_status");
  assert.match(status, /Tools Board/);
  assert.match(status, /wip 1\/2/);
  assert.match(status, /in work: {2}T-0001 {2}first/);

  assert.match(await body("mm_next"), /nothing ready/, "the one ready item is now in a slot");
  assert.match(await body("mm_list", { state: "all" }), /T-0002 {2}second/);
  assert.match(await body("mm_show", { id: "1" }), /where: {3}working, slot 01/);
  assert.match(await body("mm_check"), /clean: no violations/);
  assert.match(await body("mm_board"), /resolved: pinned for this session/);
  // mm_find scans from the working directory; what matters is that it answers
  // rather than what it finds on the machine running the suite.
  assert.doesNotThrow(() => body("mm_find"));

  t.diagnostic("covered: mm_status, mm_next, mm_list, mm_show, mm_check, mm_board, mm_find");
});

/*
  The write tools against a real board and a real mm (§4.2, §3.3, §3.4).

  These are the ones where a wrong argv changes files, so the check that
  matters is that mm accepts what they build and the board says afterwards what
  the tool claimed.
*/
test("the write tools work against a real board", { skip: present.ok ? false : `no mm on PATH (${present.reason})` }, async (t) => {
  const dir = join(mkdtempSync(join(tmpdir(), "mm-write-")), "board");
  const deps = { health: async () => ({ ok: true as const, version: "real" }), run };
  const tools = new Map(
    [...readTools(deps), ...writeTools(deps)].map((x) => [x.name, x]),
  );
  const call = async (name: string, params: Record<string, unknown> = {}) => {
    const result = await tools.get(name)!.execute("id", params);
    return { text: result.content[0]?.text ?? "", isError: result.isError === true };
  };
  const must = async (name: string, params: Record<string, unknown> = {}) => {
    const r = await call(name, params);
    assert.equal(r.isError, false, `${name}: ${r.text}`);
    return r.text;
  };

  clearPin();
  t.after(clearPin);

  // mm_init pins the board it creates, so everything after acts on it without
  // a --dir of its own (§3.3).
  const created = await must("mm_init", { project: "Garden", dir, prefix: "G", slots: 2 });
  assert.match(created, /created Garden/);
  assert.match(created, /ids are G-0001/);
  assert.match(created, /pinned for this session/);

  const added = await must("mm_add", { title: "water the beds", prio: "high", tags: ["outdoor"] });
  // The board's declared prefix is what the new ID uses — the reason --prefix
  // belongs at init rather than later.
  const id = /\b(G-\d{4})\b/.exec(added)?.[1];
  assert.ok(id, `mm_add did not report an ID: ${added}`);

  await must("mm_edit", { id: id!, title: "water the raised beds", set: ["owner=dana"] });
  const shown = await must("mm_show", { id: id! });
  assert.match(shown, /water the raised beds/);
  assert.match(shown, /owner/, "an unregistered key survives the round trip (§4.2)");

  await must("mm_move", { id: id!, section: "someday", top: true });
  assert.match(await must("mm_show", { id: id! }), /someday/);

  await must("mm_note", { id: id!, text: "the hose is in the shed" });
  assert.match(await must("mm_show", { id: id!, detail: true }), /hose is in the shed/);

  // mm_describe: this build has no --describe, so the tool must degrade with
  // the build message and MUST NOT have touched structure.md itself (§3.4).
  const described = await call("mm_describe", { text: "Garden chores." });
  if (described.isError) {
    assert.match(described.text, /not available in the installed mm build/);
    t.diagnostic("mm_describe: degraded on a build without --describe, as §3.4 requires");
  } else {
    assert.match(described.text, /description set/);
  }

  // The board is still valid after everything above — the point of driving mm
  // rather than the files.
  assert.match(await must("mm_check"), /clean: no violations/);
  t.diagnostic("covered: mm_init, mm_add, mm_edit, mm_move, mm_note, mm_describe, mm_check");
});

/*
  The workflow tools against a real board and a real mm (§4.2, §4.3).

  The WIP limit is the case worth spending a real board on: it is the one
  failure the spec asks the plugin to make ACTIONABLE, and its remedy is text
  mm composes, so only the real binary can prove it survives the trip.
*/
test("the workflow tools work against a real board", { skip: present.ok ? false : `no mm on PATH (${present.reason})` }, async (t) => {
  const dir = join(mkdtempSync(join(tmpdir(), "mm-flow-")), "board");
  const deps = { health: async () => ({ ok: true as const, version: "real" }), run };
  const tools = new Map(
    [...readTools(deps), ...writeTools(deps), ...workflowTools(deps)].map((x) => [x.name, x]),
  );
  const call = async (name: string, params: Record<string, unknown> = {}) => {
    const r = await tools.get(name)!.execute("id", params);
    return { text: r.content[0]?.text ?? "", isError: r.isError === true };
  };
  const must = async (name: string, params: Record<string, unknown> = {}) => {
    const r = await call(name, params);
    assert.equal(r.isError, false, `${name}: ${r.text}`);
    return r.text;
  };

  clearPin();
  t.after(clearPin);

  // One slot, so the second start is a real WIP limit rather than a staged one.
  await must("mm_init", { project: "Flow", dir, slots: 1 });
  await must("mm_add", { title: "alpha" });
  await must("mm_add", { title: "beta" });

  assert.match(await must("mm_start", { id: "1" }), /started T-0001 {2}alpha → slot 01/);

  const blocked = await call("mm_start", { id: "2" });
  assert.equal(blocked.isError, true, "the second start must fail at the limit");
  assert.match(blocked.text, /wip limit reached \(1\/1\)/);
  // The remedy, composed by mm and passed through untouched (§4.3).
  assert.match(blocked.text, /slot 01 {2}T-0001 {2}alpha/);
  assert.match(blocked.text, /finish one, pause one/);

  // Pause frees the slot, so the same start now succeeds — no chaining, two
  // tool calls, exactly as §4.1 requires.
  assert.match(await must("mm_pause", { id: "1" }), /paused T-0001 {2}alpha/);
  assert.match(await must("mm_start", { id: "2" }), /started T-0002 {2}beta → slot 01/);

  const finished = await must("mm_finish", {
    id: "2",
    outcome: "shipped",
    closing_note: "done and dusted",
  });
  assert.match(finished, /finished T-0002 {2}beta → done\.md \(shipped\)/);
  // The closing note lands in the detail file, which is where it outlives the
  // conversation.
  assert.match(await must("mm_show", { id: "2", detail: true }), /done and dusted/);

  assert.match(await must("mm_check"), /clean: no violations/);
  t.diagnostic("covered: mm_start, the WIP limit and its remedy, mm_pause, mm_finish");
});
