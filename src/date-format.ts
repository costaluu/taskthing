import type { Role } from "./theme";

// ── date formatter ───────────────────────────────────────────────────────────
//
// Turns a task's date into the phrase and semantic role a listing draws
// (story 19/20/21). Pure and terminal-independent: it buckets by *calendar day*
// (not a 24h window) relative to an injected "now", and returns text + a theme
// role — never a colour. The asymmetry is deliberate: the past counts relative
// days to convey lateness; the future shows absolute dates to aid planning.
//
// Day boundaries are computed in UTC so the buckets are deterministic; task
// DTSTARTs are stored with a `Z` offset.

export interface FormattedDate {
  text: string;
  role: Role;
}

export interface FormatInput {
  /**
   * The already-resolved date: a task's DTSTART, or its completion instant.
   * Null for an undated open task, which has no date segment to draw.
   */
  date: Date | null;
  /** The reference instant, injected so buckets are deterministic. */
  now: Date;
  /** When true, `date` is the completion timestamp — shown absolute, not bucketed. */
  completed?: boolean;
  /** Layout for the completed absolute date; the user's configured order. */
  dateFormat?: "america" | "europe";
}

function utcMidnight(d: Date): number {
  return Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate());
}

const MS_PER_DAY = 86_400_000;

// prettier-ignore
const MONTHS = ["jan", "feb", "mar", "apr", "may", "jun",
                "jul", "aug", "sep", "oct", "nov", "dec"];

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** A full UTC date + time laid out in the user's configured order, e.g. `2026/07/25 09:00`. */
function absoluteDateTime(date: Date, dateFormat: "america" | "europe"): string {
  const y = date.getUTCFullYear();
  const m = pad(date.getUTCMonth() + 1);
  const d = pad(date.getUTCDate());
  const time = `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`;
  const ymd = dateFormat === "europe" ? `${d}/${m}/${y}` : `${y}/${m}/${d}`;
  return `${ymd} ${time}`;
}

/**
 * The next occurrence of a freshly-added task, shown as an absolute date + time
 * in the user's format (mehorias item 2). Unlike {@link formatTaskDate}, it never
 * buckets into "today"/"tomorrow": the `add` feedback states exactly when the
 * task next comes due, for planning.
 */
export function formatNextOccurrence(date: Date, dateFormat: "america" | "europe"): string {
  return absoluteDateTime(date, dateFormat);
}

export function formatTaskDate({
  date,
  now,
  completed,
  dateFormat = "america",
}: FormatInput): FormattedDate | null {
  // An undated open task has nothing to draw.
  if (date === null) return null;

  // A completed task shows when it was finished, absolute in the configured
  // format, never bucketed (story 21).
  if (completed) {
    const y = date.getUTCFullYear();
    const m = pad(date.getUTCMonth() + 1);
    const d = pad(date.getUTCDate());
    const time = `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}`;
    const ymd = dateFormat === "europe" ? `${d}/${m}/${y}` : `${y}/${m}/${d}`;
    return { text: `${ymd} ${time}`, role: "text-secondary" };
  }

  const days = Math.round((utcMidnight(date) - utcMidnight(now)) / MS_PER_DAY);

  if (days === 0) return { text: "today", role: "date-today" };
  if (days === 1) return { text: "tomorrow", role: "date-tomorrow" };
  if (days === -1) return { text: "yesterday", role: "date-yesterday" };
  if (days <= -2) return { text: `${-days} days ago`, role: "date-overdue" };

  // Future (days >= 2): absolute, for planning, always low-importance secondary.
  const month = MONTHS[date.getUTCMonth()];
  if (date.getUTCFullYear() === now.getUTCFullYear()) {
    // Later this year: day + month.
    return { text: `${date.getUTCDate()} ${month}`, role: "text-secondary" };
  }
  if (days <= 365) {
    // Into next year but within a year out: month + year.
    return { text: `${month} ${date.getUTCFullYear()}`, role: "text-secondary" };
  }
  // More than a year out: year only.
  return { text: `${date.getUTCFullYear()}`, role: "text-secondary" };
}
