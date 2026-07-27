import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { ConfigScreen } from "./config-screen";
import { createTheme } from "./theme";

// The config TUI is `taskthing config` with no args (story 44): it navigates
// every configurable key EXCEPT the current workspace — date format, nerdfont,
// theme — and routes each to its editor. The workspace moves only through `use`
// (Spec 0003), so it must not appear here. It returns the partial config update.
// Every level carries a help line and can be left at any time (mehorias items
// 8-9): esc steps back to the menu (or exits), q exits outright.

const theme = createTheme();
const tick = () => new Promise((r) => setTimeout(r, 10));
// A lone ESC is held briefly by ink to tell it apart from an escape sequence
// (arrow keys start with ESC), so it needs a longer settle than a plain key.
const escTick = () => new Promise((r) => setTimeout(r, 60));
const DOWN = String.fromCharCode(27) + "[B";
const ESC = String.fromCharCode(27);

test("the menu lists the configurable keys but never the workspace", () => {
  const { lastFrame } = render(<ConfigScreen theme={theme} onSubmit={() => {}} />);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("date format");
  expect(frame).toContain("nerdfont");
  expect(frame).toContain("theme");
  // The current workspace is changed only via `use`, never here (story 44).
  expect(frame).not.toContain("workspace");
});

test("every level shows a help line (mehorias item 8)", async () => {
  const { lastFrame, stdin } = render(<ConfigScreen theme={theme} onSubmit={() => {}} />);

  // The menu's help mentions moving, selecting, and quitting.
  expect(lastFrame() ?? "").toContain("quit");

  // A sub-screen's help also offers going back.
  stdin.write("\r"); // open "date format"
  await tick();
  const frame = lastFrame() ?? "";
  expect(frame).toContain("back");
  expect(frame).toContain("quit");
});

test("selecting date format routes to its editor and returns the value", async () => {
  let update: unknown;
  const { lastFrame, stdin } = render(
    <ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />,
  );

  stdin.write("\r"); // pick "date format" (first item)
  await tick();
  expect(lastFrame() ?? "").toContain("american (yyyy/mm/dd)");
  stdin.write("\r"); // pick american
  await tick();
  expect(update).toEqual({ dateFormat: "america" });
});

test("selecting nerdfont routes to a y/n and returns the boolean", async () => {
  let update: unknown;
  const { lastFrame, stdin } = render(
    <ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />,
  );

  stdin.write(DOWN); // to "nerdfont"
  await tick();
  stdin.write("\r");
  await tick();
  expect(lastFrame() ?? "").toContain("enable nerdfont support?");
  stdin.write("y");
  await tick();
  expect(update).toEqual({ nerdfont: true });
});

test("selecting theme routes to the theme screen and returns its value", async () => {
  let update: unknown;
  const { lastFrame, stdin } = render(
    <ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />,
  );

  stdin.write(DOWN); // to "nerdfont"
  await tick();
  stdin.write(DOWN); // to "theme"
  await tick();
  stdin.write("\r");
  await tick();
  // The theme entry opens the per-role editor (mehorias item 9), not a chooser.
  expect(lastFrame() ?? "").toContain("highlight");
  stdin.write("\r"); // save the untouched map → the default theme value
  await tick();
  expect(update).toEqual({ theme: "" });
});

test("esc from a sub-screen returns to the menu without submitting (item 9)", async () => {
  let update: unknown;
  const { lastFrame, stdin } = render(
    <ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />,
  );

  stdin.write("\r"); // open "date format"
  await tick();
  expect(lastFrame() ?? "").toContain("american (yyyy/mm/dd)");

  stdin.write(ESC); // back to the menu
  await escTick();
  const frame = lastFrame() ?? "";
  expect(frame).toContain("date format");
  expect(frame).toContain("theme");
  // Backing out is not a choice, so nothing was submitted.
  expect(update).toBeUndefined();
});

test("esc at the menu exits with an empty update (item 9)", async () => {
  let update: unknown;
  const { stdin } = render(<ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />);

  stdin.write(ESC);
  await escTick();
  expect(update).toEqual({});
});

test("q exits from a sub-screen with an empty update (item 9)", async () => {
  let update: unknown;
  const { stdin } = render(<ConfigScreen theme={theme} onSubmit={(u) => (update = u)} />);

  stdin.write("\r"); // open "date format"
  await tick();
  stdin.write("q");
  await tick();
  expect(update).toEqual({});
});
