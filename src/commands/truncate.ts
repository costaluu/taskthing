import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

import { spinnerMessages } from "../spinner-messages";
import { runSyncing } from "../sync-verbs";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "truncate" as const,
  description: "bound a branch's history to its most recent commits",
  options: {
    keep: option(z.coerce.number().int().min(1), {
      description: "the number of commits to keep",
    }),
    branch: option(z.string().optional(), {
      description: "the branch to truncate (default: this peer's own)",
    }),
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    await runSyncing(flags.workspace, ({ name, manager }) => ({
      copy: spinnerMessages.truncate(name, flags.branch ?? ""),
      operation: () => manager.truncateHistory({ branch: flags.branch, keepN: flags.keep }),
    }));
  },
});
