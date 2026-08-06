/*
  Configuration (spec-pi-mm-plugin.md §9).

  The rule that matters most is the trust one: a project-local file is read
  ONLY for a trusted project, because an untrusted project pointing the plugin
  at a board is exactly what pi's trust model exists to stop. Everything else
  is "a broken config is a warning, not a failure".
*/

import test from "node:test";
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { CONFIG_FILENAME, DEFAULT_CONFIG, loadConfig } from "./config.ts";

/** Builds a home and a project with the given config documents. */
function scene(globalDoc?: unknown, projectDoc?: unknown) {
  const root = mkdtempSync(join(tmpdir(), "mm-config-"));
  const home = join(root, "home");
  const cwd = join(root, "project");
  mkdirSync(join(home, ".pi", "agent"), { recursive: true });
  mkdirSync(join(cwd, ".pi"), { recursive: true });
  if (globalDoc !== undefined) {
    writeFileSync(
      join(home, ".pi", "agent", CONFIG_FILENAME),
      typeof globalDoc === "string" ? globalDoc : JSON.stringify(globalDoc),
    );
  }
  if (projectDoc !== undefined) {
    writeFileSync(
      join(cwd, ".pi", CONFIG_FILENAME),
      typeof projectDoc === "string" ? projectDoc : JSON.stringify(projectDoc),
    );
  }
  return { home, cwd };
}

test("no files at all is the defaults, silently", () => {
  const { home, cwd } = scene();
  const loaded = loadConfig({ home, cwd, projectTrusted: true });
  assert.deepEqual(loaded.config, DEFAULT_CONFIG);
  // A missing file is the normal case, not something to warn about.
  assert.deepEqual(loaded.warnings, []);
  assert.deepEqual(loaded.sources, []);
});

test("the global file is read, and every key lands", () => {
  const { home, cwd } = scene({
    board: "/boards/one",
    context: false,
    contextLines: 4,
    timeoutMs: 5000,
    confirmRemove: false,
  });
  const { config, warnings } = loadConfig({ home, cwd });
  assert.deepEqual(config, {
    board: "/boards/one",
    context: false,
    contextLines: 4,
    timeoutMs: 5000,
    confirmRemove: false,
  });
  assert.deepEqual(warnings, []);
});

test("the project file is read ONLY for a trusted project (§9)", () => {
  const { home, cwd } = scene({ board: "/boards/global" }, { board: "/boards/project" });

  const untrusted = loadConfig({ home, cwd, projectTrusted: false });
  assert.equal(untrusted.config.board, "/boards/global", "the project's pin is not honoured");
  assert.equal(untrusted.sources.length, 1);

  const trusted = loadConfig({ home, cwd, projectTrusted: true });
  assert.equal(trusted.config.board, "/boards/project", "…and overrides the global one when it is");
  assert.equal(trusted.sources.length, 2);
});

test("project keys override global keys one at a time", () => {
  const { home, cwd } = scene(
    { context: false, timeoutMs: 1000, confirmRemove: false },
    { timeoutMs: 2000 },
  );
  const { config } = loadConfig({ home, cwd, projectTrusted: true });
  assert.equal(config.timeoutMs, 2000, "the project's key wins");
  assert.equal(config.context, false, "…and the global's others survive");
  assert.equal(config.confirmRemove, false);
});

test("a key of the wrong type is named and dropped, never coerced", () => {
  const { home, cwd } = scene({
    context: "yes",
    contextLines: "six",
    timeoutMs: "30s",
    confirmRemove: 1,
    board: 42,
  });
  const { config, warnings } = loadConfig({ home, cwd });
  assert.deepEqual(config, DEFAULT_CONFIG, "nothing was guessed at");
  for (const key of ["context", "contextLines", "timeoutMs", "confirmRemove", "board"]) {
    assert.ok(
      warnings.some((w) => w.startsWith(key)),
      `${key} should be named: ${warnings.join(" | ")}`,
    );
  }
});

test("contextLines cannot raise §6's ceiling", () => {
  const { home, cwd } = scene({ contextLines: 40 });
  const { config } = loadConfig({ home, cwd });
  // The limit is the spec's; the key only lowers it.
  assert.equal(config.contextLines, 8);
});

test("an unknown key is reported, because it is usually a typo", () => {
  const { home, cwd } = scene({ contextLine: 4, bord: "/x" });
  const { config, warnings } = loadConfig({ home, cwd });
  assert.deepEqual(config, DEFAULT_CONFIG);
  assert.equal(warnings.length, 2);
  assert.match(warnings.join(" "), /contextLine/);
  assert.match(warnings.join(" "), /bord/);
});

test("unreadable JSON is a warning and the defaults, not a failure", () => {
  const { home, cwd } = scene("{not json at all");
  const { config, warnings } = loadConfig({ home, cwd });
  assert.deepEqual(config, DEFAULT_CONFIG);
  assert.match(warnings[0] ?? "", /not valid JSON/);
});

test("a JSON array is not a config", () => {
  const { home, cwd } = scene([1, 2, 3]);
  const { config, warnings } = loadConfig({ home, cwd });
  assert.deepEqual(config, DEFAULT_CONFIG);
  assert.match(warnings[0] ?? "", /not a JSON object/);
});

test("the defaults are §9's", () => {
  assert.deepEqual(DEFAULT_CONFIG, {
    context: true,
    contextLines: 6,
    timeoutMs: 30_000,
    confirmRemove: true,
  });
});
