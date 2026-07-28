import { $ } from "bun";

// ── author source (ADR-0002) ────────────────────────────────────────────────
//
// Every event is attributed to a peer, and the peer is the machine's *global*
// git identity — global, because attribution has to work in a purely local
// workspace that is not a git repository at all.

/**
 * The current peer's name, from global git `user.name`. A missing or unset
 * identity is a hard error: an event with a guessed author would silently
 * misattribute changes among the peers sharing a workspace.
 */
export async function resolveGitAuthor(): Promise<string> {
  const result = await $`git config --global user.name`.quiet().nothrow();
  const name = result.text().trim();
  if (result.exitCode !== 0 || name.length === 0) {
    throw new Error(
      "no git identity: set one with `git config --global user.name \"<your name>\"`",
    );
  }
  return name;
}
