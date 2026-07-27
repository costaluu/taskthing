import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { BoardList } from "./board-list";
import { createTheme } from "./theme";

// The board list draws the plain data Spec 0003's `boards` produces (story
// 26-28): a "Boards" title and each board numbered from the kv_store, drawn in
// its own colour with its icon and name. The virtual inbox uses its accent role.

const theme = createTheme();

function renderBoards(rows: Parameters<typeof BoardList>[0]["rows"]) {
  return render(<BoardList rows={rows} nerdfont={false} theme={theme} />);
}

test("it draws the Boards title and a numbered board with its icon and name", () => {
  const { lastFrame } = renderBoards([
    { number: 1, board: { id: "b1", name: "work", icon: "W", color: "blue" } },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Boards");
  expect(frame).toContain("1.");
  expect(frame).toContain("W");
  expect(frame).toContain("work");
});

test("an empty row list shows an empty-state message instead of nothing", () => {
  const { lastFrame } = renderBoards([]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Boards");
  expect(frame).toContain("no boards here");
});

test("the virtual inbox is shown with its own glyph", () => {
  const { lastFrame } = renderBoards([
    { number: 1, board: { id: "inbox", name: "inbox", icon: "", color: "" } },
  ]);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("inbox");
  // Without nerdfont the inbox glyph is 📬, not the (empty) passed icon.
  expect(frame).toContain("📬");
});
