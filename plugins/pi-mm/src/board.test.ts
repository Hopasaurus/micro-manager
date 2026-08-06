/*
  Board resolution and the pin (spec-pi-mm-plugin.md §3.2, §3.2.1, §7).

  The interesting cases are the ones where the plugin must NOT act: it must not
  re-resolve once pinned, must not guess between two candidates, and must not
  carry a pin across a branch that did not set it.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  PIN_DETAILS_KEY,
  boardFromEnvelope,
  boardStatusLine,
  clearPin,
  listBoards,
  pinFromBranch,
  pinnedBoard,
  resolveBoard,
  setPin,
} from "./board.ts";

/** A `mm` shim that answers --status and --find with the given envelopes. */
function shim(status: unknown, find: unknown = { ok: true, result: { directories: [] } }, statusExit = 0) {
  const dir = mkdtempSync(join(tmpdir(), "mm-board-"));
  const path = join(dir, "mm");
  const log = join(dir, "argv");
  const q = (v: unknown) => JSON.stringify(v).replaceAll("'", "'\\''");
  writeFileSync(
    path,
    [
      "#!/bin/sh",
      `echo "$*" >> ${log}`,
      `case "$*" in`,
      `  *--find*) printf '%s' '${q(find)}' ;;`,
      `  *) printf '%s' '${q(status)}'; exit ${statusExit} ;;`,
      "esac",
    ].join("\n"),
  );
  chmodSync(path, 0o755);
  return { path, calls: () => readFileSync(log, "utf8").split("\n").filter(Boolean) };
}

const STATUS = (path: string, project = "Runner Board") => ({
  ok: true,
  operation: "status",
  directory: { path, project, wipLimit: 1, wipUsed: 0 },
  result: { counts: { ready: 1 }, wip: { limit: 1, used: 0 }, path, project },
});

test("resolution reads the board out of the first envelope and pins it (§3.2)", async (t) => {
  t.after(clearPin);
  clearPin();
  const mm = shim(STATUS("/boards/one"));

  const first = await resolveBoard({ command: mm.path });
  assert.equal(first.ok, true);
  if (!first.ok) return;
  assert.equal(first.pin.path, "/boards/one");
  assert.equal(first.pin.project, "Runner Board");
  assert.equal(pinnedBoard()?.path, "/boards/one");

  // Pinned means pinned: a second resolution runs nothing at all, which is
  // what stops a mid-session re-resolution flipping boards (§3.2).
  const before = mm.calls().length;
  const second = await resolveBoard({ command: mm.path });
  assert.equal(second.ok, true);
  assert.equal(mm.calls().length, before, "a pinned board is not re-resolved");
});

test("the first resolution asks mm with no --dir, so mm applies MM_DIR then discovery", async (t) => {
  t.after(clearPin);
  clearPin();
  const mm = shim(STATUS("/boards/one"));
  await resolveBoard({ command: mm.path });
  assert.deepEqual(mm.calls(), ["--status --json"], "no --dir: resolution is mm's to do");
});

test("a path resolves that board and marks the source as a pin", async (t) => {
  t.after(clearPin);
  clearPin();
  const mm = shim(STATUS("/boards/two", "Two"));
  const res = await resolveBoard({ command: mm.path, path: "/boards/two" });
  assert.equal(res.ok, true);
  if (!res.ok) return;
  assert.equal(res.pin.source, "pin");
  assert.match(mm.calls()[0] ?? "", /--dir \/boards\/two/);
});

test("MM_DIR is named as the source only when it IS the resolved board (§3.2.1)", async (t) => {
  t.after(clearPin);
  const previous = process.env["MM_DIR"];
  try {
    clearPin();
    process.env["MM_DIR"] = "/boards/one";
    const same = await resolveBoard({ command: shim(STATUS("/boards/one")).path });
    assert.equal(same.ok && same.pin.source, "MM_DIR");

    clearPin();
    process.env["MM_DIR"] = "/boards/elsewhere";
    const other = await resolveBoard({ command: shim(STATUS("/boards/one")).path });
    // Saying MM_DIR whenever it is merely SET would be a guess, and a wrong
    // one whenever discovery overrode nothing.
    assert.equal(other.ok && other.pin.source, "discovery");
  } finally {
    if (previous === undefined) delete process.env["MM_DIR"];
    else process.env["MM_DIR"] = previous;
    clearPin();
  }
});

