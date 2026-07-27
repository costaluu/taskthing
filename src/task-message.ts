import { summarize } from "./spinner-messages";

// ── task feedback copy ────────────────────────────────────────────────────────
//
// Every task-mutation command's outcome line (mehorias items 2-3): `add`,
// `set`, `check`/`uncheck`, `star`/`unstar`, `delete`/`recover`, `clear`. They
// all read `task <title> <predicate>`, optionally trailed by the next occurrence
// and recurrence, so the copy is built in one place. The success line is a list
// of ordered segments: the variable spans (title, date, recurrence) are flagged
// so the renderer paints them on a secondary background, while the fixed words —
// the past-tense predicate and connectives — stay plain. The glyph (success ✓ /
// failure 𐄂, nerdfont-aware) is chosen at render, not here.

/** A run of the line; `variable` spans are the values the renderer highlights. */
export interface MessageSegment {
  text: string;
  variable?: boolean;
}

export interface TaskOutcome {
  title: string;
  /** The past-tense predicate: "created", "completed", "title updated", "moved"… */
  predicate: string;
  /** A highlighted value with its connector after the predicate, e.g. {" to ", boardName}. */
  value?: { connector: string; text: string } | null;
  /** The formatted next-occurrence date+time, appended as ". next occurrence: <date>". */
  nextOccurrence?: string | null;
  /** The human recurrence phrasing, appended after the date. Only meaningful with a date. */
  recurrence?: string | null;
}

export function taskMessage({
  title,
  predicate,
  value = null,
  nextOccurrence = null,
  recurrence = null,
}: TaskOutcome): MessageSegment[] {
  const segments: MessageSegment[] = [
    { text: "task " },
    { text: title, variable: true },
    { text: ` ${predicate}` },
  ];

  // A trailing highlighted value (e.g. the board a task moved to); no command
  // pairs it with a next occurrence, so the two never contend for the tail.
  if (value !== null) {
    segments.push({ text: value.connector }, { text: value.text, variable: true });
  }

  if (nextOccurrence !== null) {
    segments.push({ text: ". next occurrence: " }, { text: nextOccurrence, variable: true });
    // A recurrence is only ever shown alongside its date — a recurring task is
    // always dated (its DTSTART is the next occurrence).
    if (recurrence !== null) {
      segments.push({ text: " " }, { text: recurrence, variable: true });
    }
  }

  return segments;
}

/**
 * The failure line: `taskthing couldn't <verb> this task`, with a legible
 * first-line summary of the error. `verb` is the command's own action —
 * "create", "update", "complete", "star", "permanently delete"…
 */
export function taskFailure(verb: string, error: unknown): string {
  return `taskthing couldn't ${verb} this task. error: ${summarize(error)}`;
}
