/*
  The /mm command (spec-pi-mm-plugin.md §5).

  "A human-facing mirror of the tools, sharing the same argv builders and
  formatters." This file takes that literally: it parses the command line into
  the parameters a TOOL takes and then calls that tool. Not a second code path
  that resembles the first — the same one.

  That is what makes §5's rules true rather than merely intended:

    - "The command MUST print the same formatted results the tools return" —
      it prints exactly what the tool returned, because it is the tool's result.
    - "MUST resolve the board exactly like the tools" — it does not resolve
      anything; the tool does.
    - Removal "confirms the same way" — the same guard runs, from the same code
      (§8.2), with the command's ctx handed straight through.

  The recommended operations (T-0184) are here on the same terms. §5 names
  `/mm tick` and `/mm archive` explicitly; block, unblock, search and report
  follow from §5's opening sentence — the command is "a human-facing mirror of
  the tools" — and mirror them the same way, by calling them. When the
  installed `mm` lacks one, its tool was never registered (§4.2) and the
  command says so, because runCommand looks the tool up rather than assuming.
*/

import { setContextEnabled, contextEnabled } from "./settings.ts";
import type { ToolContext, ToolDefinition } from "./tools-read.ts";

/** One parsed command line. */
export interface ParsedCommand {
  readonly op: string;
  /**
   * Every line after the first, blanks dropped.
   *
   * One command is normally one line, and every other operation reads only
   * that. `/mm add-many` is the exception the spec asks for (§5): its items
   * arrive as the lines below the command, which is what makes pasting a list
   * into the TUI work at all.
   */
  readonly lines: readonly string[];
  /** Everything that was not a flag, in order. */
  readonly words: readonly string[];
  /** `--key value`, accumulating: `--tag a --tag b` gives both. */
  readonly flags: ReadonlyMap<string, readonly string[]>;
  /** `--flag` with no value. */
  readonly switches: ReadonlySet<string>;
}

/**
 * Splits a command line into words, respecting quotes.
 *
 * A title is one argument — `/mm add "Fix the deploy script"` — for the same
 * reason the CLI insists on it (spec-tools.md §5.1.2): an unquoted multi-word
 * title silently becoming several arguments is the mistake this prevents.
 */
export function tokenize(line: string): string[] {
  const out: string[] = [];
  let current = "";
  let quote: '"' | "'" | undefined;
  let has = false;

  for (const ch of line) {
    if (quote) {
      if (ch === quote) quote = undefined;
      else current += ch;
      continue;
    }
    if (ch === '"' || ch === "'") {
      quote = ch;
      has = true;
      continue;
    }
    if (ch === " " || ch === "\t" || ch === "\n") {
      if (current || has) out.push(current);
      current = "";
      has = false;
      continue;
    }
    current += ch;
  }
  if (current || has) out.push(current);
  return out;
}

/** Flags that take a value; everything else prefixed with -- is a switch. */
const VALUE_FLAGS = new Set([
  "project",
  "dir",
  "reason",
  "field",
  "period",
  "since",
  "until",
  "group-by",
  "age",
  "slots",
  "slot-width",
  "prefix",
  "description",
  "section",
  "state",
  "prio",
  "tag",
  "limit",
  "title",
  "set",
  "unset",
  "untag",
  "position",
  "before",
  "after",
  "blocked",
  "outcome",
  "closing-note",
  "detail-text",
  "slot",
]);

export function parseCommand(line: string): ParsedCommand {
  // The first line is the command; the rest are data for the operation that
  // wants them. Splitting here rather than in the tokenizer keeps quoting and
  // flags working exactly as they do for every other operation.
  const [head = "", ...rest] = line.split("\n");
  const lines = rest.map((l) => l.trim()).filter(Boolean);
  const tokens = tokenize(head.trim());
  const op = (tokens.shift() ?? "").toLowerCase();
  const words: string[] = [];
  const flags = new Map<string, string[]>();
  const switches = new Set<string>();

  for (let i = 0; i < tokens.length; i += 1) {
    const token = tokens[i] as string;
    if (!token.startsWith("--")) {
      words.push(token);
      continue;
    }
    const [name, inline] = splitFlag(token.slice(2));
    if (VALUE_FLAGS.has(name)) {
      const value = inline ?? tokens[++i] ?? "";
      const list = flags.get(name) ?? [];
      list.push(value);
      flags.set(name, list);
      continue;
    }
    switches.add(name);
  }
  return { op, words, flags, switches, lines };
}

