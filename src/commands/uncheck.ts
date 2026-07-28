import { defineCommand } from "@bunli/core";

import { completeTask } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "uncheck" as const,
  description: "reopen a task",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await completeTask(positional[0] ?? "", false, flags.workspace);
  },
});
