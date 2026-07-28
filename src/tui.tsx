import { render } from "ink";

import { BoardList, type BoardRow } from "./board-list";
import { CommandSpinner, type CommandCopy } from "./command-spinner";
import { DownloadProgress, type DownloadPhase } from "./download-progress";
import { OutcomeLine } from "./outcome-line";
import type { MessageSegment } from "./task-message";
import { ConfigScreen, type ConfigUpdate } from "./config-screen";
import { InstallForm, type InstallResult } from "./install-form";
import { MigrationList, type MigrationRow } from "./migration-list";
import { Spinner } from "./spinner";
import { TaskList, type TaskGroup } from "./task-list";
import { ThemeScreen } from "./theme-screen";
import { WorkspaceList } from "./workspace-list";
import { createTheme } from "./theme";
import type { Config } from "./schema";

// ── tui bridge ───────────────────────────────────────────────────────────────
//
// The one place the CLI (plain `.ts`, no JSX) reaches into the ink presentation
// layer. Each helper mounts a component, drives it to a result, unmounts, and
// resolves — so `cli.ts` stays a thin porcelain that gathers plain data and
// hands it here to be drawn. Interactive helpers need a TTY (raw mode); the CLI
// checks `stdout.isTTY` before calling them.

/**
 * Run a slow command behind its spinner: draw the pending line, run the
 * operation, settle into the command's success or failure line. Resolves to
 * whether it succeeded, so the caller can set the exit code.
 */
export async function renderCommandSpinner(
  copy: CommandCopy,
  operation: () => Promise<unknown>,
  config: Config,
): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    let app: ReturnType<typeof render>;
    app = render(
      <CommandSpinner
        copy={copy}
        operation={operation}
        nerdfont={config.nerdfont}
        theme={createTheme(config.theme)}
        onDone={(ok) => {
          app.unmount();
          resolve(ok);
        }}
      />,
    );
  });
}

/**
 * Like {@link renderCommandSpinner}, but the async work itself resolves to the
 * finished success line — used where the success copy is dynamic, such as
 * update-check's latest-vs-pending outcomes.
 */
export async function renderSpinner(
  pending: string,
  run: () => Promise<string>,
  config: Config,
): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    let app: ReturnType<typeof render>;
    app = render(
      <Spinner
        pending={pending}
        run={run}
        nerdfont={config.nerdfont}
        theme={createTheme(config.theme)}
        onDone={(ok) => {
          app.unmount();
          resolve(ok);
        }}
      />,
    );
  });
}

/**
 * Like {@link renderSpinner}, but the work reports download progress the bar
 * draws live — used by update apply's binary download.
 */
export async function renderDownloadProgress(
  pending: string,
  run: (report: (phase: DownloadPhase) => void) => Promise<string>,
  config: Config,
): Promise<boolean> {
  return await new Promise<boolean>((resolve) => {
    let app: ReturnType<typeof render>;
    app = render(
      <DownloadProgress
        pending={pending}
        run={run}
        nerdfont={config.nerdfont}
        theme={createTheme(config.theme)}
        onDone={(ok) => {
          app.unmount();
          resolve(ok);
        }}
      />,
    );
  });
}

/** Paint a static screen once and leave it on the terminal. */
async function renderStatic(element: React.ReactElement): Promise<void> {
  const app = render(element);
  // Let ink paint the first frame, then finalize it in place.
  await new Promise((r) => setTimeout(r, 30));
  app.unmount();
}

/** Paint a mutation command's success line once and leave it on the terminal. */
export async function renderOutcomeLine(segments: MessageSegment[], config: Config): Promise<void> {
  await renderStatic(
    <OutcomeLine segments={segments} nerdfont={config.nerdfont} theme={createTheme(config.theme)} />,
  );
}

export async function renderTaskList(
  groups: TaskGroup[],
  config: Config,
  now: Date,
): Promise<void> {
  await renderStatic(
    <TaskList
      groups={groups}
      now={now}
      dateFormat={config.dateFormat}
      nerdfont={config.nerdfont}
      theme={createTheme(config.theme)}
    />,
  );
}

export async function renderBoardList(rows: BoardRow[], config: Config): Promise<void> {
  await renderStatic(
    <BoardList rows={rows} nerdfont={config.nerdfont} theme={createTheme(config.theme)} />,
  );
}

export async function renderMigrationList(rows: MigrationRow[], config: Config): Promise<void> {
  await renderStatic(
    <MigrationList rows={rows} nerdfont={config.nerdfont} theme={createTheme(config.theme)} />,
  );
}

export async function renderWorkspaceList(
  workspaces: string[],
  current: string,
  config: Config,
): Promise<void> {
  await renderStatic(
    <WorkspaceList
      workspaces={workspaces}
      current={current}
      nerdfont={config.nerdfont}
      theme={createTheme(config.theme)}
    />,
  );
}

/** Drive an interactive screen to the value it collects, then unmount. */
async function runInteractive<T>(build: (resolve: (value: T) => void) => React.ReactElement): Promise<T> {
  return await new Promise<T>((resolve) => {
    let app: ReturnType<typeof render>;
    const done = (value: T) => {
      app.unmount();
      resolve(value);
    };
    app = render(build(done));
  });
}

export async function runInstallForm(config: Config): Promise<InstallResult> {
  return await runInteractive((done) => (
    <InstallForm theme={createTheme(config.theme)} onComplete={done} />
  ));
}

export async function runConfigScreen(config: Config): Promise<ConfigUpdate> {
  return await runInteractive((done) => (
    <ConfigScreen theme={createTheme(config.theme)} onSubmit={done} />
  ));
}

export async function runThemeScreen(config: Config): Promise<string> {
  return await runInteractive((done) => (
    <ThemeScreen theme={createTheme(config.theme)} onSubmit={done} />
  ));
}
