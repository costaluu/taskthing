import { defineCommand } from "@bunli/core";

import { MIGRATIONS } from "../build";
import type { MigrationRow } from "../migration-list";
import { readConfig, recordedMigrations, targetWorkspace } from "../porcelain";
import { presenter } from "../presenter";
import { workspaceOption } from "../command-options";

/**
 * Report the workspace's migration state: every known migration (the binary's
 * embedded set, plus any already recorded here) with whether it has been applied
 * — so the user can audit whether they are fully migrated (Spec 0003 story 33).
 */
export default defineCommand({
  name: "migrations" as const,
  description: "report applied and pending migrations",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    const root = await targetWorkspace(flags.workspace);
    const recorded = await recordedMigrations(root);
    const versions = [...new Set([...MIGRATIONS.map((m) => m.version), ...recorded])].sort((a, b) =>
      Bun.semver.order(a, b),
    );
    const rows: MigrationRow[] = versions.map((version, i) => ({
      number: i + 1,
      version,
      applied: recorded.has(version),
    }));

    await presenter().migrationList(rows, await readConfig());
  },
});
