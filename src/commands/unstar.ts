import { defineCommand } from "@bunli/core";

import { starTask } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "unstar" as const,
  description: "remove a task's star",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await starTask(positional[0] ?? "", false, flags.workspace);
  },
});
