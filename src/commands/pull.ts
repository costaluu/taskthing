import { defineCommand } from "@bunli/core";

import { spinnerMessages } from "../spinner-messages";
import { runSyncing } from "../sync-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "pull" as const,
  description: "replay peers' events into this workspace",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    await runSyncing(flags.workspace, ({ name, manager }) => ({
      copy: spinnerMessages.pull(name),
      operation: () => manager.pull(),
    }));
  },
});
