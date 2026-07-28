import { chmod, mkdtemp, readdir, rename, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import { RRule } from "rrule";
import * as chrono from "chrono-node";

import { resolveGitAuthor } from "./author";
import { createMonotonicClock } from "./clock";
import { createGlyphs } from "./glyph";
import { createGlobalKvStore, createKvStore, type KvStore } from "./kv-store";
import { createManager, type Manager } from "./manager";
import { createMdwal, INBOX, type Mdwal } from "./mdwal";
import { boardFailure, boardMessage, type BoardOutcome } from "./board-message";
import { taskFailure, taskMessage, type MessageSegment, type TaskOutcome } from "./task-message";
import { workspaceFailure, workspaceMessage, type WorkspaceOutcome } from "./workspace-message";
import { formatNextOccurrence } from "./date-format";
import { recurs, recurrenceToText } from "./recurrence";
import { createRepository, type Repository } from "./repository";
import { summarize } from "./spinner-messages";
import { CONFIG_MIGRATIONS, MIGRATIONS, TRANSFORMS, VERSION } from "./build";
import { createMigrationRunner } from "./migration-runner";
import { createUpdater, type ConfigStore, type UpdateState } from "./updater";
import { renderOutcomeLine } from "./tui";
import type { TaskGroup } from "./task-list";
import { boardSchema, configSchema, taskSchema, type Board, type Config, type Task } from "./schema";

// ── porcelain shared surface ────────────────────────────────────────────────
//
// Everything the porcelain commands stand on: where the data lives, how a
// workspace is opened, how an outcome is reported, and how what the user typed
// becomes an entity. The command modules under `commands/` hold only their own
// verb; this module holds what more than one of them needs.

/** Where taskthing keeps its config and workspaces. */
export function dataHome(): string {
  return process.env.TASKTHING_HOME ?? join(homedir(), ".taskthing");
}

/**
 * config.md, read and written through the same markdown+frontmatter path as any
 * entity — it simply lives outside every workspace and outside LWW.
 */
export const configPath = () => join(dataHome(), "config.md");

export async function readConfig(): Promise<Config> {
  const text = await Bun.file(configPath()).text();
  return configSchema.parse(frontmatter().parseEntity(text));
}

export async function writeConfig(config: Config): Promise<void> {
  await Bun.write(configPath(), frontmatter().serializeEntity(config));
}

/** An engine bound to nothing, used only for its serialize/parse path. */
function frontmatter(): Mdwal {
  return createMdwal({
    root: dataHome(),
    schemas: {},
    mode: "local",
    clock: () => 0n,
    author: () => "",
  });
}

export async function currentWorkspaceName(): Promise<string> {
  return (await readConfig()).currentWorkspace;
}

export function workspacePath(name: string): string {
  return join(dataHome(), "workspaces", name);
}

/**
 * The workspace a command acts on: the one it names with `--workspace`, else the
 * current one from config.md — which is why the flag is rarely needed.
 */
export async function targetWorkspaceName(named?: string): Promise<string> {
  return named !== undefined && named.length > 0 ? named : await currentWorkspaceName();
}

export async function targetWorkspace(named?: string): Promise<string> {
  return workspacePath(await targetWorkspaceName(named));
}

// The schemas as mdwal stores them: the id names the file, not a field.
export const taskFields = taskSchema.omit({ id: true });
export const boardFields = boardSchema.omit({ id: true });

export type TaskRepository = Repository<Task, z.input<typeof taskFields>>;
export type BoardRepository = Repository<Board, z.input<typeof boardFields>>;

export const taskRepository = (mdwal: Mdwal): TaskRepository => createRepository(mdwal, "task");
export const boardRepository = (mdwal: Mdwal): BoardRepository => createRepository(mdwal, "board");

/**
 * The largest timestamp this peer has already logged here, or 0 if it has logged
 * nothing. The log is strictly increasing, so this is just its last line's ts —
 * read raw, since it must be known before the engine (and its clock) exist.
 */
async function lastLoggedTimestamp(root: string): Promise<bigint> {
  const text = await Bun.file(join(root, "events.log"))
    .text()
    .catch(() => "");
  const last = text.split("\n").filter((l) => l.length > 0).at(-1);
  return last === undefined ? 0n : BigInt(last.split("::")[0]!);
}

/**
 * Open a workspace with its engine and its manager. The two are mutually
 * dependent by design: the workspace is remote exactly while it has a remote to
 * publish to, which only the manager can answer, and the manager needs the
 * engine to read and derive state.
 */
export async function openWorkspace(root: string): Promise<{ mdwal: Mdwal; manager: Manager }> {
  const author = await resolveGitAuthor();
  const clock = createMonotonicClock({
    now: () => BigInt(Date.now()) * 1_000_000n,
    // Seed from this peer's own last logged timestamp so the log stays strictly
    // increasing even across process restarts and a wall clock that steps back —
    // the invariant purge-by-timestamp relies on (ADR-0006).
    seed: await lastLoggedTimestamp(root),
  });

  let manager: Manager;
  const mdwal = createMdwal({
    root,
    schemas: { task: taskFields, board: boardFields },
    mode: async () => ((await manager.isAssociated()) ? "remote" : "local"),
    clock,
    author: () => author,
    // Every event stamps the authoring binary's version, so a newer binary can
    // migrate-on-read older events and the version barrier can refuse a future
    // one (ADR-0007). Without this the barrier is inert in the real binary.
    version: VERSION,
    // The migrate-on-read chain replay runs per event to bring an older-schema
    // event up to the current schema (ADR-0007). Shared by pull/rebuild, which
    // derive through this same engine.
    transforms: TRANSFORMS,
  });
  manager = createManager({ root, mdwal, author: () => author, clock });

  return { mdwal, manager };
}

/** Now, which tests pin so natural-language dates resolve deterministically. */
export function now(): Date {
  const pinned = process.env.TASKTHING_NOW;
  return pinned === undefined ? new Date() : new Date(pinned);
}

export function parseDate(input: string): Date {
  const date = chrono.parseDate(input, now());
  if (date === null) throw new Error(`not a date: ${input}`);
  return date;
}

// ── outcome reporting ───────────────────────────────────────────────────────

/**
 * Report a mutation command's outcome (mehorias items 2-4): run the work, then
 * draw its success line — styled on a TTY, plain when piped — or, when the work
 * throws, the command's failure line and a non-zero exit. `failure` formats that
 * line from the caught error; the work returns the segments to render and
 * receives the config so it can format values in the user's settings.
 */
export async function report(
  failure: (error: unknown) => string,
  work: (config: Config) => Promise<MessageSegment[]>,
): Promise<void> {
  const config = await readConfig();
  try {
    const segments = await work(config);
    if (process.stdout.isTTY) {
      await renderOutcomeLine(segments, config);
    } else {
      // Piped (scripts, logs) get the same words plainly, so the outcome — and on
      // failure its reason — is never swallowed by the styling.
      const line = segments.map((s) => s.text).join("");
      console.log(`${createGlyphs(config.nerdfont).success} ${line}`);
    }
  } catch (error) {
    console.error(`${createGlyphs(config.nerdfont).failure} ${failure(error)}`);
    process.exit(1);
  }
}

/** Report a task command's outcome: its failure names the action on "this task". */
export async function reportTask(
  verb: string,
  work: (config: Config) => Promise<TaskOutcome>,
): Promise<void> {
  await report(
    (error) => taskFailure(verb, error),
    async (config) => taskMessage(await work(config)),
  );
}

/** Report a board command's outcome: its failure names the whole action phrase. */
export async function reportBoard(
  action: string,
  work: (config: Config) => Promise<BoardOutcome>,
): Promise<void> {
  await report(
    (error) => boardFailure(action, error),
    async (config) => boardMessage(await work(config)),
  );
}

export async function reportWorkspace(
  action: string,
  work: (config: Config) => Promise<WorkspaceOutcome>,
): Promise<void> {
  await report(
    (error) => workspaceFailure(action, error),
    async (config) => workspaceMessage(await work(config)),
  );
}

/**
 * A success outcome whose date lives in `rrule`: its next occurrence is the
 * rule's DTSTART, and the recurrence is shown only when the rule truly recurs
 * (never for a DTSTART-only dated task, which the library misreads as yearly).
 */
export function datedOutcome(
  title: string,
  predicate: string,
  rrule: string | null,
  config: Config,
): TaskOutcome {
  const dtstart = rrule === null ? null : RRule.fromString(rrule).options.dtstart;
  return {
    title,
    predicate,
    nextOccurrence: dtstart === null ? null : formatNextOccurrence(dtstart, config.dateFormat),
    recurrence: rrule !== null && recurs(rrule) ? recurrenceToText(rrule) : null,
  };
}

/** The setting names as they read in a feedback line. */
const CONFIG_LABELS: Record<string, string> = {
  dateFormat: "date format",
  nerdfont: "nerdfont",
  theme: "theme",
};

/**
 * Persist a config change from the TUI and report it (mehorias item 9), in the
 * install line's shape: a success line on write, a summarized failure line and a
 * non-zero exit otherwise. The label reads as the setting the user just changed.
 */
export async function reportConfig(
  config: Config,
  key: string,
  write: () => Promise<void>,
): Promise<void> {
  const glyphs = createGlyphs(config.nerdfont);
  const label = CONFIG_LABELS[key] ?? key;
  try {
    await write();
    console.log(`${glyphs.success} ${label} updated successfully!`);
  } catch (error) {
    console.error(
      `${glyphs.failure} something went wrong updating ${label}. error: ${summarize(error)}`,
    );
    process.exit(1);
  }
}

// ── filters and references ──────────────────────────────────────────────────

/** The listing filters, one per `list` flag. */
export interface TaskFilters {
  checked: boolean;
  deleted: boolean;
  starred: boolean;
  hasDescription: boolean;
  period?: string | undefined;
  inBoard?: string | undefined;
}

/**
 * The far end of a `--period=<N>d|m|y` window, counted from now. A window is
 * how far ahead the user wants to look, so an undated task never falls inside
 * one — the filter asks "when", and an undated task has no answer.
 */
function periodEnd(period: string, from: Date): Date {
  const match = period.match(/^(\d+)([dmy])$/);
  if (match === null) {
    throw new Error(`not a period: ${period} — try 2d, 3m or 1y`);
  }

  const amount = Number(match[1]);
  const end = new Date(from);
  if (match[2] === "d") end.setUTCDate(end.getUTCDate() + amount);
  if (match[2] === "m") end.setUTCMonth(end.getUTCMonth() + amount);
  if (match[2] === "y") end.setUTCFullYear(end.getUTCFullYear() + amount);
  return end;
}

function inPeriod(rrule: string | null, period: string): boolean {
  if (rrule === null) return false;
  const dtstart = RRule.fromString(rrule).options.dtstart;
  return dtstart <= periodEnd(period, now());
}

/** Every workspace in the data home, by name. */
export async function allWorkspaces(): Promise<string[]> {
  return (await readdir(join(dataHome(), "workspaces"), { withFileTypes: true }))
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);
}

