import { summarize } from "./spinner-messages";
import type { MessageSegment } from "./task-message";

// ── board feedback copy ───────────────────────────────────────────────────────
//
// Every board-mutation command's outcome line (mehorias item 4): `add --board`,
// `set --board name/icon/color`, `delete --board`, `recover --board`. They read
// `board <name> <predicate>`, optionally trailed by a highlighted new value
// ("renamed to <new>", "icon updated: <icon>") or a plain note ("deleted. its
// tasks were moved to inbox"). The variable spans — the board name and any new
// value — are flagged so the renderer paints them on a secondary background; the
// glyph is chosen at render. `MessageSegment` is the shared line contract, and
// it lives with the task copy that first defined it.

export interface BoardOutcome {
  /** The primary highlighted name — the *old* name when a board is renamed. */
  name: string;
  /** The past-tense predicate: "created", "renamed", "icon updated", "deleted"… */
  predicate: string;
  /** A highlighted new value with its connector, e.g. {" to ", newName} or {": ", icon}. */
  value?: { connector: string; text: string } | null;
  /** A plain trailing note, e.g. ". its tasks were moved to inbox". */
  suffix?: string | null;
}

export function boardMessage({
  name,
  predicate,
  value = null,
  suffix = null,
}: BoardOutcome): MessageSegment[] {
  const segments: MessageSegment[] = [
    { text: "board " },
    { text: name, variable: true },
    { text: ` ${predicate}` },
  ];

  if (value !== null) {
    segments.push({ text: value.connector }, { text: value.text, variable: true });
  }
  if (suffix !== null) {
    segments.push({ text: suffix });
  }

  return segments;
}

/**
 * The failure line: `taskthing couldn't <action>`, with a legible first-line
 * summary of the error. `action` is the whole phrase after "couldn't" — "create
 * this board", "update this board's icon" — since a board's failures don't all
 * share one verb+noun shape the way a task's do.
 */
export function boardFailure(action: string, error: unknown): string {
  return `taskthing couldn't ${action}. error: ${summarize(error)}`;
}
