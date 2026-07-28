import { plainLines, type PlainLine } from "./presenter-plain";
import { CommandFailure, type Presenter, type PresenterCall } from "./presenter";
import type { Config } from "./schema";

// ── recording adapter ────────────────────────────────────────────────────────
//
// The adapter a test runs against. It keeps each call rather than drawing it, so
// a test can assert two ways from one run: on `stdout`/`stderr`, which it
// renders through the plain adapter's own formatter, and on `calls`, which is
// the data the ink adapter would have been handed — the wiring no assertion on
// text can reach.
//
// Where the shipped adapters exit, this one throws: an in-process test runner
// cannot survive `process.exit`, and the harness wants the exit code as a value
// anyway.

export interface RecordingPresenter extends Presenter {
  /** What the command asked to draw, in order — including the ink adapter's inputs. */
  readonly calls: PresenterCall[];
  /**
   * The plain lines, rendered as they were recorded. Mutable so a harness can
   * splice in what a command wrote straight to the console — a handful of lines
   * are deliberately plain in both modes and never reach the presenter.
   */
  readonly lines: PlainLine[];
  readonly stdout: string;
  readonly stderr: string;
}

export function recordingPresenter(): RecordingPresenter {
  const calls: PresenterCall[] = [];
  const lines: PlainLine[] = [];

  // Rendered as it is recorded rather than on demand, so console output a
  // harness splices in keeps its place in the sequence.
  const record = (call: PresenterCall, config: Config) => {
    calls.push(call);
    lines.push(...plainLines(call, config));
  };

  const joined = (stream: "stdout" | "stderr") =>
    lines
      .filter((line) => line.stream === stream)
      .map((line) => line.text)
      .join("\n");

  return {
    calls,
    lines,
    get stdout() {
      return joined("stdout");
    },
    get stderr() {
      return joined("stderr");
    },

    async outcome(segments, config) {
      record({ method: "outcome", segments }, config);
    },
    async taskList(groups, now, config) {
      record({ method: "taskList", groups, now }, config);
    },
    async boardList(rows, config) {
      record({ method: "boardList", rows }, config);
    },
    async workspaceList(workspaces, current, config) {
      record({ method: "workspaceList", workspaces, current }, config);
    },
    async migrationList(rows, config) {
      record({ method: "migrationList", rows }, config);
    },
    async progress(copy, work, config) {
      try {
        await work();
        record({ method: "progress", success: copy.success }, config);
      } catch (error) {
        const message = copy.failure(error);
        record({ method: "progressFailed", message }, config);
        throw new CommandFailure(message);
      }
    },
    async progressDynamic(_pending, failure, run, config) {
      try {
        record({ method: "progress", success: await run() }, config);
      } catch (error) {
        const message = failure(error);
        record({ method: "progressFailed", message }, config);
        throw new CommandFailure(message);
      }
    },
    async downloadProgress(_pending, failure, run, config) {
      try {
        record({ method: "progress", success: await run(() => {}) }, config);
      } catch (error) {
        const message = failure(error);
        record({ method: "progressFailed", message }, config);
        throw new CommandFailure(message);
      }
    },
    fail(message, config): never {
      record({ method: "fail", message }, config);
      throw new CommandFailure(message);
    },
  };
}