export async function assertWorkspaceExists(name: string): Promise<void> {
  if (!(await allWorkspaces()).includes(name)) {
    throw new Error(`no such workspace: ${name}`);
  }
}

/** Persist which workspace is current. Only `use` ever changes it. */
export async function setCurrentWorkspace(name: string): Promise<void> {
  await writeConfig({ ...(await readConfig()), currentWorkspace: name });
}

/**
 * The filter every listing applies: a task shows only if it satisfies every
 * active flag. The two defaults — hide finished and binned work — answer the
 * question a task list is usually asked, each one flag away.
 */
export function taskFilter(
  filters: TaskFilters,
  inBoardId: string | null,
): (task: Task) => boolean {
  return (task) =>
    (filters.checked || !task.completed) &&
    // `--deleted` is the bin: it shows the deleted ones *instead of* the live
    // ones, the way `--starred` shows only the starred.
    (filters.deleted ? task.deleted : !task.deleted) &&
    (!filters.starred || task.star) &&
    (!filters.hasDescription || task.description !== null) &&
    (filters.period === undefined || inPeriod(task.rrule, filters.period)) &&
    // The board filter compares ids: `--in-board` is resolved from a name/number/
    // inbox to a board id up front, so a task's stored id (the `inbox` sentinel
    // included, ADR-0004) can be matched directly.
    (inBoardId === null || task.board === inBoardId);
}

