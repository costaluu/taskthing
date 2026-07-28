import { defineCommand } from "@bunli/core";

import { starTask } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "star" as const,
  description: "star a task",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await starTask(positional[0] ?? "", true, flags.workspace);
  },
});
