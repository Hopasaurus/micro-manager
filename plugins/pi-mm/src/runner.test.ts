/*
  The mm runner (spec-pi-mm-plugin.md §4.1, §4.3, Appendix C).

  The envelopes below are REAL — captured from the Go `mm` while writing this,
  not invented — because the whole runner is a claim about what that CLI does.
  A fixture that guessed at the shape would let the runner agree with the guess
  instead of with the tool.

  The shims are `#!/bin/sh` files that print one of those envelopes and exit
  with the matching code. That keeps the tests honest about subprocess
  behaviour (a mocked `spawn` cannot fail with ENOENT) without needing a built
  Go binary on the machine running them.
*/

import test from "node:test";
import assert from "node:assert/strict";
import { chmodSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { DEFAULT_TIMEOUT_MS, buildArgs, run } from "./runner.ts";

/* ------------------------------------------------------------ fixtures */

// mm --status --json on a fresh board.
const OK_ENVELOPE = JSON.stringify({
  ok: true,
  operation: "status",
  directory: {
    path: "/boards/rb",
    projectId: "755a218e39da",
    project: "Runner Board",
    nextId: "T-0003",
    wipLimit: 1,
    wipUsed: 1,
  },
  result: { counts: { blocked: 0, done: 0, ready: 1, someday: 0 } },
  changes: [],
  warnings: [],
  errors: [],
});

// mm --start T-0002 --json with the one slot occupied. The message is the
// CLI's own, newlines and all: §4.3 says the plugin surfaces the remedy the
// CLI names, and this is what that remedy looks like.
const WIP_ENVELOPE = JSON.stringify({
  ok: false,
  operation: "start",
  directory: { path: "/boards/rb", project: "Runner Board", wipLimit: 1, wipUsed: 1 },
  result: null,
  changes: [],
  warnings: [],
  errors: [
    {
      code: "WipLimitReached",
      message:
        "wip limit reached (1/1)\n  slot 01  T-0001  one\nfinish one, pause one, or raise the limit with --wip",
      id: "T-0002",
    },
  ],
});

// mm --nonsense --json.
const USAGE_ENVELOPE = JSON.stringify({
  ok: false,
  operation: "",
  result: null,
  changes: [],
  warnings: [],
  errors: [{ code: "InvalidArgument", message: "unknown switch --nonsense" }],
});

/** Writes an `mm` shim that prints `body` and exits `code`. */
function shim(body: string, code = 0): string {
  const dir = mkdtempSync(join(tmpdir(), "mm-runner-"));
  const path = join(dir, "mm");
  const quoted = body.replaceAll("'", "'\\''");
  writeFileSync(path, `#!/bin/sh\nprintf '%s' '${quoted}'\nexit ${code}\n`);
  chmodSync(path, 0o755);
  return path;
}

/** An `mm` shim that records its argv, one argument per line. */
function argvShim(): { path: string; argv: () => string[] } {
  const dir = mkdtempSync(join(tmpdir(), "mm-argv-"));
  const path = join(dir, "mm");
  const log = join(dir, "argv");
  writeFileSync(
    path,
    `#!/bin/sh\nfor a in "$@"; do echo "$a" >> ${log}; done\nprintf '%s' '${OK_ENVELOPE.replaceAll("'", "'\\''")}'\n`,
  );
  chmodSync(path, 0o755);
  return {
    path,
    argv: () => readFileSync(log, "utf8").split("\n").filter(Boolean),
  };
}

/* ---------------------------------------------------------------- argv */

test("every invocation carries --json, and --dir once pinned (§4.1)", async () => {
  assert.deepEqual(buildArgs(["--status"]), ["--status", "--json"]);
  assert.deepEqual(buildArgs(["--status"], "/boards/rb"), [
    "--status",
    "--json",
    "--dir",
    "/boards/rb",
  ]);

  const mm = argvShim();
  await run({ args: ["--show", "T-0001", "--detail"], dir: "/boards/rb", command: mm.path });
  assert.deepEqual(mm.argv(), ["--show", "T-0001", "--detail", "--json", "--dir", "/boards/rb"]);
});

test("a caller may not smuggle in --json or --dir", () => {
  // A plugin bug, not a user error: the runner owns both, and two of either
  // would mean two callers disagree about which board is being written.
  assert.throws(() => buildArgs(["--status", "--json"]), /runner owns --json/);
  assert.throws(() => buildArgs(["--status", "--dir", "/x"]), /runner owns --dir/);
  assert.throws(() => buildArgs(["--status", "--dir=/x"]), /runner owns --dir=/);
});

test("one tool call is exactly one subprocess (§4.1, §8.4)", async () => {
  // Counting the runs is how "never retry a failed mutation automatically"
  // gets asserted: an absence of retry logic is invisible, a second spawn is
  // not.
  const dir = mkdtempSync(join(tmpdir(), "mm-count-"));
  const path = join(dir, "mm");
  const counter = join(dir, "runs");
  writeFileSync(
    path,
    `#!/bin/sh\necho x >> ${counter}\nprintf '%s' '${WIP_ENVELOPE.replaceAll("'", "'\\''")}'\nexit 4\n`,
  );
  chmodSync(path, 0o755);
  writeFileSync(counter, "");

  const outcome = await run({ args: ["--start", "T-0002"], command: path });
  assert.equal(outcome.ok, false);
  assert.equal(readFileSync(counter, "utf8").split("\n").filter(Boolean).length, 1);
});

/* ------------------------------------------------------------ outcomes */

test("exit 0 returns the parsed envelope", async () => {
  const outcome = await run({ args: ["--status"], command: shim(OK_ENVELOPE) });
  assert.equal(outcome.ok, true);
  if (!outcome.ok) return;
  assert.equal(outcome.envelope.directory?.project, "Runner Board");
  assert.equal(outcome.envelope.operation, "status");
});

test("exit 4 surfaces the remedy the CLI named, verbatim (§4.3)", async () => {
  const outcome = await run({ args: ["--start", "T-0002"], command: shim(WIP_ENVELOPE, 4) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "precondition");
  assert.equal(outcome.failure.exit, 4);
  // The slot listing is the remedy; losing it would make the limit a mystery.
  assert.match(outcome.failure.message, /slot 01 {2}T-0001 {2}one/);
  assert.match(outcome.failure.message, /finish one, pause one, or raise the limit/);
  assert.equal(outcome.failure.errors[0]?.code, "WipLimitReached");
  assert.equal(outcome.failure.errors[0]?.id, "T-0002");
  // The envelope rides along for details and state reconstruction (§4.1).
  assert.equal(outcome.failure.envelope?.directory?.wipUsed, 1);
});

test("exit 2 is the build's age, not a plugin bug (§4.3)", async () => {
  const outcome = await run({ args: ["--nonsense"], command: shim(USAGE_ENVELOPE, 2) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "usage");
  assert.match(outcome.failure.message, /unknown switch --nonsense/);
  assert.match(outcome.failure.message, /not available in the installed mm build/);
});

test("exit 1 points at mm_check", async () => {
  const envelope = JSON.stringify({
    ok: false,
    operation: "add",
    errors: [{ code: "InvariantViolation", message: "backlog.md:11: T-0001 is already defined at backlog.md:9" }],
  });
  const outcome = await run({ args: ["--add", "x"], command: shim(envelope, 1) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "invariant");
  assert.match(outcome.failure.message, /backlog\.md:11/, "the finding is verbatim, path:line included");
  assert.match(outcome.failure.message, /mm_check/);
});

test("exit 3 distinguishes an unknown id from an ambiguous board", async () => {
  const unknown = JSON.stringify({
    ok: false,
    errors: [{ code: "NotFound", message: "T-9999 not found" }],
  });
  const one = await run({ args: ["--show", "T-9999"], command: shim(unknown, 3) });
  assert.equal(one.ok, false);
  if (one.ok) return;
  assert.equal(one.failure.kind, "not-found");
  assert.doesNotMatch(one.failure.message, /Pin one/, "an unknown id is not a pin problem");

  const ambiguous = JSON.stringify({
    ok: false,
    errors: [{ code: "Ambiguous", message: "two directories match: /a, /b" }],
  });
  const two = await run({ args: ["--status"], command: shim(ambiguous, 3) });
  assert.equal(two.ok, false);
  if (two.ok) return;
  // §3.2: the plugin must not guess among candidates; it says a pin is needed.
  assert.match(two.failure.message, /Pin one with mm_board/);
});

test("exit 5 tells the agent to re-read, and says nothing was retried", async () => {
  const envelope = JSON.stringify({
    ok: false,
    errors: [{ code: "Concurrent", message: "backlog.md changed on disk since it was read" }],
  });
  const outcome = await run({ args: ["--finish", "T-0001"], command: shim(envelope, 5) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "concurrent");
  assert.match(outcome.failure.message, /Re-read with mm_status or mm_list/);
  assert.match(outcome.failure.message, /nothing was retried/);
});

test("exit 6 is reported as it came", async () => {
  const envelope = JSON.stringify({
    ok: false,
    errors: [{ code: "Io", message: "reading backlog.md: permission denied" }],
  });
  const outcome = await run({ args: ["--status"], command: shim(envelope, 6) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "io");
  assert.match(outcome.failure.message, /permission denied/);
});

/* ------------------------------------------- failures mm never reported */

test("no mm on PATH gives the install remedy, not a stack trace", async () => {
  const outcome = await run({
    args: ["--status"],
    command: join(tmpdir(), "definitely-not-a-real-binary-mm"),
  });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "spawn");
  assert.match(outcome.failure.message, /no `mm` on PATH/);
  assert.match(outcome.failure.message, /go build/, "one explanation, shared with the startup check");
});

test("a hang is bounded and never awaited forever (§4.1)", async () => {
  const dir = mkdtempSync(join(tmpdir(), "mm-hang-"));
  const path = join(dir, "mm");
  writeFileSync(path, "#!/bin/sh\nsleep 30\n");
  chmodSync(path, 0o755);

  const started = Date.now();
  const outcome = await run({ args: ["--status"], command: path, timeoutMs: 150 });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "timeout");
  assert.match(outcome.failure.message, /150ms/);
  assert.match(outcome.failure.message, /Nothing was retried/);
  assert.ok(Date.now() - started < 5_000);
});

test("an aborted turn kills the subprocess", async () => {
  const dir = mkdtempSync(join(tmpdir(), "mm-abort-"));
  const path = join(dir, "mm");
  writeFileSync(path, "#!/bin/sh\nsleep 30\n");
  chmodSync(path, 0o755);

  const controller = new AbortController();
  const pending = run({ args: ["--status"], command: path, signal: controller.signal });
  setTimeout(() => controller.abort(), 50);
  const outcome = await pending;
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "timeout");
});

test("output that is not the envelope is a failure, not a salvage job", async () => {
  // §9.2 promises one JSON object on stdout and nothing else, on success AND
  // on failure. Anything else is that promise breaking; scanning for the first
  // brace would hide it.
  const outcome = await run({ args: ["--status"], command: shim("panic: runtime error\n", 2) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "envelope");
  assert.match(outcome.failure.message, /without the --json envelope/);
  assert.match(outcome.failure.message, /panic: runtime error/, "the output is quoted so it can be debugged");
});

test("a zero exit with no output is still a failure", async () => {
  const outcome = await run({ args: ["--status"], command: shim("", 0) });
  assert.equal(outcome.ok, false);
  if (outcome.ok) return;
  assert.equal(outcome.failure.kind, "envelope");
});

test("the default timeout is §9's 30 seconds", () => {
  assert.equal(DEFAULT_TIMEOUT_MS, 30_000);
});

test("stdin is a pipe only when there is something to send (§4.2, mm_add_many)", async () => {
  // The batch of --add-many travels on stdin, because the plugin creates no
  // files (§8.1) and a temp file would be one. The shim echoes what it read
  // back into the envelope's project, which is the cheapest way to prove the
  // bytes really arrived.
  const dir = mkdtempSync(join(tmpdir(), "mm-stdin-"));
  const path = join(dir, "mm");
  writeFileSync(
    path,
    // The newlines are stripped on the way back: they are what separates the
    // items, and one inside a JSON string would make the envelope unparseable.
    `#!/bin/sh\nseen=$(cat | tr -d '\\n')\nprintf '{"ok":true,"operation":"add-many","directory":{"path":"/b","project":"%s"},"result":[],"changes":[],"warnings":[],"errors":[]}' "$seen"\n`,
  );
  chmodSync(path, 0o755);

  const sent = await run({ args: ["--add-many"], command: path, stdin: "One\nTwo\n" });
  assert.equal(sent.ok, true, JSON.stringify(sent));
  if (!sent.ok) return;
  assert.equal(sent.envelope.directory?.project, "OneTwo", "the batch reached mm's stdin");

  // Without it the child must not be handed a pipe at all: an operation that
  // waits on stdin it was never going to get is a hang.
  const none = await run({ args: ["--status"], command: path });
  assert.equal(none.ok, true, JSON.stringify(none));
  if (!none.ok) return;
  assert.equal(none.envelope.directory?.project, "", "no stdin means immediate EOF, not a wait");
});

test("MM_DIR reaches mm untouched (§3.2)", async () => {
  // Board resolution is mm's, not the plugin's: MM_DIR is step 2 of the order,
  // and a runner that scrubbed the environment would quietly change which
  // board a user's shell means.
  const dir = mkdtempSync(join(tmpdir(), "mm-env-"));
  const path = join(dir, "mm");
  const seen = join(dir, "seen");
  writeFileSync(path, `#!/bin/sh\necho "$MM_DIR" > ${seen}\nprintf '%s' '${OK_ENVELOPE.replaceAll("'", "'\\''")}'\n`);
  chmodSync(path, 0o755);

  const previous = process.env["MM_DIR"];
  process.env["MM_DIR"] = "/boards/from-the-shell";
  try {
    const outcome = await run({ args: ["--status"], command: path });
    assert.equal(outcome.ok, true);
    assert.equal(readFileSync(seen, "utf8").trim(), "/boards/from-the-shell");
  } finally {
    if (previous === undefined) delete process.env["MM_DIR"];
    else process.env["MM_DIR"] = previous;
  }
});