/** The board id a `--in-board=<ref>` filter targets, or null when the flag is absent. */
export async function inBoardFilter(
  filters: TaskFilters,
  boards: BoardRepository,
  root: string,
): Promise<string | null> {
  if (filters.inBoard === undefined) return null;
  return await resolveBoard(boards, root, filters.inBoard);
}

/** The date a task line shows: completion instant when done, else its DTSTART. */
async function taskDate(task: Task, mdwal: Mdwal): Promise<Date | null> {
  if (task.completed) {
    // The completion date is the `completed` field's LWW timestamp (ns), never
    // the rrule (story 21).
    const raw = await mdwal.read("task", task.id);
    const ns = raw[`completed_lastModified`] as bigint | undefined;
    return ns === undefined ? null : new Date(Number(ns / 1_000_000n));
  }
  if (task.rrule === null) return null;
  return RRule.fromString(task.rrule).options.dtstart;
}

/** Gather the board-grouped, numbered rows the TaskList draws for one workspace. */
export async function gatherTaskGroups(
  name: string,
  filters: TaskFilters,
  store: KvStore,
  global: boolean,
): Promise<TaskGroup[]> {
  const root = workspacePath(name);
  const { mdwal } = await openWorkspace(root);
  const tasks = taskRepository(mdwal);
  const boards = boardRepository(mdwal);

  const shown = await tasks.filter(taskFilter(filters, await inBoardFilter(filters, boards, root)));

  // Look up each board's display once; the virtual inbox has no entity.
  const boardById = new Map((await boards.findAll()).map((b) => [b.id, b]));
  const display = (id: string) =>
    id === INBOX
      ? { id: INBOX, name: "inbox", icon: "", color: "" }
      : (() => {
          const b = boardById.get(id);
          return { id, name: b?.name ?? id, icon: b?.icon ?? "", color: b?.color ?? "" };
        })();

  // Group in first-seen board order, numbering as we go.
  const groups: TaskGroup[] = [];
  const groupByBoard = new Map<string, TaskGroup>();
  for (const task of shown) {
    let group = groupByBoard.get(task.board);
    if (group === undefined) {
      // A global listing tags each group with its workspace, so the shared board
      // header can draw "(name)"; a scoped one leaves it off.
      group = { board: display(task.board), rows: [], ...(global ? { workspace: name } : {}) };
      groupByBoard.set(task.board, group);
      groups.push(group);
    }
    // Global numbers are handed out from the store beside the workspaces, keyed by
    // `<workspace>/<id>` so a number remembers which workspace its task lives in.
    const number = await store.set(global ? `${name}/${task.id}` : task.id);
    group.rows.push({ number, task, date: await taskDate(task, mdwal) });
  }
  return groups;
}

