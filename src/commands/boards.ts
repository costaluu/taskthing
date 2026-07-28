import { defineCommand } from "@bunli/core";

import { listBoards } from "./board";
import { workspaceOption } from "../command-options";

/** A first-class shortcut for `board list`, the way `workspaces` is for `workspace list`. */
export default defineCommand({
  name: "boards" as const,
  description: "list boards (shortcut for `board list`)",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    await listBoards(flags.workspace);
  },
});
