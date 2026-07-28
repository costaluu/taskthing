import { test, expect } from "bun:test";

import { parseAddInput } from "./add-input";

// The add-input parser pulls the bracketed tags (`d:`/`r:`/`b:`) out of one line
// and leaves the rest as the title. These tests pin the board tag (mehorias item
// 3) and that it composes with the others without disturbing the title.

const NOW = new Date("2026-07-24T12:00:00Z");

test("a b:[…] tag names the board and is stripped from the title", () => {
  const parsed = parseAddInput("sweep the floor b:[chores]", NOW);
  expect(parsed.title).toBe("sweep the floor");
  expect(parsed.board).toBe("chores");
});

test("the board tag also spells out as board:[…] and keeps multi-word names", () => {
  expect(parseAddInput("sweep board:[deep clean]", NOW).board).toBe("deep clean");
});

test("no board tag leaves board null (the task falls to the inbox)", () => {
  expect(parseAddInput("sweep the floor", NOW).board).toBeNull();
});

test("an empty board tag names no board", () => {
  const parsed = parseAddInput("sweep b:[]", NOW);
  expect(parsed.board).toBeNull();
  expect(parsed.title).toBe("sweep");
});

test("the board tag composes with date and recurrence tags", () => {
  const parsed = parseAddInput("standup d:[tomorrow] r:[every weekday] b:[work]", NOW);
  expect(parsed.title).toBe("standup");
  expect(parsed.board).toBe("work");
  expect(parsed.rrule).not.toBeNull();
});
