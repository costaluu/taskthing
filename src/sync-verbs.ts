import { openWorkspace, readConfig, targetWorkspaceName, workspacePath } from "./porcelain";
import { renderCommandSpinner } from "./tui";
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

  // A TTY gets the live spinner; piped, the same copy is printed plainly so a
  // script (or a log) sees the outcome and, on failure, why — the spinner would
  // otherwise swallow the reason behind a half-drawn pending frame.
  if (process.stdout.isTTY) {
    const ok = await renderCommandSpinner(copy, operation, config);
    if (!ok) process.exit(1);
    return;
  }
  try {
    await operation();
    console.log(copy.success);
  } catch (error) {
    console.error(copy.failure(error));
    process.exit(1);
  }
}
