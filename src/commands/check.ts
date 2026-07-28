import { defineCommand } from "@bunli/core";

import { completeTask } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "check" as const,
  description: "complete a task",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await completeTask(positional[0] ?? "", true, flags.workspace);
  },
});