function splitFlag(s: string): [string, string | undefined] {
  const at = s.indexOf("=");
  return at < 0 ? [s, undefined] : [s.slice(0, at), s.slice(at + 1)];
}

/** How each op maps onto a tool and its parameters. */
type Plan =
  | { readonly tool: string; readonly params: Record<string, unknown> }
  | { readonly say: string };

const first = (p: ParsedCommand, name: string): string | undefined => p.flags.get(name)?.[0];
const all = (p: ParsedCommand, name: string): string[] => [...(p.flags.get(name) ?? [])];
const num = (v: string | undefined): number | undefined => {
  if (v === undefined) return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
};

/**
 * Turns a parsed line into a tool call.
 *
 * Every unknown op lands on the help text rather than on a guess: `/mm stat` is
 * a typo, and running `--status` for it would be the plugin deciding what
 * someone meant.
 */
export function plan(p: ParsedCommand): Plan {
  switch (p.op) {
    case "":
    case "help":
      return { say: helpText() };

    case "status":
      return { tool: "mm_status", params: {} };
    case "next":
      return { tool: "mm_next", params: {} };
    case "check":
      return { tool: "mm_check", params: {} };
    case "find":
      return { tool: "mm_find", params: {} };

    case "board":
      return { tool: "mm_board", params: p.words[0] ? { path: p.words[0] } : {} };

    case "init": {
      const project = first(p, "project") ?? p.words.join(" ");
      if (!project) return { say: "/mm init needs --project NAME." };
      return {
        tool: "mm_init",
        params: clean({
          project,
          dir: first(p, "dir"),
          slots: num(first(p, "slots")),
          slot_width: num(first(p, "slot-width")),
          prefix: first(p, "prefix"),
          description: first(p, "description"),
        }),
      };
    }

    case "describe": {
      const text = p.words.join(" ") || first(p, "description") || "";
      if (!text) return { say: "/mm describe needs some text." };
      return { tool: "mm_describe", params: { text } };
    }

    case "list":
      return {
        tool: "mm_list",
        params: clean({
          section: first(p, "section"),
          state: first(p, "state"),
          prio: first(p, "prio"),
          tag: first(p, "tag"),
          limit: num(first(p, "limit")),
        }),
      };

    case "show": {
      const id = p.words[0];
      if (!id) return { say: "/mm show needs an ID." };
      return { tool: "mm_show", params: clean({ id, detail: p.switches.has("detail") || undefined }) };
    }

    case "add": {
      const title = p.words.join(" ");
      if (!title) return { say: '/mm add needs a title — quote it: /mm add "Fix the deploy script".' };
      return {
        tool: "mm_add",
        params: clean({
          title,
          section: first(p, "section"),
          prio: first(p, "prio"),
          tags: all(p, "tag"),
          top: p.switches.has("top") || undefined,
          blocked: first(p, "blocked"),
          detail_text: first(p, "detail-text"),
        }),
      };
    }

    case "edit": {
      const id = p.words[0];
      if (!id) return { say: "/mm edit needs an ID." };
      return {
        tool: "mm_edit",
        params: clean({
          id,
          title: first(p, "title"),
          prio: first(p, "prio"),
          tags: all(p, "tag"),
          untags: all(p, "untag"),
          set: all(p, "set"),
          unset: all(p, "unset"),
          blocked: first(p, "blocked"),
        }),
      };
    }

    case "move": {
      const id = p.words[0];
      if (!id) return { say: "/mm move needs an ID." };
      return {
        tool: "mm_move",
        params: clean({
          id,
          section: first(p, "section"),
          position: num(first(p, "position")),
          top: p.switches.has("top") || undefined,
          end: p.switches.has("end") || undefined,
          before: first(p, "before"),
          after: first(p, "after"),
          blocked: first(p, "blocked"),
        }),
      };
    }

    case "start": {
      const id = p.words[0];
      if (!id) return { say: "/mm start needs an ID." };
      return { tool: "mm_start", params: clean({ id, slot: num(first(p, "slot")) }) };
    }
    case "pause": {
      const id = p.words[0];
      if (!id) return { say: "/mm pause needs an ID." };
      return {
        tool: "mm_pause",
        params: clean({
          id,
          section: first(p, "section"),
          end: p.switches.has("end") || undefined,
          blocked: first(p, "blocked"),
        }),
      };
    }
    case "finish": {
      const id = p.words[0];
      if (!id) return { say: "/mm finish needs an ID." };
      return {
        tool: "mm_finish",
        params: clean({
          id,
          outcome: first(p, "outcome"),
          closing_note: first(p, "closing-note"),
        }),
      };
    }

    case "note": {
      const [id, ...rest] = p.words;
      const body = rest.join(" ");
      if (!id) return { say: "/mm note needs an ID and some text." };
      if (!body) return { say: `/mm note needs some text: /mm note ${id} what you learned.` };
      return { tool: "mm_note", params: { id, text: body } };
    }

    case "remove": {
      const id = p.words[0];
      if (!id) return { say: "/mm remove needs an ID." };
      /*
        The command asserts the confirmation itself: a human typing
        `/mm remove T-0042` IS the user asking, which is precisely what
        §8.2's `confirmed` parameter means when a model sets it. The
        interactive prompt still runs — the tool asks through the ctx handed
        to it — so a typed command is confirmed twice, exactly like a tool
        call from the agent.
      */
      return { tool: "mm_remove", params: { id, confirmed: true } };
    }

    case "context": {
      const want = (p.words[0] ?? "").toLowerCase();
      if (want !== "on" && want !== "off" && want !== "") {
        return { say: "/mm context takes on or off." };
      }
      if (want === "") {
        return { say: `context injection is ${contextEnabled() ? "on" : "off"} for this session.` };
      }
      // §6: "the setting is session state, not configuration."
      setContextEnabled(want === "on");
      return { say: `context injection ${want} for this session.` };
    }

    case "add-many": {
      /*
        The items are the lines AFTER the first: a command line is one line, so
        `/mm add-many` on its own cannot carry a list. The TUI gives the
        handler everything typed, newlines included, which is what makes a
        pasted list work — and why the items are split out here rather than
        tokenized like every other op's words.
      */
      const items = p.lines;
      if (items.length === 0) {
        return {
          say: "/mm add-many takes one item per line, after the command line:\n" +
            "  /mm add-many --prio high\n  Fix the deploy script\n  Rotate the leaked token",
        };
      }
      return {
        tool: "mm_add_many",
        params: clean({
          items,
          section: first(p, "section"),
          prio: first(p, "prio"),
          tags: all(p, "tag"),
          top: p.switches.has("top") || undefined,
        }),
      };
    }

    case "block": {
      const [id, ...rest] = p.words;
      const reason = first(p, "reason") ?? rest.join(" ");
      if (!id) return { say: "/mm block needs an ID and a reason." };
      if (!reason) {
        return { say: `/mm block needs a reason: /mm block ${id} "waiting on the vendor".` };
      }
      return { tool: "mm_block", params: { id, reason } };
    }

    case "unblock": {
      const id = p.words[0];
      if (!id) return { say: "/mm unblock needs an ID." };
      return { tool: "mm_unblock", params: clean({ id, end: p.switches.has("end") || undefined }) };
    }

    case "search": {
      const query = p.words.join(" ");
      if (!query) return { say: '/mm search needs a query — quote it: /mm search "deploy script".' };
      return {
        tool: "mm_search",
        params: clean({
          query,
          fields: all(p, "field"),
          state: first(p, "state"),
          regex: p.switches.has("regex") || undefined,
          limit: num(first(p, "limit")),
        }),
      };
    }

    case "report":
      return {
        tool: "mm_report",
        params: clean({
          period: first(p, "period") ?? p.words[0],
          since: first(p, "since"),
          until: first(p, "until"),
          group_by: first(p, "group-by"),
          include_wip: p.switches.has("include-wip") || undefined,
          include_backlog: p.switches.has("include-backlog") || undefined,
          include_archives: p.switches.has("include-archives") || undefined,
        }),
      };

    case "tick":
      return { tool: "mm_tick", params: clean({ dry_run: p.switches.has("dry-run") || undefined }) };

    case "archive":
      return {
        tool: "mm_archive",
        params: clean({
          before: first(p, "before"),
          age: num(first(p, "age")),
          dry_run: p.switches.has("dry-run") || undefined,
        }),
      };

    default:
      return { say: `unknown: /mm ${p.op}\n\n${helpText()}` };
  }
}

