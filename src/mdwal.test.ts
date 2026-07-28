import { test, expect } from "bun:test";
import { mkdtemp, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import { createMdwal, type Event } from "./mdwal";

// A minimal test schema. The mdwal seam is schema-agnostic: tests supply their
// own zod schema, a temp workspace directory, and a sequence of operations,
// then assert on the two observable outputs — the derived .md files (via read)
// and the events.log lines (via parseLog).
const taskSchema = z.object({ title: z.string() });

async function tempWorkspace(): Promise<string> {
  return await mkdtemp(join(tmpdir(), "mdwal-"));
}

// Deterministic clock: yields the given nanosecond timestamps in order, so
// event timestamps are asserted exactly without touching the wall clock.
function makeClock(values: bigint[]): () => bigint {
  let i = 0;
  return () => {
    const v = values[i++];
    if (v === undefined) throw new Error("clock exhausted");
    return v;
  };
}

// Deterministic id source: yields the given ids in order, so tests can drive
// what the generator offers (including ids mdwal must refuse).
function makeIds(values: string[]): () => string {
  let i = 0;
  return () => {
    const v = values[i++];
    if (v === undefined) throw new Error("ids exhausted");
    return v;
  };
}

test("createEntity in a remote workspace writes the entity file and logs one CREATE event", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n]),
      author: () => "alice",
    });

    const created = await mdwal.createEntity("task", { title: "buy milk" });

    // Read the derived file back through the public API → a typed object.
    const readBack = await mdwal.read("task", created.id);
    expect(readBack.id).toBe(created.id);
    expect(readBack.title).toBe("buy milk");

    // Exactly one CREATE event, carrying the full initial snapshot + author + ts.
    const events = await mdwal.parseLog();
    expect(events).toHaveLength(1);
    const [ev] = events;
    expect(ev.op).toBe("CREATE");
    expect(ev.entityType).toBe("task");
    expect(ev.entityId).toBe(created.id);
    expect(ev.ts).toBe(1000n);
    expect(ev.payload.author).toBe("alice");
    expect(ev.payload.snapshot.title).toBe("buy milk");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

function createEvent(
  entityType: string,
  id: string,
  snapshot: Record<string, unknown>,
  ts: bigint,
  version?: string,
): Event {
  return {
    ts,
    op: "CREATE",
    entityType,
    entityId: id,
    payload: { author: "alice", snapshot, version },
  };
}

function updateEvent(
  entityType: string,
  id: string,
  field: string,
  value: unknown,
  ts: bigint,
  version?: string,
): Event {
  return {
    ts,
    op: "UPDATE_FIELD",
    entityType,
    entityId: id,
    payload: { author: "alice", field, value, version },
  };
}

// priority string -> number, shipped at schema-change point 0.2.0 (ADR-0007).
const priorityToNumber = {
  version: "0.2.0",
  up: (ev: Event): Event => {
    if (ev.op === "CREATE" && "priority" in ev.payload.snapshot) {
      const snapshot = { ...ev.payload.snapshot, priority: Number(ev.payload.snapshot.priority) };
      return { ...ev, payload: { ...ev.payload, snapshot } };
    }
    if (ev.op === "UPDATE_FIELD" && ev.payload.field === "priority") {
      return { ...ev, payload: { ...ev.payload, value: Number(ev.payload.value) } };
    }
    return ev;
  },
};

