import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { TaskList } from "./task-list";
import { createTheme } from "./theme";
import { taskSchema } from "./schema";

// The task list draws the plain data Spec 0003's `list` produces (story 14-25):
// a "TASKS" title, tasks grouped under their board header, each line numbered
// from the kv_store and laid out as `<check> <date> <star> <title> <recurring>`.
// It composes theme + glyph + the date formatter. Tests feed it fixtures and
// assert on the rendered frame text/structure.

const theme = createTheme();
const NOW = new Date("2026-07-23T12:00:00Z");

const inbox = { id: "inbox", name: "inbox", icon: "", color: "" };

function renderTasks(groups: Parameters<typeof TaskList>[0]["groups"]) {
  return render(
    <TaskList
      groups={groups}
      now={NOW}
      dateFormat="america"
      nerdfont={false}
      theme={theme}
    />,
  );
}

test("it draws the Tasks title and a numbered task line", () => {
  const task = taskSchema.parse({ id: "t1", title: "walk the dog" });

  const { lastFrame } = renderTasks([
    { board: inbox, rows: [{ number: 1, task, date: null }] },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("TASKS");
  // Numbered from the kv_store, an open task shows the empty checkbox fallback
  // and its plain title.
  expect(frame).toContain("1.");
  expect(frame).toContain("[ ]");
  expect(frame).toContain("walk the dog");
});

test("tasks group under a board header; inbox uses its own glyph", () => {
  const work = { id: "b1", name: "work", icon: "W", color: "blue" };
  const a = taskSchema.parse({ id: "ta", title: "ship it", board: "b1" });
  const b = taskSchema.parse({ id: "tb", title: "buy milk" });

  const { lastFrame } = renderTasks([
    { board: work, rows: [{ number: 1, task: a, date: null }] },
    { board: inbox, rows: [{ number: 2, task: b, date: null }] },
  ]);

  const frame = lastFrame() ?? "";
  // A real board shows its own name and icon.
  expect(frame).toContain("work");
  expect(frame).toContain("W");
  // The virtual inbox shows the inbox glyph (📬 without nerdfont), not a passed
  // icon.
  expect(frame).toContain("inbox");
  expect(frame).toContain("📬");
});

test("each line shows its date via the date formatter", () => {
  const today = taskSchema.parse({ id: "t1", title: "due today" });
  const overdue = taskSchema.parse({ id: "t2", title: "late" });
  const done = taskSchema.parse({
    id: "t3",
    title: "finished",
    completed: true,
  });

  const { lastFrame } = renderTasks([
    {
      board: inbox,
      rows: [
        { number: 1, task: today, date: new Date("2026-07-23T09:00:00Z") },
        { number: 2, task: overdue, date: new Date("2026-07-20T09:00:00Z") },
        // A completed task's date is its completion instant, drawn absolute.
        { number: 3, task: done, date: new Date("2026-07-22T08:15:00Z") },
      ],
    },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("today");
  expect(frame).toContain("3 days ago");
  expect(frame).toContain("2026/07/22 08:15");
});

test("an empty group list shows an empty-state message instead of nothing", () => {
  const { lastFrame } = renderTasks([]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("TASKS");
  expect(frame).toContain("no tasks here");
});

test("star and recurrence markers show only when they apply", () => {
  const plain = taskSchema.parse({ id: "t1", title: "plain" });
  const bare = renderTasks([
    { board: inbox, rows: [{ number: 1, task: plain, date: null }] },
  ]);
  const bareFrame = bare.lastFrame() ?? "";
  // Nothing to mark: no star, no recurrence glyph (story 22/24).
  expect(bareFrame).not.toContain("⭐");
  expect(bareFrame).not.toContain("🔄");

  const starred = taskSchema.parse({
    id: "t2",
    title: "important",
    star: true,
  });
  const recurring = taskSchema.parse({
    id: "t3",
    title: "weekly",
    rrule: "DTSTART:20260723T090000Z\nRRULE:FREQ=WEEKLY;BYDAY=MO",
  });
  const marked = renderTasks([
    {
      board: inbox,
      rows: [
        { number: 1, task: starred, date: null },
        { number: 2, task: recurring, date: new Date("2026-07-23T09:00:00Z") },
      ],
    },
  ]);
  const markedFrame = marked.lastFrame() ?? "";
  expect(markedFrame).toContain("⭐");
  expect(markedFrame).toContain("🔄");
});

test("a described task carries the description glyph, an undescribed one does not", () => {
  // Story: a task with notes gets one extra marker (📄 without nerdfont), so a
  // listing shows at a glance which tasks carry more than their title.
  const described = taskSchema.parse({
    id: "t1",
    title: "has notes",
    description: "the details",
  });
  const plain = taskSchema.parse({ id: "t2", title: "no notes" });

  const { lastFrame } = renderTasks([
    {
      board: inbox,
      rows: [
        { number: 1, task: described, date: null },
        { number: 2, task: plain, date: null },
      ],
    },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("📄");
  // Only the described line carries it: exactly one marker for the two tasks.
  expect((frame.match(/📄/g) ?? []).length).toBe(1);
});
