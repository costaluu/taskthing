import { RRule } from "rrule";

// ── recurrence ──────────────────────────────────────────────────────────────
//
// Completing a recurring task does not recycle it: the occurrence just finished
// stays completed with its own history, and the next one is minted as a new
// entity inheriting the rule. Entity ids are never reused, and each occurrence
// keeps its own record.

/**
 * Whether an rrule actually recurs. A `DTSTART` on its own is a dated,
 * *non*-recurring task — but the library answers such a rule with yearly
 * occurrences, so the absence of recurrence can only be read from the
 * serialized form, never from `options.freq`.
 */
export function recurs(rrule: string): boolean {
  return rrule.includes("RRULE:");
}

/**
 * The rrule of the occurrence that follows this one: the same rule, starting at
 * the next occurrence after the current `DTSTART`. It advances exactly one step
 * even when that step is still in the past, so checking an overdue recurring
 * task repeatedly catches it up one occurrence at a time rather than skipping to
 * the future. Null when the rule is exhausted — there is no next occurrence to
 * mint.
 */
/**
 * The same rrule starting on a new date. A task's date is its `DTSTART`, so this
 * is what moving a task in time means — a recurring one keeps its rule and moves
 * where the series starts.
 */
export function withDtstart(rrule: string | null, dtstart: Date): string {
  const options = rrule === null ? {} : RRule.fromString(rrule).origOptions;
  return new RRule({ ...options, dtstart }).toString();
}

export function nextOccurrence(rrule: string): string | null {
  if (!recurs(rrule)) return null;

  const rule = RRule.fromString(rrule);
  const next = rule.after(rule.options.dtstart, false);
  if (next === null) return null;

  return new RRule({ ...rule.origOptions, dtstart: next }).toString();
}

/**
 * The human phrasing of a recurring rule — "every week on Monday" — for the
 * `add` feedback's `<recurrence-to-string>`. Callers pass an actually-recurring
 * rrule (see {@link recurs}); a DTSTART-only rule would mislead the library into
 * a spurious yearly reading, so the recurrence segment is only ever built when
 * the rule truly recurs.
 */
export function recurrenceToText(rrule: string): string {
  return RRule.fromString(rrule).toText();
}
