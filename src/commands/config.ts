import { defineCommand } from "@bunli/core";

import { subcommands } from "../hybrid-command";
import { readConfig, reportConfig, writeConfig } from "../porcelain";
import { CONFIG_KEYS, configSchema } from "../schema";
import { runConfigScreen } from "../tui";

export default defineCommand({
  name: "config" as const,
  description: "open settings (piped: print them)",
  // With no subcommand: on a terminal, open the interactive TUI and persist what
  // it collects; piped, just print what is set (scriptable, no TUI).
  handler: async () => {
    const current = await readConfig();
    if (process.stdout.isTTY) {
      const update = await runConfigScreen(current);
      // Left without choosing anything: nothing to write, nothing to report.
      const [changed] = Object.keys(update);
      if (changed === undefined) return;
      await reportConfig(current, changed, () =>
        writeConfig(configSchema.parse({ ...current, ...update })),
      );
    } else {
      for (const [name, setting] of Object.entries(current)) {
        console.log(`${name} ${String(setting)}`);
      }
    }
  },
  commands: subcommands([
    defineCommand({
      name: "set" as const,
      description: "set a setting non-interactively",
      handler: async ({ positional }) => {
        const [key, ...rest] = positional;
        const value = rest.join(" ");

        // The current workspace lives here too, but is not a setting: it moves only
        // through `use`, so nothing changes it as a side effect.
        if (key === "currentWorkspace") {
          throw new Error("the current workspace changes with `taskthing use <name>`");
        }
        if (!CONFIG_KEYS.includes(key as (typeof CONFIG_KEYS)[number])) {
          throw new Error(`no such setting: ${key} — try ${CONFIG_KEYS.join(", ")}`);
        }

        // The frontmatter carries bare strings, so a typed setting is coerced before
        // the schema validates it: booleans from "true", the cadence levels to numbers
        // (schema then rejects a non-positive or non-integer value).
        const numeric = key === "autoSync" || key === "autoRebuild" || key === "autoTruncate";
        const coerced = key === "nerdfont" ? value === "true" : numeric ? Number(value) : value;
        const parsed = configSchema.parse({
          ...(await readConfig()),
          [key!]: coerced,
        });
        await writeConfig(parsed);
      },
    }),
  ]),
});
