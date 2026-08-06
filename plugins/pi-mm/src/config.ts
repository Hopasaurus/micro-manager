/*
  Configuration (spec-pi-mm-plugin.md §9).

  Two files, one merge:

    ~/.pi/agent/mm-plugin.json   global, always read
    .pi/mm-plugin.json           project-local, ONLY when the project is
                                 trusted — "the plugin must not read an
                                 untrusted project's pin"

  THIS IS THE ONE MODULE THAT TOUCHES THE FILESYSTEM. Everywhere else the
  plugin reaches a board only through `mm` (§2.1, §8.1), and the conformance
  test in index.test.ts enforces that by banning `node:fs` — with an exemption
  for this file alone, plus an assertion that the only names it opens are its
  own config files. The plugin's config is not the board, and reading it
  through a subprocess would be inventing a CLI operation for a JSON file.

  Everything here is best-effort: a missing file, unreadable JSON, or a key of
  the wrong type all fall back to the default and produce a WARNING rather than
  a failure. A plugin that refused to start over a typo in an optional config
  would be worse than one that says what it ignored.
*/

import { readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

/** The config filename, in both scopes. */
export const CONFIG_FILENAME = "mm-plugin.json";

/** §9's keys and their defaults. */
export interface PluginConfig {
  /** A board path that overrides resolution order (§3.2). */
  readonly board?: string;
  /** Per-turn context injection (§6). */
  readonly context: boolean;
  /** Max injected block lines (§6's ≤ 8, defaulting to 6). */
  readonly contextLines: number;
  /** Subprocess bound (§4.1). */
  readonly timeoutMs: number;
  /** Master switch for mm_remove's interactive prompt (§8.2). */
  readonly confirmRemove: boolean;
}

export const DEFAULT_CONFIG: PluginConfig = {
  context: true,
  contextLines: 6,
  timeoutMs: 30_000,
  confirmRemove: true,
};

export interface LoadedConfig {
  readonly config: PluginConfig;
  /** What was ignored, and why. Shown once at session start. */
  readonly warnings: readonly string[];
  /** Which files were actually read, for the same message. */
  readonly sources: readonly string[];
}

export interface LoadOptions {
  readonly cwd?: string;
  /** pi's own trust model decides whether the project file is read at all. */
  readonly projectTrusted?: boolean;
  /** pi's config directory name; rebranded builds use a different one. */
  readonly configDirName?: string;
  /** Overridable for tests; production uses the real home directory. */
  readonly home?: string;
}

/**
 * Reads and merges the two files. Project overrides global, key by key.
 *
 * Nothing here throws: the caller is a session starting up, and a config it
 * could not read is a reason to say so and carry on with defaults.
 */
export function loadConfig(opts: LoadOptions = {}): LoadedConfig {
  const warnings: string[] = [];
  const sources: string[] = [];
  let merged: Record<string, unknown> = {};

  const globalPath = join(opts.home ?? homedir(), ".pi", "agent", CONFIG_FILENAME);
  const globalDoc = readJson(globalPath, warnings);
  if (globalDoc) {
    merged = { ...merged, ...globalDoc };
    sources.push(globalPath);
  }

  // §9: the project file is read ONLY for a trusted project. An untrusted
  // project pointing the plugin at a board — or at a path that leaks where the
  // user's boards live — is exactly what pi's trust model exists to stop.
  if (opts.projectTrusted && opts.cwd) {
    const projectPath = join(opts.cwd, opts.configDirName ?? ".pi", CONFIG_FILENAME);
    const projectDoc = readJson(projectPath, warnings);
    if (projectDoc) {
      merged = { ...merged, ...projectDoc };
      sources.push(projectPath);
    }
  }

  return { config: coerce(merged, warnings), warnings, sources };
}

/** Reads one JSON object, or nothing. A missing file is not a warning. */
function readJson(path: string, warnings: string[]): Record<string, unknown> | undefined {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    // ENOENT is the normal case: neither file is required.
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      warnings.push(`${path} could not be read (${describe(err)}); using defaults`);
    }
    return undefined;
  }
  try {
    const parsed: unknown = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      warnings.push(`${path} is not a JSON object; ignored`);
      return undefined;
    }
    return parsed as Record<string, unknown>;
  } catch (err) {
    warnings.push(`${path} is not valid JSON (${describe(err)}); ignored`);
    return undefined;
  }
}

/**
 * Applies the merged document to the defaults, one key at a time.
 *
 * A key of the wrong type is named and dropped rather than coerced: a
 * `timeoutMs` of `"30s"` is a mistake worth hearing about, and quietly reading
 * it as 30 would be a different bug later.
 */
function coerce(doc: Record<string, unknown>, warnings: string[]): PluginConfig {
  const out: {
    board?: string;
    context: boolean;
    contextLines: number;
    timeoutMs: number;
    confirmRemove: boolean;
  } = { ...DEFAULT_CONFIG };

  const wrong = (key: string, want: string, got: unknown) =>
    warnings.push(`${key} should be ${want}, got ${JSON.stringify(got)}; ignored`);

  if ("board" in doc) {
    const v = doc["board"];
    if (typeof v === "string" && v.trim()) out.board = v.trim();
    else if (v !== null && v !== undefined) wrong("board", "a path", v);
  }
  if ("context" in doc) {
    const v = doc["context"];
    if (typeof v === "boolean") out.context = v;
    else wrong("context", "true or false", v);
  }
  if ("contextLines" in doc) {
    const v = doc["contextLines"];
    // §6 caps the block at 8 lines whatever the config says: the limit is the
    // spec's, and the key only lowers it.
    if (typeof v === "number" && Number.isFinite(v) && v >= 1) {
      out.contextLines = Math.min(8, Math.trunc(v));
    } else wrong("contextLines", "a number of lines from 1 to 8", v);
  }
  if ("timeoutMs" in doc) {
    const v = doc["timeoutMs"];
    if (typeof v === "number" && Number.isFinite(v) && v > 0) out.timeoutMs = Math.trunc(v);
    else wrong("timeoutMs", "a positive number of milliseconds", v);
  }
  if ("confirmRemove" in doc) {
    const v = doc["confirmRemove"];
    if (typeof v === "boolean") out.confirmRemove = v;
    else wrong("confirmRemove", "true or false", v);
  }

  for (const key of Object.keys(doc)) {
    if (!(key in DEFAULT_CONFIG) && key !== "board") {
      // Named rather than ignored silently: an unknown key is usually a typo
      // for a real one, and a config that appears to work but does nothing is
      // the worst outcome.
      warnings.push(`unknown key ${JSON.stringify(key)}; ignored`);
    }
  }
  return out;
}

function describe(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
