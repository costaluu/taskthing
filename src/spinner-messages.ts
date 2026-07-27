// ── spinner copy ─────────────────────────────────────────────────────────────
//
// The single source of the spinner wording, fixed verbatim by CONTEXT
// (interfaces (TUI) §1) and parameterized by workspace/branch/version. The
// Spinner primitive draws whatever strings these return; keeping the copy here
// means the phrasing lives in one place, not scattered through command wiring.

/** Summarize an error to its first line — a legible failure, not a raw stack (story 13). */
export function summarize(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.split("\n")[0]!.trim();
}

interface CommandCopy {
  pending: string;
  success: string;
  failure: (error: unknown) => string;
}

function workspaceCommand(
  pending: string,
  success: string,
  during: string,
): CommandCopy {
  return {
    pending,
    success,
    failure: (error) => `something went wrong during ${during}. error: ${summarize(error)}`,
  };
}

export const spinnerMessages = {
  sync: (workspace: string): CommandCopy =>
    workspaceCommand(`syncing ${workspace}...`, `${workspace} synchronized!`, "syncing"),

  rebuild: (workspace: string): CommandCopy =>
    workspaceCommand(`rebuilding ${workspace}...`, `${workspace} rebuilt!`, "rebuild"),

  pull: (workspace: string): CommandCopy =>
    workspaceCommand(`pulling ${workspace}...`, `${workspace} pulled!`, "pull"),

  truncate: (workspace: string, branch: string): CommandCopy =>
    workspaceCommand(
      `truncating branch ${branch} from ${workspace}...`,
      `${workspace} truncated!`,
      "truncating",
    ),

  install: workspaceCommand("installing taskthing...", "taskthing installed!", "taskthing installation"),

  // Update check has three outcomes: on the latest, a pending update, or an error.
  updateCheck: {
    pending: "looking for updates...",
    latest: "you're on the latest version!",
    update: (current: string, target: string) =>
      `there's a pending update ${current} → ${target}!`,
    failure: (error: unknown) =>
      `something went wrong during update check. error: ${summarize(error)}`,
  },

  // Update apply names the version it reached.
  updateApply: {
    pending: "updating taskthing...",
    success: (target: string) => `taskthing updated to version ${target}`,
    failure: (error: unknown) =>
      `something went wrong during taskthing update. error: ${summarize(error)}`,
  },
};
