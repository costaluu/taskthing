import { test, expect } from "bun:test";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import { createMigrationRunner, type Migration } from "./migration-runner";
import { createMdwal, type Mdwal } from "./mdwal";
import { boardSchema, configSchema, taskSchema, type Config } from "./schema";

// The migration runner applies the binary's embedded structural migrations to
// every workspace on the machine (Spec 0005 Seam 2). It is tested against real
// temp workspaces with the embedded set injected. A structural migration is a
// static mdwal log (folder ops); the runner replays it with logging OFF and
// records what it applied — never touching events.log.

// The new binary knows a `list` entity, so `lists/` is a fixed folder.
const schemas = {
  task: taskSchema.omit({ id: true }),
  board: boardSchema.omit({ id: true }),
  list: z.object({ name: z.string() }),
};

function engine(root: string): Mdwal {
  return createMdwal({ root, schemas, mode: "remote", clock: () => 1n, author: () => "migrator" });
}

/** Author a migration log the way a plumber command would: real ops, logged. */
async function authorLog(build: (m: Mdwal) => Promise<void>): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "tt-mig-author-"));
  await build(engine(dir));
  const content = await Bun.file(join(dir, "events.log")).text();
  await rm(dir, { recursive: true, force: true });
  return content;
}

async function migrationAddingLists(version: string): Promise<Migration> {
  // A remote-mode engine logs the folder op, so the log is the migration content.
  return { version, content: await authorLog((m) => m.createFolder("list")) };
}

test("it applies a folder-op migration with logging off", async () => {
  const root = await mkdtemp(join(tmpdir(), "tt-mig-ws-"));
  try {
    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      workspaces: [root],
      engine,
    });

    await runner.run();

    // The folder the migration creates is there...
    expect(await readdir(root)).toContain("lists");
    // ...but nothing was written to events.log — a structural change every peer
    // applies identically is not a user-authored event.
    expect(await Bun.file(join(root, "events.log")).exists()).toBe(false);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("it records each applied migration and never re-applies it", async () => {
  const root = await mkdtemp(join(tmpdir(), "tt-mig-ws-"));
  try {
    const migration = await migrationAddingLists("0.2.0");
    const runner = createMigrationRunner({ migrations: [migration], workspaces: [root], engine });

    await runner.run();

    // A frozen record of what ran lands in the gitignored migrations folder.
    const recordDir = join(root, ".taskthing", "migrations");
    expect(await readdir(recordDir)).toContain("0.2.0.md");
    expect(await Bun.file(join(recordDir, "0.2.0.md")).text()).toBe(migration.content);

    // Idempotent: remove the folder it created, run again — it is recorded as
    // applied, so it does not run a second time.
    await rm(join(root, "lists"), { recursive: true, force: true });
    await runner.run();
    expect(await readdir(root)).not.toContain("lists");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a fixed-folder migration regenerates a remote workspace's .gitignore", async () => {
  const root = await mkdtemp(join(tmpdir(), "tt-mig-ws-"));
  try {
    // A remote workspace already has a .gitignore for the old fixed folders.
    await Bun.write(join(root, ".gitignore"), "tasks/\nboards/\n.taskthing/\n");

    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      workspaces: [root],
      engine,
    });
    await runner.run();

    const gitignore = await Bun.file(join(root, ".gitignore")).text();
    // The new fixed folder is now ignored, derived from the schema...
    expect(gitignore).toContain("lists/");
    expect(gitignore).toContain("tasks/");
    // ...and the migrations records stay ignored under .taskthing/ (ADR-0003).
    expect(gitignore).toContain(".taskthing/");
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("a local workspace (no .gitignore) does not gain one", async () => {
  const root = await mkdtemp(join(tmpdir(), "tt-mig-ws-"));
  try {
    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      workspaces: [root],
      engine,
    });
    await runner.run();

    // Local workspaces never had a .gitignore; the runner must not invent one.
    expect(await Bun.file(join(root, ".gitignore")).exists()).toBe(false);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});

test("it applies a pending migration to every workspace", async () => {
  const a = await mkdtemp(join(tmpdir(), "tt-mig-a-"));
  const b = await mkdtemp(join(tmpdir(), "tt-mig-b-"));
  try {
    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      workspaces: [a, b],
      engine,
    });
    await runner.run();

    expect(await readdir(a)).toContain("lists");
    expect(await readdir(b)).toContain("lists");
  } finally {
    await rm(a, { recursive: true, force: true });
    await rm(b, { recursive: true, force: true });
  }
});

test("a failure on one workspace is surfaced, unrecorded, and does not block others", async () => {
  const bad = await mkdtemp(join(tmpdir(), "tt-mig-bad-"));
  const good = await mkdtemp(join(tmpdir(), "tt-mig-good-"));
  try {
    // An engine that fails to apply on the `bad` workspace only.
    const failing = (root: string) => {
      const real = engine(root);
      if (root !== bad) return real;
      return { ...real, createFolder: async () => {
        throw new Error("disk full");
      } };
    };

    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      // The failing workspace comes first, so a naive runner would stop here.
      workspaces: [bad, good],
      engine: failing,
    });

    // The failure is surfaced, not swallowed.
    expect(runner.run()).rejects.toThrow(/disk full/);
    await new Promise((r) => setTimeout(r, 20));

    // `bad` is not recorded as migrated, so it retries next run...
    expect(await Bun.file(join(bad, ".taskthing", "migrations", "0.2.0.md")).exists()).toBe(false);
    // ...and `good` still got migrated despite `bad` failing first.
    expect(await readdir(good)).toContain("lists");
    expect(await Bun.file(join(good, ".taskthing", "migrations", "0.2.0.md")).exists()).toBe(true);
  } finally {
    await rm(bad, { recursive: true, force: true });
    await rm(good, { recursive: true, force: true });
  }
});

