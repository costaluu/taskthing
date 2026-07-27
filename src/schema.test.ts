import { test, expect } from "bun:test";

import { boardSchema, migrationSchema, rruleSchema, taskSchema } from "./schema";

// The schema module is the single source of truth for field types: the YAML
// frontmatter carries bare values and no type information, so what a field
// accepts is decided here and nowhere else. Tests drive each schema's public
// parse and assert on what it accepts, rejects and fills in.

test("a task needs only a title, and lands in the inbox by default", () => {
  const task = taskSchema.parse({ id: "t1", title: "walk the dog" });

  // Every task belongs to a board without a real "inbox" board existing: the
  // sentinel is a schema default, so a task can never be boardless (ADR-0004).
  expect(task.board).toBe("inbox");

  expect(task.description).toBeNull();

  // Flags start off, so a new task is open, unstarred and not deleted.
  expect(task.completed).toBe(false);
  expect(task.star).toBe(false);
  expect(task.deleted).toBe(false);
});

test("a task's when lives entirely in its rrule", () => {
  // An undated task is `rrule: null` — there is no parallel date field to
  // disagree with it.
  expect(taskSchema.parse({ id: "t1", title: "x" }).rrule).toBeNull();

  // A task stores its rrule as the RFC 5545 string, so it round-trips through
  // the frontmatter unchanged...
  const dated = taskSchema.parse({
    id: "t1",
    title: "x",
    rrule: "DTSTART:20260722T090000Z",
  });
  expect(dated.rrule).toBe("DTSTART:20260722T090000Z");

  // ...and a consumer that needs the rule builds it from that string. A dated,
  // non-recurring task is a DTSTART with no rule — "non-recurring" being the
  // absence of an RRULE line. (Careful: the library still answers `.all()` with
  // yearly dates for such a rule, so occurrence logic must consult the rule
  // itself, never `options.freq`.)
  const rule = rruleSchema.parse(dated.rrule!);
  expect(rule.options.dtstart.toISOString()).toBe("2026-07-22T09:00:00.000Z");
  expect(rule.toString()).not.toContain("RRULE:");

  // A stored value that isn't a recurrence rule is rejected on the way in.
  expect(() => taskSchema.parse({ id: "t1", title: "x", rrule: "every monday" })).toThrow();

  // ...and a recurring one carries the rule alongside its start.
  const weekly = rruleSchema.parse("DTSTART:20260722T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO");
  expect(weekly.all((_, i) => i < 2).map((d) => d.toISOString().slice(0, 10))).toEqual([
    "2026-07-27",
    "2026-08-03",
  ]);

  // A string that isn't a recurrence rule is rejected, rather than silently
  // becoming a task with no date.
  expect(() => rruleSchema.parse("every monday")).toThrow();
});

test("a board carries its name and its display", () => {
  const board = boardSchema.parse({ id: "b1", name: "work" });

  // Icon and colour are how a board shows up in a list; a board with neither is
  // still a board, so they start empty rather than being demanded up front.
  expect(board.icon).toBe("");
  expect(board.color).toBe("");
  expect(board.deleted).toBe(false);

  expect(() => boardSchema.parse({ id: "b1" })).toThrow();
  expect(() => boardSchema.parse({ id: "", name: "work" })).toThrow();
});

test("a migration record pairs the version it shipped at with its log", () => {
  const migration = migrationSchema.parse({
    version: "0.2.0",
    content: "0::CREATE_FOLDER::board::::{}",
  });
  expect(migration.version).toBe("0.2.0");

  // An empty log is not a migration — there would be nothing to apply.
  expect(() => migrationSchema.parse({ version: "0.2.0", content: "" })).toThrow();
});

test("a task is rejected without an identity or a title", () => {
  expect(() => taskSchema.parse({ id: "", title: "x" })).toThrow();
  expect(() => taskSchema.parse({ id: "t1" })).toThrow();

  // The board is a real board's nanoid or the sentinel — never empty.
  expect(() => taskSchema.parse({ id: "t1", title: "x", board: "" })).toThrow();
});
