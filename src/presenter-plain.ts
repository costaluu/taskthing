import { createGlyphs } from "./glyph";
import type { Config } from "./schema";
import type { Presenter, PresenterCall } from "./presenter";

// ── plain adapter ────────────────────────────────────────────────────────────
//
// What a pipe sees: greppable lines, no styling, no cursor movement. The
// line-building is a pure function of the call so the recording adapter can
// replay it for a test's benefit without writing to the real streams — one
// implementation of the wording, two ways of reading it.

/** A plain line and the stream it belongs on. */
export interface PlainLine {
  stream: "stdout" | "stderr";
  text: string;
}

const out = (text: string): PlainLine => ({ stream: "stdout", text });
const err = (text: string): PlainLine => ({ stream: "stderr", text });

/** The lines a call prints when nobody is watching a terminal. */
export function plainLines(call: PresenterCall, config: Config): PlainLine[] {
  const glyphs = createGlyphs(config.nerdfont);

  switch (call.method) {
    case "outcome":
      return [out(`${glyphs.success} ${call.segments.map((segment) => segment.text).join("")}`)];

    case "taskList": {
      // Piped output stays a flat list in the order the numbers were handed
      // out, even though the rows arrive grouped by board for the styled view.
      // A group carries its workspace only in a global listing, which is what
      // puts the "(name)" suffix on the line.
      const rows = call.groups.flatMap((group) =>
        group.rows.map((row) => ({ ...row, workspace: group.workspace })),
      );
      rows.sort((a, b) => a.number - b.number);
      return rows.map((row) =>
        out(
          row.workspace === undefined
            ? `${row.number} ${row.task.title}`
            : `${row.number} ${row.task.title} (${row.workspace})`,
        ),
      );
    }

    case "boardList":
      return call.rows.map((row) => out(`${row.number} ${row.board.name}`));

    case "workspaceList":
      return call.workspaces.map((workspace) =>
        out(workspace === call.current ? `* ${workspace}` : `  ${workspace}`),
      );

    case "migrationList":
      return [
        ...call.rows.map((row) => out(`${row.number} ${row.version} ${row.applied ? "yes" : "no"}`)),
        out(
          call.rows.some((row) => !row.applied)
            ? "there's pending migrations. verify your taskthing installation."
            : "this workspace has no migrations pending",
        ),
      ];

    case "progress":
      return [out(call.success)];

    case "progressFailed":
      return [err(call.message)];

    case "fail":
      return [err(`${glyphs.failure} ${call.message}`)];
  }
}

function write(lines: PlainLine[]): void {
  for (const line of lines) {
    if (line.stream === "stdout") console.log(line.text);
    else console.error(line.text);
  }
}

export function plainPresenter(): Presenter {
  const draw = (call: PresenterCall, config: Config) => write(plainLines(call, config));

  return {
    async outcome(segments, config) {
      draw({ method: "outcome", segments }, config);
    },
    async taskList(groups, now, config) {
      draw({ method: "taskList", groups, now }, config);
    },
    async boardList(rows, config) {
      draw({ method: "boardList", rows }, config);
    },
    async workspaceList(workspaces, current, config) {
      draw({ method: "workspaceList", workspaces, current }, config);
    },
    async migrationList(rows, config) {
      draw({ method: "migrationList", rows }, config);
    },
    async progress(copy, work, config) {
      // The reason has to reach a pipe, or a script sees only a non-zero exit.
      try {
        await work();
        draw({ method: "progress", success: copy.success }, config);
      } catch (error) {
        draw({ method: "progressFailed", message: copy.failure(error) }, config);
        process.exit(1);
      }
    },
    async progressDynamic(_pending, failure, run, config) {
      try {
        draw({ method: "progress", success: await run() }, config);
      } catch (error) {
        draw({ method: "progressFailed", message: failure(error) }, config);
        process.exit(1);
      }
    },
    async downloadProgress(_pending, failure, run, config) {
      try {
        draw({ method: "progress", success: await run(() => {}) }, config);
      } catch (error) {
        draw({ method: "progressFailed", message: failure(error) }, config);
        process.exit(1);
      }
    },
    fail(message, config) {
      draw({ method: "fail", message }, config);
      process.exit(1);
    },
  };
}
