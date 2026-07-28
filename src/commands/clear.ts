import { defineCommand } from "@bunli/core";

import {
  openWorkspace,
  reportTask,
  resolveTask,
  taskRepository,
} from "../porcelain";
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

    await reportTask("permanently delete", async () => {
      const { root, id } = await resolveTask(reference, flags.workspace);
      const { mdwal } = await openWorkspace(root);
      const tasks = taskRepository(mdwal);

      const task = await tasks.findById(id);
      if (task === null) throw new Error(`no such task: ${reference}`);
      if (!task.deleted) {
        throw new Error(`task ${reference} is not deleted — \`delete\` it first`);
      }

      await mdwal.discard("task", id);
      return { title: task.title, predicate: "permanently deleted" };
    });
  },
});