test("ambiguity surfaces the candidates and refuses to choose (§3.2)", async (t) => {
  t.after(clearPin);
  clearPin();
  const mm = shim(
    { ok: false, errors: [{ code: "Ambiguous", message: "two directories match: /a, /b" }] },
    {
      ok: true,
      result: {
        directories: [
          { path: "/a", project: "A" },
          { path: "/b", project: "B" },
        ],
      },
    },
    3,
  );

  const res = await resolveBoard({ command: mm.path });
  assert.equal(res.ok, false);
  if (res.ok) return;
  assert.equal(res.reason, "ambiguous");
  assert.deepEqual(
    res.candidates.map((c) => c.path),
    ["/a", "/b"],
  );
  assert.equal(pinnedBoard(), undefined, "nothing was pinned; the plugin must not guess");
});

test("mm --find lists boards and marks the one in use; empty is a result (§3.2.1)", async (t) => {
  t.after(clearPin);
  clearPin();
  const mm = shim(STATUS("/boards/one"), {
    ok: true,
    result: {
      directories: [
        { path: "/boards/one", project: "One" },
        { path: "/boards/two", project: "Two" },
      ],
    },
  });
  await resolveBoard({ command: mm.path });

  const boards = await listBoards({ command: mm.path });
  assert.equal(boards.length, 2);
  assert.equal(boards[0]?.inUse, true, "the pinned board is marked");
  assert.equal(boards[1]?.inUse, false);

  const none = await listBoards({ command: shim(STATUS("/x"), { ok: true, result: { directories: [] } }).path });
  assert.deepEqual(none, [], "no boards is an empty list, not an error");
});

test("--init's board is read from result, not directory", () => {
  // The one envelope whose shape differs: there is no directory to describe
  // until --init has made one (pinned by the runner's contract test).
  const pin = boardFromEnvelope(
    { ok: true, operation: "init", result: { path: "/boards/new", project: "New" } },
    "pin",
  );
  assert.equal(pin?.path, "/boards/new");
  assert.equal(pin?.project, "New");
});

/* --------------------------------------------------------- branch state */

const toolResult = (toolName: string, details: Record<string, unknown>) => ({
  type: "message",
  message: { role: "toolResult", toolName, details },
});

test("the pin is reconstructed from the active branch (§7)", () => {
  const branch = [
    { type: "message", message: { role: "user", content: "hello" } },
    toolResult("mm_status", { [PIN_DETAILS_KEY]: { path: "/boards/one", project: "One", source: "discovery" } }),
    toolResult("mm_list", { [PIN_DETAILS_KEY]: { path: "/boards/ignored", source: "pin" } }),
    toolResult("mm_board", { [PIN_DETAILS_KEY]: { path: "/boards/two", project: "Two", source: "pin" } }),
  ];
  const pin = pinFromBranch(branch);
  // The LAST pin-bearing result wins: a session that re-pinned mid-branch
  // means the later one. mm_list is not pin-bearing, so it is skipped.
  assert.equal(pin?.path, "/boards/two");
  assert.equal(pin?.source, "pin");
});

test("a branch that never pinned reconstructs nothing", () => {
  assert.equal(pinFromBranch([]), undefined);
  assert.equal(pinFromBranch([{ type: "message", message: { role: "assistant" } }]), undefined);
  // Garbage in details is ignored rather than trusted: a transcript can hold
  // anything an older build wrote.
  assert.equal(pinFromBranch([toolResult("mm_status", { [PIN_DETAILS_KEY]: { nope: 1 } })]), undefined);
});

test("the status line names the board, and empties when there is none", () => {
  assert.equal(boardStatusLine({ path: "/boards/one", project: "One", source: "pin" }), "One — /boards/one");
  assert.equal(boardStatusLine({ path: "/boards/one", source: "pin" }), "/boards/one");
  // Cleared rather than stale: a board name that is no longer true is worse
  // than no board name (§3.2.1).
  assert.equal(boardStatusLine(undefined), "");
});

test("setPin and clearPin are what the session lifecycle drives", (t) => {
  t.after(clearPin);
  setPin({ path: "/boards/one", source: "pin" });
  assert.equal(pinnedBoard()?.path, "/boards/one");
  clearPin();
  assert.equal(pinnedBoard(), undefined);
});
