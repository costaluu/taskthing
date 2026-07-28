import { defineCommand } from "@bunli/core";

import { listWorkspaces } from "./workspace";

/** A first-class shortcut for `workspace list`, like `boards` for `board list`. */
export default defineCommand({
  name: "workspaces" as const,
  description: "list workspaces (shortcut for `workspace list`)",
  handler: async () => {
    await listWorkspaces();
  },
});
