import { test, expect } from "bun:test";

import { formatTaskDate, formatNextOccurrence } from "./date-format";

// The date formatter turns a task's date into the phrase and semantic role a
// listing draws (story 19/20/21). It is pure: given a resolved date and an
// injected "now", it buckets by calendar day and hands back text + a theme
// role, never a colour. The renderer resolves DTSTART and completion metadata
// before calling in; this module only decides the words and the role.

const NOW = new Date("2026-07-23T12:00:00Z");

test("a near open date reads as a relative phrase coloured by lateness", () => {
  // Past uses relative counting to convey lateness (story 20); the day boundary
  // is the calendar day, not a 24h window.
  expect(formatTaskDate({ date: new Date("2026-07-20T09:00:00Z"), now: NOW })).toEqual({
    text: "3 days ago",
    role: "date-overdue",
  });

  expect(formatTaskDate({ date: new Date("2026-07-22T09:00:00Z"), now: NOW })).toEqual({
    text: "yesterday",
    role: "date-yesterday",
  });

  // Today and tomorrow are standout highlights, each its own role.
  expect(formatTaskDate({ date: new Date("2026-07-23T09:00:00Z"), now: NOW })).toEqual({
    text: "today",
    role: "date-today",
  });

  expect(formatTaskDate({ date: new Date("2026-07-24T09:00:00Z"), now: NOW })).toEqual({
    text: "tomorrow",
    role: "date-tomorrow",
  });
});

test("a farther open date reads as an absolute, low-importance phrase", () => {
  // Beyond tomorrow the future is absolute, for planning (story 20), all in a
  // low-importance secondary colour (story 19).

  // Later this year: day + month, no year.
  expect(formatTaskDate({ date: new Date("2026-08-21T09:00:00Z"), now: NOW })).toEqual({
    text: "21 aug",
    role: "text-secondary",
  });

  // Into next year but within a year out: month + year.
  expect(formatTaskDate({ date: new Date("2027-03-15T09:00:00Z"), now: NOW })).toEqual({
    text: "mar 2027",
    role: "text-secondary",
  });

  // More than a year out: year only.
  expect(formatTaskDate({ date: new Date("2028-02-01T09:00:00Z"), now: NOW })).toEqual({
    text: "2028",
    role: "text-secondary",
  });
});

test("a completed task shows its completion date/time, never a bucket", () => {
  // A done item shows *when it was finished* (the completed field's
  // lastModified), full date + time in the user's configured format, in a plain
  // secondary colour — never the rrule, never a relative bucket (story 21).
  const completedAt = new Date("2026-07-23T09:30:00Z");

  expect(
    formatTaskDate({ date: completedAt, now: NOW, completed: true, dateFormat: "america" }),
  ).toEqual({ text: "2026/07/23 09:30", role: "text-secondary" });

  expect(
    formatTaskDate({ date: completedAt, now: NOW, completed: true, dateFormat: "europe" }),
  ).toEqual({ text: "23/07/2026 09:30", role: "text-secondary" });
});

test("formatNextOccurrence renders an absolute date+time in the user's format", () => {
  // The `add` feedback shows the next occurrence as a full date + time in the
  // configured order (mehorias item 2, <task-date-with-date-time-user-format>) —
  // never a relative bucket, since a freshly-added task's date is for planning.
  const at = new Date("2026-07-25T09:00:00Z");

  expect(formatNextOccurrence(at, "america")).toBe("2026/07/25 09:00");
  expect(formatNextOccurrence(at, "europe")).toBe("25/07/2026 09:00");
});

test("an undated open task has no date segment", () => {
  // A task with `rrule: null` has no date to draw; the renderer gets null and
  // simply omits the segment, so the call site stays uniform.
  expect(formatTaskDate({ date: null, now: NOW })).toBeNull();
});
