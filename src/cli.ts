import { chmod, mkdir, mkdtemp, readdir, rename, rm } from "node:fs/promises";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { z } from "zod";

import * as chrono from "chrono-node";
import { RRule } from "rrule";

import { parseAddInput } from "./add-input";
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
import { nextOccurrence, recurs, recurrenceToText, withDtstart } from "./recurrence";
import { createRepository, type Repository } from "./repository";
import { spinnerMessages, summarize } from "./spinner-messages";
import { CONFIG_MIGRATIONS, MIGRATIONS, TRANSFORMS, VERSION } from "./build";
import { createMigrationRunner } from "./migration-runner";
import { createUpdater, type ConfigStore, type UpdateState } from "./updater";
import type { BoardRow } from "./board-list";
import type { MigrationRow } from "./migration-list";
import type { TaskGroup } from "./task-list";
import {
  renderOutcomeLine,
  renderBoardList,
  renderWorkspaceList,
  renderCommandSpinner,
  renderMigrationList,
  renderSpinner,
  renderTaskList,
  runConfigScreen,
  runConfirmation,
  runInstallForm,
  runThemeScreen,
} from "./tui";
import {
  boardSchema,
  configSchema,
  CONFIG_KEYS,
  taskSchema,
  type Board,
  type Config,
  type Task,
} from "./schema";

// ── porcelain CLI ───────────────────────────────────────────────────────────
//
// The daily-driver surface: ergonomic verbs over the repositories and manager
// primitives. Plumber and porcelain share one binary and signal themselves
// through syntax (positional entity vs. --<entity> flag), git-style.

/** Where taskthing keeps its config and workspaces. */
function dataHome(): string {
  return process.env.TASKTHING_HOME ?? join(homedir(), ".taskthing");
}

/**
 * config.md, read and written through the same markdown+frontmatter path as any
 * entity — it simply lives outside every workspace and outside LWW.
 */
const configPath = () => join(dataHome(), "config.md");

async function readConfig(): Promise<Config> {
  const text = await Bun.file(configPath()).text();
  return configSchema.parse(frontmatter().parseEntity(text));
}

