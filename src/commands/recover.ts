import { defineCommand } from "@bunli/core";

import { setTaskDeleted } from "../task-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "recover" as const,
  description: "restore a deleted task",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    await setTaskDeleted(positional[0] ?? "", false, flags.workspace);
  },
});
