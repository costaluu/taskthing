import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

import { createGlobalKvStore, createKvStore } from "../kv-store";
import {
  allWorkspaces,
  currentWorkspaceName,
  dataHome,
  gatherTaskGroups,
  now,
  readConfig,
  workspacePath,
  type TaskFilters,
} from "../porcelain";
import type { TaskGroup } from "../task-list";
import { presenter } from "../presenter";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "list" as const,
  description: "list tasks",
  options: {
    global: option(z.coerce.boolean().default(false), {
      description: "list across every workspace",
      argumentKind: "flag",
    }),
    checked: option(z.coerce.boolean().default(false), {
      description: "include completed tasks",
      argumentKind: "flag",
    }),
    deleted: option(z.coerce.boolean().default(false), {
      description: "show the bin instead of the live tasks",
      argumentKind: "flag",
    }),
    starred: option(z.coerce.boolean().default(false), {
      description: "only starred tasks",
      argumentKind: "flag",
    }),
    hasDescription: option(z.coerce.boolean().default(false), {
      description: "only tasks that carry a description",
      argumentKind: "flag",
    }),
    period: option(z.string().optional(), {
      description: "only tasks due within a window, e.g. 2d, 3m, 1y",
    }),
    "in-board": option(z.string().optional(), {
      description: "only tasks on a board (name, number or inbox)",
    }),
    workspace: workspaceOption(),
  },
  handler: async ({ flags }) => {
    const filters: TaskFilters = {
      checked: flags.checked,
      deleted: flags.deleted,
      starred: flags.starred,
      hasDescription: flags.hasDescription,
      period: flags.period,
      inBoard: flags["in-board"],
    };

    // A global listing spans workspaces, so its numbers do too: they are handed
    // out from a store beside the workspaces, and each remembers which one its
    // task lives in.
    const global = flags.global;
    const names = global
      ? await allWorkspaces()
      : [
          flags.workspace !== undefined && flags.workspace.length > 0
            ? flags.workspace
            : await currentWorkspaceName(),
        ];
    const store = global
      ? createGlobalKvStore(dataHome())
      : createKvStore(workspacePath(names[0]!), "tasks");

    // A listing hands out fresh numbers, and the most recent listing wins: every
    // numbering it replaces is cleared, or a stale one would keep answering for
    // numbers the user is now reading off a different list.
    await store.reset();
    await Promise.all(
      global
        ? names.map((name) => createKvStore(workspacePath(name), "tasks").reset())
        : [createGlobalKvStore(dataHome()).reset()],
    );

    // The rows are gathered once, however they end up being drawn: the numbers a
    // listing hands out have to be the same whether the user is looking at the
    // styled view or piping it into something. A global listing spans workspaces,
    // so its board groups carry a workspace name (mehorias item 2).
    const groups: TaskGroup[] = [];
    for (const name of names) {
      groups.push(...(await gatherTaskGroups(name, filters, store, global)));
    }

    await presenter().taskList(groups, now(), await readConfig());
  },
});