async function writeConfig(config: Config): Promise<void> {
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

async function currentWorkspaceName(): Promise<string> {
  return (await readConfig()).currentWorkspace;
}

function workspacePath(name: string): string {
  return join(dataHome(), "workspaces", name);
}

/**
 * The workspace a command acts on: the one it names with `--workspace`, else the
 * current one from config.md — which is why the flag is rarely needed.
 */
async function targetWorkspaceName(args: string[] = []): Promise<string> {
  const named = parseFlags(args).get("workspace");
  return named !== undefined && named.length > 0 ? named : await currentWorkspaceName();
}

async function targetWorkspace(args: string[] = []): Promise<string> {
  return workspacePath(await targetWorkspaceName(args));
}

// The schemas as mdwal stores them: the id names the file, not a field.
const taskFields = taskSchema.omit({ id: true });
const boardFields = boardSchema.omit({ id: true });

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
async function openWorkspace(root: string): Promise<{ mdwal: Mdwal; manager: Manager }> {
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
function now(): Date {
  const pinned = process.env.TASKTHING_NOW;
  return pinned === undefined ? new Date() : new Date(pinned);
}

/**
 * Report a mutation command's outcome (mehorias items 2-4): run the work, then
 * draw its success line — styled on a TTY, plain when piped — or, when the work
 * throws, the command's failure line and a non-zero exit. `failure` formats that
 * line from the caught error; the work returns the segments to render and
 * receives the config so it can format values in the user's settings.
 */
async function report(
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
async function reportTask(
  verb: string,
  work: (config: Config) => Promise<TaskOutcome>,
): Promise<void> {
  await report(
    (error) => taskFailure(verb, error),
    async (config) => taskMessage(await work(config)),
  );
}

/** Report a board command's outcome: its failure names the whole action phrase. */
async function reportBoard(
  action: string,
  work: (config: Config) => Promise<BoardOutcome>,
): Promise<void> {
  await report(
    (error) => boardFailure(action, error),
    async (config) => boardMessage(await work(config)),
  );
}

async function reportWorkspace(
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
function datedOutcome(
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

async function add(args: string[]): Promise<void> {
  const flags = parseFlags(args);
  const input = args.filter((arg) => !arg.startsWith("--")).join(" ");

  // Porcelain names its entity with a flag; the task is the default, since it is
  // what a task manager is mostly asked to do.
  if (flags.has("board")) {
    await reportBoard("create this board", async () => {
      const { mdwal } = await openWorkspace(await targetWorkspace(args));
      const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");
      // `--icon=<glyph>` sets the board's icon at creation; without it the board
      // keeps the schema default (no icon).
      await boards.create({ name: input, icon: flags.get("icon") ?? "" });
      return { name: input, predicate: "created" };
    });
    return;
  }

  await reportTask("create", async (config) => {
    const root = await targetWorkspace(args);
    const { mdwal } = await openWorkspace(root);
    const { title, rrule, board } = parseAddInput(input, now());

    // A `b:[…]` tag names the board (by name/number/inbox); without one the task
    // falls to the inbox. Resolving here surfaces an unknown board as this
    // command's failure line rather than a silent miss.
    const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");
    const boardId = board === null ? INBOX : await resolveBoard(boards, root, board);

    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");
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
}

/** What the user typed that isn't a flag: the entity they mean. */
function positional(args: string[]): string {
  return args.find((arg) => !arg.startsWith("--")) ?? "";
}

/** Flags as typed: `--checked`, `--in-board=work`. */
function parseFlags(args: string[]): Map<string, string> {
  const flags = new Map<string, string>();
  for (const arg of args) {
    if (!arg.startsWith("--")) continue;
    const [name, value] = arg.slice(2).split("=", 2);
    flags.set(name!, value ?? "");
  }
  return flags;
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
async function allWorkspaces(): Promise<string[]> {
  return (await readdir(join(dataHome(), "workspaces"), { withFileTypes: true }))
    .filter((entry) => entry.isDirectory())
    .map((entry) => entry.name);
}

async function list(args: string[]): Promise<void> {
  const flags = parseFlags(args);
  if (flags.has("board")) return await listBoards(args);

  // A global listing spans workspaces, so its numbers do too: they are handed
  // out from a store beside the workspaces, and each remembers which one its
  // task lives in.
  const global = flags.has("global");
  const named = flags.get("workspace");
  const names = global
    ? await allWorkspaces()
    : [named !== undefined && named.length > 0 ? named : await currentWorkspaceName()];
  const store = global
    ? createGlobalKvStore(dataHome())
    : createKvStore(workspacePath(names[0]!), "tasks");

  // A listing hands out fresh numbers, and the most recent listing wins: every
  // numbering it replaces is cleared, or a stale one would keep answering for
  // numbers the user is now reading off a different list.
  await store.reset();
  await Promise.all(
    global
      ? names.map((name) => createKvStore(workspacePath(name), "tasks").reset())
      : [createGlobalKvStore(dataHome()).reset()],
  );

  // On an interactive terminal the styled TUI is drawn; piped (scripts, tests)
  // gets the plain, greppable lines. A global listing spans workspaces: its board
  // groups carry a workspace name in the header (mehorias item 2), so they gather
  // into one board-grouped view rather than staying a flat list.
  if (process.stdout.isTTY) {
    const groups: TaskGroup[] = [];
    for (const name of names) {
      groups.push(...(await gatherTaskGroups(name, flags, store, global)));
    }
    await renderTaskList(groups, await readConfig(), now());
  } else {
    for (const name of names) {
      await listWorkspace(name, flags, store, global);
    }
  }
}

/**
 * The filter every listing applies: a task shows only if it satisfies every
 * active flag. The two defaults — hide finished and binned work — answer the
 * question a task list is usually asked, each one flag away.
 */
function taskFilter(
  flags: Map<string, string>,
  inBoardId: string | null,
): (task: Task) => boolean {
  return (task) =>
    (flags.has("checked") || !task.completed) &&
    // `--deleted` is the bin: it shows the deleted ones *instead of* the live
    // ones, the way `--starred` shows only the starred.
    (flags.has("deleted") ? task.deleted : !task.deleted) &&
    (!flags.has("starred") || task.star) &&
    (!flags.has("hasDescription") || task.description !== null) &&
    (!flags.has("period") || inPeriod(task.rrule, flags.get("period")!)) &&
    // The board filter compares ids: `--in-board` is resolved from a name/number/
    // inbox to a board id up front, so a task's stored id (the `inbox` sentinel
    // included, ADR-0004) can be matched directly.
    (inBoardId === null || task.board === inBoardId);
}

/** The board id a `--in-board=<ref>` filter targets, or null when the flag is absent. */
async function inBoardFilter(
  flags: Map<string, string>,
  boards: Repository<Board, z.input<typeof boardFields>>,
  root: string,
): Promise<string | null> {
  if (!flags.has("in-board")) return null;
  return await resolveBoard(boards, root, flags.get("in-board")!);
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
async function gatherTaskGroups(
  name: string,
  flags: Map<string, string>,
  store: KvStore,
  global: boolean,
): Promise<TaskGroup[]> {
  const root = workspacePath(name);
  const { mdwal } = await openWorkspace(root);
  const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");
  const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");

  const shown = await tasks.filter(taskFilter(flags, await inBoardFilter(flags, boards, root)));

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

/** Boards number independently of tasks, in a store of their own. */
async function listBoards(args: string[]): Promise<void> {
  const root = await targetWorkspace(args);
  const { mdwal } = await openWorkspace(root);
  const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(
    mdwal,
    "board",
  );

  const store = createKvStore(root, "boards");
  await store.reset();

  const rows: BoardRow[] = [];
  for (const board of await boards.filter((board) => !board.deleted)) {
    const number = await store.set(board.id);
    rows.push({ number, board: { id: board.id, name: board.name, icon: board.icon, color: board.color } });
  }

  if (process.stdout.isTTY) {
    await renderBoardList(rows, await readConfig());
  } else {
    for (const row of rows) console.log(`${row.number} ${row.board.name}`);
  }
}

async function listWorkspace(
  name: string,
  flags: Map<string, string>,
  store: KvStore,
  global: boolean,
): Promise<void> {
  const root = workspacePath(name);
  const { mdwal } = await openWorkspace(root);
  const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");
  const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");

  const shown = await tasks.filter(taskFilter(flags, await inBoardFilter(flags, boards, root)));

  for (const task of shown) {
    const number = await store.set(global ? `${name}/${task.id}` : task.id);
    console.log(global ? `${number} ${task.title} (${name})` : `${number} ${task.title}`);
  }
}

/**
 * Turn what the user typed into the entity it refers to. Numbers only mean
 * something after a listing handed them out, so an unknown one says to list
 * rather than acting on a guess.
 */
async function resolveTask(reference: string, args: string[] = []): Promise<{ root: string; id: string }> {
  const root = await targetWorkspace(args);
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

async function star(reference: string, value: boolean, args: string[]): Promise<void> {
  await reportTask(value ? "star" : "unstar", async () => {
    const { root, id } = await resolveTask(reference, args);
    const { mdwal } = await openWorkspace(root);
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    await tasks.update({ ...task, star: value });
    return { title: task.title, predicate: value ? "starred" : "unstarred" };
  });
}

/** The fields `set` may write on a task. */
const TASK_FIELDS = ["title", "description", "date-time", "board"] as const;

async function setTask(args: string[]): Promise<void> {
  await reportTask("update", async (config) => {
    const [field, reference, ...rest] = args.filter((arg) => !arg.startsWith("--"));
    if (!TASK_FIELDS.includes(field as (typeof TASK_FIELDS)[number])) {
      throw new Error(`a task has no ${field} — try ${TASK_FIELDS.join(", ")}`);
    }

    const { root, id } = await resolveTask(reference ?? "", args);
    const { mdwal } = await openWorkspace(root);
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);
    const value = rest.join(" ");

    if (field === "title") {
      await tasks.update({ ...task, title: value });
      return { title: value, predicate: "title updated" };
    }

    if (field === "description") {
      await tasks.update({ ...task, description: value });
      return { title: task.title, predicate: "description updated" };
    }

    // Move a task between boards (mehorias item 4). The board is named by
    // name/number/inbox; an empty value moves it back to the inbox — the
    // "no board" state — mirroring how an empty date-time clears the schedule.
    if (field === "board") {
      const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");
      const boardId = value === "" ? INBOX : await resolveBoard(boards, root, value);
      await tasks.update({ ...task, board: boardId });
      const boardName =
        boardId === INBOX ? "inbox" : ((await boards.findById(boardId))?.name ?? value);
      return { title: task.title, predicate: "moved", value: { connector: " to ", text: boardName } };
    }

    // A task's date *is* its DTSTART: setting it moves the task in time, and for
    // a recurring one moves where the series starts, keeping the rule intact. An
    // empty value clears the schedule (rrule → null), leaving an undated task.
    if (value === "") {
      await tasks.update({ ...task, rrule: null });
      return { title: task.title, predicate: "schedule removed" };
    }

    const rrule = withDtstart(task.rrule, parseDate(value));
    await tasks.update({ ...task, rrule });
    return datedOutcome(task.title, "schedule updated", rrule, config);
  });
}

function parseDate(input: string): Date {
  const date = chrono.parseDate(input, now());
  if (date === null) throw new Error(`not a date: ${input}`);
  return date;
}

/** The fields `set --board` may write; a board has no others to offer. */
const BOARD_FIELDS = ["name", "icon", "color"] as const;

async function setBoard(args: string[]): Promise<void> {
  const [field] = args.filter((arg) => !arg.startsWith("--"));

  // The failure phrase depends on the field, so it is chosen from the parsed
  // field before the work runs; an unknown field falls back to the generic one
  // and the work reports it.
  const action =
    field === "name"
      ? "rename this board"
      : field === "icon"
        ? "update this board's icon"
        : field === "color"
          ? "update this board's color"
          : "update this board";

  await reportBoard(action, async () => {
    const [, reference, ...value] = args.filter((arg) => !arg.startsWith("--"));
    if (!BOARD_FIELDS.includes(field as (typeof BOARD_FIELDS)[number])) {
      throw new Error(`a board has no ${field} — try ${BOARD_FIELDS.join(", ")}`);
    }

    const root = await targetWorkspace(args);
    const { mdwal } = await openWorkspace(root);
    const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");

    const id = await resolveBoard(boards, root, reference ?? "");
    const board = await boards.findById(id);
    if (board === null) throw new Error(`no such board: ${reference}`);

    const next = value.join(" ");
    await boards.update({ ...board, [field!]: next });

    // Renaming keeps the old name as the primary chip and names the new one;
    // icon/color echo back the value the user just applied.
    if (field === "name") {
      return { name: board.name, predicate: "renamed", value: { connector: " to ", text: next } };
    }
    if (field === "icon") {
      return { name: board.name, predicate: "icon updated", value: { connector: ": ", text: next } };
    }
    return { name: board.name, predicate: "color updated", value: { connector: ": ", text: next } };
  });
}

/**
 * Turn what the user typed into a board id. A board answers to, in order: the
 * name `inbox` (the virtual board, ADR-0004, which is never numbered); a number
 * from `list --board`; its own name (the first live board that carries it); or
 * its raw id — the only handle a *deleted* board has, since it has no list
 * number and its name is excluded from the live lookup, which is how `recover
 * --board <id>` reaches it.
 */
async function resolveBoard(
  boards: Repository<Board, z.input<typeof boardFields>>,
  root: string,
  reference: string,
): Promise<string> {
  if (reference.trim().toLowerCase() === "inbox") return INBOX;

  const number = Number(reference);
  if (reference.trim() !== "" && Number.isInteger(number)) {
    const id = await createKvStore(root, "boards").get(number);
    if (id === null) {
      throw new Error(`no board numbered ${number} — run \`taskthing list --board\` first`);
    }
    return id;
  }

  const named = (await boards.filter((board) => !board.deleted && board.name === reference))[0];
  if (named !== undefined) return named.id;

  // A raw id, so a deleted board stays recoverable by the handle it still has.
  if ((await boards.findById(reference)) !== null) return reference;
  throw new Error(`no such board: ${reference}`);
}

async function check(reference: string, args: string[], value = true): Promise<void> {
  await reportTask(value ? "complete" : "reopen", async (config) => {
    const { root, id } = await resolveTask(reference, args);
    const { mdwal } = await openWorkspace(root);
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

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

/** Soft-delete and recover are the same operation with opposite values. */
async function setDeleted(reference: string, value: boolean, args: string[]): Promise<void> {
  await reportTask(value ? "delete" : "recover", async () => {
    const { root, id } = await resolveTask(reference, args);
    const { mdwal } = await openWorkspace(root);
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

    // Read the task first so its title is available for the outcome line — the
    // soft-delete/recover itself only reports whether the task existed.
    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);

    const done = value ? await tasks.delete(id) : await tasks.recover(id);
    if (!done) throw new Error(`no such task: ${reference}`);
    return { title: task.title, predicate: value ? "deleted" : "recovered" };
  });
}

/**
 * Take an already-deleted task's file off this machine. Purely local: no event,
 * no LWW, nothing propagated — and since the task's history is untouched, a
 * later pull or rebuild may rebuild the file. Short-term tidying, not removal.
 */
async function clear(reference: string, args: string[]): Promise<void> {
  await reportTask("permanently delete", async () => {
    const { root, id } = await resolveTask(reference, args);
    const { mdwal } = await openWorkspace(root);
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

    const task = await tasks.findById(id);
    if (task === null) throw new Error(`no such task: ${reference}`);
    if (!task.deleted) {
      throw new Error(`task ${reference} is not deleted — \`delete\` it first`);
    }

    await mdwal.discard("task", id);
    return { title: task.title, predicate: "permanently deleted" };
  });
}

async function deleteBoard(reference: string, args: string[]): Promise<void> {
  await reportBoard("delete this board", async () => {
    const root = await targetWorkspace(args);
    const { mdwal } = await openWorkspace(root);
    const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");
    const tasks: Repository<Task, z.input<typeof taskFields>> = createRepository(mdwal, "task");

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
}

async function recoverBoard(reference: string, args: string[]): Promise<void> {
  await reportBoard("recover this board", async () => {
    const root = await targetWorkspace(args);
    const { mdwal } = await openWorkspace(root);
    const boards: Repository<Board, z.input<typeof boardFields>> = createRepository(mdwal, "board");

    const id = await resolveBoard(boards, root, reference);
    const board = await boards.findById(id);
    if (board === null) throw new Error(`no such board: ${reference}`);
    if (!(await boards.recover(id))) throw new Error(`no such board: ${reference}`);
    return { name: board.name, predicate: "recovered" };
  });
}

/** Persist which workspace is current. Only `use` ever changes it. */
async function setCurrentWorkspace(name: string): Promise<void> {
  await writeConfig({ ...(await readConfig()), currentWorkspace: name });
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
async function reportConfig(
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

async function config(args: string[]): Promise<void> {
  const [subcommand, key, value] = args.filter((arg) => !arg.startsWith("--"));

  // With no arguments: on a terminal, open the interactive TUI and persist what
  // it collects; piped, just print what is set (scriptable, no TUI).
  if (subcommand === undefined) {
    const current = await readConfig();
    if (process.stdout.isTTY) {
      const update = await runConfigScreen(current);
      // Left without choosing anything: nothing to write, nothing to report.
      const [changed] = Object.keys(update);
      if (changed === undefined) return;
      await reportConfig(current, changed, () =>
        writeConfig(configSchema.parse({ ...current, ...update })),
      );
    } else {
      for (const [name, setting] of Object.entries(current)) {
        console.log(`${name} ${String(setting)}`);
      }
    }
    return;
  }

  if (subcommand !== "set") throw new Error(`unknown config command: ${subcommand}`);

  // The current workspace lives here too, but is not a setting: it moves only
  // through `use`, so nothing changes it as a side effect.
  if (key === "currentWorkspace") {
    throw new Error("the current workspace changes with `taskthing use <name>`");
  }
  if (!CONFIG_KEYS.includes(key as (typeof CONFIG_KEYS)[number])) {
    throw new Error(`no such setting: ${key} — try ${CONFIG_KEYS.join(", ")}`);
  }

  // The frontmatter carries bare strings, so a typed setting is coerced before
  // the schema validates it: booleans from "true", the cadence levels to numbers
  // (schema then rejects a non-positive or non-integer value).
  const numeric = key === "autoSync" || key === "autoRebuild" || key === "autoTruncate";
  const coerced = key === "nerdfont" ? value === "true" : numeric ? Number(value) : value;
  const parsed = configSchema.parse({
    ...(await readConfig()),
    [key!]: coerced,
  });
  await writeConfig(parsed);
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
async function runAllMigrations(): Promise<void> {
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
function buildUpdater() {
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

async function update(args: string[]): Promise<void> {
  const [subcommand] = args.filter((arg) => !arg.startsWith("--"));
  const config = await readConfig();
  const updater = buildUpdater();
  const force = parseFlags(args).has("force");

  if (subcommand === "check" || subcommand === undefined) {
    // The three outcomes (latest / pending <cur> → <target> / error) use
    // CONTEXT's copy; the success line is chosen from the result, so it goes
    // through the plain Spinner rather than the fixed-copy command spinner.
    const run = async () => {
      const result = await updater.check({ force });
      return result.status === "latest"
        ? spinnerMessages.updateCheck.latest
        : spinnerMessages.updateCheck.update(result.current, result.target);
    };
    if (process.stdout.isTTY) {
      const ok = await renderSpinner(spinnerMessages.updateCheck.pending, run, config);
      if (!ok) process.exit(1);
    } else {
      try {
        console.log(await run());
      } catch (error) {
        console.error(spinnerMessages.updateCheck.failure(error));
        process.exit(1);
      }
    }
    return;
  }

  if (subcommand === "apply") {
    const confirm = async () => {
      if (config.autoUpdate === "silent") return true;
      if (!process.stdout.isTTY) {
        throw new Error("update apply needs a terminal to confirm, or set `autoUpdate: silent`");
      }
      return await runConfirmation("Update", "update taskthing now?", config);
    };
    const run = async () => {
      const result = await updater.apply({ confirm });
      if (result.status === "up-to-date") return spinnerMessages.updateCheck.latest;
      if (result.status === "declined") return "update cancelled";
      return spinnerMessages.updateApply.success(result.target);
    };
    if (process.stdout.isTTY) {
      const ok = await renderSpinner(spinnerMessages.updateApply.pending, run, config);
      if (!ok) process.exit(1);
    } else {
      try {
        console.log(await run());
      } catch (error) {
        console.error(spinnerMessages.updateApply.failure(error));
        process.exit(1);
      }
    }
    return;
  }

  throw new Error(`unknown update command: ${subcommand}`);
}

/** The structural migrations recorded as applied in a workspace. */
async function recordedMigrations(root: string): Promise<Set<string>> {
  try {
    const files = await readdir(join(root, ".taskthing", "migrations"));
    return new Set(files.map((f) => f.replace(/\.md$/, "")));
  } catch {
    return new Set();
  }
}

/**
 * Report the workspace's migration state: every known migration (the binary's
 * embedded set, plus any already recorded here) with whether it has been applied
 * — so the user can audit whether they are fully migrated (Spec 0003 story 33).
 */
async function migrations(): Promise<void> {
  const root = await targetWorkspace([]);
  const recorded = await recordedMigrations(root);
  const versions = [...new Set([...MIGRATIONS.map((m) => m.version), ...recorded])].sort(
    (a, b) => Bun.semver.order(a, b),
  );
  const rows: MigrationRow[] = versions.map((version, i) => ({
    number: i + 1,
    version,
    applied: recorded.has(version),
  }));

  if (process.stdout.isTTY) {
    await renderMigrationList(rows, await readConfig());
    return;
  }
  for (const row of rows) console.log(`${row.number} ${row.version} ${row.applied ? "yes" : "no"}`);
  console.log(
    rows.some((row) => !row.applied)
      ? "there's pending migrations. verify your taskthing installation."
      : "this workspace has no migrations pending",
  );
}

/**
 * Scaffold a fresh taskthing: create the local workspace and write config.md
 * naming it current. Settings come from the interactive first-run form on a
 * terminal, or from flags/defaults when piped (american dates, no nerdfont).
 * Refuses to clobber an existing install without --force.
 */
async function install(args: string[]): Promise<void> {
  // The nerdfont preference decides which glyph the closing line wears; it is
  // only known once the form (or --nerdfont) has run, so it starts at the
  // no-nerdfont default that also covers failures raised before that point.
  let nerdfont = false;
  try {
    const flags = parseFlags(args);
    if ((await Bun.file(configPath()).exists()) && !flags.has("force")) {
      throw new Error("taskthing is already installed — pass --force to reinstall");
    }

    let dateFormat: "america" | "europe" = "america";
    if (process.stdout.isTTY) {
      const seed = configSchema.parse({ currentWorkspace: "local" });
      ({ dateFormat, nerdfont } = await runInstallForm(seed));
    } else {
      const df = flags.get("date-format");
      if (df === "america" || df === "europe") dateFormat = df;
      nerdfont = flags.has("nerdfont");
    }

    // The local workspace folder, and config.md naming it the current one.
    await mkdir(workspacePath("local"), { recursive: true });
    await writeConfig(
      configSchema.parse({ currentWorkspace: "local", dateFormat, nerdfont, theme: "" }),
    );
  } catch (error) {
    // install owns its own outcome line, so the failure is reported here rather
    // than re-thrown to the top-level handler that prints a bare message.
    const glyphs = createGlyphs(nerdfont);
    console.error(
      `${glyphs.failure} something went wrong during installation. error: ${summarize(error)}`,
    );
    process.exit(1);
  }

  const glyphs = createGlyphs(nerdfont);
  console.log(`${glyphs.success} taskthing installed successfully!`);
}

/**
 * The `theme` shortcut: jump straight to the theme editor (default vs. custom)
 * and persist the chosen value. Interactive, so it needs a terminal.
 */
async function theme(): Promise<void> {
  const current = await readConfig();
  if (!process.stdout.isTTY) {
    throw new Error("`theme` needs an interactive terminal");
  }
  const value = await runThemeScreen(current);
  await reportConfig(current, "theme", () =>
    writeConfig(configSchema.parse({ ...current, theme: value })),
  );
}

async function assertWorkspaceExists(name: string): Promise<void> {
  if (!(await allWorkspaces()).includes(name)) {
    throw new Error(`no such workspace: ${name}`);
  }
}

/**
 * Attach the workspace to a git remote, or detach it. Association is what turns
 * a workspace remote: from then on its operations are logged as events for peers
 * to replay, and detaching leaves the remote's content untouched.
 */
async function workspaceRemote(args: string[]): Promise<void> {
  const [, action, url] = args.filter((arg) => !arg.startsWith("--"));

  switch (action) {
    case "add":
      await reportWorkspace("add this remote", async () => {
        if (url === undefined) throw new Error("remote add needs a URL");
        const name = await targetWorkspaceName(args);
        const { manager } = await openWorkspace(workspacePath(name));
        // Joining a remote that already has this peer's branch while there is
        // local work is unreconcilable; the manager refuses without this.
        await manager.init(url, { confirmDestroy: parseFlags(args).has("confirm") });
        return { name, predicate: "remote added", value: { connector: ": ", text: url } };
      });
      return;

    case "remove":
      await reportWorkspace("remove this remote", async () => {
        const name = await targetWorkspaceName(args);
        const { manager } = await openWorkspace(workspacePath(name));
        await manager.disassociate();
        return { name, predicate: "remote removed" };
      });
      return;

    default:
      throw new Error(`unknown remote command: ${action ?? ""}`);
  }
}

/**
 * The git-facing verbs, each a thin porcelain over a manager primitive: publish
 * this peer's events, re-derive from the consolidated snapshot, consolidate
 * every peer into a new one, or bound a branch's history.
 */
async function syncing(command: string, args: string[]): Promise<void> {
  const name = await targetWorkspaceName(args);
  const { manager } = await openWorkspace(workspacePath(name));
  const config = await readConfig();

  // Each git-facing verb runs behind its spinner (Spec 0004): the copy is fixed
  // by the command, the operation is the manager primitive.
  let copy;
  let operation: () => Promise<unknown>;
  switch (command) {
    case "sync":
      copy = spinnerMessages.sync(name);
      operation = () => manager.sync();
      break;
    case "pull":
      copy = spinnerMessages.pull(name);
      operation = () => manager.pull();
      break;
    case "rebuild":
      copy = spinnerMessages.rebuild(name);
      operation = () => manager.rebuild();
      break;
    case "truncate": {
      const branch = parseFlags(args).get("branch");
      const keep = Number(parseFlags(args).get("keep") ?? "");
      if (!Number.isInteger(keep) || keep < 1) {
        throw new Error("truncate needs --keep=<n>, the number of commits to keep");
      }
      copy = spinnerMessages.truncate(name, branch ?? "");
      operation = () => manager.truncateHistory({ branch, keepN: keep });
      break;
    }
    default:
      throw new Error(`unknown command: ${command}`);
  }

  // A TTY gets the live spinner; piped, the same copy is printed plainly so a
  // script (or a log) sees the outcome and, on failure, why — the spinner would
  // otherwise swallow the reason behind a half-drawn pending frame.
  if (process.stdout.isTTY) {
    const ok = await renderCommandSpinner(copy, operation, config);
    if (!ok) process.exit(1);
    return;
  }
  try {
    await operation();
    console.log(copy.success);
  } catch (error) {
    console.error(copy.failure(error));
    process.exit(1);
  }
}

async function workspace(args: string[]): Promise<void> {
  const [subcommand, name, second] = args.filter((arg) => !arg.startsWith("--"));
  if (subcommand === "remote") return await workspaceRemote(args);

  switch (subcommand) {
    case "list": {
      // The listing marks the current one, since every unscoped command acts on
      // it and the user needs to know which that is. A TTY gets the Selection-
      // style list; piped stays the plain, greppable `* name` lines.
      const current = await currentWorkspaceName();
      const workspaces = await allWorkspaces();
      if (process.stdout.isTTY) {
        await renderWorkspaceList(workspaces, current, await readConfig());
      } else {
        for (const workspace of workspaces) {
          console.log(workspace === current ? `* ${workspace}` : `  ${workspace}`);
        }
      }
      return;
    }

    case "create":
      await reportWorkspace("create this workspace", async () => {
        await mkdir(workspacePath(name ?? ""), { recursive: true });
        return { name: name ?? "", predicate: "created" };
      });
      return;

    case "use":
      await reportWorkspace("switch workspace", async () => {
        await assertWorkspaceExists(name ?? "");
        await setCurrentWorkspace(name!);
        return { name: name!, predicate: "now in use" };
      });
      return;

    case "rename":
      await reportWorkspace("rename this workspace", async () => {
        await assertWorkspaceExists(name ?? "");
        await rename(workspacePath(name!), workspacePath(second ?? ""));
        // The rename follows the user: renaming the workspace they are working in
        // must not leave them pointing at a name that no longer exists.
        if ((await currentWorkspaceName()) === name) await setCurrentWorkspace(second!);
        return { name: name!, predicate: "renamed", value: { connector: " to ", text: second ?? "" } };
      });
      return;

    case "delete":
      await reportWorkspace("delete this workspace", async () => {
        await assertWorkspaceExists(name ?? "");
        if ((await currentWorkspaceName()) === name) {
          throw new Error(
            `${name} is the current workspace — \`taskthing use <other>\` before deleting it`,
          );
        }
        // Deleting removes the local folder outright, so it is never automatic.
        if (!parseFlags(args).has("confirm")) {
          throw new Error(`deleting ${name} removes its local folder — pass --confirm`);
        }
        await rm(workspacePath(name!), { recursive: true, force: true });
        return { name: name!, predicate: "deleted" };
      });
      return;

    default:
      throw new Error(`unknown workspace command: ${subcommand ?? ""}`);
  }
}

/**
 * Author a structural migration's log line(s) by running the folder op against
 * a throwaway workspace in logging mode, then returning what it wrote. The dev's
 * real workspaces are never touched — this is a dev-time authoring tool (ADR-0007,
 * Spec 0005 story 20), so its only output is the `content` to embed in build.ts.
 */
async function authorEntityLog(op: string, name: string, newName?: string): Promise<string> {
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
      default:
        throw new Error(`unknown entity op: ${op} (expected create|rename|delete)`);
    }
    return await Bun.file(join(scratch, "events.log")).text();
  } finally {
    await rm(scratch, { recursive: true, force: true });
  }
}

/**
 * The plumber `entity` command: a dev-time tool for authoring the structural
 * migration that introduces/renames/removes a fixed entity folder. It prints the
 * mdwal log line(s) to embed in the binary; `--log` signals that authoring intent
 * (without it there is nothing to produce).
 */
async function entity(args: string[]): Promise<void> {
  if (!parseFlags(args).has("log")) {
    throw new Error("entity authors migrations: pass --log to emit the migration line");
  }
  const [op, name, newName] = args.filter((a) => !a.startsWith("--"));
  if (op === undefined || name === undefined) {
    throw new Error("usage: taskthing entity <create|rename|delete> <name> [<new-name>] --log");
  }
  process.stdout.write(await authorEntityLog(op, name, newName));
}

/** Print the command surface. Shown by `help`, by a bare invocation, and after an unknown command. */
function help(): void {
  console.log(`taskthing — a git-based task manager

Usage: taskthing <command> [args]

Tasks:
  add "<input>"                    add a task (d:[…] date, r:[…] recurrence, b:[…] board)
  list [filters]                   list tasks (--checked --starred --deleted
                                   --hasDescription --period=<Nd|Nm|Ny> --in-board=<name>)
  check | uncheck <id>             complete / reopen a task
  star | unstar <id>               toggle a task's star
  delete | recover <id>            soft-delete / restore a task
  clear <id>                       remove an already-deleted task locally
  set title|description|date-time|board <id> <value>   edit a task field
                                   (set board <id> <name|number|inbox> moves it)

Boards:
  add --board "<name>" [--icon=<glyph>]   create a board
  boards                           list boards
  set --board name|icon|color <id> <value>   edit a board
  delete | recover --board <id>    soft-delete / restore a board

Workspaces:
  workspaces                       list workspaces (shortcut for workspace list)
  use <name>                       switch the current workspace
  workspace create|rename|delete <name>
  workspace remote add <url> | remove

Sync:
  sync | pull | rebuild            git sync operations
  truncate --keep=<n> [--branch <b>]

Config & themes:
  config                           open settings (piped: print them)
  config set <key> <value>         set a setting non-interactively
  theme                            open the theme editor

Updates & migrations:
  update check | apply             self-update from GitHub
  migrations                       report applied/pending migrations

Setup:
  install                          first-run scaffold and setup
  help                             show this help`);
}

const [command, ...rest] = process.argv.slice(2);

try {
  switch (command) {
    case "install":
      await install(rest);
      break;
    case "add":
      await add(rest);
      break;
    case "list":
      await list(rest);
      break;
    case "sync":
    case "pull":
    case "rebuild":
    case "truncate":
      await syncing(command, rest);
      break;
    case "config":
      await config(rest);
      break;
    // A first-class shortcut for the theme editor, like `boards` for `list --board`.
    case "theme":
      await theme();
      break;
    case "boards":
      await list(["--board", ...rest]);
      break;
    case "migrations":
      await migrations();
      break;
    // Plumber: author a structural migration's log for a fixed entity folder.
    case "entity":
      await entity(rest);
      break;
    case "update":
      await update(rest);
      break;
    case "workspace":
      await workspace(rest);
      break;
    // A first-class shortcut for `workspace list`, like `boards` for `list --board`.
    case "workspaces":
      await workspace(["list", ...rest]);
      break;
    // A first-class shortcut for `workspace use`: switching is common enough to
    // deserve its own verb, and it is the only way the current workspace moves.
    case "use":
      await workspace(["use", ...rest]);
      break;
    case "clear":
      await clear(positional(rest), rest);
      break;
    case "check":
      await check(positional(rest), rest);
      break;
    case "uncheck":
      await check(positional(rest), rest, false);
      break;
    case "star":
      await star(positional(rest), true, rest);
      break;
    case "unstar":
      await star(positional(rest), false, rest);
      break;
    case "delete":
      if (parseFlags(rest).has("board")) {
        await deleteBoard(positional(rest), rest);
      } else {
        await setDeleted(positional(rest), true, rest);
      }
      break;
    case "recover":
      if (parseFlags(rest).has("board")) {
        await recoverBoard(positional(rest), rest);
      } else {
        await setDeleted(positional(rest), false, rest);
      }
      break;
    case "set":
      if (parseFlags(rest).has("board")) {
        await setBoard(rest);
      } else {
        await setTask(rest);
      }
      break;
    case "help":
    case "--help":
    case "-h":
      help();
      break;
    default:
      // A bare invocation just shows the help; an unrecognized command shows it
      // too, but names the mistake on stderr and fails so scripts can detect it.
      if (command === undefined) {
        help();
        break;
      }
      console.error(`unknown command: ${command}`);
      help();
      process.exit(1);
  }
} catch (error) {
  // A failed command says why and exits non-zero, so scripts can detect it.
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
