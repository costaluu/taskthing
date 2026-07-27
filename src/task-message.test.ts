import { test, expect } from "bun:test";

import { taskMessage, taskFailure } from "./task-message";

// The task-command feedback copy (mehorias items 2-3). taskMessage builds the
// success line as ordered segments — the title, next-occurrence date, and
// recurrence are flagged `variable` so the renderer paints them on a secondary
// background, while the past-tense predicate and connectives stay plain. The
// glyph itself is the renderer's job. Expected copy is the spec's wording (an
// independent source of truth), not recomputed.

test("a plain outcome names the title and its predicate", () => {
  expect(taskMessage({ title: "walk the dog", predicate: "starred" })).toEqual([
    { text: "task " },
    { text: "walk the dog", variable: true },
    { text: " starred" },
  ]);
});

test("a predicate can be multiple words", () => {
  expect(taskMessage({ title: "walk the dog", predicate: "permanently deleted" })).toEqual([
    { text: "task " },
    { text: "walk the dog", variable: true },
    { text: " permanently deleted" },
  ]);
});

test("a trailing value is shown as a chip after its connector", () => {
  expect(
    taskMessage({ title: "sweep", predicate: "moved", value: { connector: " to ", text: "chores" } }),
  ).toEqual([
    { text: "task " },
    { text: "sweep", variable: true },
    { text: " moved" },
    { text: " to " },
    { text: "chores", variable: true },
  ]);
});

test("a dated outcome appends its next occurrence", () => {
  expect(
    taskMessage({ title: "walk the dog", predicate: "created", nextOccurrence: "2026/07/25 09:00" }),
  ).toEqual([
    { text: "task " },
    { text: "walk the dog", variable: true },
    { text: " created" },
    { text: ". next occurrence: " },
    { text: "2026/07/25 09:00", variable: true },
  ]);
});

test("a recurring dated outcome appends its recurrence after the date", () => {
  expect(
    taskMessage({
      title: "walk the dog",
      predicate: "completed",
      nextOccurrence: "2026/07/25 09:00",
      recurrence: "every week on Monday",
    }),
  ).toEqual([
    { text: "task " },
    { text: "walk the dog", variable: true },
    { text: " completed" },
    { text: ". next occurrence: " },
    { text: "2026/07/25 09:00", variable: true },
    { text: " " },
    { text: "every week on Monday", variable: true },
  ]);
});

test("a recurrence without a date is not shown (a recurring task is always dated)", () => {
  expect(
    taskMessage({ title: "walk the dog", predicate: "created", recurrence: "every week on Monday" }),
  ).toEqual([
    { text: "task " },
    { text: "walk the dog", variable: true },
    { text: " created" },
  ]);
});

test("a failure names the command's verb and summarizes the error to its first line", () => {
  expect(taskFailure("create", new Error("disk full\n    at write (fs.ts:9)"))).toBe(
    "taskthing couldn't create this task. error: disk full",
  );
  expect(taskFailure("permanently delete", new Error("boom"))).toBe(
    "taskthing couldn't permanently delete this task. error: boom",
  );
});
