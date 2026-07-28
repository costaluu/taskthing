import { defineCommand } from "@bunli/core";

import { spinnerMessages } from "../spinner-messages";
import { runSyncing } from "../sync-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "rebuild" as const,
  description: "re-derive this workspace from the consolidated snapshot",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    await runSyncing(flags.workspace, ({ name, manager }) => ({
      copy: spinnerMessages.rebuild(name),
      operation: () => manager.rebuild(),
    }));
  },
});