/** Drops undefined and empty-array parameters, so a tool sees only what was asked. */
function clean(params: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue;
    if (Array.isArray(value) && value.length === 0) continue;
    out[key] = value;
  }
  return out;
}

export function helpText(): string {
  return [
    "/mm status | next | check | find",
    "/mm board [PATH]                      show the board in use, or pin one",
    "/mm init --project NAME [--dir P] [--slots N] [--prefix P] [--description T]",
    "/mm describe TEXT",
    "/mm list [--section S] [--state S] [--prio P] [--tag T] [--limit N]",
    '/mm show ID [--detail]',
    '/mm add "TITLE" [--section S] [--prio P] [--tag T]... [--top]',
    "/mm add-many [--section S] [--prio P] [--tag T]... [--top]",
    "    …then one item per line, below the command",
    "/mm edit ID [--title T] [--prio P] [--tag T]... [--set K=V]...",
    "/mm move ID [--section S] [--position N | --top | --end]",
    "/mm start ID | /mm pause ID | /mm finish ID [--outcome O] [--closing-note T]",
    "/mm note ID TEXT",
    "/mm remove ID                         asks before deleting",
    '/mm block ID "REASON" | /mm unblock ID [--end]',
    '/mm search "QUERY" [--field F]... [--state S] [--regex] [--limit N]',
    "/mm report [PERIOD] [--since D] [--until D] [--group-by G] [--include-wip]",
    "/mm tick [--dry-run]                  fire due someday schedules",
    "/mm archive [--before YYYY-MM | --age DAYS] [--dry-run]",
    "/mm context [on|off]                  per-turn board context",
    "/mm help",
  ].join("\n");
}

