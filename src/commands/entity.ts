import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

import { rejectUnknownSubcommand, subcommands } from "../hybrid-command";
import { authorEntityLog } from "../porcelain";

/**
 * The plumber `entity` command: a dev-time tool for authoring the structural
 * migration that introduces/renames/removes a fixed entity folder. It prints the
 * mdwal log line(s) to embed in the binary; `--log` signals that authoring intent
 * (without it there is nothing to produce).
 */
const logOption = option(z.coerce.boolean().default(false), {
  description: "emit the migration line (required — this command only authors)",
  argumentKind: "flag",
});

function entityOp(op: "create" | "rename" | "delete", description: string, usage: string) {
  return defineCommand({
    name: op,
    description,
    options: { log: logOption },
    handler: async ({ flags, positional }) => {
      if (!flags.log) {
        throw new Error("entity authors migrations: pass --log to emit the migration line");
      }
      const [name, newName] = positional;
      if (name === undefined) throw new Error(`usage: ${usage}`);
      process.stdout.write(await authorEntityLog(op, name, newName));
    },
  });
}

export default defineCommand({
  name: "entity" as const,
  description: "author a structural migration for an entity folder (plumber)",
  handler: rejectUnknownSubcommand(
    (op) => `unknown entity op: ${op} (expected create|rename|delete)`,
  ),
  commands: subcommands([
    entityOp("create", "author a folder creation", "taskthing entity create <name> --log"),
    entityOp("rename", "author a folder rename", "taskthing entity rename <name> <new-name> --log"),
    entityOp("delete", "author a folder removal", "taskthing entity delete <name> --log"),
  ]),
});
