import { defineCommand } from "@bunli/core";

import { parseAddInput } from "../add-input";
import { INBOX } from "../mdwal";
import {
  boardRepository,
  datedOutcome,
  now,
  openWorkspace,
  reportTask,
  resolveBoard,
  targetWorkspace,
  taskRepository,
} from "../porcelain";
import { workspaceOption } from "../command-options";

export default defineCommand({
  name: "add" as const,
  description: "add a task (d:[…] date, r:[…] recurrence, b:[…] board)",
  options: {
    workspace: workspaceOption(),
  },
  handler: async ({ flags, positional }) => {
    const input = positional.join(" ");

    await reportTask("create", async (config) => {
      const root = await targetWorkspace(flags.workspace);
      const { mdwal } = await openWorkspace(root);
      const { title, rrule, board } = parseAddInput(input, now());

      // A `b:[…]` tag names the board (by name/number/inbox); without one the task
      // falls to the inbox. Resolving here surfaces an unknown board as this
      // command's failure line rather than a silent miss.
      const boards = boardRepository(mdwal);
      const boardId = board === null ? INBOX : await resolveBoard(boards, root, board);

      const tasks = taskRepository(mdwal);
      await tasks.create({ title, rrule, board: boardId });

      // Confirm the board only when the user actually chose one — the inbox is the
      // default, so it needs no announcing. The "in <board>" chip sits before the
      // next-occurrence tail.
      const outcome = datedOutcome(title, "created", rrule, config);
      if (boardId !== INBOX) {
        const boardName = (await boards.findById(boardId))?.name ?? board!;
        outcome.value = { connector: " in ", text: boardName };
      }
      return outcome;
    });
  },
});