/**
 * Turn what the user typed into the entity it refers to. Numbers only mean
 * something after a listing handed them out, so an unknown one says to list
 * rather than acting on a guess.
 */
export async function resolveTask(
  reference: string,
  workspace?: string,
): Promise<{ root: string; id: string }> {
  const root = await targetWorkspace(workspace);
  const number = Number(reference);
  if (!Number.isInteger(number)) return { root, id: reference };

  // The current workspace's own numbering answers first; a number it doesn't
  // know may still have come from a global listing, which says which workspace
  // its task lives in.
  const own = await createKvStore(root, "tasks").get(number);
  if (own !== null) return { root, id: own };

  const global = await createGlobalKvStore(dataHome()).get(number);
  if (global === null) {
    throw new Error(`no task numbered ${number} — run \`taskthing list\` first`);
  }
  const separator = global.indexOf("/");
  return {
    root: workspacePath(global.slice(0, separator)),
    id: global.slice(separator + 1),
  };
}

/**
 * Turn what the user typed into a board id. A board answers to, in order: the
 * name `inbox` (the virtual board, ADR-0004, which is never numbered); a number
 * from `board list`; its own name (the first live board that carries it); or
 * its raw id — the only handle a *deleted* board has, since it has no list
 * number and its name is excluded from the live lookup, which is how `board
 * recover <id>` reaches it.
 */
export async function resolveBoard(
  boards: BoardRepository,
  root: string,
  reference: string,
): Promise<string> {
  if (reference.trim().toLowerCase() === "inbox") return INBOX;

  const number = Number(reference);
  if (reference.trim() !== "" && Number.isInteger(number)) {
    const id = await createKvStore(root, "boards").get(number);
    if (id === null) {
      throw new Error(`no board numbered ${number} — run \`taskthing board list\` first`);
    }
    return id;
  }

  const named = (await boards.filter((board) => !board.deleted && board.name === reference))[0];
  if (named !== undefined) return named.id;

  // A raw id, so a deleted board stays recoverable by the handle it still has.
  if ((await boards.findById(reference)) !== null) return reference;
  throw new Error(`no such board: ${reference}`);
}

// ── self-update (Spec 0005) ──────────────────────────────────────────────────

/** The config.md binary keys, adapted to the updater's UpdateState shape. */
const updateConfigStore: ConfigStore = {
  read: async () => {
    const c = await readConfig();
    return {
      lastCheck: c.lastUpdateCheck,
      updateAvailable: c.updateAvailable,
      target: c.updateTarget,
      autoUpdate: c.autoUpdate,
    };
  },
  write: async (state: UpdateState) => {
    await writeConfig({
      ...(await readConfig()),
      lastUpdateCheck: state.lastCheck,
      updateAvailable: state.updateAvailable,
      updateTarget: state.target,
      autoUpdate: state.autoUpdate,
    });
  },
};

