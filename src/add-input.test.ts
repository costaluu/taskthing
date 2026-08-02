import { test, expect } from "bun:test";

import { parseAddInput } from "./add-input";

// The add-input parser pulls the bracketed tags (`d:`/`r:`/`b:`) out of one line
// and leaves the rest as the title. These tests pin the board tag (mehorias item
// 3) and that it composes with the others without disturbing the title.

const NOW = new Date("2026-07-24T12:00:00Z");

test("a b:[…] tag names the board and is stripped from the title", () => {
  const parsed = parseAddInput("sweep the floor b:[chores]", NOW, "america");
  expect(parsed.title).toBe("sweep the floor");
  expect(parsed.board).toBe("chores");
});

test("the board tag also spells out as board:[…] and keeps multi-word names", () => {
  expect(parseAddInput("sweep board:[deep clean]", NOW, "america").board).toBe("deep clean");
});

test("no board tag leaves board null (the task falls to the inbox)", () => {
  expect(parseAddInput("sweep the floor", NOW, "america").board).toBeNull();
});

test("an empty board tag names no board", () => {
  const parsed = parseAddInput("sweep b:[]", NOW, "america");
  expect(parsed.board).toBeNull();
  expect(parsed.title).toBe("sweep");
});

test("the board tag composes with date and recurrence tags", () => {
  const parsed = parseAddInput("standup d:[tomorrow] r:[every weekday] b:[work]", NOW, "america");
  expect(parsed.title).toBe("standup");
  expect(parsed.board).toBe("work");
  expect(parsed.rrule).not.toBeNull();
});

// A slash date is ambiguous without a configured reading order: "10/08/2026" is
// October 8th read month-first (america) but August 10th read day-first
// (europe) — the same string must resolve differently per config.md's setting.
test("a d:[…] slash date reads month-first under the america format", () => {
  const parsed = parseAddInput("walk the dog d:[10/08/2026]", NOW, "america");
  expect(parsed.rrule).toBe("DTSTART:20261008T000000Z");
});

test("a d:[…] slash date reads day-first under the europe format", () => {
  const parsed = parseAddInput("walk the dog d:[10/08/2026]", NOW, "europe");
  expect(parsed.rrule).toBe("DTSTART:20260810T000000Z");
});

// A date with no time of its own carries no hour: chrono would otherwise imply
// whatever moment `add` ran at, which is not a meaningful time to store.
test("a relative d:[…] date with no time of day resolves to UTC midnight", () => {
  const parsed = parseAddInput("walk the dog d:[tomorrow]", NOW, "america");
  expect(parsed.rrule).toBe("DTSTART:20260725T000000Z");
});
