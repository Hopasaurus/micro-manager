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

  What is built so far: the skeleton, the install, and the mm-on-PATH check
  (T-0175). The runner (T-0176), board resolution (T-0177) and the tool surface
  (T-0178 onward) land in this directory next; `health()` below is what they
  gate on.
*/

import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";

import { presence, presenceMessage, resetPresence, type Presence } from "./presence.ts";

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
    The probe runs at session_start rather than at load (§2.1: "verify mm exists
    at session start"). Two reasons, and pi's own extension guidance gives the
    second: a factory may run in an invocation that never starts a session, and
    spawning a subprocess from one would be work nobody asked for.
  */
  pi.on("session_start", async (_event, ctx: ExtensionContext) => {
    // A reload is the moment a user has just installed mm and wants the plugin
    // to notice; anything else keeps the session's cached answer.
    if (_event.reason === "reload") resetPresence();

    const state = await health();
    if (state.ok) {
      // The board's identity replaces this once resolution exists (T-0177,
      // §3.2.1's persistent status line). Until then the status says what the
      // plugin knows: which mm it will drive.
      ctx.ui.setStatus?.(STATUS_KEY, state.version);
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
    The plugin holds no resources — no watcher, no timer, no open handle — so
    shutdown has nothing to release (§7). The handler exists to make that a
    statement rather than an omission: if something here ever starts holding a
    resource, this is where releasing it belongs, and its absence would be the
    bug.
  */
  pi.on("session_shutdown", () => {
    resetPresence();
  });
}
