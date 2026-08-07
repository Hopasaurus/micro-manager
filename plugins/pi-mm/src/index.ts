/*
  The micro-manager pi plugin (project/spec-pi-mm-plugin.md).

  A pi extension that makes a micro-manager board — backlog.md / working.NN.md /
  done.md / details/ — something an agent can work with as typed tools, a /mm
  command, and per-turn context, WITHOUT ever editing the markdown itself.

  This file is the entry point pi loads (§3.1, directory form). It is
  deliberately thin: registration and lifecycle only. Everything with behaviour
  lives in a helper module beside it, which is what keeps the factory readable
  as the tool surface grows.

  Two rules from the spec shape everything here and are worth stating where they
  are easiest to break:

    §2.1  The plugin drives `mm` for every board interaction and never reads or
          writes the files. There is no fallback path that parses markdown — if
          `mm` is missing, the plugin is unavailable and says so.
    §2.2  The plugin is a guest. No locks, no watchers, no timers, and nothing
          mutates the board except in direct response to a tool call or a /mm
          command. Not on session_start, not on agent_settled, not ever.

  What is built: the skeleton and the mm check (T-0175), the runner (T-0176),
  board resolution and the pin (T-0177), the required READ tools (T-0178), the
  WRITE tools (T-0179), the workflow tools (T-0180) and mm_remove with its
  double guard (T-0181) — the whole required surface of §4.2 — the /mm command
  (T-0182), per-turn context injection with the §9 config (T-0183), the
  recommended tools, registered against the installed build (T-0184), and
  mm_add_many, which hands a whole list to one transaction (T-0187).
*/

import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

import {
  boardStatusLine,
  clearPin,
  pinFromBranch,
  pinnedBoard,
  setPin,
} from "./board.ts";
import { capabilities, hasOperation, resetCapabilities } from "./capabilities.ts";
import { presence, presenceMessage, resetPresence, type Presence } from "./presence.ts";
import { run } from "./runner.ts";
import { helpText, runCommand } from "./command.ts";
import { DEFAULT_CONFIG, loadConfig, type PluginConfig } from "./config.ts";
import { resetContext, turnContext } from "./context.ts";
import { readTools, type ToolContext, type ToolDefinition } from "./tools-read.ts";
import { resetSettings } from "./settings.ts";
import { removeTools } from "./tools-remove.ts";
import { RECOMMENDED_OPS, recommendedTools } from "./tools-recommended.ts";
import { workflowTools } from "./tools-workflow.ts";
import { writeTools } from "./tools-write.ts";

/** The status-line and notification key. One id, so nothing else is clobbered. */
export const STATUS_KEY = "micro-manager";

/**
 * Is the plugin able to do anything at all?
 *
 * Every board tool and the /mm command call this first and refuse with
 * `message` when it is not ok (§2.1, §4.1's "a no-board state makes every board
 * tool return an error naming the cause and the remedy, never a fake empty
 * board"). It is here rather than in presence.ts so that later gates — a board
 * that will not resolve, an `mm` too old for an operation — join it in one
 * place a caller checks once.
 */
export async function health(): Promise<
  { readonly ok: true; readonly version: string } | { readonly ok: false; readonly message: string }
> {
  const p: Presence = await presence();
  if (p.ok) return { ok: true, version: p.version };
  return { ok: false, message: presenceMessage(p) };
}