test("replay applies a version-keyed transform to an event authored under an older schema", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "alice",
      version: "0.2.0",
      transforms: [priorityToNumber],
    });

    // An event authored at 0.1.0 carries a stringy priority; the log stays as-is.
    const id = "task-1";
    const old = createEvent("task", id, { title: "x", priority: "2" }, 1000n, "0.1.0");

    const state = mdwal.replay({}, [old]);
    // migrate-on-read rewrites it to the current-schema value (a number).
    expect(state[`task/${id}`]!.priority).toBe(2);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("replay refuses an event authored by a newer binary version (version barrier)", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "alice",
      version: "0.2.0",
      transforms: [],
    });

    // An event from a future binary can't be transformed down — replay must stop
    // and tell the user to update, never read future-schema data best-effort.
    const future = createEvent("task", "task-1", { title: "x" }, 1000n, "0.3.0");
    expect(() => mdwal.replay({}, [future])).toThrow(/update/i);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("replay resolves LWW per field and is independent of event order", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "alice",
    });

    const id = "task-1";
    const events = [
      createEvent("task", id, { title: "buy milk" }, 1000n),
      updateEvent("task", id, "title", "b", 2000n),
      updateEvent("task", id, "title", "a", 3000n),
    ];

    const forward = mdwal.replay({}, events);
    const reversed = mdwal.replay({}, [...events].reverse());

    // Convergence: two orderings of the same events reach identical state.
    expect(forward).toEqual(reversed);

    // Per-field LWW: the highest-timestamp value wins; the loser isn't applied.
    const key = `task/${id}`;
    expect(forward[key]!.title).toBe("a");
    expect(forward[key]!.title).not.toBe("b");
    expect(forward[key]!.title_lastModified).toBe(3000n);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("createdAt is the create timestamp; updatedAt is the greatest field _lastModified", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 5000n]),
      author: () => "alice",
    });
    const created = await mdwal.createEntity("task", { title: "x" });

    // At creation, every field was last modified at the create ts, so
    // updatedAt == createdAt.
    let entity = await mdwal.read("task", created.id);
    expect(entity.createdAt).toBe(1000n);
    expect(entity.updatedAt).toBe(1000n);

    // After a later field edit, createdAt is stable and updatedAt tracks the
    // most recent field change.
    await mdwal.updateField("task", created.id, "title", "y");
    entity = await mdwal.read("task", created.id);
    expect(entity.createdAt).toBe(1000n);
    expect(entity.updatedAt).toBe(5000n);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("purge drops this peer's events with ts <= threshold and keeps newer ones", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
    });
    const created = await mdwal.createEntity("task", { title: "a" });
    await mdwal.updateField("task", created.id, "title", "b");
    await mdwal.updateField("task", created.id, "title", "c");

    await mdwal.purge(2000n);

    // Only the post-threshold event survives; a consolidated event never remains.
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.ts)).toEqual([3000n]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("delete is a soft-delete that stays retrievable, and recover undoes it", async () => {
  const root = await tempWorkspace();
  try {
    const deletableSchema = z.object({ title: z.string(), deleted: z.boolean() });
    const mdwal = createMdwal({
      root,
      schemas: { task: deletableSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
    });
    const created = await mdwal.createEntity("task", { title: "x", deleted: false });

    // Soft-delete: deleted=true via a field update; the file is NOT removed.
    await mdwal.updateField("task", created.id, "deleted", true);
    const afterDelete = await mdwal.read("task", created.id);
    expect(afterDelete.deleted).toBe(true);
    expect(
      await Bun.file(join(root, "tasks", `${created.id}.md`)).exists(),
    ).toBe(true);

    // Recover: deleted=false through the same mechanism.
    await mdwal.updateField("task", created.id, "deleted", false);
    const afterRecover = await mdwal.read("task", created.id);
    expect(afterRecover.deleted).toBe(false);

    // Both are ordinary UPDATE_FIELD events (soft-delete is subject to LWW).
    const ops = (await mdwal.parseLog()).map((e) => e.op);
    expect(ops).toEqual(["CREATE", "UPDATE_FIELD", "UPDATE_FIELD"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("operations stamp the binary version, so a newer binary migrates from the real log", async () => {
  const root = await tempWorkspace();
  try {
    // Binary A at 0.1.0 authors a task whose priority is a string under its schema.
    const schemaV1 = z.object({ title: z.string(), priority: z.string() });
    const a = createMdwal({
      root,
      schemas: { task: schemaV1 },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
      version: "0.1.0",
    });
    const created = await a.createEntity("task", { title: "x", priority: "2" });
    await a.updateField("task", created.id, "priority", "5");

    // Every event it wrote carries the authoring binary's version.
    const log = await a.parseLog();
    expect(log.map((e) => e.payload.version)).toEqual(["0.1.0", "0.1.0"]);

    // Binary B at 0.2.0 replays A's real (unmigrated) log and migrate-on-reads
    // priority into a number — the log stays immutable, the derived value updates.
    const b = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "bob",
      version: "0.2.0",
      transforms: [priorityToNumber],
    });
    const state = b.replay({}, log);
    expect(state[`task/${created.id}`]!.priority).toBe(5);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a local workspace applies operations to files but writes no events.log", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "local",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
    });

    const created = await mdwal.createEntity("task", { title: "buy milk" });
    await mdwal.updateField("task", created.id, "title", "milk 2%");

    // Derived state is identical to a remote workspace — the _lastModified
    // metadata exists in both; only the presence of events.log differs.
    const readBack = await mdwal.read("task", created.id);
    expect(readBack.title).toBe("milk 2%");
    expect(readBack.title_lastModified).toBe(2000n);

    // ...but a local workspace logs nothing: no events, no events.log file.
    expect(await mdwal.parseLog()).toEqual([]);
    expect(await Bun.file(join(root, "events.log")).exists()).toBe(false);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

async function isDirectory(path: string): Promise<boolean> {
  try {
    return (await stat(path)).isDirectory();
  } catch {
    return false;
  }
}

test("createFolder creates an entity folder and logs one CREATE_FOLDER event", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n]),
      author: () => "alice",
    });

    await mdwal.createFolder("board");

    // The entity folder now exists in the workspace...
    expect(await isDirectory(join(root, "boards"))).toBe(true);

    // ...and the operation is recorded as one atomic folder event: the folder
    // name sits in the 3rd header field, the entity-id position stays empty.
    const events = await mdwal.parseLog();
    expect(events).toHaveLength(1);
    const ev = events[0]!;
    expect(ev.op).toBe("CREATE_FOLDER");
    expect(ev.entityType).toBe("board");
    expect(ev.entityId).toBe("");
    expect(ev.ts).toBe(1000n);
    expect(ev.payload.author).toBe("alice");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("renameFolder moves the entity folder with its contents and logs RENAME_FOLDER", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema, chore: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
    });
    const created = await mdwal.createEntity("task", { title: "buy milk" });

    await mdwal.renameFolder("task", "chore");

    // The folder moved and took its entities with it — the same entity is now
    // readable under the new type, unchanged.
    expect(await isDirectory(join(root, "tasks"))).toBe(false);
    expect((await mdwal.read("chore", created.id)).title).toBe("buy milk");

    // The header carries the *previous* folder name; the new one lives in the
    // payload, so a chain of renames replays unambiguously.
    const ev = (await mdwal.parseLog())[1]!;
    expect(ev.op).toBe("RENAME_FOLDER");
    expect(ev.entityType).toBe("task");
    expect(ev.entityId).toBe("");
    expect(ev.ts).toBe(2000n);
    expect(ev.payload.to).toBe("chore");
    expect(ev.payload.author).toBe("alice");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("renaming the workspace root logs the $root sentinel, inside the renamed folder", async () => {
  const parent = await tempWorkspace();
  const root = join(parent, "work");
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
    });
    const created = await mdwal.createEntity("task", { title: "buy milk" });

    // The workspace root is just another folder, addressed by the sentinel.
    await mdwal.renameFolder("$root", "play");

    const renamed = join(parent, "play");
    expect(await isDirectory(renamed)).toBe(true);
    expect(await isDirectory(root)).toBe(false);

    // The workspace keeps working after the rename: entities and log follow it.
    expect((await mdwal.read("task", created.id)).title).toBe("buy milk");
    const events = await mdwal.parseLog();
    expect(await Bun.file(join(renamed, "events.log")).exists()).toBe(true);

    // The header never carries the folder's current or previous *name* — only
    // the fixed sentinel — so several renames in a row replay unambiguously.
    const ev = events[1]!;
    expect(ev.op).toBe("RENAME_FOLDER");
    expect(ev.entityType).toBe("$root");
    expect(ev.payload.to).toBe("play");

    // A second rename still says $root, not "play".
    await mdwal.renameFolder("$root", "done");
    expect((await mdwal.parseLog())[2]!.entityType).toBe("$root");
  } finally {
    await rm(parent, { recursive: true, force: true });
  }
});

