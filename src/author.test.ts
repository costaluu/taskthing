import { test, expect } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { resolveGitAuthor } from "./author";

// The author seam reads the machine's *global* git identity (ADR-0002). Tests
// point git at a throwaway global config file rather than injecting a fake
// runner, so what is exercised is the real git lookup.
async function withGlobalGitConfig<T>(
  contents: string | null,
  body: () => Promise<T>,
): Promise<T> {
  const dir = await mkdtemp(join(tmpdir(), "gitconfig-"));
  const path = join(dir, "config");
  if (contents !== null) await Bun.write(path, contents);
  const previous = process.env.GIT_CONFIG_GLOBAL;
  process.env.GIT_CONFIG_GLOBAL = path;
  try {
    return await body();
  } finally {
    if (previous === undefined) delete process.env.GIT_CONFIG_GLOBAL;
    else process.env.GIT_CONFIG_GLOBAL = previous;
    await rm(dir, { recursive: true, force: true });
  }
}

test("the author comes from the machine's global git identity", async () => {
  const author = await withGlobalGitConfig(
    "[user]\n\tname = Ada Lovelace\n\temail = ada@example.com\n",
    resolveGitAuthor,
  );

  // Attribution works the same in a local workspace with no repository at all,
  // which is why the identity is read globally rather than per repo.
  expect(author).toBe("Ada Lovelace");
});

test("a missing git identity is a hard error, not an anonymous author", async () => {
  await withGlobalGitConfig(null, async () => {
    // Every event carries an author; guessing one would silently misattribute
    // changes across the peers sharing a workspace.
    await expect(resolveGitAuthor()).rejects.toThrow(/user\.name/i);
  });
});
