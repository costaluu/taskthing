import { defineCommand } from "@bunli/core";

import { setTaskDeleted } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "delete" as const,
  description: "soft-delete a task",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await setTaskDeleted(positional[0] ?? "", true, flags.workspace);
  },
});
