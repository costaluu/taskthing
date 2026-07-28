import { createGlyphs } from "./glyph";
import type { Presenter } from "./presenter";
import {
  renderBoardList,
  renderCommandSpinner,
  renderMigrationList,
  renderOutcomeLine,
  renderSpinner,
  renderTaskList,
  renderWorkspaceList,
} from "./tui";

// ── ink adapter ──────────────────────────────────────────────────────────────
//
// What a terminal sees. Each method is the tui bridge call the command used to
// make for itself; the bridge still owns mounting and unmounting ink, so this
// module is only the mapping from a presenter call to the screen that draws it.
//
// Failure keeps the plain shape even here: a spinner has already settled into
// its own failure line by the time it resolves false, and a mutation's failure
// is a single line that ink would style no differently.

export function inkPresenter(): Presenter {
  return {
    async outcome(segments, config) {
      await renderOutcomeLine(segments, config);
    },
    async taskList(groups, now, config) {
      await renderTaskList(groups, config, now);
    },
    async boardList(rows, config) {
      await renderBoardList(rows, config);
    },
    async workspaceList(workspaces, current, config) {
      await renderWorkspaceList(workspaces, current, config);
    },
    async migrationList(rows, config) {
      await renderMigrationList(rows, config);
    },
    async progress(copy, work, config) {
      // The spinner draws its own failure line, so all that is left to carry is
      // the exit code.
      if (!(await renderCommandSpinner(copy, work, config))) process.exit(1);
    },
    async progressDynamic(pending, _failure, run, config) {
      if (!(await renderSpinner(pending, run, config))) process.exit(1);
    },
    fail(message, config) {
      console.error(`${createGlyphs(config.nerdfont).failure} ${message}`);
      process.exit(1);
    },
  };
}
