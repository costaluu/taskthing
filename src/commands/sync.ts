import { defineCommand } from "@bunli/core";

import { spinnerMessages } from "../spinner-messages";
import { runSyncing } from "../sync-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "sync" as const,
  description: "publish this peer's events to the remote",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    await runSyncing(flags.workspace, ({ name, manager }) => ({
      copy: spinnerMessages.sync(name),
      operation: () => manager.sync(),
    }));
  },
});
