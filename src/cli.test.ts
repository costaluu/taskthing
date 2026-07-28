import { test, expect } from "bun:test";
import { mkdir, mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { buildCli } from "./app";
import { VERSION } from "./build";
import { createManager } from "./manager";
import { createMdwal } from "./mdwal";
import { CommandFailure, setPresenter, type PresenterCall } from "./presenter";
import { recordingPresenter } from "./presenter-record";
import { boardSchema, taskSchema } from "./schema";

// The CLI seam is the command tree: tests drive `cli.execute` in process against
// a temporary data home, then assert on the workspace files it produced, on the
// kv_store, and on the command's plain output and exit code. Output is read back
// through the recording presenter, which renders exactly what a pipe would see.
//
// A few tests still spawn the real entrypoint, because argv routing, the help
// verb and the process's own exit code live above this seam — see `spawn` below.
const ENTRYPOINT = join(import.meta.dir, "index.ts");

interface Run {
  exitCode: number;
  stdout: string;
  stderr: string;
  /** What was drawn, including the data the styled adapter would have been handed. */
  calls: PresenterCall[];
}

// Natural-language dates are relative to now, so tests pin now rather than
// asserting against a moving target.
const NOW = "2026-07-22T09:00:00.000Z";

const cli = await buildCli();

/**
 * Run one command the way a piped invocation would, in this process.
 *
 * `path` is the command's full path — "board set name", not "board" — because
 * `cli.execute` resolves the name it is handed and leaves the rest as arguments.
 * Anything after it is argv: positionals and flags alike.
 */
async function taskthing(home: string, path: string, ...args: string[]): Promise<Run> {
  process.env.TASKTHING_HOME = home;
  process.env.TASKTHING_NOW = NOW;

  const recorder = recordingPresenter();
  setPresenter(recorder);

  // A spawned run always had a pipe for stdout; an in-process one inherits the
  // test runner's, which is a terminal when the suite is run by hand. Four
  // commands still ask `isTTY` for themselves (install, config, theme, update
  // apply) and would block on an interactive form, and ink would write escape
  // sequences into the output under assertion. So the run is made to look piped,
  // which is what it is meant to be.
  const tty = process.stdout.isTTY;
  process.stdout.isTTY = false;

  // A few lines are deliberately plain in both modes (install's closing line,
  // the config listing, the entity plumber's log) and so never reach the
  // presenter. They are spliced into the same sequence so a test reads one
  // stdout regardless of which route a line took.
  const { log, error } = console;
  const write = process.stdout.write.bind(process.stdout);
  console.log = (...parts: unknown[]) =>
    void recorder.lines.push({ stream: "stdout", text: parts.join(" ") });
  console.error = (...parts: unknown[]) =>
    void recorder.lines.push({ stream: "stderr", text: parts.join(" ") });
  process.stdout.write = ((chunk: string) => {
    recorder.lines.push({ stream: "stdout", text: String(chunk) });
    return true;
  }) as typeof process.stdout.write;

  try {
    await cli.execute(path, args);
    return { exitCode: 0, stdout: recorder.stdout, stderr: recorder.stderr, calls: recorder.calls };
  } catch (error_) {
    // Where the shipped adapters exit, the recording one throws; anything else
    // reaching here is a handler that threw without reporting, which Bunli would
    // have turned into a non-zero exit too.
    const stderr =
      error_ instanceof CommandFailure
        ? recorder.stderr
        : [recorder.stderr, error_ instanceof Error ? error_.message : String(error_)]
            .filter((part) => part.length > 0)
            .join("\n");
    return { exitCode: 1, stdout: recorder.stdout, stderr, calls: recorder.calls };
  } finally {
    console.log = log;
    console.error = error;
    process.stdout.write = write;
    process.stdout.isTTY = tty;
  }
}

/** Run the real binary as its own process, for what sits above the command tree. */
async function spawn(home: string, ...args: string[]): Promise<Run> {
  const proc = Bun.spawn(["bun", ENTRYPOINT, ...args], {
    env: { ...process.env, TASKTHING_HOME: home, TASKTHING_NOW: NOW },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  return { exitCode: await proc.exited, stdout, stderr, calls: [] };
}

/** A data home with one workspace, which config.md names as the current one. */
async function dataHome(workspace = "main"): Promise<string> {
  const home = await mkdtemp(join(tmpdir(), "tt-home-"));
  await mkdir(join(home, "workspaces", workspace), { recursive: true });
  await Bun.write(
    join(home, "config.md"),
    `---\ncurrentWorkspace: ${JSON.stringify(workspace)}\n---\n`,
  );
  return home;
}

/** Read the workspace back through the engine, as any other consumer would. */
function workspaceMdwal(home: string, workspace = "main") {
  return createMdwal({
    root: join(home, "workspaces", workspace),
    schemas: {
      task: taskSchema.omit({ id: true }),
      board: boardSchema.omit({ id: true }),
    },
    mode: "local",
    clock: () => 0n,
    author: () => "test",
  });
}

test("add reads a d:[…] tag as the task's date and keeps it out of the title", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "add", "walk the dog d:[tomorrow]");
    expect(run.exitCode).toBe(0);

    const task = (await workspaceMdwal(home).readAll("task"))[0]!;

    // The tag and its brackets are removed wholesale, so the stored title is
    // what the user meant to write.
    expect(task.title).toBe("walk the dog");

    // A date with no recurrence: a DTSTART on its own, resolved against now and
    // stored as the RFC 5545 string.
    expect(task.rrule).toBe("DTSTART:20260723T090000Z");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("add reads an r:[…] tag as recurrence, with d:[…] fixing where it starts", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog r:[every monday]");
    await taskthing(home, "add", "water plants d:[in 3 days] recurrence:[every day]");

    const tasks = await workspaceMdwal(home).readAll("task");
    const byTitle = new Map(tasks.map((t) => [t.title, t.rrule as string]));

    // Recurrence alone: the rule starts from now.
    expect(byTitle.get("walk the dog")).toContain("RRULE:FREQ=WEEKLY;BYDAY=MO");
    expect(byTitle.get("walk the dog")).toContain("DTSTART:20260722T090000Z");

    // Both tags: the given date is the DTSTART the recurrence runs from, and
    // the long spellings work the same as the short ones.
    expect(byTitle.get("water plants")).toBe(
      "DTSTART:20260725T090000Z\nRRULE:FREQ=DAILY",
    );
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("add only treats a tag as a tag when brackets close it", async () => {
  const home = await dataHome();
  try {
    // The brackets are the pattern. Free text that merely contains a colon —
    // or a tag word without brackets — is title, not syntax.
    await taskthing(home, "add", "remind Ed: buy milk");
    await taskthing(home, "add", "read about r: recurrence");

    const titles = (await workspaceMdwal(home).readAll("task")).map((t) => t.title).sort();
    expect(titles).toEqual(["read about r: recurrence", "remind Ed: buy milk"]);

    const undated = (await workspaceMdwal(home).readAll("task")).every((t) => t.rrule === null);
    expect(undated).toBe(true);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("a command logs events exactly when the workspace has a remote", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    // A workspace with no remote is local: files change, nothing is logged,
    // because there is no peer to converge with.
    await taskthing(home, "add", "local only");
    expect(await workspaceMdwal(home).parseLog()).toEqual([]);

    // Associate it, exactly as `remote add` will.
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    const root = join(home, "workspaces", "main");
    const manager = createManager({
      root,
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    });
    await manager.init(remote);

    await taskthing(home, "add", "shared work");

    // Now the same command records its change as an event, so other peers can
    // replay it — the workspace's mode follows its association, not a flag
    // decided when the process started.
    const events = await workspaceMdwal(home).parseLog();
    expect(events).toHaveLength(1);
    expect(events[0]!.op).toBe("CREATE");
    expect(events[0]!.payload.snapshot.title).toBe("shared work");
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("a logged event carries the binary version, for migrate-on-read (ADR-0007)", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    const root = join(home, "workspaces", "main");
    const manager = createManager({
      root,
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    });
    await manager.init(remote);

    await taskthing(home, "add", "shared work");

    // Every event stamps the version of the binary that authored it, so a newer
    // binary can migrate-on-read and the version barrier can refuse an event
    // authored by a future schema (ADR-0007). Without this the barrier is dead.
    const events = await workspaceMdwal(home).parseLog();
    expect(events[0]!.payload.version).toBe(VERSION);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("the event clock is seeded from the log, staying monotonic across invocations (ADR-0006)", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    const root = join(home, "workspaces", "main");
    const manager = createManager({
      root,
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    });
    await manager.init(remote);

    // A prior event dated far in the future — as if this peer's wall clock had
    // run ahead and then stepped back (NTP, VM restore, manual set). The log is
    // this peer's own strictly-increasing record, so the next event it writes
    // must still land above the last line, not at the smaller wall-clock now.
    const future = 9_000_000_000_000_000_000n;
    const seeded = `${future}::CREATE::task::seeded::{"author":"alice","snapshot":{"title":"seed"},"version":"${VERSION}"}\n`;
    const logPath = join(root, "events.log");
    await Bun.write(logPath, (await Bun.file(logPath).text().catch(() => "")) + seeded);

    await taskthing(home, "add", "after the step-back");

    const events = await workspaceMdwal(home).parseLog();
    const mine = events.filter((e) => e.payload.snapshot?.title === "after the step-back");
    expect(mine).toHaveLength(1);
    // Seeded from the log's last ts, the new event is future+1 — not the smaller
    // wall-clock now (which seed:0n would produce, reopening the purge hole).
    expect(mine[0]!.ts).toBeGreaterThan(future);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("pull refuses a log holding an event authored by a newer binary (version barrier, ADR-0007)", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    const root = join(home, "workspaces", "main");
    const manager = createManager({
      root,
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    });
    await manager.init(remote);

    // An event stamped with a version this binary does not know — as if a peer
    // on a newer release authored it. Migrate-on-read has no transform to bring
    // a *future* schema down, so replay must refuse rather than misread it.
    const ahead = `1800000000000000000::CREATE::task::ahead::{"author":"alice","snapshot":{"title":"future"},"version":"9.9.9"}\n`;
    const logPath = join(root, "events.log");
    await Bun.write(logPath, (await Bun.file(logPath).text().catch(() => "")) + ahead);

    const run = await taskthing(home, "pull");
    // Replay refuses rather than misreading the future event: the command fails,
    // never reports a successful pull, and names the version to update to.
    expect(run.exitCode).toBe(1);
    expect(run.stdout).not.toMatch(/pulled/);
    expect(`${run.stdout}${run.stderr}`).toMatch(/9\.9\.9/);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("a piped git-facing command surfaces why it failed, not a stalled spinner (Spec 0004)", async () => {
  const home = await dataHome();
  try {
    // pull on a workspace with no remote fails in git. Piped (no TTY), the
    // reason must still reach the output rather than being swallowed by the
    // spinner — otherwise a script sees only exit 1 and a half-drawn frame.
    const run = await taskthing(home, "pull");
    expect(run.exitCode).toBe(1);
    expect(`${run.stdout}${run.stderr}`).toMatch(/something went wrong during pull/);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("entity create authors a CREATE_FOLDER migration line for the new folder (--log)", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "entity create", "list", "--log");
    expect(run.exitCode).toBe(0);

    // The output is a migration `content` line: a static mdwal log op the dev
    // embeds in build.ts. It names the op and the entity, and carries the
    // authoring binary's version like any event (ADR-0007). Nothing else runs.
    const events = workspaceMdwal(home).parseEvents(run.stdout);
    expect(events).toHaveLength(1);
    expect(events[0]!.op).toBe("CREATE_FOLDER");
    expect(events[0]!.entityType).toBe("list");
    expect(events[0]!.payload.version).toBe(VERSION);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("entity rename authors a RENAME_FOLDER line naming the new folder (--log)", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "entity rename", "list", "collection", "--log");
    expect(run.exitCode).toBe(0);

    const events = workspaceMdwal(home).parseEvents(run.stdout);
    expect(events).toHaveLength(1);
    expect(events[0]!.op).toBe("RENAME_FOLDER");
    expect(events[0]!.entityType).toBe("list");
    expect(events[0]!.payload.to).toBe("collection");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("entity delete authors a DELETE_FOLDER line (--log)", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "entity delete", "list", "--log");
    expect(run.exitCode).toBe(0);

    const events = workspaceMdwal(home).parseEvents(run.stdout);
    expect(events).toHaveLength(1);
    expect(events[0]!.op).toBe("DELETE_FOLDER");
    expect(events[0]!.entityType).toBe("list");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("entity without --log refuses, since it only exists to author a migration line", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "entity create", "list");
    expect(run.exitCode).toBe(1);
    expect(run.stderr).toMatch(/--log/);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

// Help and routing errors are the framework's to render, not this CLI's. Piped —
// which is how these tests run, and how an agent or a script reads the CLI — that
// rendering is a structured envelope rather than prose; on a terminal it is the
// familiar text. The assertions below are on the envelope's shape and payload,
// the part a consumer actually depends on.

/** The `{ ok, data }` / `{ ok, error }` envelope a piped run prints. */
function envelope(text: string): { ok: boolean; data?: any; error?: any } {
  return JSON.parse(text);
}

test("`help`, no command, and --help/-h all show the command surface and exit cleanly", async () => {
  const home = await dataHome();
  try {
    for (const args of [["help"], [] as string[], ["--help"], ["-h"]]) {
      const run = await spawn(home, ...args);
      expect(run.exitCode).toBe(0);

      const { ok, data } = envelope(run.stdout);
      expect(ok).toBe(true);
      expect(data.type).toBe("help");
      expect(data.cliName).toBe("taskthing");

      // A sample from each layer of the command surface.
      for (const cmd of ["add", "list", "board", "sync", "config", "update", "workspace"]) {
        expect(data.text).toContain(cmd);
      }
    }
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("an unknown command names itself and lists the real ones, but still fails", async () => {
  const home = await dataHome();
  try {
    const run = await spawn(home, "frobnicate");
    // Still a non-zero exit so scripts detect the mistake...
    expect(run.exitCode).toBe(1);

    const { ok, error } = envelope(run.stderr);
    expect(ok).toBe(false);
    expect(error.kind).toBe("command-not-found");
    expect(error.command).toBe("frobnicate");
    // ...and the real commands come back with it, to guide the user.
    expect(error.available).toContain("add");
    expect(error.available).toContain("board");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("the entrypoint boots as its own process and runs a command end to end", async () => {
  const home = await dataHome();
  try {
    // Everything else drives the command tree directly, which cannot catch a
    // command that exists but was never registered in the entry, nor a failure
    // to start at all. One real invocation covers both.
    expect((await spawn(home, "add", "walk the dog")).exitCode).toBe(0);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(1);

    const listed = await spawn(home, "list");
    expect(listed.exitCode).toBe(0);
    expect(listed.stdout).toContain("walk the dog");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

/** The ephemeral number↔nanoid map a listing leaves behind. */
async function kvStore(home: string, kind: string, workspace = "main") {
  const path = join(home, "workspaces", workspace, ".taskthing", "kv", `${kind}.json`);
  return JSON.parse(await Bun.file(path).text()) as Record<string, string>;
}

test("list numbers the tasks it shows and records the mapping for later commands", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");
    await taskthing(home, "add", "buy milk");

    const run = await taskthing(home, "list");
    expect(run.exitCode).toBe(0);

    // A listing is what gives a user something typeable: numbers, not nanoids.
    expect(run.stdout).toContain("1");
    expect(run.stdout).toContain("walk the dog");
    expect(run.stdout).toContain("2");
    expect(run.stdout).toContain("buy milk");
    expect(run.stdout).not.toContain("inbox");

    // The mapping outlives the process, because the next command is a new one.
    const store = await kvStore(home, "tasks");
    const tasks = await workspaceMdwal(home).readAll("task");
    const ids = new Set(tasks.map((t) => t.id));
    expect(Object.keys(store).sort()).toEqual(["1", "2"]);
    expect(ids.has(store["1"]!)).toBe(true);
    expect(ids.has(store["2"]!)).toBe(true);
    expect(store["1"]).not.toBe(store["2"]);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("each listing resets the numbers rather than accumulating them", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "first");
    await taskthing(home, "add", "second");
    await taskthing(home, "list");

    const first = await kvStore(home, "tasks");
    expect(Object.keys(first)).toHaveLength(2);

    // The same entities listed again get numbers again — not new ones stacked
    // on top of the old, or the store would grow without bound and old numbers
    // would keep resolving to entities the user can no longer see.
    await taskthing(home, "list");
    const second = await kvStore(home, "tasks");
    expect(Object.keys(second).sort()).toEqual(["1", "2"]);

    // A number that no longer belongs to any listed entity stops resolving.
    await taskthing(home, "add", "third");
    await taskthing(home, "list");
    expect(Object.keys(await kvStore(home, "tasks")).sort()).toEqual(["1", "2", "3"]);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("a listed number resolves to its entity in a later command", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");
    await taskthing(home, "add", "buy milk");
    await taskthing(home, "list");

    const store = await kvStore(home, "tasks");
    const run = await taskthing(home, "star", "2");
    expect(run.exitCode).toBe(0);

    // The number reached the entity the listing said it would — and only that
    // one.
    const tasks = await workspaceMdwal(home).readAll("task");
    const starred = tasks.filter((t) => t.star === true);
    expect(starred).toHaveLength(1);
    expect(starred[0]!.id).toBe(store["2"]!);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("every task verb refuses an unknown reference the same way", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");

    // The reference rules live in one place now, so every verb that names a task
    // is held to them identically — no verb gets to be quietly more forgiving.
    const verbs: string[][] = [
      ["check", "9"],
      ["uncheck", "9"],
      ["star", "9"],
      ["unstar", "9"],
      ["delete", "9"],
      ["recover", "9"],
      ["clear", "9"],
      ["set title", "9", "new title"],
      ["set description", "9", "notes"],
      ["set date-time", "9", "tomorrow"],
      ["set board", "9", "work"],
    ];

    for (const [path, ...args] of verbs) {
      const run = await taskthing(home, path!, ...args);
      expect(run.exitCode).toBe(1);
      expect(run.stderr).toMatch(/list/i);
    }

    // ...and none of them touched the task that does exist.
    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks).toHaveLength(1);
    expect(tasks[0]!.title).toBe("walk the dog");
    expect(tasks[0]!.completed).toBe(false);
    expect(tasks[0]!.deleted).toBe(false);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("a number nobody listed is an error, not a guess", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");

    // Nothing has been listed, so no number means anything yet. Acting on a
    // guess would mutate an entity the user never pointed at.
    const run = await taskthing(home, "star", "1");
    expect(run.exitCode).toBe(1);
    expect(run.stderr).toMatch(/list/i);

    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks.every((t) => t.star === false)).toBe(true);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

/** Put a task straight into the workspace, to set up a state no verb reaches yet. */
async function seedTask(
  home: string,
  fields: Record<string, unknown>,
  workspace = "main",
): Promise<void> {
  await createMdwal({
    root: join(home, "workspaces", workspace),
    schemas: { task: taskSchema.omit({ id: true }) },
    mode: "local",
    clock: () => 1n,
    author: () => "test",
  }).createEntity("task", fields);
}

test("list hides completed tasks unless asked for them", async () => {
  const home = await dataHome();
  try {
    await seedTask(home, { title: "still open" });
    await seedTask(home, { title: "already done", completed: true });

    // A task manager's default question is "what's left", so a finished task
    // stops taking up room in the listing.
    const plain = await taskthing(home, "list");
    expect(plain.stdout).toContain("still open");
    expect(plain.stdout).not.toContain("already done");

    // ...but the history is one flag away.
    const checked = await taskthing(home, "list", "--checked");
    expect(checked.stdout).toContain("still open");
    expect(checked.stdout).toContain("already done");

    // Numbers describe what was shown: the narrower listing numbers only what
    // it displayed.
    await taskthing(home, "list");
    expect(Object.keys(await kvStore(home, "tasks"))).toHaveLength(1);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("list filters narrow what is shown, and compose", async () => {
  const home = await dataHome();
  try {
    await seedTask(home, { title: "plain" });
    await seedTask(home, { title: "favourite", star: true });
    await seedTask(home, { title: "documented", description: "with notes" });
    await seedTask(home, { title: "favourite and documented", star: true, description: "why" });
    await seedTask(home, { title: "in the bin", deleted: true });

    // Soft-deleted tasks are out of the way by default — deleting is meant to
    // clear the list — but remain retrievable.
    const plain = await taskthing(home, "list");
    expect(plain.stdout).not.toContain("in the bin");

    // ...and `--deleted` is the bin itself: only what was deleted, not the live
    // tasks as well, or a number read off it would point at a live task.
    const bin = await taskthing(home, "list", "--deleted");
    expect(bin.stdout).toContain("in the bin");
    expect(bin.stdout).not.toContain("plain");
    expect(bin.stdout.trim().split("\n")).toHaveLength(1);

    const starred = await taskthing(home, "list", "--starred");
    expect(starred.stdout).toContain("favourite");
    expect(starred.stdout).not.toContain("plain");

    const documented = await taskthing(home, "list", "--hasDescription");
    expect(documented.stdout).toContain("documented");
    expect(documented.stdout).not.toContain("plain");

    // Filters narrow together rather than widening: a task must satisfy all of
    // them to show.
    const both = await taskthing(home, "list", "--starred", "--hasDescription");
    expect(both.stdout).toContain("favourite and documented");
    expect(both.stdout).not.toContain("with notes");
    expect(both.stdout.trim().split("\n")).toHaveLength(1);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("--in-board scopes to a board by name, and to the virtual inbox without any lookup", async () => {
  const home = await dataHome();
  try {
    // Real boards, so `--in-board` can resolve the name the user types to the
    // board id the tasks actually carry.
    const mdwal = workspaceMdwal(home);
    const work = await mdwal.createEntity("board", { name: "work" });
    const house = await mdwal.createEntity("board", { name: "home" });

    await seedTask(home, { title: "unfiled" });
    await seedTask(home, { title: "at work", board: work.id });
    await seedTask(home, { title: "at home", board: house.id });

    const inWork = await taskthing(home, "list", "--in-board=work");
    expect(inWork.stdout).toContain("at work");
    expect(inWork.stdout).not.toContain("at home");
    expect(inWork.stdout).not.toContain("unfiled");

    // "inbox" is a sentinel, not an entity: it resolves to the inbox id with no
    // board to look up (ADR-0004).
    const inbox = await taskthing(home, "list", "--in-board=inbox");
    expect(inbox.stdout).toContain("unfiled");
    expect(inbox.stdout).not.toContain("at work");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("a listing hands the styled view the same rows the piped one prints", async () => {
  const home = await dataHome();
  try {
    const board = await workspaceMdwal(home).createEntity("board", { name: "work" });
    await seedTask(home, { title: "at work", board: board.id });
    await seedTask(home, { title: "unfiled" });

    const run = await taskthing(home, "list");
    const drawn = run.calls[0]!;
    if (drawn.method !== "taskList") throw new Error(`drew ${drawn.method}, not a task list`);

    // Both views come from these groups, so the numbers a user reads off the
    // styled list are the ones a piped run would have printed — the two cannot
    // drift apart the way separately-gathered rows could.
    const rows = drawn.groups.flatMap((group) => group.rows);
    expect(rows.map((row) => row.task.title).sort()).toEqual(["at work", "unfiled"]);
    expect(rows.map((row) => row.number).sort()).toEqual([1, 2]);

    for (const row of rows) {
      expect(run.stdout).toContain(`${row.number} ${row.task.title}`);
    }

    // Grouping is the styled view's alone: it is in the rows either way, but the
    // piped lines stay flat and in number order.
    expect(drawn.groups).toHaveLength(2);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("--global lists every workspace, and its numbers still act on the right one", async () => {
  const home = await dataHome();
  try {
    await mkdir(join(home, "workspaces", "home"), { recursive: true });
    await seedTask(home, { title: "ship the release" });
    await seedTask(home, { title: "fix the sink" }, "home");

    // Without the flag a listing is about the current workspace only.
    const local = await taskthing(home, "list");
    expect(local.stdout).toContain("ship the release");
    expect(local.stdout).not.toContain("fix the sink");

    const global = await taskthing(home, "list", "--global");
    expect(global.stdout).toContain("ship the release");
    expect(global.stdout).toContain("fix the sink");

    // Each listed task says where it lives, since the same number space now
    // spans workspaces.
    expect(global.stdout).toContain("main");
    expect(global.stdout).toContain("home");

    // A number handed out by a global listing acts on its own workspace, not on
    // whichever one happens to be current.
    const numbers = JSON.parse(
      await Bun.file(join(home, ".kv", "global.json")).text(),
    ) as Record<string, string>;
    const other = Object.entries(numbers).find(([, value]) => value.startsWith("home/"))!;

    const run = await taskthing(home, "star", other[0]!);
    expect(run.exitCode).toBe(0);

    const elsewhere = await workspaceMdwal(home, "home").readAll("task");
    expect(elsewhere[0]!.star).toBe(true);
    const here = await workspaceMdwal(home).readAll("task");
    expect(here[0]!.star).toBe(false);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("board add creates a board, the entity naming its own verb tree", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "board add", "work");
    expect(run.exitCode).toBe(0);

    const boards = await workspaceMdwal(home).readAll("board");
    expect(boards).toHaveLength(1);
    expect(boards[0]!.name).toBe("work");
    expect(boards[0]!.id).toHaveLength(21);

    // A board with no icon or colour yet is still a board; those are set later.
    expect(boards[0]!.icon).toBe("");
    expect(boards[0]!.color).toBe("");
    expect(boards[0]!.deleted).toBe(false);

    // The default entity is still the task: the flag is what changes it.
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(0);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("board list numbers boards in a store of their own", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "board add", "work");
    await taskthing(home, "board add", "home");
    await taskthing(home, "add", "a task");
    await taskthing(home, "list");

    const run = await taskthing(home, "board list");
    expect(run.exitCode).toBe(0);
    expect(run.stdout).toContain("work");
    expect(run.stdout).toContain("home");
    expect(run.stdout).not.toContain("a task");

    // Boards number independently of tasks: listing one must not disturb the
    // numbers the other handed out.
    const boards = await kvStore(home, "boards");
    const tasks = await kvStore(home, "tasks");
    expect(Object.keys(boards).sort()).toEqual(["1", "2"]);
    expect(Object.keys(tasks)).toHaveLength(1);

    const ids = new Set((await workspaceMdwal(home).readAll("board")).map((b) => b.id));
    expect(ids.has(boards["1"]!)).toBe(true);
    expect(ids.has(boards["2"]!)).toBe(true);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("board set renames a board and gives it an icon and a colour", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "board add", "wrok");
    await taskthing(home, "board list");

    expect((await taskthing(home, "board set name", "1", "work")).exitCode).toBe(0);
    expect((await taskthing(home, "board set icon", "1", "💼")).exitCode).toBe(0);
    expect((await taskthing(home, "board set color", "1", "blue")).exitCode).toBe(0);

    const board = (await workspaceMdwal(home).readAll("board"))[0]!;
    expect(board.name).toBe("work");
    expect(board.icon).toBe("💼");
    expect(board.color).toBe("blue");

    // A field the board doesn't have is a mistake worth reporting, not a new
    // field invented on the spot.
    const bad = await taskthing(home, "board set", "size", "1", "large");
    expect(bad.exitCode).toBe(1);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("deleting a board sends its tasks to the inbox, and recovering it does not take them back", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "board add", "work");
    await taskthing(home, "board list");
    const boardId = (await kvStore(home, "boards"))["1"]!;

    await seedTask(home, { title: "at work", board: boardId });
    await seedTask(home, { title: "at work too", board: boardId });
    await seedTask(home, { title: "unfiled" });

    expect((await taskthing(home, "board delete", "1")).exitCode).toBe(0);

    // A task can never be left pointing at a board that is gone, so every task
    // of that board falls back to the virtual one.
    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks.filter((t) => t.board === "inbox")).toHaveLength(3);
    expect(tasks.some((t) => t.board === boardId)).toBe(false);

    // The board itself is soft-deleted, like any other entity.
    const board = await workspaceMdwal(home).read("board", boardId);
    expect(board.deleted).toBe(true);

    // Recovering the board brings the board back, not its former tasks: each
    // task now genuinely belongs to the inbox, and only the user can move it.
    expect((await taskthing(home, "board recover", boardId)).exitCode).toBe(0);
    expect((await workspaceMdwal(home).read("board", boardId)).deleted).toBe(false);

    const after = await workspaceMdwal(home).readAll("task");
    expect(after.every((t) => t.board === "inbox")).toBe(true);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("check completes a task, and a one-off task mints nothing", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");
    await taskthing(home, "add", "pay the bill d:[tomorrow]");
    await taskthing(home, "list");

    expect((await taskthing(home, "check", "1")).exitCode).toBe(0);
    expect((await taskthing(home, "check", "2")).exitCode).toBe(0);

    // Completing is a field change like any other; nothing new appears, whether
    // the task was undated or merely dated.
    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks).toHaveLength(2);
    expect(tasks.every((t) => t.completed === true)).toBe(true);

    // ...and a completed task leaves the default listing.
    expect((await taskthing(home, "list")).stdout.trim()).toBe("");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("checking a recurring task mints its next occurrence as a new entity", async () => {
  const home = await dataHome();
  try {
    // A weekly task whose start is already in the past — two occurrences
    // overdue, now being 2026-07-22.
    await seedTask(home, {
      title: "water plants",
      rrule: "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO",
    });
    await taskthing(home, "list");
    const original = (await kvStore(home, "tasks"))["1"]!;

    expect((await taskthing(home, "check", "1")).exitCode).toBe(0);

    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks).toHaveLength(2);

    // The occurrence just completed keeps its own history: it is not recycled,
    // and its date stays where it was.
    const done = tasks.find((t) => t.id === original)!;
    expect(done.completed).toBe(true);
    expect(done.rrule).toBe("DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO");

    // The next occurrence is a brand-new entity inheriting the rule, advanced
    // by exactly one step — 2026-07-06, still in the past. Checking again is how
    // a user catches up on an overdue recurring task, one occurrence at a time,
    // instead of skipping straight to the future.
    const next = tasks.find((t) => t.id !== original)!;
    expect(next.title).toBe("water plants");
    expect(next.completed).toBe(false);
    expect(next.rrule).toBe("DTSTART:20260706T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("an exhausted recurrence mints nothing when checked", async () => {
  const home = await dataHome();
  try {
    // A rule with one occurrence left, and one that stops before today: both
    // are recurring tasks with nothing after the current occurrence.
    await seedTask(home, {
      title: "last one",
      rrule: "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;COUNT=1",
    });
    await seedTask(home, {
      title: "ran out",
      rrule: "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;UNTIL=20260705T000000Z",
    });
    await taskthing(home, "list");

    await taskthing(home, "check", "1");
    await taskthing(home, "check", "2");

    // Completing the final occurrence ends the series: there is no next date to
    // give a new entity, so none is invented.
    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks).toHaveLength(2);
    expect(tasks.every((t) => t.completed === true)).toBe(true);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("delete soft-deletes a task and recover brings it back, both as events", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    await taskthing(home, "add", "walk the dog");
    const root = join(home, "workspaces", "main");
    await createManager({
      root,
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    }).init(remote);

    await taskthing(home, "list");
    const id = (await kvStore(home, "tasks"))["1"]!;

    expect((await taskthing(home, "delete", "1")).exitCode).toBe(0);

    // The file is never removed: deleting is a field change, so it converges
    // between peers like any other edit and can be undone.
    const deleted = await workspaceMdwal(home).read("task", id);
    expect(deleted.deleted).toBe(true);
    expect((await taskthing(home, "list")).stdout.trim()).toBe("");

    // The number to recover by comes from the listing that shows the bin.
    await taskthing(home, "list", "--deleted");
    expect((await taskthing(home, "recover", "1")).exitCode).toBe(0);
    expect((await workspaceMdwal(home).read("task", id)).deleted).toBe(false);

    // Both are ordinary logged field events, so other peers see them too.
    const fields = (await workspaceMdwal(home).parseLog())
      .filter((e) => e.op === "UPDATE_FIELD")
      .map((e) => e.payload.field);
    expect(fields).toEqual(["deleted", "deleted"]);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("clear removes an already-deleted task locally, and refuses a live one", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    await taskthing(home, "add", "keep me");
    await taskthing(home, "add", "bin me");
    await createManager({
      root: join(home, "workspaces", "main"),
      mdwal: workspaceMdwal(home),
      author: () => "alice",
      clock: () => 1n,
    }).init(remote);

    await taskthing(home, "list");
    const live = (await kvStore(home, "tasks"))["1"]!;

    // Clearing is only for tidying away what is already in the bin: on a live
    // task it would look like deletion, but silently and without a way back.
    const refused = await taskthing(home, "clear", "1");
    expect(refused.exitCode).toBe(1);
    expect(refused.stderr).toMatch(/delete/i);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(2);

    await taskthing(home, "delete", "2");
    await taskthing(home, "list", "--deleted");
    const eventsBefore = await workspaceMdwal(home).parseLog();

    expect((await taskthing(home, "clear", "1")).exitCode).toBe(0);

    // The file is gone from this machine, and the live task is untouched.
    const remaining = await workspaceMdwal(home).readAll("task");
    expect(remaining).toHaveLength(1);
    expect(remaining[0]!.id).toBe(live);

    // Nothing was authored: other peers never hear about it, and a later
    // pull/rebuild may well bring the file back — this is short-term tidying,
    // not removal from history.
    expect(await workspaceMdwal(home).parseLog()).toEqual(eventsBefore);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("unstar undoes star, and set writes a task's description and date", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dog");
    await taskthing(home, "list");

    await taskthing(home, "star", "1");
    expect((await workspaceMdwal(home).readAll("task"))[0]!.star).toBe(true);
    expect((await taskthing(home, "unstar", "1")).exitCode).toBe(0);
    expect((await workspaceMdwal(home).readAll("task"))[0]!.star).toBe(false);

    expect(
      (await taskthing(home, "set description", "1", "walk him twice")).exitCode,
    ).toBe(0);
    expect((await workspaceMdwal(home).readAll("task"))[0]!.description).toBe("walk him twice");

    // A task's date is its DTSTART: setting it rewrites where the task sits in
    // time, without inventing a parallel date field.
    expect((await taskthing(home, "set date-time", "1", "tomorrow")).exitCode).toBe(0);
    expect((await workspaceMdwal(home).readAll("task"))[0]!.rrule).toBe(
      "DTSTART:20260723T090000Z",
    );

    // Setting the date of a recurring task moves where the series starts and
    // leaves the rule alone.
    await seedTask(home, {
      title: "water plants",
      rrule: "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO",
    });
    await taskthing(home, "list");
    const recurring = (await taskthing(home, "list")).stdout
      .split("\n")
      .find((line) => line.includes("water plants"))!
      .split(" ")[0]!;

    await taskthing(home, "set date-time", recurring, "2026-08-10 09:00 UTC");
    const moved = (await workspaceMdwal(home).readAll("task")).find(
      (t) => t.title === "water plants",
    )!;
    expect(moved.rrule).toBe("DTSTART:20260810T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("--workspace points a command at another workspace, leaving the current one alone", async () => {
  const home = await dataHome();
  try {
    await mkdir(join(home, "workspaces", "home"), { recursive: true });

    // Every workspace-scoped command takes the flag; without it they act on the
    // current workspace, which is what makes the flag rarely needed.
    await taskthing(home, "add", "fix the sink", "--workspace=home");
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(0);
    expect(await workspaceMdwal(home, "home").readAll("task")).toHaveLength(1);

    const listed = await taskthing(home, "list", "--workspace=home");
    expect(listed.stdout).toContain("fix the sink");

    // The numbers a scoped listing hands out belong to that workspace, and the
    // commands that consume them must follow the flag too.
    await taskthing(home, "star", "1", "--workspace=home");
    await taskthing(home, "set description", "1", "--workspace=home", "under the sink");

    const there = (await workspaceMdwal(home, "home").readAll("task"))[0]!;
    expect(there.star).toBe(true);
    expect(there.description).toBe("under the sink");

    // Boards are workspace-scoped in the same way.
    await taskthing(home, "board add", "plumbing", "--workspace=home");
    expect(await workspaceMdwal(home, "home").readAll("board")).toHaveLength(1);
    expect(await workspaceMdwal(home).readAll("board")).toHaveLength(0);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("--period keeps only the tasks dated inside a window from now", async () => {
  const home = await dataHome();
  try {
    // Now is 2026-07-22.
    await seedTask(home, { title: "tomorrow", rrule: "DTSTART:20260723T090000Z" });
    await seedTask(home, { title: "in a week", rrule: "DTSTART:20260729T090000Z" });
    await seedTask(home, { title: "in two months", rrule: "DTSTART:20260922T090000Z" });
    await seedTask(home, { title: "undated" });

    const twoDays = await taskthing(home, "list", "--period=2d");
    expect(twoDays.stdout).toContain("tomorrow");
    expect(twoDays.stdout).not.toContain("in a week");

    const oneMonth = await taskthing(home, "list", "--period=1m");
    expect(oneMonth.stdout).toContain("tomorrow");
    expect(oneMonth.stdout).toContain("in a week");
    expect(oneMonth.stdout).not.toContain("in two months");

    const oneYear = await taskthing(home, "list", "--period=1y");
    expect(oneYear.stdout).toContain("in two months");

    // A task with no date is outside every window — the filter asks "when", and
    // an undated task has no answer.
    expect(twoDays.stdout).not.toContain("undated");
    expect(oneYear.stdout).not.toContain("undated");

    // A window nobody can read is refused rather than quietly ignored.
    const bad = await taskthing(home, "list", "--period=soon");
    expect(bad.exitCode).toBe(1);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("workspace create, list, use and rename move the current workspace around", async () => {
  const home = await dataHome();
  try {
    expect((await taskthing(home, "workspace create", "work")).exitCode).toBe(0);

    const listed = await taskthing(home, "workspace list");
    expect(listed.stdout).toContain("main");
    expect(listed.stdout).toContain("work");

    // The listing says which one is current, since every unscoped command acts
    // on it.
    expect(listed.stdout).toMatch(/\*\s*main|main\s*\*/);

    // Switching is what `use` is for — and it is the only way, so that the
    // current workspace is never changed as a side effect of something else.
    expect((await taskthing(home, "use", "work")).exitCode).toBe(0);
    await taskthing(home, "add", "in the new one");
    expect(await workspaceMdwal(home, "work").readAll("task")).toHaveLength(1);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(0);

    // Renaming carries the workspace's contents, and follows the current one.
    expect((await taskthing(home, "workspace rename", "work", "office")).exitCode).toBe(0);
    expect((await taskthing(home, "workspace list")).stdout).toContain("office");
    expect(await workspaceMdwal(home, "office").readAll("task")).toHaveLength(1);

    await taskthing(home, "add", "still following");
    expect(await workspaceMdwal(home, "office").readAll("task")).toHaveLength(2);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("workspace delete refuses the active one, and needs confirming", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "workspace create", "scratch");

    // Deleting the active workspace would leave the user with no current one.
    const active = await taskthing(home, "workspace delete", "main");
    expect(active.exitCode).toBe(1);
    expect(active.stderr).toMatch(/current|active/i);

    // Deleting any other is destructive — it removes the local folder — so it
    // takes an explicit confirmation.
    const unconfirmed = await taskthing(home, "workspace delete", "scratch");
    expect(unconfirmed.exitCode).toBe(1);
    expect(unconfirmed.stderr).toMatch(/confirm/i);
    expect((await taskthing(home, "workspace list")).stdout).toContain("scratch");

    const confirmed = await taskthing(home, "workspace delete", "scratch", "--confirm");
    expect(confirmed.exitCode).toBe(0);
    expect((await taskthing(home, "workspace list")).stdout).not.toContain("scratch");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("workspace remote add associates the workspace, and remove detaches it", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    await taskthing(home, "add", "written before sharing");

    expect((await taskthing(home, "workspace remote add", remote)).exitCode).toBe(0);

    // The branch model exists on the remote. This remote was empty, so this
    // workspace bootstraps it: the work that predates the association travels
    // in master's consolidated snapshot rather than as events.
    const format = "--format=%(refname:short)";
    const branches = (
      await Bun.$`git --git-dir=${remote} for-each-ref ${format} refs/heads`.text()
    ).trim().split("\n");
    expect(branches).toContain("master");
    expect(branches.some((b) => b.startsWith("users/"))).toBe(true);

    const onMaster = await Bun.$`git --git-dir=${remote} ls-tree -r --name-only master`.text();
    expect(onMaster).toContain("tasks/");

    // From here the workspace is remote: new commands record events for peers.
    await taskthing(home, "add", "written after");
    const events = await workspaceMdwal(home).parseLog();
    expect(events.filter((e) => e.op === "CREATE")).toHaveLength(1);
    expect(events[0]!.payload.snapshot.title).toBe("written after");

    expect((await taskthing(home, "workspace remote remove")).exitCode).toBe(0);

    // Detaching leaves the remote alone — other peers are unaffected — and the
    // local tasks survive; the workspace simply stops logging.
    const after = (
      await Bun.$`git --git-dir=${remote} for-each-ref ${format} refs/heads`.text()
    ).trim().split("\n");
    expect(after).toEqual(branches);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(2);

    await taskthing(home, "add", "written alone again");
    expect(await workspaceMdwal(home).parseLog()).toEqual([]);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("sync publishes this peer's events, and pull re-derives from master", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    await taskthing(home, "workspace remote add", remote);
    await taskthing(home, "add", "walk the dog");

    expect((await taskthing(home, "sync")).exitCode).toBe(0);

    // The peer's branch now carries the event, so other peers can replay it.
    const format = "--format=%(refname:short)";
    const branch = (
      await Bun.$`git --git-dir=${remote} for-each-ref ${format} refs/heads/users`.text()
    ).trim();
    const published = await Bun.$`git --git-dir=${remote} show ${`${branch}:events.log`}`.text();
    expect(published).toContain("CREATE");
    expect(published).toContain("walk the dog");

    // Another peer consolidates a task of their own into master.
    const clone = await mkdtemp(join(tmpdir(), "tt-peer-"));
    await Bun.$`git clone --quiet --branch master ${remote} ${clone}`.quiet();
    await Bun.$`git -C ${clone} config user.name bob`.quiet();
    await Bun.$`git -C ${clone} config user.email bob@taskthing.local`.quiet();
    await Bun.write(
      join(clone, "tasks", "bob-1.md"),
      `---\ncreatedAt: 500\ntitle: "bob's task"\ntitle_lastModified: 500\ntitle_lastModifiedBy: "bob"\n---\n`,
    );
    await Bun.$`git -C ${clone} add -A`.quiet();
    await Bun.$`git -C ${clone} commit -m ${"taskthing: consolidated snapshot"}`.quiet();
    await Bun.$`git -C ${clone} push origin master`.quiet();
    await rm(clone, { recursive: true, force: true });

    expect((await taskthing(home, "pull")).exitCode).toBe(0);

    // Pulling brings everyone's consolidated work down, without losing our own
    // un-consolidated task.
    const titles = (await workspaceMdwal(home).readAll("task")).map((t) => t.title).sort();
    expect(titles).toEqual(["bob's task", "walk the dog"]);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("rebuild consolidates into master, and truncate bounds a branch's history", async () => {
  const home = await dataHome();
  const remote = await mkdtemp(join(tmpdir(), "tt-remote-"));
  try {
    await Bun.$`git init --bare --initial-branch=master ${remote}`.quiet();
    await taskthing(home, "workspace remote add", remote);

    for (const title of ["one", "two", "three"]) {
      await taskthing(home, "add", title);
      await taskthing(home, "sync");
    }

    expect((await taskthing(home, "rebuild")).exitCode).toBe(0);

    // Consolidation puts everyone's work into master's snapshot, and the closing
    // sync purges what is now consolidated from this peer's own log.
    const onMaster = await Bun.$`git --git-dir=${remote} ls-tree -r --name-only master`.text();
    expect(onMaster.split("\n").filter((f) => f.startsWith("tasks/"))).toHaveLength(3);
    expect(await workspaceMdwal(home).parseLog()).toEqual([]);

    const format = "--format=%(refname:short)";
    const branch = (
      await Bun.$`git --git-dir=${remote} for-each-ref ${format} refs/heads/users`.text()
    ).trim();
    const before = Number(
      (await Bun.$`git --git-dir=${remote} rev-list --count ${branch}`.text()).trim(),
    );
    expect(before).toBeGreaterThan(2);

    expect((await taskthing(home, "truncate", "--keep=2")).exitCode).toBe(0);

    // History is bounded without touching the current state.
    const after = Number(
      (await Bun.$`git --git-dir=${remote} rev-list --count ${branch}`.text()).trim(),
    );
    expect(after).toBe(2);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(3);

    // A truncate with no size is a mistake worth reporting, not a guess.
    expect((await taskthing(home, "truncate")).exitCode).toBe(1);
  } finally {
    await rm(home, { recursive: true, force: true });
    await rm(remote, { recursive: true, force: true });
  }
});

test("set title rewrites a task's title, and uncheck reopens a completed one", async () => {
  const home = await dataHome();
  try {
    await taskthing(home, "add", "walk the dgo");
    await taskthing(home, "list");

    expect((await taskthing(home, "set title", "1", "walk the dog")).exitCode).toBe(0);
    expect((await workspaceMdwal(home).readAll("task"))[0]!.title).toBe("walk the dog");

    await taskthing(home, "check", "1");
    expect((await workspaceMdwal(home).readAll("task"))[0]!.completed).toBe(true);

    // Reopening is the inverse of checking — a task finished by mistake goes
    // back on the list.
    await taskthing(home, "list", "--checked");
    expect((await taskthing(home, "uncheck", "1")).exitCode).toBe(0);
    expect((await workspaceMdwal(home).readAll("task"))[0]!.completed).toBe(false);
    expect((await taskthing(home, "list")).stdout).toContain("walk the dog");

    // Unchecking a recurring task must not mint anything: only completing does.
    await seedTask(home, {
      title: "water plants",
      completed: true,
      rrule: "DTSTART:20260701T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO",
    });
    await taskthing(home, "list", "--checked");
    const number = (await taskthing(home, "list", "--checked")).stdout
      .split("\n")
      .find((line) => line.includes("water plants"))!
      .split(" ")[0]!;

    await taskthing(home, "uncheck", number);
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(2);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("config set writes settings, refuses unknown values, and can't switch workspace", async () => {
  const home = await dataHome();
  try {
    expect((await taskthing(home, "config set", "dateFormat", "europe")).exitCode).toBe(0);
    expect((await taskthing(home, "config set", "nerdfont", "true")).exitCode).toBe(0);

    const shown = await taskthing(home, "config");
    expect(shown.stdout).toContain("europe");
    expect(shown.stdout).toContain("nerdfont");

    // A cadence level (mehorias item 10) is a bare string in frontmatter, so it
    // is coerced to a number and stored — later read back and printed as one.
    expect((await taskthing(home, "config set", "autoSync", "6")).exitCode).toBe(0);
    expect((await taskthing(home, "config")).stdout).toContain("autoSync 6");

    // A cadence must be a positive integer: zero, negative, and non-numeric
    // values are all refused rather than silently stored.
    for (const junk of ["0", "-1", "nope"]) {
      const rejected = await taskthing(home, "config set", "autoRebuild", junk);
      expect(rejected.exitCode).toBe(1);
    }

    // A date format outside the supported set is refused: rendering has to know
    // how to draw it.
    const bad = await taskthing(home, "config set", "dateFormat", "klingon");
    expect(bad.exitCode).toBe(1);
    expect(bad.stderr).toMatch(/america|europe/i);

    // The current workspace lives in config.md too, but is deliberately not
    // settable here — it moves only through `use`, so nothing changes it as a
    // side effect.
    const workspace = await taskthing(home, "config set", "currentWorkspace", "other");
    expect(workspace.exitCode).toBe(1);
    expect(workspace.stderr).toMatch(/use/i);

    // Settings survive, and so does the workspace pointer they sit beside.
    await taskthing(home, "add", "still the same workspace");
    expect(await workspaceMdwal(home).readAll("task")).toHaveLength(1);
    expect((await taskthing(home, "config")).stdout).toContain("europe");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("add creates a task from a title alone, in the current workspace", async () => {
  const home = await dataHome();
  try {
    const run = await taskthing(home, "add", "walk the dog");
    expect(run.exitCode).toBe(0);

    // The task exists in the workspace config.md points at — no --workspace
    // needed for the common case.
    const tasks = await workspaceMdwal(home).readAll("task");
    expect(tasks).toHaveLength(1);
    const task = tasks[0]!;
    expect(task.title).toBe("walk the dog");

    // Defaults that make a bare task well-formed: it belongs to the virtual
    // board, has no date, and is open.
    expect(task.board).toBe("inbox");
    expect(task.rrule).toBeNull();
    expect(task.completed).toBe(false);
    expect(task.deleted).toBe(false);

    // The id is a nanoid minted at creation, and it names the file.
    expect(task.id).toHaveLength(21);
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

/** A brand-new, empty data home — what `install` is run against. */
async function emptyHome(): Promise<string> {
  return await mkdtemp(join(tmpdir(), "tt-fresh-"));
}

test("install scaffolds a fresh home with the local workspace and default config", async () => {
  const home = await emptyHome();
  try {
    const run = await taskthing(home, "install");
    expect(run.exitCode).toBe(0);

    // The local workspace folder is created, ready to hold tasks/boards.
    expect(await readdir(join(home, "workspaces"))).toContain("local");

    // config.md names the local workspace as current, with the documented
    // defaults (american dates, no nerdfont) — read back through the CLI itself.
    const shown = await taskthing(home, "config");
    expect(shown.stdout).toContain("currentWorkspace local");
    expect(shown.stdout).toContain("dateFormat america");
    expect(shown.stdout).toContain("nerdfont false");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("install takes date-format and nerdfont from flags when piped", async () => {
  const home = await emptyHome();
  try {
    const run = await taskthing(home, "install", "--date-format=europe", "--nerdfont");
    expect(run.exitCode).toBe(0);

    const shown = await taskthing(home, "config");
    expect(shown.stdout).toContain("dateFormat europe");
    expect(shown.stdout).toContain("nerdfont true");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("install refuses to clobber an existing install without --force", async () => {
  const home = await emptyHome();
  try {
    await taskthing(home, "install", "--date-format=europe");

    // A second install must not silently reset the settings.
    const again = await taskthing(home, "install", "--date-format=america");
    expect(again.exitCode).toBe(1);
    expect(again.stderr).toMatch(/already installed/i);
    // The original choice survives.
    expect((await taskthing(home, "config")).stdout).toContain("dateFormat europe");

    // --force reinstalls, overwriting.
    const forced = await taskthing(home, "install", "--date-format=america", "--force");
    expect(forced.exitCode).toBe(0);
    expect((await taskthing(home, "config")).stdout).toContain("dateFormat america");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("migrations reports recorded migrations as applied and none pending", async () => {
  const home = await dataHome();
  try {
    // Pre-seed a migration record, as the runner would after applying it.
    const recordDir = join(home, "workspaces", "main", ".taskthing", "migrations");
    await mkdir(recordDir, { recursive: true });
    await Bun.write(join(recordDir, "0.1.0.md"), "0::CREATE_FOLDER::list::::{}");

    const run = await taskthing(home, "migrations");
    expect(run.exitCode).toBe(0);
    expect(run.stdout).toContain("0.1.0");
    expect(run.stdout).toContain("yes");
    // The embedded set is empty, so nothing is pending — the applied record is
    // not a pending one.
    expect(run.stdout).toContain("no migrations pending");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

/** A data home whose config.md has the update cache pre-seeded (fresh). */
async function updateHome(cache: {
  lastUpdateCheck: number;
  updateAvailable: boolean;
  updateTarget: string | null;
  autoUpdate: "confirm" | "silent";
}): Promise<string> {
  const home = await mkdtemp(join(tmpdir(), "tt-upd-"));
  await mkdir(join(home, "workspaces", "main"), { recursive: true });
  const fm = [
    "---",
    'currentWorkspace: "main"',
    `lastUpdateCheck: ${cache.lastUpdateCheck}`,
    `updateAvailable: ${cache.updateAvailable}`,
    cache.updateTarget === null ? "updateTarget: null" : `updateTarget: "${cache.updateTarget}"`,
    `autoUpdate: "${cache.autoUpdate}"`,
    "---",
    "",
  ].join("\n");
  await Bun.write(join(home, "config.md"), fm);
  return home;
}

test("update check reports the cached pending update without hitting the network", async () => {
  // A fresh cache (checked just now) means check answers from config.md alone —
  // the release source is never consulted, so no network in this test.
  const home = await updateHome({
    lastUpdateCheck: Date.now(),
    updateAvailable: true,
    updateTarget: "9.9.9",
    autoUpdate: "confirm",
  });
  try {
    const run = await taskthing(home, "update check");
    expect(run.exitCode).toBe(0);
    expect(run.stdout).toContain("there's a pending update 0.1.0 → 9.9.9!");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});

test("update apply reports up-to-date from a fresh cache, without downloading", async () => {
  const home = await updateHome({
    lastUpdateCheck: Date.now(),
    updateAvailable: false,
    updateTarget: null,
    autoUpdate: "confirm",
  });
  try {
    const run = await taskthing(home, "update apply");
    expect(run.exitCode).toBe(0);
    expect(run.stdout).toContain("you're on the latest version!");
  } finally {
    await rm(home, { recursive: true, force: true });
  }
});
