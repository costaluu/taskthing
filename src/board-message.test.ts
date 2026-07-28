import { test, expect } from "bun:test";

import { boardMessage, boardFailure } from "./board-message";

// The board-command feedback copy (mehorias item 4). boardMessage builds the
// success line as ordered segments — the board name and any new value are
// flagged `variable` so the renderer paints them on a secondary background,
// while the predicate and connectives stay plain. Expected copy is the spec's
// wording (an independent source of truth), not recomputed.

test("a plain outcome names the board and its predicate", () => {
  expect(boardMessage({ name: "work", predicate: "created" })).toEqual([
    { text: "board " },
    { text: "work", variable: true },
    { text: " created" },
  ]);
});

test("a rename shows the old name and the new one", () => {
  expect(
    boardMessage({ name: "work", predicate: "renamed", value: { connector: " to ", text: "office" } }),
  ).toEqual([
    { text: "board " },
    { text: "work", variable: true },
    { text: " renamed" },
    { text: " to " },
    { text: "office", variable: true },
  ]);
});

test("an icon/color update shows the new value after a colon", () => {
  expect(
    boardMessage({ name: "work", predicate: "icon updated", value: { connector: ": ", text: "" } }),
  ).toEqual([
    { text: "board " },
    { text: "work", variable: true },
    { text: " icon updated" },
    { text: ": " },
    { text: "", variable: true },
  ]);
});

test("a delete that moved tasks appends the plain inbox note", () => {
  expect(
    boardMessage({ name: "work", predicate: "deleted", suffix: ". its tasks were moved to inbox" }),
  ).toEqual([
    { text: "board " },
    { text: "work", variable: true },
    { text: " deleted" },
    { text: ". its tasks were moved to inbox" },
  ]);
});

test("a failure names the whole action phrase and summarizes the error", () => {
  expect(boardFailure("create this board", new Error("disk full\n    at x"))).toBe(
    "taskthing couldn't create this board. error: disk full",
  );
  expect(boardFailure("update this board's icon", new Error("boom"))).toBe(
    "taskthing couldn't update this board's icon. error: boom",
  );
});
