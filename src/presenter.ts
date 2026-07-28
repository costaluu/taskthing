import type { BoardRow } from "./board-list";
import type { CommandCopy } from "./command-spinner";
import type { MessageSegment } from "./task-message";
import type { MigrationRow } from "./migration-list";
import type { TaskGroup } from "./task-list";
import type { Config } from "./schema";

// ── presenter ────────────────────────────────────────────────────────────────
//
// How a command's result reaches the user. The command decides *what* to show —
// an outcome line, a listing, a slow operation's progress — and the presenter
// decides *how*: ink on a terminal, plain greppable lines when piped. It is the
// only place that reads `process.stdout.isTTY`, so the fork lives once rather
// than at each command that happens to draw something.
//
// Every method takes the config it draws with, the way the tui bridge already
// does: the ink adapter needs the theme and the nerdfont preference, and the
// plain one needs the glyphs. Passing it in also keeps this module free of any
// import back into porcelain.
//
// Failure is the presenter's business too, because "print the reason and exit
// non-zero" is a presentation policy: `fail` renders the line and terminates.
// The progress methods carry their own failure copy and so terminate on their
// own, which is why they don't hand back a result for the caller to check.

export interface Presenter {
  /** A mutation command's success line. */
  outcome(segments: MessageSegment[], config: Config): Promise<void>;

  taskList(groups: TaskGroup[], now: Date, config: Config): Promise<void>;
  boardList(rows: BoardRow[], config: Config): Promise<void>;
  workspaceList(workspaces: string[], current: string, config: Config): Promise<void>;
  migrationList(rows: MigrationRow[], config: Config): Promise<void>;

  /** Run slow work behind its fixed copy; terminates non-zero if it rejects. */
  progress(copy: CommandCopy, work: () => Promise<unknown>, config: Config): Promise<void>;

  /**
   * Like {@link Presenter.progress}, but the work resolves to its own success
   * line — used where the outcome picks the copy, as update-check's latest-vs-
   * pending does.
   */
  progressDynamic(
    pending: string,
    failure: (error: unknown) => string,
    run: () => Promise<string>,
    config: Config,
  ): Promise<void>;

  /** Draw a failure line and terminate non-zero. Never returns. */
  fail(message: string, config: Config): never;
}

/**
 * A recorded presenter call. The recording adapter keeps these so a test can
 * assert on what a command asked to be drawn — including the data the ink
 * adapter would have been handed, which no assertion on stdout can see.
 */
export type PresenterCall =
  | { method: "outcome"; segments: MessageSegment[] }
  | { method: "taskList"; groups: TaskGroup[]; now: Date }
  | { method: "boardList"; rows: BoardRow[] }
  | { method: "workspaceList"; workspaces: string[]; current: string }
  | { method: "migrationList"; rows: MigrationRow[] }
  | { method: "progress"; success: string }
  // A progress failure carries no glyph, unlike `fail` — the spinner's copy is
  // already a whole sentence, and prefixing it would change what piped callers
  // have always read.
  | { method: "progressFailed"; message: string }
  | { method: "fail"; message: string };

/**
 * Raised by the recording adapter where the shipped adapters would exit. A test
 * harness catches it and reads a non-zero exit code off it; nothing in the
 * shipped binary throws it, since there `fail` really does exit.
 */
export class CommandFailure extends Error {
  override readonly name = "CommandFailure";
}

// The adapter in force. The entry picks it once from `process.stdout.isTTY`;
// tests swap in the recording one per run. A CLI process draws through exactly
// one of these, so an ambient choice costs nothing and keeps the presenter out
// of every handler's signature.
let current: Presenter | null = null;

export function setPresenter(presenter: Presenter): void {
  current = presenter;
}

export function presenter(): Presenter {
  if (current === null) throw new Error("no presenter set — the entry sets one before running");
  return current;
}
