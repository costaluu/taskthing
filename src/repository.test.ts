import { test, expect } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import { createMdwal, type Mdwal } from "./mdwal";
import { createRepository } from "./repository";

// The repository seam is its seven methods against a temp workspace, over a
// real mdwal — the point of the adapter is that model objects go in and come
// back out, so the engine underneath is exercised, never stubbed.
const taskSchema = z.object({
  title: z.string(),
  deleted: z.boolean().default(false),
  labels: z.array(z.string()).default([]),
});
type Task = z.infer<typeof taskSchema> & { id: string };
type TaskInput = z.input<typeof taskSchema>;

async function workspace(timestamps: bigint[]): Promise<[string, Mdwal]> {
  const root = await mkdtemp(join(tmpdir(), "repo-"));
  let i = 0;
  return [
    root,
    createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: () => timestamps[i++]!,
      author: () => "alice",
    }),
  ];
}

test("update writes only the fields that actually changed, one event each", async () => {
  const [root, mdwal] = await workspace([1000n, 2000n]);
  try {
    const tasks = createRepository<Task, TaskInput>(mdwal, "task");
    const created = await tasks.create({ title: "a", deleted: false });

    const ok = await tasks.update({ ...created!, title: "b" });
    expect(ok).toBe(true);

    expect((await tasks.findById(created!.id))!.title).toBe("b");

    // One atomic field event — `deleted` was passed in unchanged, so it must not
    // produce an event that could win a later LWW race against a peer's edit.
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "UPDATE_FIELD"]);
    expect(events[1]!.payload.field).toBe("title");

    // Updating something that doesn't exist reports failure rather than
    // creating it.
    expect(
      await tasks.update({ id: "nope", title: "x", deleted: false, labels: [] }),
    ).toBe(false);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("findAll returns every entity and filter runs a predicate over them", async () => {
  const [root, mdwal] = await workspace([1000n, 2000n, 3000n, 4000n]);
  try {
    const tasks = createRepository<Task, TaskInput>(mdwal, "task");
    const a = await tasks.create({ title: "buy milk", deleted: false });
    const b = await tasks.create({ title: "buy bread", deleted: false });
    const c = await tasks.create({ title: "call mom", deleted: true });

    // findAll excludes nothing structurally — a soft-deleted entity stays
    // retrievable, so callers decide whether to filter it.
    const all = await tasks.findAll();
    expect(all.map((t) => t.id).sort()).toEqual([a!.id, b!.id, c!.id].sort());

    // filter is an in-memory predicate over the materialized models.
    const buys = await tasks.filter((t) => t.title.startsWith("buy"));
    expect(buys.map((t) => t.title).sort()).toEqual(["buy bread", "buy milk"]);

    // ...so filtering out the deleted ones is the caller's choice, not a rule
    // baked into the repository.
    const live = await tasks.filter((t) => !t.deleted);
    expect(live.map((t) => t.id).sort()).toEqual([a!.id, b!.id].sort());
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("delete soft-deletes and keeps the entity retrievable; recover undoes it", async () => {
  const [root, mdwal] = await workspace([1000n, 2000n, 3000n]);
  try {
    const tasks = createRepository<Task, TaskInput>(mdwal, "task");
    const created = await tasks.create({ title: "x", deleted: false });

    expect(await tasks.delete(created!.id)).toBe(true);

    // Soft-delete: the model is still there, flagged — the file is never removed
    // and the deletion itself is an ordinary field event, subject to LWW.
    const deleted = await tasks.findById(created!.id);
    expect(deleted!.deleted).toBe(true);
    expect(await tasks.findAll()).toHaveLength(1);

    expect(await tasks.recover(created!.id)).toBe(true);
    expect((await tasks.findById(created!.id))!.deleted).toBe(false);

    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "UPDATE_FIELD", "UPDATE_FIELD"]);
    expect(events.slice(1).map((e) => e.payload.field)).toEqual(["deleted", "deleted"]);

    // Deleting something that was never created reports failure.
    expect(await tasks.delete("nope")).toBe(false);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("update compares field values by content, not by identity", async () => {
  const [root, mdwal] = await workspace([1000n, 2000n, 3000n]);
  try {
    const tasks = createRepository<Task, TaskInput>(mdwal, "task");
    const created = await tasks.create({
      title: "a",
      deleted: false,
      labels: ["home", "urgent"],
    });

    // A caller who read the model, changed one field and wrote it back hands us
    // a *fresh* array for `labels` holding the same values.
    const ok = await tasks.update({
      ...created!,
      title: "b",
      labels: ["home", "urgent"],
    });
    expect(ok).toBe(true);

    // Only `title` really changed. Re-logging an untouched array would enter an
    // LWW race the caller never meant to start — and would beat a concurrent
    // peer's genuine edit to `labels`.
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.payload.field).filter((f) => f !== undefined)).toEqual(["title"]);

    // A real change to the array is still written.
    await tasks.update({ ...created!, title: "b", labels: ["home"] });
    const fields = (await mdwal.parseLog())
      .map((e) => e.payload.field)
      .filter((f) => f !== undefined);
    expect(fields).toEqual(["title", "labels"]);
    expect((await tasks.findById(created!.id))!.labels).toEqual(["home"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("create returns the persisted model and findById reads it back", async () => {
  const [root, mdwal] = await workspace([1000n]);
  try {
    const tasks = createRepository<Task, TaskInput>(mdwal, "task");

    const created = await tasks.create({ title: "buy milk", deleted: false });

    // The id is minted by the engine, so the caller learns it from the model
    // it gets back, not from the one it passed in.
    expect(created!.id).toBeString();
    expect(created!.title).toBe("buy milk");

    const found = await tasks.findById(created!.id);
    expect(found!.title).toBe("buy milk");

    // An id that was never minted is a miss, not an error.
    expect(await tasks.findById("nope")).toBeNull();
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
