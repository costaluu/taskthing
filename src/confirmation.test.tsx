import { test, expect } from "bun:test";
import { render } from "ink-testing-library";

import { Confirmation } from "./confirmation";
import { createTheme } from "./theme";

// The confirmation UI guards destructive/important actions (story 32-35). It is
// an ink component: it draws a titled y/n prompt and reports the answer as a
// boolean. Tests drive it through ink's test renderer — asserting on the frame
// text and the value it returns — never on private component state.

const theme = createTheme();

test("it draws the title, the question verbatim, and a (y/n) prompt", () => {
  // The question is passed in already phrased and ending in `?` — for a
  // recurring `set date-time` it is the parsed result, so the user catches a
  // parsing mistake before saving (story 35).
  const question = "every monday, starting tomorrow (23 jul) — confirm?";

  const { lastFrame } = render(
    <Confirmation
      title="Confirm"
      question={question}
      nerdfont={false}
      theme={theme}
      onSubmit={() => {}}
    />,
  );

  const frame = lastFrame() ?? "";
  expect(frame).toContain("Confirm");
  expect(frame).toContain(question);
  expect(frame).toContain("(y/n)");
  // Without nerdfont the selector is the plain arrow fallback.
  expect(frame).toContain("→");
});

const tick = () => new Promise((r) => setTimeout(r, 10));

test("pressing y answers true and n answers false", async () => {
  let yesAnswer: boolean | undefined;
  const yes = render(
    <Confirmation
      title="Confirm"
      question="delete workspace?"
      nerdfont={false}
      theme={theme}
      onSubmit={(a) => (yesAnswer = a)}
    />,
  );
  yes.stdin.write("y");
  await tick();
  expect(yesAnswer).toBe(true);

  let noAnswer: boolean | undefined;
  const no = render(
    <Confirmation
      title="Confirm"
      question="delete workspace?"
      nerdfont={false}
      theme={theme}
      onSubmit={(a) => (noAnswer = a)}
    />,
  );
  no.stdin.write("n");
  await tick();
  expect(noAnswer).toBe(false);
});

test("an unrelated key does not submit — approval must be conscious", () => {
  let answered = false;
  const { stdin } = render(
    <Confirmation
      title="Confirm"
      question="delete workspace?"
      nerdfont={false}
      theme={theme}
      onSubmit={() => (answered = true)}
    />,
  );
  // Enter and a stray letter must not stand in for a y/n — there is no bypass
  // path (story 34), the user has to answer.
  stdin.write("\r");
  stdin.write("x");
  expect(answered).toBe(false);
});
