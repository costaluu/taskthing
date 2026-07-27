import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { ThemeScreen } from "./theme-screen";
import { createTheme } from "./theme";

// The theme screen (story 45-46, mehorias item 9) is a live per-role editor: one
// row per semantic role with a swatch and its ANSI index, ←/→ recolour the
// focused role, and a dummy task list previews the theme in real time. It returns
// the config `theme` value: "" when the map is at the default, else the role→index
// JSON of the overrides.

const theme = createTheme();
const tick = () => new Promise((r) => setTimeout(r, 10));
const UP = String.fromCharCode(27) + "[A";
const DOWN = String.fromCharCode(27) + "[B";
const RIGHT = String.fromCharCode(27) + "[C";
const LEFT = String.fromCharCode(27) + "[D";

test("it lists every role with its index, a help line, and a live task preview", () => {
  const { lastFrame } = render(<ThemeScreen theme={theme} onSubmit={() => {}} />);

  const frame = lastFrame() ?? "";
  // A guiding help line — every TUI carries one (mehorias item 9).
  expect(frame).toContain("enter save");
  // Rows for the roles, each showing the ANSI index it maps to.
  expect(frame).toContain("highlight");
  expect(frame).toContain("text-secondary");
  expect(frame).toContain("< ansi 4 >"); // highlight's default index
  // The dummy workspace beneath the rows previews the theme.
  expect(frame).toContain("Tasks");
  expect(frame).toContain("team standup");
});

test("submitting an untouched map returns the empty (default) theme value", async () => {
  let value: string | undefined;
  const { stdin } = render(<ThemeScreen theme={theme} onSubmit={(v) => (value = v)} />);

  stdin.write("\r");
  await tick();
  expect(value).toBe("");
});

test("recolouring a role saves only that override as JSON", async () => {
  let value: string | undefined;
  const { lastFrame, stdin } = render(<ThemeScreen theme={theme} onSubmit={(v) => (value = v)} />);

  // The cursor opens on the first role (highlight, default index 4). One step
  // right advances it to 5, which the row reflects live.
  stdin.write(RIGHT);
  await tick();
  expect(lastFrame() ?? "").toContain("< ansi 5 >");

  stdin.write("\r");
  await tick();
  expect(value).toBe('{"highlight":5}');
});

test("←/→ wrap within the 16 ANSI slots", async () => {
  let value: string | undefined;
  const { stdin } = render(<ThemeScreen theme={theme} onSubmit={(v) => (value = v)} />);

  // highlight defaults to 4; four steps left lands on 0, one more wraps to 15.
  for (let i = 0; i < 5; i++) {
    stdin.write(LEFT);
    await tick();
  }
  stdin.write("\r");
  await tick();
  expect(value).toBe('{"highlight":15}');
});

test("↑/↓ move the cursor between roles", async () => {
  let value: string | undefined;
  const { stdin } = render(<ThemeScreen theme={theme} onSubmit={(v) => (value = v)} />);

  // Down to the second role (error, default 1), bump it, then back up and bump
  // the first (highlight, default 4): both overrides are saved.
  stdin.write(DOWN);
  await tick();
  stdin.write(RIGHT);
  await tick();
  stdin.write(UP);
  await tick();
  stdin.write(RIGHT);
  await tick();
  stdin.write("\r");
  await tick();
  expect(value).toBe('{"highlight":5,"error":2}');
});