/** What a command run produced: text to show, and whether it went wrong. */
export interface CommandOutput {
  readonly text: string;
  readonly isError: boolean;
}

/**
 * Runs one /mm line against the registered tools.
 *
 * `tools` is the same map the extension registered with pi, so every guard,
 * every gate and every formatter is shared with the agent's path (§5).
 */
export async function runCommand(
  tools: ReadonlyMap<string, ToolDefinition>,
  line: string,
  ctx?: ToolContext,
  signal?: AbortSignal,
): Promise<CommandOutput> {
  const parsed = parseCommand(line);
  const decided = plan(parsed);
  if ("say" in decided) return { text: decided.say, isError: false };

  const tool = tools.get(decided.tool);
  if (!tool) {
    // Normally this is the recommended set (§4.2): those tools are registered
    // only when the installed `mm` has the operation, so the honest report is
    // the build's age — the same thing exit 2 says (§4.3) — not a plugin bug.
    return {
      text: `/mm ${parsed.op} needs ${decided.tool}, which this session does not have: the installed mm does not provide that operation.`,
      isError: true,
    };
  }
  const result = await tool.execute(`mm-command:${parsed.op}`, decided.params, signal, undefined, ctx);
  return {
    text: result.content.map((c) => c.text).join("\n"),
    isError: result.isError === true,
  };
}