// ── config migrations (Spec 0005 §"config migration") ────────────────────────
//
// A migration may also rewrite config.md. Config is global (one per machine),
// lives outside the event/LWW domain, and is NOT replayed from a log — so the
// rewrite is an explicit version-keyed code transform applied directly, once,
// through an injected config boundary. mdwal never coerces a value on its own.

/** An in-memory config boundary, standing in for the real config.md path. */
function configBoundary(initial: Config) {
  let stored = initial;
  return {
    boundary: {
      read: async () => stored,
      write: async (c: Config) => {
        stored = c;
      },
    },
    get: () => stored,
  };
}

test("it applies a pending config migration, rewriting config.md under the new schema", async () => {
  const home = await mkdtemp(join(tmpdir(), "tt-mig-home-"));
  try {
    // The old config uses the american date format the new schema is dropping.
    const config = configBoundary(configSchema.parse({ currentWorkspace: "local", dateFormat: "america" }));

    const runner = createMigrationRunner({
      migrations: [],
      workspaces: [],
      engine,
      config: config.boundary,
      configHome: home,
      // The migration author states, explicitly, how to bring the old value forward.
      configMigrations: [{ version: "0.3.0", apply: (c) => ({ ...c, dateFormat: "europe" }) }],
    });

    await runner.run();

    // The value was rewritten to one valid under the new schema.
    expect(config.get().dateFormat).toBe("europe");
    expect(() => configSchema.parse(config.get())).not.toThrow();
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("a config migration is recorded once per machine and never re-applied", async () => {
  const home = await mkdtemp(join(tmpdir(), "tt-mig-home-"));
  try {
    const config = configBoundary(configSchema.parse({ currentWorkspace: "local", dateFormat: "america" }));
    let applied = 0;
    const configMigrations = [
      {
        version: "0.3.0",
        apply: (c: Config): Config => {
          applied++;
          return { ...c, dateFormat: "europe" };
        },
      },
    ];
    const runner = createMigrationRunner({
      migrations: [],
      workspaces: [],
      engine,
      config: config.boundary,
      configHome: home,
      configMigrations,
    });

    await runner.run();
    expect(applied).toBe(1);
    // Recorded in the global migrations/ beside config.md, not per workspace.
    expect(await readdir(join(home, "migrations"))).toContain("0.3.0.md");

    // A second run finds it recorded, so the transform never runs again — a
    // rewrite applied twice (e.g. a field rename) would otherwise corrupt config.
    await runner.run();
    expect(applied).toBe(1);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("config migration is attempted even when a workspace fails, both surfacing", async () => {
  const bad = await mkdtemp(join(tmpdir(), "tt-mig-bad-"));
  const home = await mkdtemp(join(tmpdir(), "tt-mig-home-"));
  try {
    const failing = (root: string) => {
      const real = engine(root);
      return {
        ...real,
        createFolder: async () => {
          throw new Error("disk full");
        },
      };
    };
    const config = configBoundary(configSchema.parse({ currentWorkspace: "local", dateFormat: "america" }));

    const runner = createMigrationRunner({
      migrations: [await migrationAddingLists("0.2.0")],
      workspaces: [bad],
      engine: failing,
      config: config.boundary,
      configHome: home,
      configMigrations: [{ version: "0.3.0", apply: (c) => ({ ...c, dateFormat: "europe" }) }],
    });

    // The workspace failure is still surfaced...
    expect(runner.run()).rejects.toThrow(/disk full/);
    await new Promise((r) => setTimeout(r, 20));

    // ...but config migration is a separate dimension: a workspace failure must
    // not skip it (story 34 — a failure in one place cannot block the others).
    expect(config.get().dateFormat).toBe("europe");
    expect(await readdir(join(home, "migrations"))).toContain("0.3.0.md");
  } finally {
    await rm(bad, { recursive: true, force: true });
    await rm(home, { recursive: true, force: true });
  }
});

test("a failing config migration is surfaced, leaves config.md untouched, and is not recorded", async () => {
  const home = await mkdtemp(join(tmpdir(), "tt-mig-home-"));
  try {
    const config = configBoundary(configSchema.parse({ currentWorkspace: "local", dateFormat: "america" }));

    const runner = createMigrationRunner({
      migrations: [],
      workspaces: [],
      engine,
      config: config.boundary,
      configHome: home,
      configMigrations: [
        {
          version: "0.3.0",
          apply: (): Config => {
            throw new Error("bad config transform");
          },
        },
      ],
    });

    // The failure surfaces...
    expect(runner.run()).rejects.toThrow(/bad config transform/);
    await new Promise((r) => setTimeout(r, 20));

    // ...config.md is left exactly as it was (transforms are all-or-nothing:
    // nothing is written until every pending one applied)...
    expect(config.get().dateFormat).toBe("america");
    // ...and it is not recorded as applied, so it retries next run (story 34).
    expect(await Bun.file(join(home, "migrations", "0.3.0.md")).exists()).toBe(false);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});
