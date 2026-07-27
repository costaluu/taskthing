import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { TextInput } from "./text-input";
import { createTheme } from "./theme";

// The text input is the surface for free-form edits: `set` values (title,
// description, date-time) single-line, and the custom-theme JSON as a multiline
// textarea (story 40-41). Tests drive typed input through ink's mock stdin and
// assert on the frame and the submitted value.

const theme = createTheme();
const tick = () => new Promise((r) => setTimeout(r, 10));

// Control keys as ink sees them on raw stdin.
const BACKSPACE = String.fromCharCode(127); // DEL
const CTRL_D = String.fromCharCode(4); // Ctrl+D submits a textarea

test("it shows a placeholder, builds the value as you type, and submits on Enter", async () => {
  let submitted: string | undefined;
  const { lastFrame, stdin } = render(
    <TextInput placeholder="title" nerdfont={false} theme={theme} onSubmit={(t) => (submitted = t)} />,
  );

  // Empty to start: the placeholder stands in for the value.
  expect(lastFrame() ?? "").toContain("title");

  stdin.write("walk the dog");
  await tick();
  expect(lastFrame() ?? "").toContain("walk the dog");

  stdin.write("\r");
  await tick();
  expect(submitted).toBe("walk the dog");
});

test("backspace deletes the last character", async () => {
  let submitted: string | undefined;
  const { stdin } = render(
    <TextInput nerdfont={false} theme={theme} onSubmit={(t) => (submitted = t)} />,
  );

  stdin.write("cat");
  await tick();
  stdin.write(BACKSPACE);
  await tick();
  stdin.write("\r");
  await tick();
  expect(submitted).toBe("ca");
});

test("multiline treats Enter as a newline and submits on Escape", async () => {
  let submitted: string | undefined;
  const { stdin } = render(
    <TextInput multiline nerdfont={false} theme={theme} onSubmit={(t) => (submitted = t)} />,
  );

  stdin.write("line one");
  await tick();
  stdin.write("\r"); // newline, not submit
  await tick();
  stdin.write("line two");
  await tick();
  stdin.write(CTRL_D); // submits
  await tick();
  expect(submitted).toBe("line one\nline two");
});

test("a validate hook shows a live error that clears when the input is valid", async () => {
  // The custom-theme textarea wires this to the theme validator, so a bad
  // mapping is flagged immediately (story 41). Here a stand-in validator keeps
  // the test to the primitive's behaviour.
  const validate = (text: string) => (text === "ok" ? null : "invalid");

  const { lastFrame, stdin } = render(
    <TextInput multiline validate={validate} nerdfont={false} theme={theme} onSubmit={() => {}} />,
  );

  // Empty is invalid: the error shows up front.
  expect(lastFrame() ?? "").toContain("invalid");

  stdin.write("ok");
  await tick();
  // Now valid: the error is gone.
  expect(lastFrame() ?? "").not.toContain("invalid");
});
