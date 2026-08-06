/*
  Session-scoped settings (spec-pi-mm-plugin.md §5, §6).

  One thing lives here so far, and it is here rather than in the config reader
  because the spec is explicit about which it is: `/mm context off` disables
  per-turn injection "for the session; the setting is session state, not
  configuration" (§5).

  So: a value the command sets and the injector reads, reset when a session
  starts. Configuration — §9's `context` key, which sets the DEFAULT — is
  T-0183's, and will seed this rather than replace it.
*/

/** The default is on: §9's `context` key defaults to true. */
let enabled = true;

export function contextEnabled(): boolean {
  return enabled;
}

export function setContextEnabled(value: boolean): void {
  enabled = value;
}

/**
 * Back to the default at session start.
 *
 * A toggle that survived into the next session would be configuration wearing
 * a command's clothes, which is the distinction §5 draws.
 */
export function resetSettings(): void {
  enabled = true;
}
