import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

import { subcommands } from "../hybrid-command";
import { buildUpdater, readConfig } from "../porcelain";
import { presenter } from "../presenter";
import { spinnerMessages } from "../spinner-messages";

// The check/apply pair share their shape: slow work behind a spinner, whose
// success line the work itself picks (latest vs. a pending update).
async function spin(
  pending: string,
  failure: (error: unknown) => string,
  run: () => Promise<string>,
): Promise<void> {
  await presenter().progressDynamic(pending, failure, run, await readConfig());
}

const forceOption = option(z.coerce.boolean().default(false), {
  description: "check even if the cached answer is still fresh",
  argumentKind: "flag",
});

/** Is a newer release available? The bare `update` asks this too. */
async function check(force: boolean): Promise<void> {
  // The three outcomes (latest / pending <cur> → <target> / error) use
  // CONTEXT's copy; the success line is chosen from the result, so it goes
  // through the plain Spinner rather than the fixed-copy command spinner.
  const updater = buildUpdater();
  await spin(spinnerMessages.updateCheck.pending, spinnerMessages.updateCheck.failure, async () => {
    const result = await updater.check({ force });
    return result.status === "latest"
      ? spinnerMessages.updateCheck.latest
      : spinnerMessages.updateCheck.update(result.current, result.target);
  });
}

export default defineCommand({
  name: "update" as const,
  description: "self-update from GitHub",
  options: { force: forceOption },
  handler: async ({ flags }) => {
    await check(flags.force);
  },
  commands: subcommands([
    defineCommand({
      name: "check" as const,
      description: "report whether a newer release is available",
      options: { force: forceOption },
      handler: async ({ flags }) => {
        await check(flags.force);
      },
    }),

    defineCommand({
      name: "apply" as const,
      description: "download and swap in the newest release",
      handler: async () => {
        const updater = buildUpdater();

        // Running `update apply` is itself the confirmation — no y/n form.
        const confirm = async () => true;

        await presenter().downloadProgress(
          spinnerMessages.updateApply.pending,
          spinnerMessages.updateApply.failure,
          async (report) => {
            const result = await updater.apply({
              confirm,
              onProgress: (downloaded, total) => report({ kind: "downloading", downloaded, total }),
              onApplying: () => report({ kind: "applying" }),
            });
            if (result.status === "up-to-date") return spinnerMessages.updateCheck.latest;
            if (result.status === "declined") return "update cancelled";
            return spinnerMessages.updateApply.success(result.target);
          },
          await readConfig(),
        );
      },
    }),
  ]),
});
