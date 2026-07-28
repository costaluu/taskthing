import { plainLines } from "./presenter-plain";
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
  readonly calls: PresenterCall[];
  readonly stdout: string;
  readonly stderr: string;
}

export function recordingPresenter(): RecordingPresenter {
  const calls: PresenterCall[] = [];
  // Each call is rendered with the config it was drawn with, since that decides
  // the glyphs.
  const configs: Config[] = [];

  const record = (call: PresenterCall, config: Config) => {
    calls.push(call);
    configs.push(config);
  };

  const lines = (stream: "stdout" | "stderr") =>
    calls
      .flatMap((call, index) => plainLines(call, configs[index]!))
      .filter((line) => line.stream === stream)
      .map((line) => line.text)
      .join("\n");

  return {
    calls,
    get stdout() {
      return lines("stdout");
    },
    get stderr() {
      return lines("stderr");
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
    fail(message, config): never {
      record({ method: "fail", message }, config);
      throw new CommandFailure(message);
    },
  };
}
