import { nextOccurrence } from "./recurrence";
import {
  datedOutcome,
  openWorkspace,
  reportTask,
  resolveTask,
  taskRepository,
} from "./porcelain";

// The task verbs that come in pairs: complete/reopen, star/unstar, delete/recover
// are each one operation with opposite values, so the pair shares a body and the
// two commands differ only in the value they pass.

export async function completeTask(
  reference: string,
  value: boolean,
  workspace?: string,
): Promise<void> {
  await reportTask(value ? "complete" : "reopen", async (config) => {
    const { root, id } = await resolveTask(reference, workspace);
    const { mdwal } = await openWorkspace(root);
    const tasks = taskRepository(mdwal);

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    await tasks.update({ ...task, completed: value });

    // Only completing mints: reopening a task by mistake must not create an
    // occurrence, so an uncheck stops here.
    if (!value) return { title: task.title, predicate: "reopened" };

    // A recurring task lives on as a new occurrence, inheriting the rule. The one
    // just completed keeps its own record — occurrences are never recycled — and
    // the feedback names when that next occurrence falls due.
    const next = task.rrule === null ? null : nextOccurrence(task.rrule);
    if (next !== null) {
      // A new entity, so a new id: ids are never recycled, and each occurrence
      // keeps its own history.
      const { id: _completed, ...fields } = task;
      await tasks.create({ ...fields, completed: false, rrule: next });
      return datedOutcome(task.title, "completed", next, config);
    }
    return { title: task.title, predicate: "completed" };
  });
}

export async function starTask(
  reference: string,
  value: boolean,
  workspace?: string,
): Promise<void> {
  await reportTask(value ? "star" : "unstar", async () => {
    const { root, id } = await resolveTask(reference, workspace);
    const { mdwal } = await openWorkspace(root);
    const tasks = taskRepository(mdwal);

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    await tasks.update({ ...task, star: value });
    return { title: task.title, predicate: value ? "starred" : "unstarred" };
  });
}

/** Soft-delete and recover are the same operation with opposite values. */
export async function setTaskDeleted(
  reference: string,
  value: boolean,
  workspace?: string,
): Promise<void> {
  await reportTask(value ? "delete" : "recover", async () => {
    const { root, id } = await resolveTask(reference, workspace);
    const { mdwal } = await openWorkspace(root);
    const tasks = taskRepository(mdwal);

    // Read the task first so its title is available for the outcome line — the
    // soft-delete/recover itself only reports whether the task existed.
    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    const done = value ? await tasks.delete(id) : await tasks.recover(id);
    if (!done) throw new Error(`no such task: ${reference}`);
    return { title: task.title, predicate: value ? "deleted" : "recovered" };
  });
}