test("truncateFolder clears a folder's entities as one event, keeping the folder", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
    });
    const a = await mdwal.createEntity("task", { title: "a" });
    const b = await mdwal.createEntity("task", { title: "b" });

    await mdwal.truncateFolder("task");

    // The folder survives; its entities are gone — clearing all tasks is one
    // recorded action, not N deletions.
    expect(await isDirectory(join(root, "tasks"))).toBe(true);
    expect(mdwal.read("task", a.id)).rejects.toThrow();
    expect(mdwal.read("task", b.id)).rejects.toThrow();

    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "CREATE", "TRUNCATE_FOLDER"]);
    const ev = events[2]!;
    expect(ev.entityType).toBe("task");
    expect(ev.entityId).toBe("");
    expect(ev.ts).toBe(3000n);
    expect(ev.payload.author).toBe("alice");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("deleteFolder removes the entity folder and logs DELETE_FOLDER", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
    });
    await mdwal.createEntity("task", { title: "a" });

    await mdwal.deleteFolder("task");

    // Unlike truncate, the folder itself is gone; the log survives it (it lives
    // at the workspace root, not inside the entity folder).
    expect(await isDirectory(join(root, "tasks"))).toBe(false);

    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "DELETE_FOLDER"]);
    const ev = events[1]!;
    expect(ev.entityType).toBe("task");
    expect(ev.entityId).toBe("");
    expect(ev.ts).toBe(2000n);
    expect(ev.payload.author).toBe("alice");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("readAll materializes every entity of a type, and is empty for an absent folder", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema, board: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
    });

    // A type whose folder was never created materializes as nothing, not an error.
    expect(await mdwal.readAll("task")).toEqual([]);

    const a = await mdwal.createEntity("task", { title: "a" });
    const b = await mdwal.createEntity("task", { title: "b" });
    await mdwal.createEntity("board", { title: "work" });

    const tasks = await mdwal.readAll("task");

    // Every entity of the type, typed exactly as `read` returns it — and nothing
    // from a sibling entity type.
    expect(tasks.map((t) => t.id).sort()).toEqual([a.id, b.id].sort());
    const byId = new Map(tasks.map((t) => [t.id, t]));
    expect(byId.get(a.id)!.title).toBe("a");
    expect(byId.get(b.id)).toEqual(await mdwal.read("task", b.id));
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a task created with no board defaults to the inbox sentinel, in file and event", async () => {
  const root = await tempWorkspace();
  try {
    // The virtual board is a schema default (ADR-0004): no real "inbox" board
    // entity exists, yet every task belongs to a board.
    const boardedSchema = z.object({
      title: z.string(),
      board: z.string().default("inbox"),
    });
    const mdwal = createMdwal({
      root,
      schemas: { task: boardedSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
    });

    const created = await mdwal.createEntity("task", { title: "buy milk" });
    expect(created.board).toBe("inbox");
    expect((await mdwal.read("task", created.id)).board).toBe("inbox");

    // The default is materialized into the CREATE snapshot, so a peer receiving
    // the event alone reconstructs the entity without knowing our schema.
    const ev = (await mdwal.parseLog())[0]!;
    expect(ev.payload.snapshot.board).toBe("inbox");

    // An explicit board is untouched by the default.
    const onBoard = await mdwal.createEntity("task", { title: "x", board: "b1" });
    expect(onBoard.board).toBe("b1");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("entity ids never collide with the inbox sentinel", async () => {
  const root = await tempWorkspace();
  try {
    // A rigged generator that offers the sentinel first: mdwal must refuse it
    // and take the next id, so a real board can never be minted as "inbox"
    // (ADR-0004) — the virtual board would otherwise be shadowed by an entity.
    const ids = makeIds(["inbox", "board-1"]);
    const mdwal = createMdwal({
      root,
      schemas: { board: taskSchema },
      mode: "remote",
      clock: makeClock([1000n]),
      author: () => "alice",
      ids,
    });

    const created = await mdwal.createEntity("board", { title: "work" });
    expect(created.id).toBe("board-1");

    // The refused id leaves no trace: no file, no event under "inbox".
    expect(await Bun.file(join(root, "boards", "inbox.md")).exists()).toBe(false);
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.entityId)).toEqual(["board-1"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an operation run with logging off mutates the workspace but appends no event", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n, 4000n]),
      author: () => "alice",
    });

    // A migration replays a static log with logging off: the workspace changes,
    // but nothing enters this peer's authored history (so it is never mistaken
    // for a user edit subject to LWW).
    const created = await mdwal.createEntity("task", { title: "a" }, { log: false });
    await mdwal.updateField("task", created.id, "title", "b", { log: false });
    await mdwal.createFolder("board", { log: false });

    // Files reflect every operation, LWW metadata included...
    const entity = await mdwal.read("task", created.id);
    expect(entity.title).toBe("b");
    expect(entity.title_lastModified).toBe(2000n);
    expect(await isDirectory(join(root, "boards"))).toBe(true);

    // ...but the remote workspace's log stayed untouched.
    expect(await mdwal.parseLog()).toEqual([]);

    // Logging is still on by default for ordinary operations.
    await mdwal.updateField("task", created.id, "title", "c");
    expect((await mdwal.parseLog()).map((e) => e.op)).toEqual(["UPDATE_FIELD"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the fixed entity folders are derived from the schema, not configured separately", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema, board: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "alice",
    });

    // Whatever entity types the schema declares are exactly the folders that
    // hold derived state — so adding an entity type in a migration updates this
    // set with no second place to keep in sync.
    expect(mdwal.folders().sort()).toEqual(["boards", "tasks"]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("snapshot reads the workspace's derived state back into replayable form", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema, board: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n, 3000n]),
      author: () => "alice",
      ids: makeIds(["t1", "b1"]),
    });
    await mdwal.createEntity("task", { title: "a" });
    await mdwal.createEntity("board", { title: "work" });
    await mdwal.updateField("task", "t1", "title", "b");

    // The derived files read back as the same shape replay folds over, so the
    // workspace on disk can serve as the base of a replay.
    expect(await mdwal.snapshot()).toEqual({
      "task/t1": {
        title: "b",
        title_lastModified: 3000n,
        title_lastModifiedBy: "alice",
        createdAt: 1000n,
      },
      "board/b1": {
        title: "work",
        title_lastModified: 2000n,
        title_lastModifiedBy: "alice",
        createdAt: 2000n,
      },
    });
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("materialize writes a replayed snapshot to disk without logging anything", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([]),
      author: () => "alice",
    });

    // The state a replay produced — no file for it exists yet.
    const state = mdwal.replay({}, [
      createEvent("task", "t1", { title: "a" }, 1000n),
      updateEvent("task", "t1", "title", "b", 3000n),
    ]);

    await mdwal.materialize(state);

    // Derived files now hold the folded values and their LWW companions, so a
    // later replay can fold over them.
    const entity = await mdwal.read("task", "t1");
    expect(entity.title).toBe("b");
    expect(entity.title_lastModified).toBe(3000n);
    expect(entity.title_lastModifiedBy).toBe("alice");
    expect(await mdwal.snapshot()).toEqual(state);

    // Materializing is re-deriving, not authoring: nothing enters the log.
    expect(await mdwal.parseLog()).toEqual([]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("parseEvents reads a log that came from somewhere other than this workspace", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
      ids: makeIds(["t1"]),
    });
    await mdwal.createEntity("task", { title: "a" });
    await mdwal.updateField("task", "t1", "title", "b");

    // A peer's log arrives as text — read out of git, never written to disk.
    // Parsing it must give the same events as parsing our own log file.
    const text = await Bun.file(join(root, "events.log")).text();
    expect(mdwal.parseEvents(text)).toEqual(await mdwal.parseLog());

    // An empty log is no events, not a parse error.
    expect(mdwal.parseEvents("")).toEqual([]);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("logExisting turns an already-stored entity into a retroactive CREATE", async () => {
  const root = await tempWorkspace();
  try {
    // A workspace that has been purely local so far: files, no log.
    let associated = false;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: async () => (associated ? "remote" : "local"),
      clock: makeClock([1000n, 9000n]),
      author: () => "alice",
      ids: makeIds(["t1"]),
    });
    const created = await mdwal.createEntity("task", { title: "buy milk" });
    expect(await mdwal.parseLog()).toEqual([]);

    // On associating, prior work has to enter the shared history — as a CREATE
    // carrying the entity's full current snapshot, stamped at the association
    // moment, under the id it already has.
    associated = true;
    await mdwal.logExisting("task", created.id);

    const events = await mdwal.parseLog();
    expect(events).toHaveLength(1);
    const ev = events[0]!;
    expect(ev.op).toBe("CREATE");
    expect(ev.entityId).toBe("t1");
    expect(ev.ts).toBe(9000n);
    expect(ev.payload.author).toBe("alice");
    expect(ev.payload.snapshot).toEqual({ title: "buy milk" });

    // The file is untouched: this records history, it doesn't rewrite state.
    expect((await mdwal.read("task", created.id)).title_lastModified).toBe(1000n);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("serializing a parsed log reproduces it, even with :: inside user text", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
      ids: makeIds(["t1"]),
    });

    // Field values are free-form user text and may contain the separator; the
    // fixed 4-field header is what keeps them from colliding with it.
    await mdwal.createEntity("task", { title: "a::b" });
    await mdwal.updateField("task", "t1", "title", "c::d::e");

    const text = await Bun.file(join(root, "events.log")).text();
    const events = mdwal.parseEvents(text);

    // Reading a log and writing it back must not perturb it.
    expect(mdwal.serializeLog(events)).toBe(text);
    expect(mdwal.parseEvents(mdwal.serializeLog(events))).toEqual(events);
    expect(events[1]!.payload.value).toBe("c::d::e");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("the workspace mode may be answered asynchronously", async () => {
  const root = await tempWorkspace();
  try {
    // Whether a workspace is remote is a fact about the world — for the manager,
    // whether the repository has a remote configured — and finding that out is
    // itself asynchronous. Every mutating operation is already async, so asking
    // costs the engine nothing.
    let associated = true;
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: async () => (associated ? "remote" : "local"),
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
      ids: makeIds(["t1"]),
    });

    await mdwal.createEntity("task", { title: "a" });
    expect(await mdwal.parseLog()).toHaveLength(1);

    associated = false;
    await mdwal.updateField("task", "t1", "title", "b");
    expect((await mdwal.read("task", "t1")).title).toBe("b");
    expect(await mdwal.parseLog()).toHaveLength(1);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("discard removes an entity's local file without recording anything", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
      ids: makeIds(["t1", "t2"]),
    });
    await mdwal.createEntity("task", { title: "a" });
    await mdwal.createEntity("task", { title: "b" });

    await mdwal.discard("task", "t1");

    // The file is gone from this machine...
    expect(await mdwal.readAll("task")).toHaveLength(1);
    expect(await Bun.file(join(root, "tasks", "t1.md")).exists()).toBe(false);

    // ...but nothing was authored: this is local tidying, not a change other
    // peers should learn about. The entity's own history is untouched, so a
    // later replay can rebuild the file.
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "CREATE"]);
    expect(mdwal.replay({}, events)["task/t1"]!.title).toBe("a");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("an entity's frontmatter round-trips through parse and serialize", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "local",
      clock: makeClock([]),
      author: () => "alice",
    });

    // The same markdown+frontmatter path serves files that live outside any
    // workspace and outside LWW — config.md above all — so it is usable without
    // going through an entity at all.
    const fields = { currentWorkspace: "main", nerdfont: true, columns: 3 };
    const text = mdwal.serializeEntity(fields);

    expect(mdwal.parseEntity(text)).toEqual(fields);

    // Values keep their types: the frontmatter carries no type information, so
    // round-tripping is what proves the encoding is honest.
    expect(mdwal.parseEntity(mdwal.serializeEntity(mdwal.parseEntity(text)))).toEqual(fields);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("updateField appends one UPDATE_FIELD event and atomically writes value + LWW companions", async () => {
  const root = await tempWorkspace();
  try {
    const mdwal = createMdwal({
      root,
      schemas: { task: taskSchema },
      mode: "remote",
      clock: makeClock([1000n, 2000n]),
      author: () => "alice",
    });

    const created = await mdwal.createEntity("task", { title: "buy milk" });
    await mdwal.updateField("task", created.id, "title", "milk 2%");

    // The derived file reflects the new value plus the per-field LWW metadata,
    // written together as one atomic update (never observable half-applied).
    const readBack = await mdwal.read("task", created.id);
    expect(readBack.title).toBe("milk 2%");
    expect(readBack.title_lastModified).toBe(2000n);
    expect(readBack.title_lastModifiedBy).toBe("alice");

    // The log now carries CREATE then one UPDATE_FIELD for the field.
    const events = await mdwal.parseLog();
    expect(events.map((e) => e.op)).toEqual(["CREATE", "UPDATE_FIELD"]);
    const update = events[1]!;
    expect(update.entityId).toBe(created.id);
    expect(update.ts).toBe(2000n);
    expect(update.payload.field).toBe("title");
    expect(update.payload.value).toBe("milk 2%");
    expect(update.payload.author).toBe("alice");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
