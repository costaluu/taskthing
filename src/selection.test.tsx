import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { Selection } from "./selection";
import { createTheme } from "./theme";

// The selection UI picks from finite choices (story 36-39): date format, theme
// mode, board colour. It renders each item with a selected/unselected dot and a
// current-item selector, and returns the chosen values — always an array, so a
// mono-select is a one-element array. Tests drive it through ink's renderer.

const theme = createTheme();

const dateFormats = [
  { label: "american (yyyy/mm/dd)", value: "america" },
  { label: "europe (dd/mm/yyyy)", value: "europe" },
];

// ink parses raw stdin: the down arrow is the ESC [ B sequence.
const DOWN = "[B";
const tick = () => new Promise((r) => setTimeout(r, 10));

test("it draws the title, every item, and the selector on the current item", () => {
  const { lastFrame } = render(
    <Selection
      title="Date format"
      items={dateFormats}
      nerdfont={false}
      theme={theme}
      onSubmit={() => {}}
    />,
  );

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Date format");
  expect(frame).toContain("american (yyyy/mm/dd)");
  expect(frame).toContain("europe (dd/mm/yyyy)");

  // Without nerdfont the current-item selector is the plain arrow, and the dots
  // are the plain unicode circles (story 9).
  expect(frame).toContain("→");
  expect(frame).toContain("○");
  // Mono-select always has exactly one chosen item — the current one shows the
  // filled dot.
  expect(frame).toContain("●");
});

test("mono-select returns the current item as a one-element array", async () => {
  // Enter on the first item submits it — one choice, still an array (story 37).
  let first: string[] | undefined;
  const a = render(
    <Selection
      title="Date format"
      items={dateFormats}
      nerdfont={false}
      theme={theme}
      onSubmit={(s) => (first = s)}
    />,
  );
  a.stdin.write("\r");
  await tick();
  expect(first).toEqual(["america"]);

  // Arrow-down moves the cursor, so Enter now submits the second item.
  let second: string[] | undefined;
  const b = render(
    <Selection
      title="Date format"
      items={dateFormats}
      nerdfont={false}
      theme={theme}
      onSubmit={(s) => (second = s)}
    />,
  );
  b.stdin.write(DOWN);
  await tick();
  b.stdin.write("\r");
  await tick();
  expect(second).toEqual(["europe"]);
});

test("multi-select toggles with space and returns the chosen subset", async () => {
  const colors = [
    { label: "red", value: "red" },
    { label: "green", value: "green" },
    { label: "blue", value: "blue" },
  ];

  let selected: string[] | undefined;
  const { stdin } = render(
    <Selection
      title="Colours"
      items={colors}
      multi
      nerdfont={false}
      theme={theme}
      onSubmit={(s) => (selected = s)}
    />,
  );

  // Toggle the first item, move down twice, toggle the third, then submit.
  stdin.write(" ");
  await tick();
  stdin.write(DOWN);
  await tick();
  stdin.write(DOWN);
  await tick();
  stdin.write(" ");
  await tick();
  stdin.write("\r");
  await tick();

  // Returned in item order, only the toggled ones (story 37).
  expect(selected).toEqual(["red", "blue"]);
});
