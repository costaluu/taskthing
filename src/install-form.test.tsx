import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { InstallForm } from "./install-form";
import { createTheme } from "./theme";

// The install form is the first-run screen (story 42-43): a mono-select for the
// date format, then a y/n confirmation for nerdfont support (default no). It
// only draws the form and returns the collected values — persisting them is
// Spec 0003. It composes the Selection and Confirmation primitives.

const theme = createTheme();
const tick = () => new Promise((r) => setTimeout(r, 10));
// ink parses raw stdin: the down arrow is the ESC [ B sequence.
const DOWN = String.fromCharCode(27) + "[B";

test("it opens on the date-format selection", () => {
  const { lastFrame } = render(<InstallForm theme={theme} onComplete={() => {}} />);

  const frame = lastFrame() ?? "";
  expect(frame).toContain("american (yyyy/mm/dd)");
  expect(frame).toContain("europe (dd/mm/yyyy)");
});

test("choosing a date format advances to nerdfont, then returns both values", async () => {
  let result: { dateFormat: string; nerdfont: boolean } | undefined;
  const { lastFrame, stdin } = render(
    <InstallForm theme={theme} onComplete={(r) => (result = r)} />,
  );

  // Pick the first (american) and advance.
  stdin.write("\r");
  await tick();
  // The second step is the nerdfont confirmation.
  expect(lastFrame() ?? "").toContain("do you support nerdfonts?");

  // Answer yes.
  stdin.write("y");
  await tick();
  expect(result).toEqual({ dateFormat: "america", nerdfont: true });
});

test("a different path returns europe with nerdfont off", async () => {
  let result: { dateFormat: string; nerdfont: boolean } | undefined;
  const { stdin } = render(<InstallForm theme={theme} onComplete={(r) => (result = r)} />);

  stdin.write(DOWN); // move to europe
  await tick();
  stdin.write("\r");
  await tick();
  stdin.write("n"); // no nerdfont
  await tick();
  expect(result).toEqual({ dateFormat: "europe", nerdfont: false });
});
