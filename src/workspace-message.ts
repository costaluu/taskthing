import { summarize } from "./spinner-messages";
import type { MessageSegment } from "./task-message";

// ── workspace feedback copy ───────────────────────────────────────────────────
//
// Every workspace-mutation command's outcome line: `use`, `workspace create`,
// `workspace rename`, `workspace delete`, and `workspace remote add/remove`.
// They read `workspace <name> <predicate>`, optionally trailed by a highlighted
// value ("renamed to <new>", "remote added: <url>"). The variable spans — the
// workspace name and any new value — are flagged so the renderer paints them on
// a secondary background; the glyph is chosen at render. `MessageSegment` is the
// shared line contract, and it lives with the task copy that first defined it.

export interface WorkspaceOutcome {
  /** The primary highlighted name — the *old* name when a workspace is renamed. */
  name: string;
  /** The past-tense predicate: "created", "renamed", "now in use", "deleted"… */
  predicate: string;
  /** A highlighted new value with its connector, e.g. {" to ", newName} or {": ", url}. */
  value?: { connector: string; text: string } | null;
}

export function workspaceMessage({
  name,
  predicate,
  value = null,
}: WorkspaceOutcome): MessageSegment[] {
  const segments: MessageSegment[] = [
    { text: "workspace " },
    { text: name, variable: true },
    { text: ` ${predicate}` },
  ];

  if (value !== null) {
    segments.push({ text: value.connector }, { text: value.text, variable: true });
  }

  return segments;
}

/**
 * The failure line: `taskthing couldn't <action>`, with a legible first-line
 * summary of the error. `action` is the whole phrase after "couldn't" — "switch
 * workspace", "create this workspace", "add this remote" — since a workspace's
 * failures don't all share one verb+noun shape.
 */
export function workspaceFailure(action: string, error: unknown): string {
  return `taskthing couldn't ${action}. error: ${summarize(error)}`;
}