/** Run the embedded structural migrations across every workspace on the machine. */
export async function runAllMigrations(): Promise<void> {
  const author = await resolveGitAuthor();
  const clock = createMonotonicClock({ now: () => BigInt(Date.now()) * 1_000_000n, seed: 0n });
  const runner = createMigrationRunner({
    migrations: MIGRATIONS,
    workspaces: (await allWorkspaces()).map(workspacePath),
    engine: (root) =>
      createMdwal({
        root,
        schemas: { task: taskFields, board: boardFields },
        mode: "local",
        clock,
        author: () => author,
      }),
    // Config migrations rewrite the global config.md once, through the same
    // frontmatter+schema path any command uses, recorded beside it in dataHome().
    config: { read: readConfig, write: writeConfig },
    configHome: dataHome(),
    configMigrations: CONFIG_MIGRATIONS,
  });
  await runner.run();
}

/**
 * The updater wired to the real world: GitHub as the release source, fetch as
 * the downloader, and the running binary as the swap target. Everything the
 * orchestrator (Spec 0005) needs; the network/binary boundaries are only reached
 * when an update is actually downloaded.
 */
export function buildUpdater() {
  const repo = process.env.TASKTHING_REPO ?? "costaluu/taskthing";
  return createUpdater({
    currentVersion: VERSION,
    platform: { os: process.platform, arch: process.arch },
    clock: { now: () => Date.now() },
    config: updateConfigStore,
    releaseSource: {
      latest: async () => {
        const res = await fetch(`https://api.github.com/repos/${repo}/releases/latest`, {
          headers: { "User-Agent": "taskthing", Accept: "application/vnd.github+json" },
        });
        if (!res.ok) throw new Error(`GitHub release lookup failed (${res.status})`);
        const json = (await res.json()) as {
          tag_name: string;
          assets: { name: string; browser_download_url: string }[];
        };
        return {
          version: json.tag_name.replace(/^v/, ""),
          assets: (json.assets ?? []).map((a) => ({ name: a.name, url: a.browser_download_url })),
        };
      },
    },
    downloader: {
      download: async (url) => new Uint8Array(await (await fetch(url)).arrayBuffer()),
    },
    fs: {
      // Download-then-replace: write beside the running binary, then rename over
      // it, so a failure mid-download never leaves a half-written binary.
      swap: async (bytes) => {
        const target = process.execPath;
        const tmp = `${target}.new`;
        await Bun.write(tmp, bytes);
        await chmod(tmp, 0o755);
        await rename(tmp, target);
      },
    },
    runMigrations: runAllMigrations,
  });
}

/** The structural migrations recorded as applied in a workspace. */
export async function recordedMigrations(root: string): Promise<Set<string>> {
  try {
    const files = await readdir(join(root, ".taskthing", "migrations"));
    return new Set(files.map((f) => f.replace(/\.md$/, "")));
  } catch {
    return new Set();
  }
}

/**
 * Author a structural migration's log line(s) by running the folder op against
 * a throwaway workspace in logging mode, then returning what it wrote. The dev's
 * real workspaces are never touched — this is a dev-time authoring tool (ADR-0007,
 * Spec 0005 story 20), so its only output is the `content` to embed in build.ts.
 */
export async function authorEntityLog(
  op: "create" | "rename" | "delete",
  name: string,
  newName?: string,
): Promise<string> {
  const author = await resolveGitAuthor();
  const scratch = await mkdtemp(join(tmpdir(), "taskthing-authoring-"));
  try {
    const mdwal = createMdwal({
      root: scratch,
      schemas: {},
      // Remote mode is what makes an op log; the throwaway root keeps it isolated.
      mode: "remote",
      clock: createMonotonicClock({ now: () => BigInt(Date.now()) * 1_000_000n, seed: 0n }),
      author: () => author,
      version: VERSION,
    });
    switch (op) {
      case "create":
        await mdwal.createFolder(name, { log: true });
        break;
      case "rename":
        if (newName === undefined) throw new Error("entity rename needs a <new-name>");
        // The source folder must exist for the rename to log against it.
        await mdwal.createFolder(name, { log: false });
        await mdwal.renameFolder(name, newName, { log: true });
        break;
      case "delete":
        await mdwal.deleteFolder(name, { log: true });
        break;
    }
    return await Bun.file(join(scratch, "events.log")).text();
  } finally {
    await rm(scratch, { recursive: true, force: true });
  }
}
