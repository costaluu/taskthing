import { defineCommand, option } from "@bunli/core";
import { mkdir } from "node:fs/promises";
import { z } from "zod";

import { createGlyphs } from "../glyph";
import { configPath, workspacePath, writeConfig } from "../porcelain";
import { configSchema } from "../schema";
import { summarize } from "../spinner-messages";
import { runInstallForm } from "../tui";

/**
 * Scaffold a fresh taskthing: create the local workspace and write config.md
 * naming it current. Settings come from the interactive first-run form on a
 * terminal, or from flags/defaults when piped (american dates, no nerdfont).
 * Refuses to clobber an existing install without --force.
 */
export default defineCommand({
  name: "install" as const,
  description: "first-run scaffold and setup",
  options: {
    force: option(z.coerce.boolean().default(false), {
      description: "reinstall over an existing install",
      argumentKind: "flag",
    }),
    "date-format": option(z.enum(["america", "europe"]).optional(), {
      description: "date order to use when piped",
    }),
    nerdfont: option(z.coerce.boolean().default(false), {
      description: "use nerdfont glyphs when piped",
      argumentKind: "flag",
    }),
  },
  handler: async ({ flags }) => {
    // The nerdfont preference decides which glyph the closing line wears; it is
    // only known once the form (or --nerdfont) has run, so it starts at the
    // no-nerdfont default that also covers failures raised before that point.
    let nerdfont = false;
    try {
      if ((await Bun.file(configPath()).exists()) && !flags.force) {
        throw new Error("taskthing is already installed — pass --force to reinstall");
      }

      let dateFormat: "america" | "europe" = "america";
      if (process.stdout.isTTY) {
        const seed = configSchema.parse({ currentWorkspace: "local" });
        ({ dateFormat, nerdfont } = await runInstallForm(seed));
      } else {
        dateFormat = flags["date-format"] ?? "america";
        nerdfont = flags.nerdfont;
      }

      // The local workspace folder, and config.md naming it the current one.
      await mkdir(workspacePath("local"), { recursive: true });
      await writeConfig(
        configSchema.parse({ currentWorkspace: "local", dateFormat, nerdfont, theme: "" }),
      );
    } catch (error) {
      // install owns its own outcome line, so the failure is reported here rather
      // than re-thrown to the top-level handler that prints a bare message.
      const glyphs = createGlyphs(nerdfont);
      console.error(
        `${glyphs.failure} something went wrong during installation. error: ${summarize(error)}`,
      );
      process.exit(1);
    }

    const glyphs = createGlyphs(nerdfont);
    console.log(`${glyphs.success} taskthing installed successfully!`);
  },
});
