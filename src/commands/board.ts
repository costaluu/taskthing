import { defineCommand, option } from "@bunli/core";
import { z } from "zod";

import type { BoardRow } from "../board-list";
import { INBOX } from "../mdwal";
import { createKvStore } from "../kv-store";
import {
  boardRepository,
  openWorkspace,
  readConfig,
  reportBoard,
  resolveBoard,
  targetWorkspace,
  taskRepository,
} from "../porcelain";
import { presenter } from "../presenter";
import { workspaceOption } from "../command-options";
import { rejectUnknownSubcommand, subcommands } from "../hybrid-command";

// The board entity's own verb tree. Boards were reached through a `--board` flag
// on the task verbs before; they are a group now, so the router — not each
// handler — decides which entity a command acts on.

const options = { workspace: workspaceOption() };

/** `board list` is also reachable as the top-level `boards` shortcut. */
export async function listBoards(workspace?: string): Promise<void> {
  const root = await targetWorkspace(workspace);
  const { mdwal } = await openWorkspace(root);
  const boards = boardRepository(mdwal);

  // Boards number independently of tasks, in a store of their own.
  const store = createKvStore(root, "boards");
  await store.reset();

  const rows: BoardRow[] = [];
  for (const board of await boards.filter((board) => !board.deleted)) {
    const number = await store.set(board.id);
    rows.push({
      number,
      board: { id: board.id, name: board.name, icon: board.icon, color: board.color },
    });
  }

  await presenter().boardList(rows, await readConfig());
}

/** The fields `board set` may write; a board has no others to offer. */
const BOARD_FIELDS = ["name", "icon", "color"] as const;

/** `board set <field>` — one subcommand per field. */
function setBoardField(
  field: "name" | "icon" | "color",
  action: string,
  describe: (previous: string, next: string) => { predicate: string; connector: string },
) {
  return defineCommand({
    name: field,
    description: action,
    options,
    handler: async ({ flags, positional }) => {
      await reportBoard(action, async () => {
        const [reference, ...rest] = positional;

        const root = await targetWorkspace(flags.workspace);
        const { mdwal } = await openWorkspace(root);
        const boards = boardRepository(mdwal);

        const id = await resolveBoard(boards, root, reference ?? "");
        const board = await boards.findById(id);
        if (board === null) throw new Error(`no such board: ${reference}`);

        const next = rest.join(" ");
        await boards.update({ ...board, [field]: next });

        // Renaming keeps the old name as the primary chip and names the new one;
        // icon/color echo back the value the user just applied.
        const { predicate, connector } = describe(board.name, next);
        return { name: board.name, predicate, value: { connector, text: next } };
      });
    },
  });
}

export default defineCommand({
  name: "board" as const,
  description: "create, list and edit boards",
  handler: rejectUnknownSubcommand((verb) => `unknown board command: ${verb}`),
  commands: subcommands([
    defineCommand({
      name: "add" as const,
      description: "create a board",
      options: {
        // `--icon=<glyph>` sets the board's icon at creation; without it the board
        // keeps the schema default (no icon).
        icon: option(z.string().default(""), { description: "the board's icon" }),
        workspace: workspaceOption(),
      },
      handler: async ({ flags, positional }) => {
        const name = positional.join(" ");
        await reportBoard("create this board", async () => {
          const { mdwal } = await openWorkspace(await targetWorkspace(flags.workspace));
          const boards = boardRepository(mdwal);
          await boards.create({ name, icon: flags.icon });
          return { name, predicate: "created" };
        });
      },
    }),

    defineCommand({
      name: "list" as const,
      description: "list boards",
      options,
      handler: async ({ flags }) => {
        await listBoards(flags.workspace);
      },
    }),

    defineCommand({
      name: "set" as const,
      description: "edit a board field",
      handler: rejectUnknownSubcommand(
        (field) => `a board has no ${field} — try ${BOARD_FIELDS.join(", ")}`,
      ),
      commands: subcommands([
        setBoardField("name", "rename this board", () => ({
          predicate: "renamed",
          connector: " to ",
        })),
        setBoardField("icon", "update this board's icon", () => ({
          predicate: "icon updated",
          connector: ": ",
        })),
        setBoardField("color", "update this board's color", () => ({
          predicate: "color updated",
          connector: ": ",
        })),
      ]),
    }),

    defineCommand({
      name: "delete" as const,
      description: "soft-delete a board, moving its tasks to the inbox",
      options,
      handler: async ({ flags, positional }) => {
        const reference = positional[0] ?? "";
        await reportBoard("delete this board", async () => {
          const root = await targetWorkspace(flags.workspace);
          const { mdwal } = await openWorkspace(root);
          const boards = boardRepository(mdwal);
          const tasks = taskRepository(mdwal);

          const id = await resolveBoard(boards, root, reference);
          const board = await boards.findById(id);
          if (board === null) throw new Error(`no such board: ${reference}`);

          // A task may never point at a board that is gone, so each one falls back to
          // the virtual board — one field event apiece, so the move converges under LWW
          // like any other edit. Recovering the board later brings back the board, not
          // these tasks: they now genuinely belong to the inbox.
          const orphaned = await tasks.filter((task) => task.board === id);
          for (const task of orphaned) {
            await tasks.update({ ...task, board: INBOX });
          }

          await boards.delete(id);
          // The note is shown only when tasks actually moved, so a board with none
          // reads simply as "deleted".
          return orphaned.length > 0
            ? { name: board.name, predicate: "deleted", suffix: ". its tasks were moved to inbox" }
            : { name: board.name, predicate: "deleted" };
        });
      },
    }),

    defineCommand({
      name: "recover" as const,
      description: "restore a deleted board",
      options,
      handler: async ({ flags, positional }) => {
        const reference = positional[0] ?? "";
        await reportBoard("recover this board", async () => {
          const root = await targetWorkspace(flags.workspace);
          const { mdwal } = await openWorkspace(root);
          const boards = boardRepository(mdwal);

          const id = await resolveBoard(boards, root, reference);
          const board = await boards.findById(id);
          if (board === null) throw new Error(`no such board: ${reference}`);
          if (!(await boards.recover(id))) throw new Error(`no such board: ${reference}`);
          return { name: board.name, predicate: "recovered" };
        });
      },
    }),
  ]),
});
