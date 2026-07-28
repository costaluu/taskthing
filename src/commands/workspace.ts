import { defineCommand, option } from "@bunli/core";
import { mkdir, rename, rm } from "node:fs/promises";
import { z } from "zod";

import {
  allWorkspaces,
  assertWorkspaceExists,
  currentWorkspaceName,
  openWorkspace,
  readConfig,
  reportWorkspace,
  setCurrentWorkspace,
  targetWorkspaceName,
  workspacePath,
} from "../porcelain";
import { renderWorkspaceList } from "../tui";
import { workspaceOption } from "../command-options";
import { rejectUnknownSubcommand, subcommands } from "../hybrid-command";

/** `workspace list` is also reachable as the top-level `workspaces` shortcut. */
export async function listWorkspaces(): Promise<void> {
  // The listing marks the current one, since every unscoped command acts on
  // it and the user needs to know which that is. A TTY gets the Selection-
  // style list; piped stays the plain, greppable `* name` lines.
  const current = await currentWorkspaceName();
  const workspaces = await allWorkspaces();
  if (process.stdout.isTTY) {
    await renderWorkspaceList(workspaces, current, await readConfig());
  } else {
    for (const workspace of workspaces) {
      console.log(workspace === current ? `* ${workspace}` : `  ${workspace}`);
    }
  }
}

/** `workspace use <name>` is also reachable as the top-level `use` shortcut. */
export async function useWorkspace(name: string): Promise<void> {
  await reportWorkspace("switch workspace", async () => {
    await assertWorkspaceExists(name);
    await setCurrentWorkspace(name);
    return { name, predicate: "now in use" };
  });
}

const confirmOption = (description: string) =>
  option(z.coerce.boolean().default(false), { description, argumentKind: "flag" });

export default defineCommand({
  name: "workspace" as const,
  description: "create, switch and remove workspaces",
  handler: rejectUnknownSubcommand((verb) => `unknown workspace command: ${verb}`),
  commands: subcommands([
    defineCommand({
      name: "list" as const,
      description: "list workspaces, marking the current one",
      handler: async () => {
        await listWorkspaces();
      },
    }),

    defineCommand({
      name: "create" as const,
      description: "create a workspace",
      handler: async ({ positional }) => {
        const name = positional[0] ?? "";
        await reportWorkspace("create this workspace", async () => {
          await mkdir(workspacePath(name), { recursive: true });
          return { name, predicate: "created" };
        });
      },
    }),

    defineCommand({
      name: "use" as const,
      description: "switch the current workspace",
      handler: async ({ positional }) => {
        await useWorkspace(positional[0] ?? "");
      },
    }),

    defineCommand({
      name: "rename" as const,
      description: "rename a workspace",
      handler: async ({ positional }) => {
        const [name, second] = positional;
        await reportWorkspace("rename this workspace", async () => {
          await assertWorkspaceExists(name ?? "");
          await rename(workspacePath(name!), workspacePath(second ?? ""));
          // The rename follows the user: renaming the workspace they are working in
          // must not leave them pointing at a name that no longer exists.
          if ((await currentWorkspaceName()) === name) await setCurrentWorkspace(second!);
          return {
            name: name!,
            predicate: "renamed",
            value: { connector: " to ", text: second ?? "" },
          };
        });
      },
    }),

    defineCommand({
      name: "delete" as const,
      description: "delete a workspace's local folder",
      options: {
        confirm: confirmOption("confirm that the local folder is removed"),
      },
      handler: async ({ flags, positional }) => {
        const name = positional[0] ?? "";
        await reportWorkspace("delete this workspace", async () => {
          await assertWorkspaceExists(name);
          if ((await currentWorkspaceName()) === name) {
            throw new Error(
              `${name} is the current workspace — \`taskthing use <other>\` before deleting it`,
            );
          }
          // Deleting removes the local folder outright, so it is never automatic.
          if (!flags.confirm) {
            throw new Error(`deleting ${name} removes its local folder — pass --confirm`);
          }
          await rm(workspacePath(name), { recursive: true, force: true });
          return { name, predicate: "deleted" };
        });
      },
    }),

    /**
     * Attach the workspace to a git remote, or detach it. Association is what turns
     * a workspace remote: from then on its operations are logged as events for peers
     * to replay, and detaching leaves the remote's content untouched.
     */
    defineCommand({
      name: "remote" as const,
      description: "attach or detach this workspace's git remote",
      handler: rejectUnknownSubcommand((verb) => `unknown remote command: ${verb}`),
      commands: subcommands([
        defineCommand({
          name: "add" as const,
          description: "attach a git remote by URL",
          options: {
            confirm: confirmOption("confirm destroying local work to join the remote"),
            workspace: workspaceOption(),
          },
          handler: async ({ flags, positional }) => {
            const url = positional[0];
            await reportWorkspace("add this remote", async () => {
              if (url === undefined) throw new Error("remote add needs a URL");
              const name = await targetWorkspaceName(flags.workspace);
              const { manager } = await openWorkspace(workspacePath(name));
              // Joining a remote that already has this peer's branch while there is
              // local work is unreconcilable; the manager refuses without this.
              await manager.init(url, { confirmDestroy: flags.confirm });
              return { name, predicate: "remote added", value: { connector: ": ", text: url } };
            });
          },
        }),

        defineCommand({
          name: "remove" as const,
          description: "detach this workspace's git remote",
          options: {
            workspace: workspaceOption(),
          },
          handler: async ({ flags }) => {
            await reportWorkspace("remove this remote", async () => {
              const name = await targetWorkspaceName(flags.workspace);
              const { manager } = await openWorkspace(workspacePath(name));
              await manager.disassociate();
              return { name, predicate: "remote removed" };
            });
          },
        }),
      ]),
    }),
  ]),
});
