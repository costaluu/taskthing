import type { Mdwal } from "./mdwal";
import {
  boardRepository,
  openWorkspace,
  reportBoard,
  reportTask,
  resolveBoard,
  resolveTask,
  targetWorkspace,
  taskRepository,
  type BoardRepository,
  type TaskRepository,
} from "./porcelain";
import type { BoardOutcome } from "./board-message";
import type { TaskOutcome } from "./task-message";
import type { Board, Config, Task } from "./schema";

// ── entity actions ───────────────────────────────────────────────────────────
//
// Acting on an entity the user named always ran the same five steps first:
// resolve what they typed to an id, open the workspace, build the repository,
// read the entity, and refuse if it isn't there. Every verb wrote those out
// again before doing its own one distinctive thing.
//
// They live here instead. A verb says which entity it acts on and what to do
// with it, and gets back the entity, its repository, and the config to format
// with. The reference rules — a number only means something after a listing,
// a board can be named or numbered or be the virtual inbox — are now in one
// place rather than restated at each call site.
//
// Creating is deliberately not here: `add` and `board add` have nothing to
// resolve, so they would only be borrowing the shape, not the behaviour.

export interface TaskContext {
  task: Task;
  tasks: TaskRepository;
  mdwal: Mdwal;
  root: string;
  config: Config;
}

export interface BoardContext {
  board: Board;
  boards: BoardRepository;
  mdwal: Mdwal;
  root: string;
  config: Config;
}

/**
 * Act on the task the user named, reporting the outcome under `verb`.
 *
 * `reference` is what they typed — a number from the last listing, or a
 * `workspace/id` pair from a global one. An unknown reference fails before
 * `edit` runs, so a verb never has to consider a missing task.
 */
export async function onTask(
  verb: string,
  reference: string,
  workspace: string | undefined,
  edit: (context: TaskContext) => Promise<TaskOutcome>,
): Promise<void> {
  await reportTask(verb, async (config) => {
    const { root, id } = await resolveTask(reference, workspace);
    const { mdwal } = await openWorkspace(root);
    const tasks = taskRepository(mdwal);

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    return await edit({ task, tasks, mdwal, root, config });
  });
}

/**
 * Act on the board the user named, reporting the outcome under `action`.
 *
 * A board is referred to by name, by number, or as the sentinel `inbox`; a
 * deleted one answers only to its raw id, which is what makes `board recover`
 * reachable at all.
 */
export async function onBoard(
  action: string,
  reference: string,
  workspace: string | undefined,
  edit: (context: BoardContext) => Promise<BoardOutcome>,
): Promise<void> {
  await reportBoard(action, async (config) => {
    const root = await targetWorkspace(workspace);
    const { mdwal } = await openWorkspace(root);
    const boards = boardRepository(mdwal);

    const board = await boards.findById(await resolveBoard(boards, root, reference));
    if (board === null) throw new Error(`no such board: ${reference}`);

    return await edit({ board, boards, mdwal, root, config });
  });
}