export default function micromanager(pi: ExtensionAPI): void {
  /*
    The tool surface (§4.2). Registered at load rather than at session_start so
    the tools exist for every mode pi runs in; each one gates on health() and on
    a resolved board itself, so registering them before either is known is safe
    — and an agent that calls one without mm gets the remedy rather than a
    missing tool.
  */
  const setStatus = (ctx: ExtensionContext, line: string) => {
    ctx.ui.setStatus?.(STATUS_KEY, line || "no board");
  };
  let ui: ExtensionContext | undefined;
  /*
    Configuration (§9) is read at session_start, not here: a factory may run in
    an invocation that never starts a session, and the project file's trust
    answer only exists once there is a context to ask. Until then the defaults
    stand, which is what every key already means when its file is absent.
  */
  let config: PluginConfig = DEFAULT_CONFIG;
  const deps = {
    health,
    run,
    onPin: (line: string) => {
      if (ui) setStatus(ui, line);
    },
    get timeoutMs(): number {
      return config.timeoutMs;
    },
    get confirmRemove(): boolean {
      return config.confirmRemove;
    },
  };
  const tools = new Map<string, ToolDefinition>();
  const register = (tool: ToolDefinition) => {
    tools.set(tool.name, tool);
    pi.registerTool(tool as never);
  };
  for (const tool of [
    ...readTools(deps),
    ...writeTools(deps),
    ...workflowTools(deps),
    ...removeTools(deps),
  ]) {
    register(tool);
  }

  /*
    The RECOMMENDED set (§4.2) is registered at session_start instead, because
    whether to register it is a question about the installed `mm`: "expose only
    when the installed mm has the op". Answering it needs a subprocess, and a
    factory may run in an invocation that never starts a session — the same
    reason the presence probe waits (see the session_start handler below).

    pi supports registering after startup: a tool registered inside
    session_start is refreshed into the same session and callable without a
    reload. Registration is idempotent here, so a /reload that re-probes does
    not double-register.
  */
  const registerRecommended = async (): Promise<string[]> => {
    const caps = await capabilities();
    const added: string[] = [];
    for (const tool of recommendedTools(deps)) {
      const op = RECOMMENDED_OPS[tool.name];
      // A tool with no entry in the map would be exposed unconditionally; that
      // is a bug in tools-recommended.ts rather than a build to gate on, and
      // refusing to register it is how it gets noticed.
      if (!op || !hasOperation(caps, op)) continue;
      if (tools.has(tool.name)) continue;
      register(tool);
      added.push(tool.name);
    }
    return added;
  };

  /*
    The /mm command (§5): the human-facing mirror of the tools, running the
    SAME tools rather than a parallel implementation. That is what makes "the
    command MUST print the same formatted results the tools return" true by
    construction, and what gives /mm remove the same double guard the agent's
    path has (§8.2) — the command's own ctx is handed to the tool, so the
    confirmation dialog is the tool's.
  */
  pi.registerCommand("mm", {
    description: "micro-manager: status, next, list, add, start, finish… (/mm help)",
    getArgumentCompletions: (prefix: string) => {
      const ops = [
        "status", "next", "check", "find", "board", "init", "describe", "list",
        "show", "add", "add-many", "edit", "move", "start", "pause", "finish", "note",
        "remove", "block", "unblock", "search", "report", "tick", "archive",
        "context", "help",
      ];
      const matches = ops.filter((op) => op.startsWith(prefix));
      return matches.length > 0 ? matches.map((op) => ({ value: op, label: op })) : null;
    },
    handler: async (args: string, ctx: ExtensionContext) => {
      const output = await runCommand(tools, args, ctx as ToolContext);
      // §5: print what the tool returned. An error is a "warning" rather than
      // an "error" notification because most of them are the board saying no —
      // a WIP limit, an unknown id — which is information, not a malfunction.
      ctx.ui.notify?.(output.text || helpText(), output.isError ? "warning" : "info");
    },
  } as never);

  /*
    The probe runs at session_start rather than at load (§2.1: "verify mm exists
    at session start"). Two reasons, and pi's own extension guidance gives the
    second: a factory may run in an invocation that never starts a session, and
    spawning a subprocess from one would be work nobody asked for.
  */
  pi.on("session_start", async (_event, ctx: ExtensionContext) => {
    // A reload is the moment a user has just installed mm — or replaced it
    // with a newer build — and wants the plugin to notice; anything else keeps
    // the session's cached answers.
    if (_event.reason === "reload") {
      resetPresence();
      resetCapabilities();
    }

    ui = ctx;
    const state = await health();
    if (state.ok) {
      /*
        §7: the pin is reconstructed from the ACTIVE BRANCH, never carried over
        from the previous session's memory. A fork that changed the board must
        not leak into the other branch, and the in-memory pin is only ever a
        cache of what the branch says.
      */
      clearPin();
      resetSettings();
      resetContext();

      /*
        §9. The global file always; the project file only for a trusted
        project — pi's own answer, never the plugin's guess. Warnings are
        surfaced once here rather than swallowed: a config that appears to work
        and does nothing is the worst outcome.
      */
      const loaded = loadConfig({
        cwd: ctx.cwd,
        projectTrusted: ctx.isProjectTrusted?.() ?? false,
      });
      config = loaded.config;
      for (const warning of loaded.warnings) {
        ctx.ui.notify?.(`micro-manager config: ${warning}`, "warning");
      }

      // §4.2's recommended set, against what this build actually has. Failing
      // to probe is not an error — hasOperation answers yes when nothing was
      // learned, and exit 2 remains the backstop (§4.3).
      await registerRecommended();

      const restored = pinFromBranch(ctx.sessionManager?.getBranch?.() ?? []);
      if (restored) setPin(restored);
      /*
        §7: "A pin set by /mm board PATH or config (§9) is the SESSION pin; it
        overrides resolution order and is re-asserted on session_start." So a
        configured board wins over what the branch remembered — the file is the
        standing instruction, the branch is where this session had got to.
      */
      if (config.board) setPin({ path: config.board, source: "pin" });

      // §3.2.1: the persistent status line says which board the agent is
      // operating on — set at session start, updated whenever the pin changes,
      // and reading "no board" rather than a stale name when there is none.
      setStatus(ctx, boardStatusLine(pinnedBoard()) || state.version);
      return;
    }

    /*
      LOUDLY, and once. §2.1 asks for loud degradation, and a notification at
      session start is the loudest thing a guest may do without hijacking the
      turn: it does not block, does not inject into the prompt (§6 forbids
      injecting when mm is absent), and does not retry.
    */
    ctx.ui.setStatus?.(STATUS_KEY, "mm unavailable");
    ctx.ui.notify?.(`micro-manager: ${state.message}`, "warning");
  });

  /*
    Per-turn context injection (§6). A read, never a write: this is a turn
    boundary, and §8.3 forbids the plugin acting on its own initiative. It
    injects nothing whenever there is any doubt — no board, no mm, injection
    switched off, or a status read that failed — because §6 is explicit that an
    empty injection beats a false one.
  */
  pi.on("before_agent_start", async (event: { systemPrompt?: string }, ctx: ExtensionContext) => {
    const block = await turnContext({
      health,
      run,
      timeoutMs: config.timeoutMs,
      contextLines: config.contextLines,
      // §6's last rule: never inject a board the project has not been trusted
      // to read. `context: false` in config is the user's own switch (§9), and
      // /mm context off is the session's (§5, checked inside turnContext).
      allowed: config.context,
    });
    if (!block) return undefined;
    void ctx;
    return { systemPrompt: `${event.systemPrompt ?? ""}\n\n${block}` };
  });

  /*
    The plugin holds no resources — no watcher, no timer, no open handle — so
    shutdown has nothing to release (§7). The handler exists to make that a
    statement rather than an omission: if something here ever starts holding a
    resource, this is where releasing it belongs, and its absence would be the
    bug.
  */
  pi.on("session_shutdown", () => {
    resetPresence();
    resetCapabilities();
    clearPin();
    resetSettings();
    resetContext();
  });
}
