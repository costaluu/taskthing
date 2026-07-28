import { defineCommand } from "@bunli/core";

import { readConfig, reportConfig, writeConfig } from "../porcelain";
import { configSchema } from "../schema";
import { runThemeScreen } from "../tui";

/**
 * The `theme` shortcut: jump straight to the theme editor (default vs. custom)
 * and persist the chosen value. Interactive, so it needs a terminal.
 */
export default defineCommand({
  name: "theme" as const,
  description: "open the theme editor",
  handler: async () => {
    const current = await readConfig();
    if (!process.stdout.isTTY) {
      throw new Error("`theme` needs an interactive terminal");
    }
    const value = await runThemeScreen(current);
    await reportConfig(current, "theme", () =>
      writeConfig(configSchema.parse({ ...current, theme: value })),
    );
  },
});
