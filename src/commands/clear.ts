import { defineCommand } from "@bunli/core";

import { onTask } from "../entity-action";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "clear" as const,
  description: "remove an already-deleted task from this machine",
  options: {
    workspace: workspaceOption(),
  },
  /**
   * Take an already-deleted task's file off this machine. Purely local: no event,
   * no LWW, nothing propagated — and since the task's history is untouched, a
   * later pull or rebuild may rebuild the file. Short-term tidying, not removal.
   */
  handler: async ({ flags, positional }) => {
    const reference = positional[0] ?? "";

    await onTask("permanently delete", reference, flags.workspace, async ({ task, mdwal }) => {
      if (!task.deleted) {
        throw new Error(`task ${reference} is not deleted — \`delete\` it first`);
      }

      await mdwal.discard("task", task.id);
      return { title: task.title, predicate: "permanently deleted" };
    });
  },
});
