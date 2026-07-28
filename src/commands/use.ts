import { defineCommand } from "@bunli/core";

import { useWorkspace } from "./workspace";

/**
 * A first-class shortcut for `workspace use`: switching is common enough to
 * deserve its own verb, and it is the only way the current workspace moves.
 */
export default defineCommand({
  name: "use" as const,
  description: "switch the current workspace (shortcut for `workspace use`)",
  handler: async ({ positional }) => {
    await useWorkspace(positional[0] ?? "");
  },
});
