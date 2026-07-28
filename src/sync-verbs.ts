import { openWorkspace, readConfig, targetWorkspaceName, workspacePath } from "./porcelain";
import { presenter } from "./presenter";
import type { spinnerMessages } from "./spinner-messages";

type Copy = ReturnType<typeof spinnerMessages.sync>;

/**
 * The git-facing verbs, each a thin porcelain over a manager primitive: publish
 * this peer's events, re-derive from the consolidated snapshot, consolidate
 * every peer into a new one, or bound a branch's history.
 *
 * Each runs behind its spinner (Spec 0004): the copy is fixed by the command,
 * the operation is the manager primitive.
 */
export async function runSyncing(
  workspace: string | undefined,
  plan: (context: {
    name: string;
    manager: Awaited<ReturnType<typeof openWorkspace>>["manager"];
  }) => { copy: Copy; operation: () => Promise<unknown> },
): Promise<void> {
  const name = await targetWorkspaceName(workspace);
  const { manager } = await openWorkspace(workspacePath(name));
  const config = await readConfig();

  const { copy, operation } = plan({ name, manager });

  await presenter().progress(copy, operation, config);
}
