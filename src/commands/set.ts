import { defineCommand } from "@bunli/core";

import { INBOX, type Mdwal } from "../mdwal";
import type { TaskOutcome } from "../task-message";
import { withDtstart } from "../recurrence";
import {
  boardRepository,
  datedOutcome,
  openWorkspace,
  parseDate,
  reportTask,
  resolveBoard,
  resolveTask,
  taskRepository,
  type TaskRepository,
} from "../porcelain";
import type { Task } from "../schema";
import { workspaceOption } from "../command-options";
import { rejectUnknownSubcommand, subcommands } from "../hybrid-command";

// `set <field> <id> <value…>` — one subcommand per field a task carries. The
// field is a subcommand rather than a positional so an unknown one is refused by
// the router (and listed in help) instead of by a hand-rolled check.

/**
 * Resolve `<id>` to its task and hand the field's body the value the user typed.
 * The whole tail is the value, so a multi-word title needs no quoting.
 */
async function editTask(
  positional: string[],
  workspace: string | undefined,
  edit: (
    task: Task,
    value: string,
    context: { tasks: TaskRepository; mdwal: Mdwal; root: string },
  ) => Promise<TaskOutcome>,
): Promise<void> {
  await reportTask("update", async () => {
    const [reference, ...rest] = positional;
    const { root, id } = await resolveTask(reference ?? "", workspace);
    const { mdwal } = await openWorkspace(root);
    const tasks = taskRepository(mdwal);

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    return await edit(task, rest.join(" "), { tasks, mdwal, root });
  });
}

const options = { workspace: workspaceOption() };

/** The fields `set` may write on a task. */
const TASK_FIELDS = ["title", "description", "date-time", "board"] as const;

export default defineCommand({
  name: "set" as const,
  description: "edit a task field",
  handler: rejectUnknownSubcommand(
    (field) => `a task has no ${field} — try ${TASK_FIELDS.join(", ")}`,
  ),
  commands: subcommands([
    defineCommand({
      name: "title" as const,
      description: "retitle a task",
      options,
      handler: async ({ flags, positional }) => {
        await editTask(positional, flags.workspace, async (task, value, { tasks }) => {
          await tasks.update({ ...task, title: value });
          return { title: value, predicate: "title updated" };
        });
      },
    }),

    defineCommand({
      name: "description" as const,
      description: "set a task's description",
      options,
      handler: async ({ flags, positional }) => {
        await editTask(positional, flags.workspace, async (task, value, { tasks }) => {
          await tasks.update({ ...task, description: value });
          return { title: task.title, predicate: "description updated" };
        });
      },
    }),

    defineCommand({
      name: "date-time" as const,
      description: "schedule a task, or clear its schedule with an empty value",
      options,
      handler: async ({ flags, positional }) => {
        // A task's date *is* its DTSTART: setting it moves the task in time, and
        // for a recurring one moves where the series starts, keeping the rule
        // intact. An empty value clears the schedule (rrule → null), leaving an
        // undated task.
        await reportTask("update", async (config) => {
          const [reference, ...rest] = positional;
          const { root, id } = await resolveTask(reference ?? "", flags.workspace);
          const { mdwal } = await openWorkspace(root);
          const tasks = taskRepository(mdwal);

          const task = await tasks.findById(id);
          if (task === null) throw new Error(`no such task: ${reference}`);
          const value = rest.join(" ");

          if (value === "") {
            await tasks.update({ ...task, rrule: null });
            return { title: task.title, predicate: "schedule removed" };
          }

          const rrule = withDtstart(task.rrule, parseDate(value));
          await tasks.update({ ...task, rrule });
          return datedOutcome(task.title, "schedule updated", rrule, config);
        });
      },
    }),

    defineCommand({
      name: "board" as const,
      description: "move a task to a board (name, number or inbox)",
      options,
      handler: async ({ flags, positional }) => {
        // Move a task between boards (mehorias item 4). The board is named by
        // name/number/inbox; an empty value moves it back to the inbox — the
        // "no board" state — mirroring how an empty date-time clears the schedule.
        await editTask(positional, flags.workspace, async (task, value, { tasks, mdwal, root }) => {
          const boards = boardRepository(mdwal);
          const boardId = value === "" ? INBOX : await resolveBoard(boards, root, value);
          await tasks.update({ ...task, board: boardId });
          const boardName =
            boardId === INBOX ? "inbox" : ((await boards.findById(boardId))?.name ?? value);
          return {
            title: task.title,
            predicate: "moved",
            value: { connector: " to ", text: boardName },
          };
        });
      },
    }),
  ]),
});
